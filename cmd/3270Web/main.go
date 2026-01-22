package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	webassets "github.com/jnnngs/3270Web"
	"github.com/jnnngs/3270Web/internal/assets"
	"github.com/jnnngs/3270Web/internal/config"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/render"
	"github.com/jnnngs/3270Web/internal/session"
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

type SampleAppConfig struct {
	ID   string
	Name string
}

type SampleAppOption struct {
	ID       string
	Name     string
	Hostname string
}

const sampleAppPrefix = "sampleapp:"

var sampleAppConfigs = []SampleAppConfig{
	{ID: "app1", Name: "Sample App 1"},
	{ID: "app2", Name: "Sample App 2"},
}

const defaultSampleAppPort = 3270

func main() {
	baseDir := resolveBaseDir()
	logFile, err := openStartupLog(baseDir)
	if err == nil {
		defer logFile.Close()
		log.SetOutput(logFile)
	}
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			msg := fmt.Sprintf("3270Web crashed during startup: %v", r)
			log.Printf("%s\n%s", msg, stack)
			showFatalError(msg)
		}
	}()
	configPath := filepath.Join(baseDir, "webapp", "WEB-INF", "3270Web-config.xml")
	if !fileExists(configPath) {
		if cwd, err := os.Getwd(); err == nil {
			fallback := filepath.Join(cwd, "webapp", "WEB-INF", "3270Web-config.xml")
			if fileExists(fallback) {
				configPath = fallback
			}
		}
	}

	cfg, err := config.Load(configPath)
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
	templatesGlob, tmplErr := resolveTemplatesGlob(baseDir)
	if tmplErr == nil {
		r.LoadHTMLGlob(templatesGlob)
	} else {
		log.Printf("Warning: %v", tmplErr)
		tmplFS, err := fs.Sub(webassets.FS, "web/templates")
		if err != nil {
			showFatalError(err.Error())
			return
		}
		r.LoadHTMLFS(http.FS(tmplFS), "*")
	}

	staticDir, staticErr := resolveStaticDir(baseDir)
	if staticErr == nil {
		r.Static("/static", staticDir)
	} else {
		log.Printf("Warning: %v", staticErr)
		staticFS, err := fs.Sub(webassets.FS, "web/static")
		if err != nil {
			showFatalError(err.Error())
			return
		}
		r.StaticFS("/static", http.FS(staticFS))
	}

	r.GET("/", app.HomeHandler)
	r.POST("/connect", app.ConnectHandler)
	r.GET("/screen", app.ScreenHandler)
	r.POST("/submit", app.SubmitHandler)
	r.POST("/prefs", app.PrefsHandler)
	r.POST("/record/start", app.RecordStartHandler)
	r.POST("/record/stop", app.RecordStopHandler)
	r.GET("/record/download", app.RecordDownloadHandler)
	r.POST("/workflow/load", app.LoadWorkflowHandler)
	r.POST("/workflow/play", app.PlayWorkflowHandler)
	r.POST("/workflow/debug", app.DebugWorkflowHandler)
	r.POST("/workflow/pause", app.PauseWorkflowHandler)
	r.POST("/workflow/step", app.StepWorkflowHandler)
	r.POST("/workflow/stop", app.StopWorkflowHandler)
	r.POST("/workflow/remove", app.RemoveWorkflowHandler)

	// Disconnect handler
	r.GET("/disconnect", app.DisconnectHandler)

	shutdownCh := make(chan struct{})
	requestShutdown := func() {
		select {
		case <-shutdownCh:
			return
		default:
			close(shutdownCh)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		requestShutdown()
	}()

	addr := ":8080"
	if runtime.GOOS == "windows" {
		addr = "127.0.0.1:8080"
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		<-shutdownCh
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Shutdown failed: %v", err)
		}
	}()

	startServer := func(errCh chan<- error) {
		log.Printf("Starting server on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}

	if runtime.GOOS == "windows" {
		errCh := make(chan error, 1)
		go startServer(errCh)
		waitForServer(addr, 5*time.Second)
		select {
		case err := <-errCh:
			log.Printf("Server failed: %v", err)
			showFatalError(fmt.Sprintf("3270Web failed to start. %v", err))
			return
		default:
		}
		runAppWindow("http://"+addr+"/", requestShutdown)
		return
	}

	errCh := make(chan error, 1)
	startServer(errCh)
	select {
	case err := <-errCh:
		log.Printf("Server failed: %v", err)
		showFatalError(fmt.Sprintf("3270Web failed to start. %v", err))
		return
	default:
	}
}

func waitForServer(addr string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func resolveBaseDir() string {
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			return dir
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func openStartupLog(baseDir string) (*os.File, error) {
	path := filepath.Join(baseDir, "3270Web.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Printf("Starting 3270Web from %s", baseDir)
	return file, nil
}

func resolveTemplatesGlob(baseDir string) (string, error) {
	primary := filepath.Join(baseDir, "web", "templates", "*")
	if matches, _ := filepath.Glob(primary); len(matches) > 0 {
		return primary, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		fallback := filepath.Join(cwd, "web", "templates", "*")
		if matches, _ := filepath.Glob(fallback); len(matches) > 0 {
			return fallback, nil
		}
	}
	return "", fmt.Errorf("templates not found. Expected %s", primary)
}

func resolveStaticDir(baseDir string) (string, error) {
	primary := filepath.Join(baseDir, "web", "static")
	if dirExists(primary) {
		return primary, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		fallback := filepath.Join(cwd, "web", "static")
		if dirExists(fallback) {
			return fallback, nil
		}
	}
	return "", fmt.Errorf("static assets not found. Expected %s", primary)
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
	if id, samplePort, ok := parseSampleAppHost(host); ok {
		host = sampleAppHostname(id)
		if samplePort > 0 {
			if !isAllowedSampleAppPort(samplePort) {
				return "", port
			}
			port = samplePort
		}
		return host, port
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

func (app *App) renderConnectPage(c *gin.Context, status int, hostname string, connectError string) {
	defaultHost := strings.TrimSpace(hostname)
	if app.Config.TargetHost.Value != "" {
		defaultHost = app.Config.TargetHost.Value
	}
	if defaultHost == "" {
		defaultHost = "localhost:3270"
	}
	samplePorts := allowedSampleAppPorts()
	c.HTML(status, "connect.html", gin.H{
		"DefaultHost":  defaultHost,
		"SampleApps":   availableSampleApps(),
		"SamplePorts":  samplePorts,
		"ConnectError": connectError,
	})
}

func (app *App) HomeHandler(c *gin.Context) {
	// Check session
	if s := app.getSession(c); s != nil {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	targetHost := strings.TrimSpace(app.Config.TargetHost.Value)
	if targetHost != "" && app.Config.TargetHost.AutoConnect {
		if err := app.connectToHost(c, targetHost); err != nil {
			log.Printf("Auto-connect failed for %q: %v", targetHost, err)
			app.renderConnectPage(c, http.StatusServiceUnavailable, targetHost, connectErrorMessage(targetHost, err))
			return
		}
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	app.renderConnectPage(c, http.StatusOK, targetHost, "")
}

func (app *App) ConnectHandler(c *gin.Context) {
	hostname := c.PostForm("hostname")
	if app.Config.TargetHost.Value != "" {
		hostname = strings.TrimSpace(app.Config.TargetHost.Value)
	} else {
		hostname = strings.TrimSpace(hostname)
	}
	if hostname == "" {
		app.renderConnectPage(c, http.StatusBadRequest, hostname, connectErrorMessage(hostname, nil))
		return
	}

	if err := app.connectToHost(c, hostname); err != nil {
		log.Printf("Connect failed for %q: %v", hostname, err)
		app.renderConnectPage(c, http.StatusServiceUnavailable, hostname, connectErrorMessage(hostname, err))
		return
	}
	c.Redirect(http.StatusFound, "/screen")
}

func connectErrorMessage(hostname string, err error) string {
	if hostname == "" {
		return "Please enter a hostname or IP address to connect."
	}
	if _, _, ok := parseSampleAppHost(hostname); ok && err != nil {
		return fmt.Sprintf("We couldn't start the sample app at %s. %v", hostname, err)
	}
	return fmt.Sprintf("We couldn't connect to %s. Please verify the address and that the TN3270 service is available, then try again.", hostname)
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

	sampleAppName := ""
	sampleAppPort := 0
	if id, _, ok := parseSampleAppHost(s.TargetHost); ok {
		if cfg, ok := sampleAppConfig(id); ok {
			sampleAppName = cfg.Name
			sampleAppPort = s.TargetPort
			if sampleAppPort == 0 {
				sampleAppPort = 3270
			}
		}
	}
	c.HTML(http.StatusOK, "screen.html", gin.H{
		"ScreenContent":         template.HTML(rendered),
		"SessionID":             s.ID,
		"ColorSchemes":          app.Config.ColorSchemes.Schemes,
		"Fonts":                 app.Config.Fonts.Fonts,
		"SelectedColorScheme":   s.Prefs.ColorScheme,
		"SelectedFont":          s.Prefs.FontName,
		"UseKeypad":             s.Prefs.UseKeypad,
		"ThemeCSS":              template.CSS(themeCSS),
		"RecordingActive":       s.Recording != nil && s.Recording.Active,
		"RecordingFile":         recordingFileName(s),
		"PlaybackActive":        s.Playback != nil && s.Playback.Active,
		"PlaybackPaused":        s.Playback != nil && s.Playback.Active && s.Playback.Paused,
		"PlaybackCompleted":     !s.PlaybackCompletedAt.IsZero(),
		"PlaybackMode":          playbackMode(s),
		"PlaybackStep":          playbackStepIndex(s),
		"PlaybackStepType":      playbackStepType(s),
		"PlaybackStepTotal":     playbackStepTotal(s),
		"PlaybackDelayRange":    playbackDelayRangeLabel(s),
		"PlaybackDelayApplied":  playbackDelayAppliedLabel(s),
		"PlaybackEvents":        playbackEvents(s),
		"LoadedWorkflow":        s.LoadedWorkflow != nil,
		"LoadedWorkflowName":    loadedWorkflowName(s),
		"LoadedWorkflowPreview": loadedWorkflowPreview(s),
		"LoadedWorkflowSize":    loadedWorkflowSize(s),
		"SampleAppName":         sampleAppName,
		"SampleAppPort":         sampleAppPort,
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
	setSessionCookie(c, "3270Web_session", "")
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
	if s.Playback != nil && s.Playback.Active {
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

func (app *App) LoadWorkflowHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	if s.Playback != nil && s.Playback.Active {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	upload, err := loadWorkflowUpload(c)
	if err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"Error": fmt.Sprintf("Load workflow failed: %v", err)})
		return
	}
	preview := prettyWorkflowPayload(upload.Payload)
	s.LoadedWorkflow = &session.LoadedWorkflow{
		Name:     upload.Name,
		Payload:  upload.Payload,
		Preview:  preview,
		LoadedAt: time.Now(),
	}
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) PlayWorkflowHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	if s.Playback != nil && s.Playback.Active {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	workflow, err := workflowFromSessionOrUpload(s, c)
	if err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"Error": fmt.Sprintf("Load workflow failed: %v", err)})
		return
	}
	hostname, err := workflowTargetHost(s, workflow)
	if err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"Error": fmt.Sprintf("Load workflow failed: %v", err)})
		return
	}
	if err := app.resetSessionHost(s, hostname); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("Workflow connection failed: %v", err)})
		return
	}
	s.PlaybackCompletedAt = time.Time{}
	s.Playback = &session.WorkflowPlayback{StartedAt: time.Now(), Mode: "play", TotalSteps: len(workflow.Steps)}
	s.PlaybackEvents = nil
	addPlaybackEvent(s, "Playback started (Play mode)")
	go app.playWorkflow(s, workflow)
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) DebugWorkflowHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	if s.Playback != nil && s.Playback.Active {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	workflow, err := workflowFromSessionOrUpload(s, c)
	if err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"Error": fmt.Sprintf("Load workflow failed: %v", err)})
		return
	}
	hostname, err := workflowTargetHost(s, workflow)
	if err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"Error": fmt.Sprintf("Load workflow failed: %v", err)})
		return
	}
	if err := app.resetSessionHost(s, hostname); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("Workflow connection failed: %v", err)})
		return
	}
	s.PlaybackCompletedAt = time.Time{}
	s.Playback = &session.WorkflowPlayback{StartedAt: time.Now(), Mode: "debug", TotalSteps: len(workflow.Steps), Paused: true}
	s.PlaybackEvents = nil
	addPlaybackEvent(s, "Playback started (Debug mode)")
	go app.playWorkflow(s, workflow)
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) StepWorkflowHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	if s.Playback == nil || !s.Playback.Active || s.Playback.Mode != "debug" {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	s.Playback.StepRequested = true
	addPlaybackEvent(s, "Step requested")
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) PauseWorkflowHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	if s.Playback == nil || !s.Playback.Active {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	s.Playback.Paused = !s.Playback.Paused
	if s.Playback.Paused {
		addPlaybackEvent(s, "Playback paused")
	} else {
		addPlaybackEvent(s, "Playback resumed")
	}
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) StopWorkflowHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	stopWorkflowPlayback(s)
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) RemoveWorkflowHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	stopWorkflowPlayback(s)
	s.LoadedWorkflow = nil
	c.Redirect(http.StatusFound, "/screen")
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
			original := normalizeInputValue(f.GetValue())

			if f.IsMultiline() {
				var parts []string
				for i := 0; i < f.Height(); i++ {
					val := c.PostForm(fmt.Sprintf("%s_%d", fieldName, i))
					// Java trimmed spaces? We should check if Gin returns empty string for missing fields.
					// If field is missing, it might mean user didn't change it or browser didn't send it?
					// Input type text sends empty string if empty.
					parts = append(parts, normalizeInputValue(val))
				}
				newValue := strings.Join(parts, "\n")
				if newValue != original {
					f.SetValue(newValue)
				}
			} else {
				val := normalizeInputValue(c.PostForm(fieldName))
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
				text = normalizeInputValue(lines[0])
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
				Text: normalizeInputValue(line),
			})
		}
	}
}

