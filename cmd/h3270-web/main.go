package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/h3270/internal/config"
	"github.com/jnnngs/h3270/internal/host"
	"github.com/jnnngs/h3270/internal/render"
	"github.com/jnnngs/h3270/internal/session"
)

type App struct {
	SessionManager *session.Manager
	Renderer       render.Renderer
	Config         *config.Config
}

func main() {
	cfg, err := config.Load("webapp/WEB-INF/h3270-config.xml")
	if err != nil {
		log.Printf("Warning: Could not load config: %v", err)
		cfg = &config.Config{ExecPath: "/usr/local/bin"}
	}

	app := &App{
		SessionManager: session.NewManager(),
		Renderer:       render.NewHtmlRenderer(),
		Config:         cfg,
	}

	r := gin.Default()
	r.LoadHTMLGlob("web/templates/*")
	r.Static("/static", "web/static")

	r.GET("/", app.HomeHandler)
	r.POST("/connect", app.ConnectHandler)
	r.GET("/screen", app.ScreenHandler)
	r.POST("/submit", app.SubmitHandler)
	r.POST("/prefs", app.PrefsHandler)

	// Disconnect handler
	r.GET("/disconnect", app.DisconnectHandler)

	log.Println("Starting server on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}

func (app *App) HomeHandler(c *gin.Context) {
	// Check session
	if s := app.getSession(c); s != nil {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	if app.Config.TargetHost.Value != "" && app.Config.TargetHost.AutoConnect {
		if err := app.connectToHost(c, app.Config.TargetHost.Value); err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": err.Error()})
			return
		}
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	c.HTML(http.StatusOK, "connect.html", gin.H{})
}

func (app *App) ConnectHandler(c *gin.Context) {
	hostname := c.PostForm("hostname")
	if app.Config.TargetHost.Value != "" {
		hostname = app.Config.TargetHost.Value
	}
	if hostname == "" {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"Error": "Missing hostname"})
		return
	}

	if err := app.connectToHost(c, hostname); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) ScreenHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	app.ensurePrefs(s)

	if err := s.Host.UpdateScreen(); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("Update screen failed: %v", err)})
		return
	}

	screen := s.Host.GetScreen()
	rendered := app.Renderer.Render(screen, "/submit", s.ID)
	themeCSS := app.buildThemeCSS(s.Prefs)

	c.HTML(http.StatusOK, "screen.html", gin.H{
		"ScreenContent":       template.HTML(rendered),
		"SessionID":           s.ID,
		"ColorSchemes":        app.Config.ColorSchemes.Schemes,
		"Fonts":               app.Config.Fonts.Fonts,
		"SelectedColorScheme": s.Prefs.ColorScheme,
		"SelectedFont":        s.Prefs.FontName,
		"UseKeypad":           s.Prefs.UseKeypad,
		"ThemeCSS":            template.CSS(themeCSS),
	})
}

func (app *App) SubmitHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}

	key := c.PostForm("key")

	if s.Host.GetScreen().IsFormatted {
		// 1. Update fields from form data
		app.updateFields(c, s)

		// 2. Submit changes to host
		if err := s.Host.SubmitScreen(); err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("Submit failed: %v", err)})
			return
		}
	} else {
		data := c.PostForm("field")
		if err := s.Host.SubmitUnformatted(data); err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("Submit failed: %v", err)})
			return
		}
	}

	// 3. Send action key
	actionKey := "Enter"
	if key != "" {
		actionKey = key
	}

	if err := s.Host.SendKey(actionKey); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("SendKey failed: %v", err)})
		return
	}

	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) DisconnectHandler(c *gin.Context) {
	if s := app.getSession(c); s != nil {
		app.SessionManager.RemoveSession(s.ID)
	}
	setSessionCookie(c, "h3270_session", "")
	c.Redirect(http.StatusFound, "/")
}

func (app *App) PrefsHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	app.ensurePrefs(s)

	if cs := c.PostForm("colorscheme"); cs != "" {
		if app.isValidColorScheme(cs) {
			s.Prefs.ColorScheme = cs
		}
	}
	if font := c.PostForm("font"); font != "" {
		if app.isValidFont(font) {
			s.Prefs.FontName = font
		}
	}
	s.Prefs.UseKeypad = c.PostForm("keypad") == "on"

	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) updateFields(c *gin.Context, s *session.Session) {
	screen := s.Host.GetScreen()
	for _, f := range screen.Fields {
		if !f.IsProtected() {
			fieldName := fmt.Sprintf("field_%d_%d", f.StartX, f.StartY)

			if f.IsMultiline() {
				var parts []string
				for i := 0; i < f.Height(); i++ {
					val := c.PostForm(fmt.Sprintf("%s_%d", fieldName, i))
					// Java trimmed spaces? We should check if Gin returns empty string for missing fields.
					// If field is missing, it might mean user didn't change it or browser didn't send it?
					// Input type text sends empty string if empty.
					parts = append(parts, val)
				}
				f.SetValue(strings.Join(parts, "\n"))
			} else {
				val := c.PostForm(fieldName)
				f.SetValue(val)
			}
		}
	}
}

func (app *App) getSession(c *gin.Context) *session.Session {
	id, err := c.Cookie("h3270_session")
	if err != nil {
		return nil
	}
	s, ok := app.SessionManager.GetSession(id)
	if !ok {
		return nil
	}
	return s
}

