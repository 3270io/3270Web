package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/h3270/internal/assets"
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

type WorkflowConfig struct {
	Host            string                      `json:"Host"`
	Port            int                         `json:"Port"`
	EveryStepDelay  *session.WorkflowDelayRange `json:"EveryStepDelay,omitempty"`
	OutputFilePath  string                      `json:"OutputFilePath,omitempty"`
	RampUpBatchSize int                         `json:"RampUpBatchSize,omitempty"`
	RampUpDelay     float64                     `json:"RampUpDelay,omitempty"`
	EndOfTaskDelay  *session.WorkflowDelayRange `json:"EndOfTaskDelay,omitempty"`
	Steps           []session.WorkflowStep      `json:"Steps"`
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
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Printf("Warning: could not set trusted proxies: %v", err)
	}
	r.LoadHTMLGlob("web/templates/*")
	r.Static("/static", "web/static")

	r.GET("/", app.HomeHandler)
	r.POST("/connect", app.ConnectHandler)
	r.GET("/screen", app.ScreenHandler)
	r.POST("/submit", app.SubmitHandler)
	r.POST("/prefs", app.PrefsHandler)
	r.POST("/record/start", app.RecordStartHandler)
	r.POST("/record/stop", app.RecordStopHandler)
	r.GET("/record/download", app.RecordDownloadHandler)

	// Disconnect handler
	r.GET("/disconnect", app.DisconnectHandler)

	log.Println("Starting server on :8080")
	if runtime.GOOS == "windows" {
		go openBrowser("http://localhost:8080/")
	}
	if err := r.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}

func openBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func parseHostPort(hostname string) (string, int) {
	host := strings.TrimSpace(hostname)
	port := 3270
	if host == "" {
		return "", port
	}
	if strings.Contains(host, ":") {
		if h, p, err := net.SplitHostPort(host); err == nil {
			host = h
			if n, err := strconv.Atoi(p); err == nil {
				port = n
			}
		}
	}
	return host, port
}

func recordingFileName(s *session.Session) string {
	if s == nil || s.Recording == nil || s.Recording.FilePath == "" {
		return ""
	}
	return filepath.Base(s.Recording.FilePath)
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
		"RecordingActive":     s.Recording != nil && s.Recording.Active,
		"RecordingFile":       recordingFileName(s),
	})
}

func (app *App) SubmitHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}

	key := c.PostForm("key")
	log.Printf("Submit: raw key=%q", key)

	if s.Host.GetScreen().IsFormatted {
		// 1. Update fields from form data
		app.updateFields(c, s)
		if s.Recording != nil && s.Recording.Active {
			recordFieldUpdates(s)
		}

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
		actionKey = normalizeKey(key)
	}
	log.Printf("Submit: normalized key=%q", actionKey)
	if s.Recording != nil && s.Recording.Active {
		recordActionKey(s, actionKey)
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

func (app *App) RecordStartHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	if s.Recording != nil && s.Recording.Active {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	host := s.TargetHost
	port := s.TargetPort
	if port == 0 {
		port = 3270
	}
	s.Recording = &session.WorkflowRecording{
		Active:         true,
		Host:           host,
		Port:           port,
		OutputFilePath: "output.html",
		Steps:          []session.WorkflowStep{{Type: "Connect"}},
		StartedAt:      time.Now(),
	}
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) RecordStopHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	if s.Recording == nil || !s.Recording.Active {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	s.Recording.Steps = append(s.Recording.Steps, session.WorkflowStep{Type: "Disconnect"})
	workflow := buildWorkflowConfig(s)
	path := filepath.Join(".", "workflow.json")
	if err := writeWorkflowFile(path, workflow); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("Record stop failed: %v", err)})
		return
	}
	s.Recording.Active = false
	s.Recording.FilePath = path
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) RecordDownloadHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil || s.Recording == nil || s.Recording.FilePath == "" {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	name := filepath.Base(s.Recording.FilePath)
	c.FileAttachment(s.Recording.FilePath, name)
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
			original := f.GetValue()

			if f.IsMultiline() {
				var parts []string
				for i := 0; i < f.Height(); i++ {
					val := c.PostForm(fmt.Sprintf("%s_%d", fieldName, i))
					// Java trimmed spaces? We should check if Gin returns empty string for missing fields.
					// If field is missing, it might mean user didn't change it or browser didn't send it?
					// Input type text sends empty string if empty.
					parts = append(parts, val)
				}
				newValue := strings.Join(parts, "\n")
				if newValue != original {
					f.SetValue(newValue)
				}
			} else {
				val := c.PostForm(fieldName)
				if val != original {
					f.SetValue(val)
				}
			}
		}
	}
}

func recordFieldUpdates(s *session.Session) {
	if s == nil || s.Recording == nil || !s.Recording.Active {
		return
	}
	screen := s.Host.GetScreen()
	for _, f := range screen.Fields {
		if f.IsProtected() || !f.Changed {
			continue
		}
		lines := f.GetValueLines()
		if !f.IsMultiline() {
			text := ""
			if len(lines) > 0 {
				text = lines[0]
			}
			s.Recording.Steps = append(s.Recording.Steps, session.WorkflowStep{
				Type: "FillString",
				Coordinates: &session.WorkflowCoordinates{
					Row:    f.StartY + 1,
					Column: f.StartX + 1,
				},
				Text: text,
			})
			continue
		}
		for i, line := range lines {
			s.Recording.Steps = append(s.Recording.Steps, session.WorkflowStep{
				Type: "FillString",
				Coordinates: &session.WorkflowCoordinates{
					Row:    f.StartY + 1 + i,
					Column: f.StartX + 1,
				},
				Text: line,
			})
		}
	}
}

