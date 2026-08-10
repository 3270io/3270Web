package main

import (
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// Administrative view of the terminal sessions this instance is running.
//
// This is a list-and-close power, deliberately nothing more: an administrator
// can see that a session exists, whose it is and where it is connected, and
// can end it — freeing its s3270 subprocess and its slot under the caps. What
// they cannot do is look at its screen or type into it. Session isolation
// (mayUseSession) has no administrator exemption, and this file does not add
// one; administering the instance is not the same power as sitting at
// somebody's authenticated mainframe terminal.

// adminSessionView is the wire shape for one live terminal session.
type adminSessionView struct {
	ID string `json:"id"`
	// Owner is the account name where one can be resolved, so the list reads
	// as people rather than IDs. OwnerID stays alongside it for the rows where
	// it cannot be — a directory account deleted mid-session, say.
	Owner       string `json:"owner"`
	OwnerID     string `json:"ownerId,omitempty"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Connected   bool   `json:"connected"`
	LastAccess  string `json:"lastAccess"`
	IdleSeconds int    `json:"idleSeconds"`
	// Busy marks a session the idle reaper would leave alone — a running
	// chaos exploration or workflow playback — so the page can warn that
	// disconnecting it interrupts unattended work, not just an idle screen.
	Busy bool `json:"busy"`
}

// ownerNames maps account IDs to usernames for the session list. Resolved
// once per request rather than per row, and tolerant of a store that cannot
// be read — a session list with raw IDs still answers "how many and where".
func (app *App) ownerNames() map[string]string {
	names := map[string]string{authz.LocalUserID: "local operator"}
	if app.authMode == authz.ModeNone {
		return names
	}
	list, err := app.userStore().List()
	if err != nil {
		return names
	}
	for _, u := range list {
		names[u.ID] = u.Username
	}
	return names
}

func (app *App) toAdminSessionView(s *session.Session, names map[string]string, now time.Time) adminSessionView {
	var last time.Time
	busy := false
	var h host.Host
	withSessionLock(s, func() {
		last = s.LastAccess
		busy = s.Playback != nil && s.Playback.Active
		// Read inside the lock the rest of this already holds: a reconnect
		// swapping s.Host while an administrator loads this page is a data
		// race, and a session mid-swap could leave the interface nil.
		h = s.Host
	})
	if !busy {
		if eng, ok := app.chaosEngines.get(s.ID); ok && eng != nil && eng.Active() {
			busy = true
		}
	}
	idle := int(now.Sub(last).Seconds())
	if idle < 0 {
		idle = 0
	}
	return adminSessionView{
		ID:          s.ID,
		Owner:       names[s.OwnerID],
		OwnerID:     s.OwnerID,
		Host:        s.TargetHost,
		Port:        s.TargetPort,
		Connected:   h != nil && h.IsConnected(),
		LastAccess:  last.UTC().Format(time.RFC3339),
		IdleSeconds: idle,
		Busy:        busy,
	}
}

// AdminListSessionsHandler returns every live terminal session.
func (app *App) AdminListSessionsHandler(c *gin.Context) {
	now := time.Now()
	names := app.ownerNames()
	list := app.SessionManager.ListSessions()
	out := make([]adminSessionView, 0, len(list))
	for _, s := range list {
		out = append(out, app.toAdminSessionView(s, names, now))
	}
	// Most recently active first, so the top of the list is the session
	// somebody is sitting at and the bottom is the one nobody has missed.
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastAccess != out[j].LastAccess {
			return out[i].LastAccess > out[j].LastAccess
		}
		return out[i].ID < out[j].ID
	})
	c.JSON(http.StatusOK, gin.H{
		"sessions": out,
		"capTotal": app.totalSessionCap(),
	})
}

// AdminCloseSessionHandler force-closes one terminal session: the mainframe
// connection is dropped and the s3270 subprocess exits. The owner's login is
// untouched — their browser learns the terminal has gone the next time it
// asks, exactly as it does when the idle reaper takes one.
func (app *App) AdminCloseSessionHandler(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing session id"})
		return
	}
	// Peek rather than Get: an administrative lookup must not stamp the
	// session as freshly active on its way out.
	s, ok := app.SessionManager.PeekSession(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "no such session"})
		return
	}

	names := app.ownerNames()
	owner := names[s.OwnerID]
	if owner == "" {
		owner = s.OwnerID
	}
	target := s.TargetHost
	if s.TargetPort > 0 {
		target = net.JoinHostPort(s.TargetHost, strconv.Itoa(s.TargetPort))
	}

	app.cleanupSession(s)

	log.Printf("admin: %s disconnected session %s (%s, owner %q)",
		adminActor(c), shortSessionID(id), target, owner)
	app.auditRequest(c, audit.EventSessionClosed, audit.Success, target,
		map[string]string{"owner": owner, "session": shortSessionID(id)})
	c.JSON(http.StatusOK, gin.H{"closed": true})
}

// shortSessionID is enough of a session ID to correlate log and trail lines
// without writing a live capability handle into either.
func shortSessionID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
