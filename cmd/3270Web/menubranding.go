package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/sessionmenu"
)

// What the selection screen says at the top.
//
// It is the first screen an operator meets, before anything of their own
// organisation's, so a deployment that cannot put its own name on it looks
// like somebody else's product.
//
// The default is a wordmark on the title bar and no artwork. A banner is
// available and empty unless a site fills it in — see sessionmenu.Branding for
// why that is the default rather than a logo.
//
// Instance-wide rather than per-user: it is the instance being named.

const menuBrandingFileName = "menu-branding.json"

// brandingStore persists the selection screen's branding.
type brandingStore struct {
	mu   sync.Mutex
	path string
}

func (app *App) branding() *brandingStore {
	app.brandingOnce.Do(func() {
		if app.brandingStore == nil {
			app.brandingStore = &brandingStore{
				path: filepath.Join(app.baseDir, menuBrandingFileName),
			}
		}
	})
	return app.brandingStore
}

// load returns the configured branding, or the default.
//
// A file that cannot be read or parsed falls back to the default rather than
// failing: this decides what a screen looks like, and an unreadable preference
// is not a reason to refuse somebody a terminal.
func (s *brandingStore) load() sessionmenu.Branding {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("menu branding: %v; using the default", err)
		}
		return sessionmenu.DefaultBranding()
	}
	var brand sessionmenu.Branding
	if err := json.Unmarshal(data, &brand); err != nil {
		log.Printf("menu branding: %s is not readable JSON: %v; using the default", s.path, err)
		return sessionmenu.DefaultBranding()
	}
	cleaned, _ := brand.Sanitise()
	return cleaned
}

// save writes branding, atomically.
func (s *brandingStore) save(brand sessionmenu.Branding) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("create the data directory: %w", err)
	}
	data, err := json.MarshalIndent(brand, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".menu-branding-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// reset removes the stored branding, so the default applies again.
func (s *brandingStore) reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// menuBranding is what a new selection screen is drawn with.
func (app *App) menuBranding() sessionmenu.Branding {
	return app.branding().load()
}

// AdminMenuBrandingHandler returns the branding and what may be put in it.
func (app *App) AdminMenuBrandingHandler(c *gin.Context) {
	current := app.menuBranding()
	c.JSON(http.StatusOK, gin.H{
		"branding": current,
		"default":  sessionmenu.DefaultBranding(),
		// Stated rather than left to be discovered by having input silently
		// truncated: an administrator drawing artwork needs the dimensions.
		"limits": gin.H{
			"bannerLines": sessionmenu.MaxBannerLines,
			"lineWidth":   sessionmenu.MaxLineWidth,
			"charset":     "printable ASCII; a 3270 display draws what its code page has",
		},
	})
}

// AdminSaveMenuBrandingHandler replaces the branding.
func (app *App) AdminSaveMenuBrandingHandler(c *gin.Context) {
	var req struct {
		sessionmenu.Branding
		// Reset asks for the default back, which is otherwise impossible to
		// express: an empty banner is a deployment saying "no artwork", not
		// "give me yours".
		Reset bool `json:"reset"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.Reset {
		if err := app.branding().reset(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not clear the branding"})
			return
		}
		app.auditRequest(c, audit.EventSettingsSaved, audit.Success, "menu branding",
			map[string]string{"change": "reset to the default"})
		c.JSON(http.StatusOK, gin.H{"branding": sessionmenu.DefaultBranding(), "reset": true})
		return
	}

	cleaned, notes := req.Branding.Sanitise()
	if err := app.branding().save(cleaned); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save the branding: " + err.Error()})
		return
	}
	app.auditRequest(c, audit.EventSettingsSaved, audit.Success, "menu branding",
		map[string]string{"change": "updated"})

	// The notes matter: silently dropping a character an administrator drew
	// artwork with is how a banner ends up looking wrong on a screen nobody
	// who edited it ever sees.
	c.JSON(http.StatusOK, gin.H{"branding": cleaned, "notes": notes})
}

// AdminPreviewMenuHandler renders the selection screen as text, so an
// administrator can see the result without opening a terminal session against
// their own instance.
func (app *App) AdminPreviewMenuHandler(c *gin.Context) {
	// The whole published set, not the caller's own assigned list. This is a
	// preview of the screen rather than of their screen: an administrator
	// checking their artwork should see the host list they administer, and
	// theirs is often short or empty precisely because they assigned the
	// hosts to other people.
	profiles, err := app.visibleProfiles(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read the host list"})
		return
	}
	entries := make([]sessionmenu.Entry, 0, len(profiles))
	for _, p := range profiles {
		entries = append(entries, sessionmenu.Entry{
			Name: p.Name, Description: p.Description, Detail: p.displayTarget(),
		})
	}
	if len(entries) == 0 {
		// Something to look at, so the preview works before any host has been
		// assigned — which is exactly when somebody is setting this up.
		entries = []sessionmenu.Entry{
			{Name: "CICSPROD", Description: "Production CICS region", Detail: "mvs1.example:992"},
			{Name: "TSO", Description: "TSO/ISPF logon", Detail: "mvs1.example:23"},
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"rows": sessionmenu.Preview(app.menuBranding(), entries),
	})
}
