package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
	"github.com/jnnngs/3270Web/internal/sessionmenu"
)

// What an operator meets when they arrive.
//
// On an instance where an administrator has assigned host profiles, the
// connect form is the wrong first thing to show: the person signing in does
// not need to be asked for a hostname they were never told, and on a shared
// instance they may not be entitled to type one. So:
//
//	no profiles     the connect form, exactly as before
//	one profile     connected to it, no question asked
//	several         a 3270 session-selection screen
//
// The middle case is the one that matters most. Somebody whose account reaches
// exactly one mainframe should sign in and be on it.

// menuStore holds the selection screen a session is currently looking at.
type menuStore struct {
	mu    sync.Mutex
	menus map[string]*sessionmenu.Menu
}

func newMenuStore() *menuStore {
	return &menuStore{menus: make(map[string]*sessionmenu.Menu)}
}

func (s *menuStore) put(sessionID string, m *sessionmenu.Menu) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.menus[sessionID]; ok && existing != nil {
		existing.Stop()
	}
	s.menus[sessionID] = m
}

func (s *menuStore) get(sessionID string) (*sessionmenu.Menu, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.menus[sessionID]
	return m, ok && m != nil
}

// take removes the menu and returns it, so the caller can stop it exactly
// once even if two requests notice the same selection together.
func (s *menuStore) take(sessionID string) (*sessionmenu.Menu, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.menus[sessionID]
	delete(s.menus, sessionID)
	return m, ok && m != nil
}

// stop closes a session's menu, if it has one. Called from session cleanup:
// the listener belongs to the session and must not outlive it.
func (s *menuStore) stop(sessionID string) {
	if m, ok := s.take(sessionID); ok {
		m.Stop()
	}
}

func (app *App) menus() *menuStore {
	app.menuOnce.Do(func() {
		if app.menuStore == nil {
			app.menuStore = newMenuStore()
		}
	})
	return app.menuStore
}

// firstScreenFor connects the caller to whatever their account should land on,
// and reports whether it did.
//
// False means "nothing was assigned, show the connect form" — not a failure.
func (app *App) firstScreenFor(c *gin.Context) bool {
	profiles, err := app.assignedProfiles(c)
	if err != nil {
		log.Printf("first screen: could not read the host list: %v", err)
		return false
	}

	switch len(profiles) {
	case 0:
		return false

	case 1:
		if err := app.connectWithProfile(c, profiles[0]); err != nil {
			log.Printf("first screen: connecting to the only assigned host %q failed: %v", profiles[0].Name, err)
			// Deliberately falls through to the connect form rather than
			// showing a dead end: the operator may be able to reach something
			// else, and an error page with no way forward is worse than a form.
			return false
		}
		return true

	default:
		return app.startSelectionScreen(c, profiles)
	}
}

// startSelectionScreen puts a menu on a loopback port and points a new session
// at it.
func (app *App) startSelectionScreen(c *gin.Context, profiles []ConnectionProfile) bool {
	entries := make([]sessionmenu.Entry, 0, len(profiles))
	for _, p := range profiles {
		entries = append(entries, sessionmenu.Entry{
			Name:        p.Name,
			Description: p.Description,
			Detail:      p.displayTarget(),
		})
	}

	menu, err := sessionmenu.Start(app.menuBranding(), entries)
	if err != nil {
		log.Printf("first screen: could not start the selection screen: %v", err)
		return false
	}

	// The session limits still apply — the menu costs an s3270 subprocess like
	// any other session — but the host checks deliberately do not, and the
	// terminal is not connected through the ordinary path.
	//
	// That path refuses loopback, which is exactly right: a terminal that
	// could be pointed at 127.0.0.1 is a way to reach whatever else this
	// server can reach on its own machine. The menu is not an exception to
	// that rule so much as outside it — the address was not supplied by
	// anybody, it came from a listener this process opened a moment ago and
	// holds the only reference to. Relaxing the check instead would widen it
	// for every caller, to allow the one address that never needed checking.
	ownerID := principalFrom(c).UserID
	if err := app.checkSessionCaps(ownerID); err != nil {
		menu.Stop()
		log.Printf("first screen: %v", err)
		return false
	}

	args := buildS3270Args(app.Config.S3270Options, "")
	args = append(args, menu.Addr())
	h := host.NewS3270(resolveS3270Path(app.Config.ExecPath), args...)
	if err := h.Start(); err != nil {
		menu.Stop()
		log.Printf("first screen: could not start the terminal for the selection screen: %v", err)
		return false
	}

	sess := app.SessionManager.CreateSessionFor(ownerID, h)
	app.applyDefaultPrefs(sess)
	setSessionCookie(c, sessionCookieName, sess.ID)
	app.rememberSession(c, sess.ID)
	app.menus().put(sess.ID, menu)

	app.auditRequest(c, audit.EventSessionOpened, audit.Success, "selection screen",
		map[string]string{"sessionId": sess.ID, "hosts": strconv.Itoa(len(entries))})
	return true
}