func normalizeInputValue(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "\n")
	for i, part := range parts {
		parts[i] = strings.Trim(part, "\x00 _")
	}
	return strings.Join(parts, "\n")
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

func loadWorkflowConfig(c *gin.Context) (*WorkflowConfig, error) {
	upload, err := loadWorkflowUpload(c)
	if err != nil {
		return nil, err
	}
	return upload.Config, nil
}

type workflowUpload struct {
	Name    string
	Payload []byte
	Config  *WorkflowConfig
}

func loadWorkflowUpload(c *gin.Context) (*workflowUpload, error) {
	file, name, err := workflowFileFromRequest(c)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	payload, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	workflow, err := parseWorkflowPayload(payload)
	if err != nil {
		return nil, err
	}
	return &workflowUpload{Name: name, Payload: payload, Config: workflow}, nil
}

func parseWorkflowPayload(payload []byte) (*WorkflowConfig, error) {
	if len(payload) == 0 {
		return nil, errors.New("workflow file is empty")
	}
	var workflow WorkflowConfig
	if err := json.Unmarshal(payload, &workflow); err != nil {
		return nil, err
	}
	if len(workflow.Steps) == 0 {
		return nil, errors.New("workflow contains no steps")
	}
	return &workflow, nil
}

func workflowFileFromRequest(c *gin.Context) (io.ReadCloser, string, error) {
	file, err := c.FormFile("workflow")
	if err == nil {
		reader, err := file.Open()
		return reader, file.Filename, err
	}
	if !errors.Is(err, http.ErrMissingFile) {
		return nil, "", err
	}
	return nil, "", errors.New("workflow file is required")
}

func prettyWorkflowPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	var out bytes.Buffer
	if err := json.Indent(&out, payload, "", "  "); err != nil {
		return string(payload)
	}
	return out.String()
}

func workflowFromSessionOrUpload(s *session.Session, c *gin.Context) (*WorkflowConfig, error) {
	if s != nil && s.LoadedWorkflow != nil {
		return parseWorkflowPayload(s.LoadedWorkflow.Payload)
	}
	upload, err := loadWorkflowUpload(c)
	if err != nil {
		return nil, err
	}
	preview := prettyWorkflowPayload(upload.Payload)
	if s != nil {
		s.LoadedWorkflow = &session.LoadedWorkflow{
			Name:     upload.Name,
			Payload:  upload.Payload,
			Preview:  preview,
			LoadedAt: time.Now(),
		}
	}
	return upload.Config, nil
}

func loadedWorkflowName(s *session.Session) string {
	if s == nil || s.LoadedWorkflow == nil {
		return ""
	}
	return s.LoadedWorkflow.Name
}

func loadedWorkflowPreview(s *session.Session) string {
	if s == nil || s.LoadedWorkflow == nil {
		return ""
	}
	return s.LoadedWorkflow.Preview
}

func loadedWorkflowSize(s *session.Session) int {
	if s == nil || s.LoadedWorkflow == nil {
		return 0
	}
	return len(s.LoadedWorkflow.Payload)
}

