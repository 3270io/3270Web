package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/authz"
)

// Administration of the published host list — the preset entries the
// session-selection screen and the connect page offer, and who each one is
// offered to.
//
// The connect page can already publish a profile, and that stays; what this
// adds is the management view: the published set on its own, with the
// audience each entry carries, editable without first saving a private copy.
// The store and the visibility rule (visibleTo) are the existing ones, so a
// preset written here and a profile published from the connect page are the
// same thing seen from two rooms.

// AdminSessionScreenPageHandler serves the session-screen management page:
// the branding and the preset host list, which together are that screen.
func (app *App) AdminSessionScreenPageHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "admin-sessionscreen.html", gin.H{
		"AuthEnabled": app.authMode != authz.ModeNone,
	})
}

// AdminListProfilesHandler returns the published set and what the audience
// pickers can offer.
func (app *App) AdminListProfilesHandler(c *gin.Context) {
	store, err := app.profileStoreForWrite(c, true)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	// Before the load, so the bundled sample apps are on the list the first
	// time it is looked at rather than after a refresh. They arrive not
	// offered, so nothing else on the instance changes; see sampleseed.go.
	app.seedSampleAppPresets(c)
	profiles, err := store.load()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read profiles: %v", err)})
		return
	}
	for i := range profiles {
		profiles[i].Shared = true
	}

	// Names for the audience pickers, so an audience is chosen rather than
	// transcribed. Under a single operator there is nobody to pick.
	var usernames []string
	if app.authMode != authz.ModeNone {
		if list, err := app.userStore().List(); err == nil {
			for _, u := range list {
				usernames = append(usernames, u.Username)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"profiles":    profiles,
		"groups":      app.knownGroups(c),
		"usernames":   usernames,
		"roles":       []string{string(authz.RoleUser), string(authz.RoleAdmin)},
		"authEnabled": app.authMode != authz.ModeNone,
		// The bundled sample apps, so a preset can offer one without anybody
		// having to know that they are addressed as "sampleapp:<id>". An
		// instance being evaluated, or one being taught on, has these and no
		// mainframe — and a host list is exactly as useful there.
		"sampleApps": sampleAppPresetOptions(),
	})
}

// AdminSaveProfileHandler adds or replaces one published preset.
func (app *App) AdminSaveProfileHandler(c *gin.Context) {
	var profile ConnectionProfile
	if err := c.ShouldBindJSON(&profile); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	profile.Shared = false
	if err := validateProfile(&profile); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	normaliseAudience(&profile)
	settleOffered(&profile)

	store, err := app.profileStoreForWrite(c, true)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if _, err := store.upsert(profile); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save profile: %v", err)})
		return
	}

	log.Printf("admin: %s published host preset %q (%s)", adminActor(c), profile.Name, profile.displayTarget())
	app.auditRequest(c, audit.EventProfilePublished, audit.Success, profile.Name,
		map[string]string{"host": profile.displayTarget(), "audience": audienceSummary(profile)})
	c.JSON(http.StatusOK, gin.H{"ok": true, "profile": profile})
}

// AdminDeleteProfileHandler removes one published preset. It only ever
// touches the published set — unlike the connect page's delete, which
// prefers the caller's own copy — because this page only shows that set,
// and deleting something the page does not show would be a trap.
func (app *App) AdminDeleteProfileHandler(c *gin.Context) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a profile name is required"})
		return
	}

	store, err := app.profileStoreForWrite(c, true)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if _, found := store.find(name); !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "no published preset with that name"})
		return
	}
	if _, err := store.delete(name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to delete profile: %v", err)})
		return
	}

	log.Printf("admin: %s removed host preset %q", adminActor(c), name)
	app.auditRequest(c, audit.EventProfileRemoved, audit.Success, name, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// sampleAppPresetOption is one bundled sample app as the preset dialog offers
// it: a name to choose, and the host and port choosing it fills in.
type sampleAppPresetOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

func sampleAppPresetOptions() []sampleAppPresetOption {
	out := make([]sampleAppPresetOption, 0, len(sampleAppConfigs))
	// Each gets its own port, so publishing all three does not produce three
	// presets fighting over one listener.
	ports := allowedSampleAppPorts()
	for i, cfg := range sampleAppConfigs {
		port := defaultSampleAppPort
		if i < len(ports) {
			port = ports[i]
		}
		out = append(out, sampleAppPresetOption{
			ID:   cfg.ID,
			Name: cfg.Name,
			Host: sampleAppHostname(cfg.ID),
			Port: port,
		})
	}
	return out
}

// sampleAppPresetLabel names a preset's target the way somebody chose it,
// rather than echoing "sampleapp:app1:3271" back at them.
//
// Returns "" for a preset that names an ordinary host, which is the caller's
// signal to show the address itself.
func sampleAppPresetLabel(p ConnectionProfile) string {
	id, _, ok := parseSampleAppHost(p.Host)
	if !ok {
		return ""
	}
	cfg, known := sampleAppConfig(id)
	if !known {
		return ""
	}
	return cfg.Name
}

// audienceSummary says who a preset reaches, for the audit line.
func audienceSummary(p ConnectionProfile) string {
	if !p.hasAudience() {
		return "everyone"
	}
	var parts []string
	if len(p.Users) > 0 {
		parts = append(parts, fmt.Sprintf("%d user(s)", len(p.Users)))
	}
	if len(p.Groups) > 0 {
		parts = append(parts, "groups "+strings.Join(p.Groups, " "))
	}
	if len(p.Roles) > 0 {
		parts = append(parts, "roles "+strings.Join(p.Roles, " "))
	}
	return strings.Join(parts, ", ")
}