// settleSelection moves a session off the menu once its operator has chosen.
//
// Called from the paths that serve a screen, so the switch happens on the
// request that carried the Enter rather than on a timer. The menu's own
// "connecting" screen is what the operator sees in between, which is a moment
// or two.
func (app *App) settleSelection(c *gin.Context, s *session.Session) {
	if s == nil {
		return
	}
	menu, ok := app.menus().get(s.ID)
	if !ok {
		return
	}

	// PF3 on the menu is "I did not want any of these". The session ends, and
	// the operator is back at the connect form — which is the only other thing
	// there is to offer.
	if menu.SignedOff() {
		app.menus().stop(s.ID)
		app.cleanupSession(s)
		return
	}

	name := menu.Chosen()
	if name == "" {
		return
	}

	profile, found := app.findVisibleProfile(c, name)
	if !found {
		// Assigned when the menu was drawn, gone by the time it was used.
		log.Printf("selection screen: %q is no longer available to this account", name)
		app.menus().stop(s.ID)
		return
	}
	// Taken before the switch, so a second request arriving at the same moment
	// finds no menu and does not start a second connection to the same host.
	app.menus().stop(s.ID)

	if err := app.swapSessionHost(c, s, profile); err != nil {
		log.Printf("selection screen: could not connect %q to %q: %v", s.ID, profile.Name, err)
		return
	}
	log.Printf("selection screen: session %s moved to %s", s.ID, profile.displayTarget())
}

// swapSessionHost re-points a live session at a different mainframe.
//
// The session keeps its identity — same ID, same owner, same cookie in the
// browser — because from the operator's point of view nothing has restarted:
// they picked a system on a menu and the system appeared. Replacing the
// session instead would mean a new cookie mid-flight, and would lose anything
// already attached to it.
func (app *App) swapSessionHost(c *gin.Context, s *session.Session, profile ConnectionProfile) error {
	// The same allowlist every other path goes through. A profile is written
	// by an administrator, but ALLOWED_HOSTS is what an instance is fenced
	// with, and a fence with one gate left open is not one.
	target := profile.displayTarget()
	if !isValidHostname(profile.Host) {
		return fmt.Errorf("profile %q has an unusable host %q", profile.Name, profile.Host)
	}
	if err := checkHostAllowed(profile.Host); err != nil {
		app.auditRequest(c, audit.EventSessionDenied, audit.Denied, target,
			map[string]string{"reason": "host not allowed", "via": "selection screen"})
		return err
	}

	args := buildS3270Args(app.Config.S3270Options, "")
	args = append(args, profile.overrideArgs()...)
	if t := strings.TrimSpace(profile.s3270Target()); t != "" {
		args = append(args, t)
	}
	next := host.NewS3270(resolveS3270Path(app.Config.ExecPath), args...)
	if err := next.Start(); err != nil {
		return fmt.Errorf("connect to %s: %w", target, err)
	}

	var previous interface{ Stop() error }
	withSessionLock(s, func() {
		if s.Host != nil {
			previous = s.Host
		}
		s.Host = next
		s.TargetHost = profile.Host
		s.TargetPort = profile.Port
	})
	if previous != nil {
		// After the swap, not before: a request in flight must never find the
		// session holding a host that has already been stopped.
		if err := previous.Stop(); err != nil {
			log.Printf("selection screen: stopping the menu connection: %v", err)
		}
	}

	setSessionCookie(c, lastTargetCookieName, profile.displayTarget())
	app.auditRequest(c, audit.EventSessionOpened, audit.Success, profile.displayTarget(),
		map[string]string{"via": "selection screen", "profile": profile.Name})
	return nil
}

// SelectionScreenActive reports whether this session is looking at the menu,
// for the browser to know that the screen it is showing is 3270Web's own.
func (app *App) SelectionScreenActive(s *session.Session) bool {
	if s == nil {
		return false
	}
	_, ok := app.menus().get(s.ID)
	return ok
}

// assignedHostSummary is what the connect page says when an account has hosts
// assigned but the operator asked for the form anyway.
func (app *App) assignedHostSummary(c *gin.Context) string {
	profiles, err := app.assignedProfiles(c)
	if err != nil || len(profiles) == 0 {
		return ""
	}
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

// APISelectionScreen reports what the selection screen would offer, so a
// client can show the same list without driving a terminal.
func (app *App) APISelectionScreen(c *gin.Context) {
	profiles, err := app.assignedProfiles(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read the host list"})
		return
	}
	out := make([]gin.H, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, gin.H{
			"name":        p.Name,
			"description": p.Description,
			"target":      p.displayTarget(),
			"shared":      p.Shared,
		})
	}
	c.JSON(http.StatusOK, gin.H{"hosts": out})
}