func playbackMode(s *session.Session) string {
	if s == nil || s.Playback == nil {
		return ""
	}
	return s.Playback.Mode
}

func playbackStepIndex(s *session.Session) int {
	if s == nil || s.Playback == nil {
		return 0
	}
	return s.Playback.CurrentStep
}

func playbackStepType(s *session.Session) string {
	if s == nil || s.Playback == nil {
		return ""
	}
	return s.Playback.CurrentStepType
}

func playbackStepTotal(s *session.Session) int {
	if s == nil || s.Playback == nil {
		return 0
	}
	return s.Playback.TotalSteps
}

func playbackDelayRangeLabel(s *session.Session) string {
	if s == nil || s.Playback == nil {
		return ""
	}
	if s.Playback.CurrentDelayMin <= 0 && s.Playback.CurrentDelayMax <= 0 {
		return ""
	}
	min := s.Playback.CurrentDelayMin
	max := s.Playback.CurrentDelayMax
	if max < min {
		max = min
	}
	if max == min {
		return fmt.Sprintf("%.2fs", min)
	}
	return fmt.Sprintf("%.2fs–%.2fs", min, max)
}

func playbackDelayAppliedLabel(s *session.Session) string {
	if s == nil || s.Playback == nil {
		return ""
	}
	if s.Playback.CurrentDelayUsed <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2fs", s.Playback.CurrentDelayUsed.Seconds())
}