func recordActionKey(s *session.Session, key string) {
	if s == nil || s.Recording == nil || !s.Recording.Active {
		return
	}
	trimmed := strings.TrimSpace(key)
	if trimmed == "" || strings.EqualFold(trimmed, "enter") {
		s.Recording.Steps = append(s.Recording.Steps, session.WorkflowStep{Type: "PressEnter"})
		return
	}
	if step := workflowStepForKey(key); step != nil {
		s.Recording.Steps = append(s.Recording.Steps, *step)
	}
}

func workflowStepForKey(key string) *session.WorkflowStep {
	upper := strings.ToUpper(strings.TrimSpace(key))
	if upper == "" {
		return nil
	}
	if upper == "ENTER" {
		return &session.WorkflowStep{Type: "PressEnter"}
	}
	if upper == "TAB" {
		return &session.WorkflowStep{Type: "PressTab"}
	}
	if strings.HasPrefix(upper, "PF(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PF("), ")")
		if n, err := strconv.Atoi(inner); err == nil && n >= 1 && n <= 24 {
			return &session.WorkflowStep{Type: fmt.Sprintf("PressPF%d", n)}
		}
	}
	if strings.HasPrefix(upper, "PF") {
		inner := strings.TrimPrefix(upper, "PF")
		if n, err := strconv.Atoi(inner); err == nil && n >= 1 && n <= 24 {
			return &session.WorkflowStep{Type: fmt.Sprintf("PressPF%d", n)}
		}
	}
	return nil
}

func buildWorkflowConfig(s *session.Session) *WorkflowConfig {
	if s == nil || s.Recording == nil {
		return &WorkflowConfig{}
	}
	host := s.Recording.Host
	port := s.Recording.Port
	if host == "" {
		host = s.TargetHost
	}
	if port == 0 {
		port = s.TargetPort
	}
	if port == 0 {
		port = 3270
	}
	return &WorkflowConfig{
		Host:            host,
		Port:            port,
		EveryStepDelay:  &session.WorkflowDelayRange{Min: 0.1, Max: 0.3},
		OutputFilePath:  s.Recording.OutputFilePath,
		RampUpBatchSize: 50,
		RampUpDelay:     1.5,
		EndOfTaskDelay:  &session.WorkflowDelayRange{Min: 60, Max: 120},
		Steps:           s.Recording.Steps,
	}
}

func writeWorkflowFile(path string, workflow *WorkflowConfig) error {
	data, err := json.MarshalIndent(workflow, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
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
		execPath := resolveS3270Path(app.Config.ExecPath)
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
	sess.TargetHost, sess.TargetPort = parseHostPort(hostname)
	app.applyDefaultPrefs(sess)
	setSessionCookie(c, "h3270_session", sess.ID)
	return nil
}

func resolveS3270Path(execPath string) string {
	if execPath != "" && execPath != "/usr/local/bin" {
		return filepath.Join(execPath, s3270BinaryName())
	}

	local := filepath.Join(".", "s3270-bin", s3270BinaryName())
	if fileExists(local) {
		return local
	}

	if embedded, err := assets.ExtractS3270(); err == nil {
		return embedded
	}

	return filepath.Join("/usr/local/bin", "s3270")
}

func s3270BinaryName() string {
	if runtime.GOOS == "windows" {
		return "s3270.exe"
	}
	return "s3270"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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

func normalizeKey(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "Enter"
	}

	upper := strings.ToUpper(trimmed)
	lower := strings.ToLower(trimmed)

	if strings.HasPrefix(upper, "PF(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PF("), ")")
		if n, err := strconv.Atoi(inner); err == nil {
			if n >= 1 && n <= 24 {
				return fmt.Sprintf("PF(%d)", n)
			}
		}
	}
	if strings.HasPrefix(upper, "PA(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PA("), ")")
		if n, err := strconv.Atoi(inner); err == nil {
			if n >= 1 && n <= 3 {
				return fmt.Sprintf("PA(%d)", n)
			}
		}
	}
	if strings.HasPrefix(upper, "PF") {
		if n, err := strconv.Atoi(strings.TrimPrefix(upper, "PF")); err == nil {
			if n >= 1 && n <= 24 {
				return fmt.Sprintf("PF(%d)", n)
			}
		}
	}
	if strings.HasPrefix(upper, "PA") {
		if n, err := strconv.Atoi(strings.TrimPrefix(upper, "PA")); err == nil {
			if n >= 1 && n <= 3 {
				return fmt.Sprintf("PA(%d)", n)
			}
		}
	}
	if strings.HasPrefix(upper, "F") {
		if n, err := strconv.Atoi(strings.TrimPrefix(upper, "F")); err == nil {
			if n >= 1 && n <= 24 {
				return fmt.Sprintf("PF(%d)", n)
			}
		}
	}

	switch lower {
	case "enter":
		return "Enter"
	case "tab":
		return "Tab"
	case "backtab":
		return "BackTab"
	case "clear":
		return "Clear"
	case "reset":
		return "Reset"
	case "eraseeof", "erase_eof":
		return "EraseEOF"
	case "eraseinput", "erase_input":
		return "EraseInput"
	case "dup":
		return "Dup"
	case "fieldmark", "field_mark":
		return "FieldMark"
	case "sysreq", "sys_req":
		return "SysReq"
	case "attn":
		return "Attn"
	case "newline", "new_line":
		return "Newline"
	case "backspace":
		return "BackSpace"
	case "delete":
		return "Delete"
	case "insert":
		return "Insert"
	case "home":
		return "Home"
	case "up":
		return "Up"
	case "down":
		return "Down"
	case "left":
		return "Left"
	case "right":
		return "Right"
	}

	return trimmed
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