func (app *App) connectToHost(c *gin.Context, hostname string) error {
	var h host.Host
	var err error

	if hostname == "mock" || hostname == "demo" {
		h, err = host.NewMockHost("webapp/WEB-INF/dump/advantis.dump")
	} else {
		execPath := "./s3270-bin/s3270-linux"
		if app.Config.ExecPath != "" && app.Config.ExecPath != "/usr/local/bin" {
			execPath = app.Config.ExecPath + "/s3270"
		}
		args := buildS3270Args(app.Config.S3270Options, hostname)
		h = host.NewS3270(execPath, args...)
	}

	if err != nil {
		return fmt.Errorf("failed to create host: %w", err)
	}
	if err := h.Start(); err != nil {
		return fmt.Errorf("failed to start host connection: %w", err)
	}

	sess := app.SessionManager.CreateSession(h)
	app.applyDefaultPrefs(sess)
	setSessionCookie(c, "h3270_session", sess.ID)
	return nil
}

func buildS3270Args(opts config.S3270Options, hostname string) []string {
	args := []string{"-model", opts.Model}
	if opts.Charset != "" && opts.Charset != "bracket" {
		args = append(args, "-charset", opts.Charset)
	}
	if opts.Additional != "" {
		args = append(args, strings.Fields(opts.Additional)...)
	}
	args = append(args, hostname)
	return args
}

func setSessionCookie(c *gin.Context, name, value string) {
	secure := c.Request.TLS != nil
	c.SetSameSite(http.SameSiteLaxMode)
	maxAge := 3600
	if value == "" {
		maxAge = -1
	}
	c.SetCookie(name, value, maxAge, "/", "", secure, true)
}

func (app *App) ensurePrefs(s *session.Session) {
	if s.Prefs.ColorScheme == "" && app.Config.ColorSchemes.Default != "" {
		s.Prefs.ColorScheme = app.Config.ColorSchemes.Default
	}
	if s.Prefs.FontName == "" && app.Config.Fonts.Default != "" {
		s.Prefs.FontName = app.Config.Fonts.Default
	}
}

func (app *App) applyDefaultPrefs(s *session.Session) {
	app.ensurePrefs(s)
}

func (app *App) isValidColorScheme(name string) bool {
	for _, cs := range app.Config.ColorSchemes.Schemes {
		if cs.Name == name {
			return true
		}
	}
	return false
}

func (app *App) isValidFont(name string) bool {
	for _, f := range app.Config.Fonts.Fonts {
		if f.Name == name {
			return true
		}
	}
	return false
}

func (app *App) buildThemeCSS(p session.Preferences) string {
	fontName := app.resolveFontName(p.FontName)
	cs, _ := app.resolveColorScheme(p.ColorScheme)

	var sb strings.Builder
	if fontName != "" {
		escapedFont := strings.ReplaceAll(fontName, "\"", "\\\"")
		sb.WriteString(fmt.Sprintf("pre, pre input, textarea { font-family: \"%s\", monospace; }\n", escapedFont))
	}
	if cs.Name != "" {
		writeRule(&sb, ".h3270-form", cs.PNBg, cs.PNFg)
		writeRule(&sb, "pre, pre input, textarea", cs.PNBg, cs.PNFg)
		writeRule(&sb, ".screen-container", cs.PNBg, cs.PNFg)
		writeRule(&sb, ".h3270-intensified", cs.PIBg, cs.PIFg)
		writeRule(&sb, ".h3270-hidden", cs.PHBg, cs.PHFg)
		writeRule(&sb, ".h3270-input", cs.UNBg, cs.UNFg)
		writeRule(&sb, ".h3270-input-intensified", cs.UIBg, cs.UIFg)
		writeRule(&sb, ".h3270-input-hidden", cs.UHBg, cs.UHFg)
	}

	return sb.String()
}

func writeRule(sb *strings.Builder, selector, bg, fg string) {
	if bg == "" && fg == "" {
		return
	}
	sb.WriteString(selector)
	sb.WriteString(" {")
	if bg != "" {
		sb.WriteString(" background-color: ")
		sb.WriteString(bg)
		sb.WriteString(";")
	}
	if fg != "" {
		sb.WriteString(" color: ")
		sb.WriteString(fg)
		sb.WriteString(";")
	}
	sb.WriteString(" }\n")
}

func (app *App) resolveColorScheme(name string) (config.ColorScheme, bool) {
	if name != "" {
		for _, cs := range app.Config.ColorSchemes.Schemes {
			if cs.Name == name {
				return cs, true
			}
		}
	}
	if app.Config.ColorSchemes.Default != "" {
		for _, cs := range app.Config.ColorSchemes.Schemes {
			if cs.Name == app.Config.ColorSchemes.Default {
				return cs, true
			}
		}
	}
	return config.ColorScheme{}, false
}

func (app *App) resolveFontName(name string) string {
	if name != "" {
		for _, f := range app.Config.Fonts.Fonts {
			if f.Name == name {
				return f.Name
			}
		}
	}
	if app.Config.Fonts.Default != "" {
		for _, f := range app.Config.Fonts.Fonts {
			if f.Name == app.Config.Fonts.Default {
				return f.Name
			}
		}
	}
	return ""
}