func playbackEvents(s *session.Session) []session.WorkflowEvent {
	if s == nil {
		return nil
	}
	return s.PlaybackEvents
}

func addPlaybackEvent(s *session.Session, message string) {
	if s == nil {
		return
	}
	entry := session.WorkflowEvent{Time: time.Now(), Message: message}
	s.PlaybackEvents = append(s.PlaybackEvents, entry)
	const maxEvents = 200
	if len(s.PlaybackEvents) > maxEvents {
		s.PlaybackEvents = s.PlaybackEvents[len(s.PlaybackEvents)-maxEvents:]
	}
}

func workflowTargetHost(s *session.Session, workflow *WorkflowConfig) (string, error) {
	if workflow != nil && workflow.Host != "" {
		host := workflow.Host
		if workflow.Port > 0 {
			host = fmt.Sprintf("%s:%d", host, workflow.Port)
		}
		return host, nil
	}
	if s != nil && s.TargetHost != "" {
		host := s.TargetHost
		if s.TargetPort > 0 {
			host = fmt.Sprintf("%s:%d", host, s.TargetPort)
		}
		return host, nil
	}
	return "", errors.New("workflow host not provided")
}

func (app *App) resetSessionHost(s *session.Session, hostname string) error {
	if s == nil {
		return errors.New("missing session")
	}
	if s.Host != nil {
		_ = s.Host.Stop()
	}
	hostName, port := parseHostPort(hostname)
	if hostName == "" {
		return errors.New("invalid host")
	}
	var h host.Host
	var err error
	if sampleID, samplePort, ok := parseSampleAppHost(hostname); ok {
		if samplePort > 0 && !isAllowedSampleAppPort(samplePort) {
			return fmt.Errorf("invalid sample app port %d", samplePort)
		}
		execPath := resolveS3270Path(app.Config.ExecPath)
		h, err = newSampleAppHost(sampleID, samplePort, execPath, app.Config.S3270Options)
	} else if hostname == "mock" || hostname == "demo" {
		execPath := resolveS3270Path(app.Config.ExecPath)
		h, err = newSampleAppHost("app1", defaultSampleAppPort, execPath, app.Config.S3270Options)
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
	s.Host = h
	s.TargetHost = hostName
	s.TargetPort = port
	return nil
}

func (app *App) playWorkflow(s *session.Session, workflow *WorkflowConfig) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("workflow panic: %v\n%s", r, debug.Stack())
		}
	}()
	defer func() {
		if s != nil && s.Playback != nil {
			s.Playback.Active = false
			s.Playback = nil
		}
	}()
	if s == nil || workflow == nil {
		return
	}
	addPlaybackEvent(s, fmt.Sprintf("Loaded %d steps", len(workflow.Steps)))
	if s.Playback == nil {
		s.Playback = &session.WorkflowPlayback{StartedAt: time.Now(), Mode: "play", TotalSteps: len(workflow.Steps)}
	}
	s.Playback.Active = true
	s.Playback.PendingInput = false
	if s.Playback.Mode == "play" {
		s.Playback.Paused = false
	}
	s.Playback.StopRequested = false
	for i, step := range workflow.Steps {
		if playbackShouldStop(s) {
			return
		}
		s.Playback.CurrentStep = i + 1
		s.Playback.CurrentStepType = step.Type
		addPlaybackEvent(s, fmt.Sprintf("Step %d/%d: %s", i+1, s.Playback.TotalSteps, step.Type))
		if rng := workflowStepDelayRange(workflow, step); rng != nil {
			s.Playback.CurrentDelayMin = rng.Min
			s.Playback.CurrentDelayMax = rng.Max
		} else {
			s.Playback.CurrentDelayMin = 0
			s.Playback.CurrentDelayMax = 0
		}
		s.Playback.CurrentDelayUsed = 0
		if s.Playback.Mode == "debug" {
			if waitForDebugStep(s) {
				return
			}
		} else if playbackWait(s, 0) {
			return
		}
		if err := app.applyWorkflowStep(s, step); err != nil {
			log.Printf("workflow step %d (%s) failed: %v", i+1, step.Type, err)
			addPlaybackEvent(s, fmt.Sprintf("Step failed: %v", err))
			return
		}
		if s.Playback.Mode == "play" {
			if delay := workflowStepDelay(workflow, step); delay > 0 {
				s.Playback.CurrentDelayUsed = delay
				log.Printf("workflow delay after step %d: %s", i+1, delay)
				addPlaybackEvent(s, fmt.Sprintf("Delay applied: %s", delay))
				if playbackWait(s, delay) {
					return
				}
			}
		}
	}
	if workflow.EndOfTaskDelay != nil {
		if delay := workflowDelay(workflow.EndOfTaskDelay); delay > 0 {
			log.Printf("workflow end delay: %s", delay)
			addPlaybackEvent(s, fmt.Sprintf("End delay: %s", delay))
			_ = playbackWait(s, delay)
		}
	}
	if s != nil {
		s.PlaybackCompletedAt = time.Now()
		addPlaybackEvent(s, "Playback complete")
	}
}

func playbackWait(s *session.Session, delay time.Duration) bool {
	if s == nil || s.Playback == nil {
		return true
	}
	deadline := time.Now().Add(delay)
	for {
		if playbackShouldStop(s) {
			return true
		}
		if s.Playback.Paused {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if delay <= 0 {
			return false
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		sleep := remaining
		if sleep > 200*time.Millisecond {
			sleep = 200 * time.Millisecond
		}
		time.Sleep(sleep)
	}
}

func waitForDebugStep(s *session.Session) bool {
	if s == nil || s.Playback == nil {
		return true
	}
	for {
		if playbackShouldStop(s) {
			return true
		}
		if s.Playback.StepRequested {
			s.Playback.StepRequested = false
			return false
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func playbackShouldStop(s *session.Session) bool {
	if s == nil || s.Playback == nil {
		return true
	}
	return !s.Playback.Active || s.Playback.StopRequested
}

func stopWorkflowPlayback(s *session.Session) {
	if s == nil || s.Playback == nil {
		return
	}
	s.Playback.StopRequested = true
	s.Playback.Active = false
	s.Playback.Paused = false
	s.Playback.PendingInput = false
	s.PlaybackCompletedAt = time.Time{}
	addPlaybackEvent(s, "Playback stopped")
}

func (app *App) applyWorkflowStep(s *session.Session, step session.WorkflowStep) error {
	if s == nil || s.Host == nil {
		return nil
	}
	if step.Coordinates != nil {
		log.Printf("workflow: step=%s row=%d col=%d", step.Type, step.Coordinates.Row, step.Coordinates.Column)
	} else {
		log.Printf("workflow: step=%s", step.Type)
	}
	switch step.Type {
	case "Connect":
		return nil
	case "Disconnect":
		return app.disconnectWorkflow(s)
	case "FillString":
		return app.applyWorkflowFill(s, step)
	case "PressEnter":
		return app.applyWorkflowKey(s, "Enter")
	default:
		if strings.HasPrefix(step.Type, "Press") {
			return app.applyWorkflowKey(s, strings.TrimPrefix(step.Type, "Press"))
		}
		return nil
	}
}

func (app *App) disconnectWorkflow(s *session.Session) error {
	if s != nil && s.Playback != nil {
		s.Playback.PendingInput = false
	}
	if s != nil && s.Host != nil {
		return s.Host.Stop()
	}
	return nil
}

func (app *App) applyWorkflowKey(s *session.Session, key string) error {
	if err := submitWorkflowPendingInput(s); err != nil {
		return err
	}
	return s.Host.SendKey(key)
}

func (app *App) applyWorkflowFill(s *session.Session, step session.WorkflowStep) error {
	if s == nil || s.Host == nil || step.Coordinates == nil {
		return nil
	}
	screen, err := waitForFormattedScreen(s, 8*time.Second)
	if err != nil {
		return err
	}
	// Workflow coordinates are 1-based, so convert to 0-based indices.
	row := step.Coordinates.Row - 1
	col := step.Coordinates.Column - 1
	if row < 0 || col < 0 {
		return nil
	}
	field := screen.GetInputFieldAt(col, row)
	if field == nil {
		return fmt.Errorf("no input field at row %d col %d", step.Coordinates.Row, step.Coordinates.Column)
	}
	field.SetValue(step.Text)
	if err := s.Host.WriteStringAt(row, col, step.Text); err != nil {
		return err
	}
	if s.Playback != nil {
		s.Playback.PendingInput = false
	}
	return nil
}

func submitWorkflowPendingInput(s *session.Session) error {
	if s == nil || s.Host == nil || s.Playback == nil || !s.Playback.PendingInput {
		return nil
	}
	if _, err := waitForFormattedScreen(s, 6*time.Second); err != nil {
		return err
	}
	if err := s.Host.SubmitScreen(); err != nil {
		return err
	}
	s.Playback.PendingInput = false
	return nil
}

func waitForFormattedScreen(s *session.Session, timeout time.Duration) (*host.Screen, error) {
	if s == nil || s.Host == nil {
		return nil, errors.New("missing host")
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := s.Host.UpdateScreen(); err != nil {
			return nil, err
		}
		screen := s.Host.GetScreen()
		if screen != nil && screen.IsFormatted {
			return screen, nil
		}
		if time.Now().After(deadline) {
			return screen, errors.New("formatted screen not ready")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func workflowStepDelay(workflow *WorkflowConfig, step session.WorkflowStep) time.Duration {
	if step.StepDelay != nil {
		return workflowDelay(step.StepDelay)
	}
	if workflow != nil && workflow.EveryStepDelay != nil {
		return workflowDelay(workflow.EveryStepDelay)
	}
	return 0
}

func workflowStepDelayRange(workflow *WorkflowConfig, step session.WorkflowStep) *session.WorkflowDelayRange {
	if step.StepDelay != nil {
		return step.StepDelay
	}
	if workflow != nil && workflow.EveryStepDelay != nil {
		return workflow.EveryStepDelay
	}
	return nil
}

func workflowDelay(delay *session.WorkflowDelayRange) time.Duration {
	if delay == nil {
		return 0
	}
	if delay.Min <= 0 && delay.Max <= 0 {
		return 0
	}
	min := delay.Min
	max := delay.Max
	if max < min {
		max = min
	}
	if max == min {
		return time.Duration(min * float64(time.Second))
	}
	span := max - min
	const precision = int64(1000000)
	n, err := rand.Int(rand.Reader, big.NewInt(precision))
	if err != nil {
		return time.Duration(min * float64(time.Second))
	}
	value := min + (span * float64(n.Int64()) / float64(precision))
	return time.Duration(value * float64(time.Second))
}

func (app *App) getSession(c *gin.Context) *session.Session {
	id, err := c.Cookie("3270Web_session")
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

	if sampleID, samplePort, ok := parseSampleAppHost(hostname); ok {
		if samplePort > 0 && !isAllowedSampleAppPort(samplePort) {
			return fmt.Errorf("invalid sample app port %d", samplePort)
		}
		execPath := resolveS3270Path(app.Config.ExecPath)
		h, err = newSampleAppHost(sampleID, samplePort, execPath, app.Config.S3270Options)
	} else if hostname == "mock" || hostname == "demo" {
		execPath := resolveS3270Path(app.Config.ExecPath)
		h, err = newSampleAppHost("app1", defaultSampleAppPort, execPath, app.Config.S3270Options)
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
	setSessionCookie(c, "3270Web_session", sess.ID)
	return nil
}

func resolveS3270Path(execPath string) string {
	if runtime.GOOS == "windows" {
		if execPath != "" {
			candidate := execPath
			if info, err := os.Stat(candidate); err == nil {
				if info.IsDir() {
					candidate = filepath.Join(candidate, s3270BinaryName())
				}
				if fileExists(candidate) {
					return candidate
				}
			}
		}

		local := filepath.Join(".", "s3270-bin", s3270BinaryName())
		if fileExists(local) {
			return local
		}

		// Embedded binary is Windows-only (s3270.exe); other platforms must use system s3270.
		if embedded, err := assets.ExtractS3270(); err == nil {
			return embedded
		}
	}

	if execPath != "" && execPath != "/usr/local/bin" {
		return filepath.Join(execPath, s3270BinaryName())
	}

	local := filepath.Join(".", "s3270-bin", s3270BinaryName())
	if fileExists(local) {
		return local
	}

	if path, err := exec.LookPath(s3270BinaryName()); err == nil {
		return path
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

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func availableSampleApps() []SampleAppOption {
	options := make([]SampleAppOption, 0, len(sampleAppConfigs))
	for _, app := range sampleAppConfigs {
		options = append(options, SampleAppOption{
			ID:       app.ID,
			Name:     app.Name,
			Hostname: sampleAppHostname(app.ID),
		})
	}
	return options
}

func sampleAppHostname(id string) string {
	return sampleAppPrefix + id
}

func parseSampleAppHost(hostname string) (string, int, bool) {
	trimmed := strings.TrimSpace(hostname)
	if !strings.HasPrefix(trimmed, sampleAppPrefix) {
		return "", 0, false
	}
	id := strings.TrimPrefix(trimmed, sampleAppPrefix)
	if id == "" {
		return "", 0, false
	}
	parts := strings.Split(id, ":")
	if len(parts) > 2 {
		return "", 0, false
	}
	if parts[0] == "" {
		return "", 0, false
	}
	if len(parts) == 2 {
		if parts[1] == "" {
			return "", 0, false
		}
		port, err := strconv.Atoi(parts[1])
		if err != nil {
			return "", 0, false
		}
		return parts[0], port, true
	}
	return parts[0], 0, true
}

func sampleAppConfig(id string) (SampleAppConfig, bool) {
	for _, cfg := range sampleAppConfigs {
		if cfg.ID == id {
			return cfg, true
		}
	}
	return SampleAppConfig{}, false
}

var allowedSampleAppPortsList = []int{3270, 3271, 3272, 3273, 3274}

var allowedSampleAppPortSet = buildAllowedSampleAppPortSet()

func buildAllowedSampleAppPortSet() map[int]struct{} {
	ports := make(map[int]struct{}, len(allowedSampleAppPortsList))
	for _, port := range allowedSampleAppPortsList {
		ports[port] = struct{}{}
	}
	return ports
}

func allowedSampleAppPorts() []int {
	return allowedSampleAppPortsList
}

func isAllowedSampleAppPort(port int) bool {
	_, ok := allowedSampleAppPortSet[port]
	return ok
}

func sampleAppPort(port int) int {
	if port <= 0 {
		return defaultSampleAppPort
	}
	return port
}

func newSampleAppHost(id string, port int, execPath string, opts config.S3270Options) (host.Host, error) {
	cfg, ok := sampleAppConfig(id)
	if !ok {
		return nil, fmt.Errorf("unknown sample app %q", id)
	}
	port = sampleAppPort(port)
	target := fmt.Sprintf("127.0.0.1:%d", port)
	args := buildS3270Args(opts, "")
	return host.NewGoSampleAppHost(cfg.ID, port, execPath, args, target)
}

func buildS3270Args(opts config.S3270Options, hostname string) []string {
	args := []string{"-model", opts.Model}
	if opts.Charset != "" && opts.Charset != "bracket" {
		args = append(args, "-charset", opts.Charset)
	}
	if opts.Additional != "" {
		args = append(args, strings.Fields(opts.Additional)...)
	}
	if strings.TrimSpace(hostname) != "" {
		args = append(args, hostname)
	}
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
		writeRule(&sb, ".renderer-form", cs.PNBg, cs.PNFg)
		writeRule(&sb, "pre, pre input, textarea", cs.PNBg, cs.PNFg)
		writeRule(&sb, ".screen-container", cs.PNBg, cs.PNFg)
		writeRule(&sb, ".color-intensified", cs.PIBg, cs.PIFg)
		writeRule(&sb, ".color-hidden", cs.PHBg, cs.PHFg)
		writeRule(&sb, ".color-input", cs.UNBg, cs.UNFg)
		writeRule(&sb, ".color-input-intensified", cs.UIBg, cs.UIFg)
		writeRule(&sb, ".color-input-hidden", cs.UHBg, cs.UHFg)
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
