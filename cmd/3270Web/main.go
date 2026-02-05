package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	webassets "github.com/jnnngs/3270Web"
	"github.com/jnnngs/3270Web/internal/config"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/render"
	"github.com/jnnngs/3270Web/internal/session"
)

type App struct {
	SessionManager *session.Manager
	Renderer       render.Renderer
	Config         *config.Config
	themeCache     map[string]string
	themeCacheMu   sync.RWMutex
	logFilePath    string
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
	envPath := filepath.Join(baseDir, ".env")
	if err := config.EnsureDotEnv(envPath); err != nil {
		log.Printf("Warning: could not ensure .env file: %v", err)
	}
	if err := config.LoadDotEnv(envPath); err != nil {
		log.Printf("Warning: could not load .env file: %v", err)
	}
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
		themeCache:     make(map[string]string),
		logFilePath:    filepath.Join(baseDir, "3270Web.log"),
	}

	r := gin.Default()
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Printf("Warning: could not set trusted proxies: %v", err)
	}
	r.Use(SecurityHeadersMiddleware())
	r.Use(OriginRefererCheckMiddleware())
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
	r.GET("/screen/content", app.ScreenContentHandler)
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
	r.GET("/workflow/status", app.WorkflowStatusHandler)

	// Logging handlers
	r.GET("/logs", app.LogsHandler)
	r.POST("/logs/toggle", app.LogsToggleHandler)
	r.POST("/logs/clear", app.LogsClearHandler)
	r.GET("/logs/download", app.LogsDownloadHandler)

	// Disconnect handler
	r.POST("/disconnect", app.DisconnectHandler)

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

func recordingFileName(s *session.Session) string {
	if s == nil {
		return ""
	}
	s.Lock()
	defer s.Unlock()
	if s.Recording == nil || s.Recording.FilePath == "" {
		return ""
	}
	return filepath.Base(s.Recording.FilePath)
}

type sessionSnapshot struct {
	Prefs                 session.Preferences
	RecordingActive       bool
	RecordingFile         string
	PlaybackActive        bool
	PlaybackPaused        bool
	PlaybackCompleted     bool
	PlaybackMode          string
	PlaybackStep          int
	PlaybackStepType      string
	PlaybackStepTotal     int
	PlaybackDelayRange    string
	PlaybackDelayApplied  string
	PlaybackEvents        []session.WorkflowEvent
	LoadedWorkflow        bool
	LoadedWorkflowName    string
	LoadedWorkflowPreview string
	LoadedWorkflowSize    int
	TargetHost            string
	TargetPort            int
}

func (app *App) snapshotSession(s *session.Session) sessionSnapshot {
	var snap sessionSnapshot
	if s == nil {
		return snap
	}
	s.Lock()
	defer s.Unlock()

	app.ensurePrefs(s)
	snap.Prefs = s.Prefs
	snap.TargetHost = s.TargetHost
	snap.TargetPort = s.TargetPort
	if s.Recording != nil {
		snap.RecordingActive = s.Recording.Active
		if s.Recording.FilePath != "" {
			snap.RecordingFile = filepath.Base(s.Recording.FilePath)
		}
	}
	snap.PlaybackCompleted = !s.PlaybackCompletedAt.IsZero()
	if s.Playback != nil {
		snap.PlaybackActive = s.Playback.Active
		snap.PlaybackPaused = s.Playback.Active && s.Playback.Paused
		snap.PlaybackMode = s.Playback.Mode
		snap.PlaybackStep = s.Playback.CurrentStep
		snap.PlaybackStepType = s.Playback.CurrentStepType
		snap.PlaybackStepTotal = s.Playback.TotalSteps
		snap.PlaybackDelayRange = formatDelayRange(s.Playback.CurrentDelayMin, s.Playback.CurrentDelayMax)
		snap.PlaybackDelayApplied = formatDelayApplied(s.Playback.CurrentDelayUsed.Seconds())
	} else {
		snap.PlaybackStep = s.LastPlaybackStep
		snap.PlaybackStepType = s.LastPlaybackStepType
		snap.PlaybackStepTotal = s.LastPlaybackStepTotal
		snap.PlaybackDelayRange = s.LastPlaybackDelayRange
		snap.PlaybackDelayApplied = s.LastPlaybackDelayApplied
	}
	if len(s.PlaybackEvents) > 0 {
		snap.PlaybackEvents = append([]session.WorkflowEvent(nil), s.PlaybackEvents...)
	}
	if s.LoadedWorkflow != nil {
		snap.LoadedWorkflow = true
		snap.LoadedWorkflowName = s.LoadedWorkflow.Name
		snap.LoadedWorkflowPreview = s.LoadedWorkflow.Preview
		snap.LoadedWorkflowSize = len(s.LoadedWorkflow.Payload)
	}
	return snap
}

func withSessionLock(s *session.Session, fn func()) {
	if s == nil || fn == nil {
		return
	}
	s.Lock()
	defer s.Unlock()
	fn()
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

	if err := s.Host.UpdateScreen(); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("Update screen failed: %v", err)})
		return
	}

	screen := s.Host.GetScreen()
	if rows, cols, ok := app.modelDimensions(); ok {
		screen = limitScreenForDisplay(screen, rows, cols)
	}
	rendered := app.Renderer.Render(screen, "/submit", s.ID)
	snap := app.snapshotSession(s)
	themeCSS := app.buildThemeCSS(snap.Prefs)

	sampleAppName := ""
	sampleAppPort := 0
	if id, _, ok := parseSampleAppHost(snap.TargetHost); ok {
		if cfg, ok := sampleAppConfig(id); ok {
			sampleAppName = cfg.Name
			sampleAppPort = snap.TargetPort
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
		"SelectedColorScheme":   snap.Prefs.ColorScheme,
		"SelectedFont":          snap.Prefs.FontName,
		"UseKeypad":             snap.Prefs.UseKeypad,
		"ThemeCSS":              template.CSS(themeCSS),
		"RecordingActive":       snap.RecordingActive,
		"RecordingFile":         snap.RecordingFile,
		"PlaybackActive":        snap.PlaybackActive,
		"PlaybackPaused":        snap.PlaybackPaused,
		"PlaybackCompleted":     snap.PlaybackCompleted,
		"PlaybackMode":          snap.PlaybackMode,
		"PlaybackStep":          snap.PlaybackStep,
		"PlaybackStepType":      snap.PlaybackStepType,
		"PlaybackStepTotal":     snap.PlaybackStepTotal,
		"PlaybackDelayRange":    snap.PlaybackDelayRange,
		"PlaybackDelayApplied":  snap.PlaybackDelayApplied,
		"PlaybackEvents":        snap.PlaybackEvents,
		"LoadedWorkflow":        snap.LoadedWorkflow,
		"LoadedWorkflowName":    snap.LoadedWorkflowName,
		"LoadedWorkflowPreview": snap.LoadedWorkflowPreview,
		"LoadedWorkflowSize":    snap.LoadedWorkflowSize,
		"SampleAppName":         sampleAppName,
		"SampleAppPort":         sampleAppPort,
	})
}

func (app *App) ScreenContentHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	if err := s.Host.UpdateScreen(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Update screen failed: %v", err)})
		return
	}
	screen := s.Host.GetScreen()
	if rows, cols, ok := app.modelDimensions(); ok {
		screen = limitScreenForDisplay(screen, rows, cols)
	}
	rendered := app.Renderer.Render(screen, "/submit", s.ID)
	c.JSON(http.StatusOK, gin.H{"html": rendered})
}

func (app *App) modelDimensions() (int, int, bool) {
	model := ""
	if app != nil && app.Config != nil {
		model = strings.TrimSpace(app.Config.S3270Options.Model)
	}
	if overrides, err := config.S3270EnvOverridesFromEnv(); err == nil && overrides.HasModel {
		model = strings.TrimSpace(overrides.Model)
	}
	if model == "" {
		return 0, 0, false
	}
	return host.ModelDimensions(model)
}

func limitScreenForDisplay(screen *host.Screen, maxRows, maxCols int) *host.Screen {
	if screen == nil || maxRows <= 0 || maxCols <= 0 {
		return screen
	}
	if screen.Height <= maxRows && screen.Width <= maxCols {
		return screen
	}

	limited := *screen
	if limited.Height > maxRows {
		limited.Height = maxRows
	}
	if limited.Width > maxCols {
		limited.Width = maxCols
	}
	limited.Buffer = nil
	limited.Fields = nil

	limited.Buffer = make([][]rune, limited.Height)
	for y := 0; y < limited.Height; y++ {
		row := make([]rune, limited.Width)
		if y < len(screen.Buffer) {
			src := screen.Buffer[y]
			for x := 0; x < limited.Width && x < len(src); x++ {
				row[x] = src[x]
			}
		}
		limited.Buffer[y] = row
	}

	for _, f := range screen.Fields {
		if f == nil {
			continue
		}
		if !fieldWithinBounds(f, limited.Height, limited.Width) {
			continue
		}
		nf := *f
		nf.Screen = &limited
		nf.Value = ""
		nf.Changed = false
		limited.Fields = append(limited.Fields, &nf)
	}

	return &limited
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
		recordFieldUpdates(s)

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
	recordActionKey(s, actionKey)

	if err := s.Host.SendKey(actionKey); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("SendKey failed: %v", err)})
		return
	}

	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) DisconnectHandler(c *gin.Context) {
	if s := app.getSession(c); s != nil {
		cleanupRecordingFile(s)
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
	cleanupRecordingFile(s)
	blocked := false
	withSessionLock(s, func() {
		if s.Recording != nil && s.Recording.Active {
			blocked = true
			return
		}
		if s.Playback != nil && s.Playback.Active {
			blocked = true
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
	})
	if blocked {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) RecordStopHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	var workflow *WorkflowConfig
	active := false
	withSessionLock(s, func() {
		if s.Recording == nil || !s.Recording.Active {
			return
		}
		active = true
		s.Recording.Steps = append(s.Recording.Steps, session.WorkflowStep{Type: "Disconnect"})
		workflow = buildWorkflowConfig(s)
	})
	if !active {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	path, err := writeWorkflowTempFile(workflow)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("Record stop failed: %v", err)})
		return
	}
	withSessionLock(s, func() {
		if s.Recording != nil {
			s.Recording.Active = false
			s.Recording.FilePath = path
		}
	})
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) RecordDownloadHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	var path string
	withSessionLock(s, func() {
		if s.Recording != nil && s.Recording.FilePath != "" {
			path = s.Recording.FilePath
		}
	})
	if path == "" {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	name := filepath.Base(path)
	c.FileAttachment(path, name)
	cleanupWorkflowFile(path)
	withSessionLock(s, func() {
		if s.Recording != nil && s.Recording.FilePath == path {
			s.Recording.FilePath = ""
		}
	})
}

func (app *App) LoadWorkflowHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	playing := false
	withSessionLock(s, func() {
		playing = s.Playback != nil && s.Playback.Active
	})
	if playing {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	upload, err := loadWorkflowUpload(c)
	if err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"Error": fmt.Sprintf("Load workflow failed: %v", err)})
		return
	}
	preview := prettyWorkflowPayload(upload.Payload)
	withSessionLock(s, func() {
		s.LoadedWorkflow = &session.LoadedWorkflow{
			Name:     upload.Name,
			Payload:  upload.Payload,
			Preview:  preview,
			LoadedAt: time.Now(),
		}
	})
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) PlayWorkflowHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	playing := false
	withSessionLock(s, func() {
		playing = s.Playback != nil && s.Playback.Active
	})
	if playing {
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
	withSessionLock(s, func() {
		resetPlaybackSummary(s)
		s.PlaybackCompletedAt = time.Time{}
		s.Playback = &session.WorkflowPlayback{StartedAt: time.Now(), Mode: "play", TotalSteps: len(workflow.Steps)}
		s.PlaybackEvents = nil
	})
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
	playing := false
	withSessionLock(s, func() {
		playing = s.Playback != nil && s.Playback.Active
	})
	if playing {
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
	withSessionLock(s, func() {
		resetPlaybackSummary(s)
		s.PlaybackCompletedAt = time.Time{}
		s.Playback = &session.WorkflowPlayback{StartedAt: time.Now(), Mode: "debug", TotalSteps: len(workflow.Steps), Paused: true}
		s.PlaybackEvents = nil
	})
	addPlaybackEvent(s, "Playback started (Debug mode)")
	go app.playWorkflow(s, workflow)
	c.Redirect(http.StatusFound, "/screen")
}

func resetPlaybackSummary(s *session.Session) {
	if s == nil {
		return
	}
	s.LastPlaybackStep = 0
	s.LastPlaybackStepType = ""
	s.LastPlaybackStepTotal = 0
	s.LastPlaybackDelayRange = ""
	s.LastPlaybackDelayApplied = ""
}

func clearWorkflowStatus(s *session.Session) {
	if s == nil {
		return
	}
	withSessionLock(s, func() {
		resetPlaybackSummary(s)
		s.PlaybackEvents = nil
		s.PlaybackCompletedAt = time.Time{}
	})
}

func (app *App) StepWorkflowHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	canStep := false
	withSessionLock(s, func() {
		if s.Playback != nil && s.Playback.Active && s.Playback.Mode == "debug" {
			s.Playback.StepRequested = true
			canStep = true
		}
	})
	if !canStep {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	addPlaybackEvent(s, "Step requested")
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) PauseWorkflowHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	var paused *bool
	withSessionLock(s, func() {
		if s.Playback == nil || !s.Playback.Active {
			return
		}
		s.Playback.Paused = !s.Playback.Paused
		val := s.Playback.Paused
		paused = &val
	})
	if paused == nil {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	if *paused {
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
	withSessionLock(s, func() {
		s.LoadedWorkflow = nil
		s.PlaybackCompletedAt = time.Time{}
	})
	clearWorkflowStatus(s)
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) WorkflowStatusHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	eventList := playbackEvents(s)
	events := make([]gin.H, 0, len(eventList))
	for _, event := range eventList {
		events = append(events, gin.H{
			"time":    event.Time.Format("15:04:05"),
			"message": event.Message,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"playbackActive":       playbackActive(s),
		"playbackPaused":       playbackPaused(s),
		"playbackCompleted":    playbackCompleted(s),
		"playbackMode":         playbackMode(s),
		"playbackStep":         playbackStepIndex(s),
		"playbackStepTotal":    playbackStepTotal(s),
		"playbackStepType":     playbackStepType(s),
		"playbackStepLabel":    playbackStepLabel(s),
		"playbackDelayRange":   playbackDelayRangeLabel(s),
		"playbackDelayApplied": playbackDelayAppliedLabel(s),
		"playbackEvents":       events,
	})
}

func (app *App) PrefsHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}
	withSessionLock(s, func() {
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
	})

	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) LogsHandler(c *gin.Context) {
	if os.Getenv("ALLOW_LOG_ACCESS") != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Log access is disabled by administrator"})
		return
	}

	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	content, err := os.ReadFile(app.logFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusOK, gin.H{
				"content": "",
				"enabled": s.Prefs.VerboseLogging,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read log file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"content": string(content),
		"enabled": s.Prefs.VerboseLogging,
	})
}

func (app *App) LogsToggleHandler(c *gin.Context) {
	if os.Getenv("ALLOW_LOG_ACCESS") != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Log access is disabled by administrator"})
		return
	}

	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	enabled := c.PostForm("enabled") == "true"
	withSessionLock(s, func() {
		s.Prefs.VerboseLogging = enabled
		if h, ok := s.Host.(interface{ SetVerboseLogging(bool) }); ok {
			h.SetVerboseLogging(enabled)
		}
	})

	log.Printf("Verbose logging %s", map[bool]string{true: "enabled", false: "disabled"}[enabled])
	c.JSON(http.StatusOK, gin.H{"enabled": enabled})
}

func (app *App) LogsClearHandler(c *gin.Context) {
	if os.Getenv("ALLOW_LOG_ACCESS") != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Log access is disabled by administrator"})
		return
	}

	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	// Clear the log file
	err := os.WriteFile(app.logFilePath, []byte(""), 0644)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear log file"})
		return
	}

	log.Printf("Log file cleared")
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (app *App) LogsDownloadHandler(c *gin.Context) {
	if os.Getenv("ALLOW_LOG_ACCESS") != "true" {
		c.String(http.StatusForbidden, "Log access is disabled by administrator")
		return
	}

	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}

	content, err := os.ReadFile(app.logFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			content = []byte("")
		} else {
			c.String(http.StatusInternalServerError, "Failed to read log file")
			return
		}
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("3270Web-%s.log", timestamp)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Data(http.StatusOK, "text/plain", content)
}

func (app *App) updateFields(c *gin.Context, s *session.Session) {
	screen := s.Host.GetScreen()
	maxRows, maxCols, hasLimit := app.modelDimensions()
	for _, f := range screen.Fields {
		if !f.IsProtected() {
			if hasLimit && !fieldWithinBounds(f, maxRows, maxCols) {
				continue
			}
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

func fieldWithinBounds(f *host.Field, maxRows, maxCols int) bool {
	if f == nil || maxRows <= 0 || maxCols <= 0 {
		return true
	}
	if f.StartY < 0 || f.StartX < 0 {
		return false
	}
	if f.StartY >= maxRows || f.StartX >= maxCols {
		return false
	}
	if f.EndY >= maxRows || f.EndX >= maxCols {
		return false
	}
	return true
}

func recordFieldUpdates(s *session.Session) {
	if s == nil {
		return
	}
	screen := s.Host.GetScreen()
	if screen == nil {
		return
	}
	withSessionLock(s, func() {
		if s.Recording == nil || !s.Recording.Active {
			return
		}
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
	})
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
	trimmed := strings.TrimSpace(key)
	if trimmed == "" || strings.EqualFold(trimmed, "enter") {
		withSessionLock(s, func() {
			if s.Recording == nil || !s.Recording.Active {
				return
			}
			s.Recording.Steps = append(s.Recording.Steps, session.WorkflowStep{Type: "PressEnter"})
		})
		return
	}
	if step := workflowStepForKey(key); step != nil {
		withSessionLock(s, func() {
			if s.Recording == nil || !s.Recording.Active {
				return
			}
			s.Recording.Steps = append(s.Recording.Steps, *step)
		})
	}
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

func writeWorkflowTempFile(workflow *WorkflowConfig) (string, error) {
	if workflow == nil {
		return "", errors.New("workflow is empty")
	}
	file, err := os.CreateTemp("", "3270Web-workflow-*.json")
	if err != nil {
		return "", err
	}
	path := file.Name()
	defer file.Close()
	data, err := json.MarshalIndent(workflow, "", "  ")
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func cleanupRecordingFile(s *session.Session) {
	if s == nil {
		return
	}
	var path string
	withSessionLock(s, func() {
		if s.Recording != nil && s.Recording.FilePath != "" {
			path = s.Recording.FilePath
			s.Recording.FilePath = ""
		}
	})
	if path == "" {
		return
	}
	cleanupWorkflowFile(path)
}

func cleanupWorkflowFile(path string) {
	if !isTempWorkflowPath(path) {
		return
	}
	_ = os.Remove(path)
}

func isTempWorkflowPath(path string) bool {
	if path == "" {
		return false
	}
	tempDir := filepath.Clean(os.TempDir())
	cleaned := filepath.Clean(path)
	prefix := tempDir + string(os.PathSeparator)
	return cleaned == tempDir || strings.HasPrefix(cleaned, prefix)
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

const maxWorkflowUploadBytes = 2 * 1024 * 1024

func loadWorkflowUpload(c *gin.Context) (*workflowUpload, error) {
	file, name, size, err := workflowFileFromRequest(c)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	limit := int64(maxWorkflowUploadBytes)
	if size > limit {
		return nil, fmt.Errorf("workflow file exceeds %d bytes", maxWorkflowUploadBytes)
	}
	limited := io.LimitReader(file, limit+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("workflow file exceeds %d bytes", maxWorkflowUploadBytes)
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

func workflowFileFromRequest(c *gin.Context) (io.ReadCloser, string, int64, error) {
	file, err := c.FormFile("workflow")
	if err == nil {
		reader, err := file.Open()
		return reader, file.Filename, file.Size, err
	}
	if !errors.Is(err, http.ErrMissingFile) {
		return nil, "", 0, err
	}
	return nil, "", 0, errors.New("workflow file is required")
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
	var payload []byte
	withSessionLock(s, func() {
		if s.LoadedWorkflow != nil {
			payload = append([]byte(nil), s.LoadedWorkflow.Payload...)
		}
	})
	if len(payload) > 0 {
		return parseWorkflowPayload(payload)
	}
	upload, err := loadWorkflowUpload(c)
	if err != nil {
		return nil, err
	}
	preview := prettyWorkflowPayload(upload.Payload)
	if s != nil {
		withSessionLock(s, func() {
			s.LoadedWorkflow = &session.LoadedWorkflow{
				Name:     upload.Name,
				Payload:  upload.Payload,
				Preview:  preview,
				LoadedAt: time.Now(),
			}
		})
	}
	return upload.Config, nil
}

func loadedWorkflowName(s *session.Session) string {
	if s == nil {
		return ""
	}
	s.Lock()
	defer s.Unlock()
	if s.LoadedWorkflow == nil {
		return ""
	}
	return s.LoadedWorkflow.Name
}

func loadedWorkflowPreview(s *session.Session) string {
	if s == nil {
		return ""
	}
	s.Lock()
	defer s.Unlock()
	if s.LoadedWorkflow == nil {
		return ""
	}
	return s.LoadedWorkflow.Preview
}

func loadedWorkflowSize(s *session.Session) int {
	if s == nil {
		return 0
	}
	s.Lock()
	defer s.Unlock()
	if s.LoadedWorkflow == nil {
		return 0
	}
	return len(s.LoadedWorkflow.Payload)
}

func playbackMode(s *session.Session) string {
	if s == nil {
		return ""
	}
	s.Lock()
	defer s.Unlock()
	if s.Playback == nil {
		return ""
	}
	return s.Playback.Mode
}

func playbackStepIndex(s *session.Session) int {
	if s == nil {
		return 0
	}
	s.Lock()
	defer s.Unlock()
	if s.Playback != nil {
		return s.Playback.CurrentStep
	}
	return s.LastPlaybackStep
}

func playbackStepType(s *session.Session) string {
	if s == nil {
		return ""
	}
	s.Lock()
	defer s.Unlock()
	if s.Playback != nil {
		return s.Playback.CurrentStepType
	}
	return s.LastPlaybackStepType
}

func playbackStepTotal(s *session.Session) int {
	if s == nil {
		return 0
	}
	s.Lock()
	defer s.Unlock()
	if s.Playback != nil {
		return s.Playback.TotalSteps
	}
	return s.LastPlaybackStepTotal
}

func playbackStepLabel(s *session.Session) string {
	step := playbackStepIndex(s)
	if step <= 0 {
		return ""
	}
	label := fmt.Sprintf("Step %d", step)
	if total := playbackStepTotal(s); total > 0 {
		label = fmt.Sprintf("%s/%d", label, total)
	}
	if t := playbackStepType(s); t != "" {
		label = fmt.Sprintf("%s: %s", label, t)
	}
	return label
}

func playbackDelayRangeLabel(s *session.Session) string {
	if s == nil {
		return ""
	}
	s.Lock()
	defer s.Unlock()
	if s.Playback != nil {
		return formatDelayRange(s.Playback.CurrentDelayMin, s.Playback.CurrentDelayMax)
	}
	return s.LastPlaybackDelayRange
}

func playbackDelayAppliedLabel(s *session.Session) string {
	if s == nil {
		return ""
	}
	s.Lock()
	defer s.Unlock()
	if s.Playback != nil {
		return formatDelayApplied(s.Playback.CurrentDelayUsed.Seconds())
	}
	return s.LastPlaybackDelayApplied
}

func formatDelayRange(min, max float64) string {
	if min <= 0 && max <= 0 {
		return ""
	}
	if max < min {
		max = min
	}
	if max == min {
		return fmt.Sprintf("%.2fs", min)
	}
	return fmt.Sprintf("%.2fs–%.2fs", min, max)
}

func formatDelayApplied(seconds float64) string {
	if seconds <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2fs", seconds)
}

func playbackActive(s *session.Session) bool {
	if s == nil {
		return false
	}
	s.Lock()
	defer s.Unlock()
	return s.Playback != nil && s.Playback.Active
}

func playbackPaused(s *session.Session) bool {
	if s == nil {
		return false
	}
	s.Lock()
	defer s.Unlock()
	return s.Playback != nil && s.Playback.Active && s.Playback.Paused
}

func playbackCompleted(s *session.Session) bool {
	if s == nil {
		return false
	}
	s.Lock()
	defer s.Unlock()
	return !s.PlaybackCompletedAt.IsZero()
}

func playbackEvents(s *session.Session) []session.WorkflowEvent {
	if s == nil {
		return nil
	}
	s.Lock()
	defer s.Unlock()
	if len(s.PlaybackEvents) == 0 {
		return nil
	}
	return append([]session.WorkflowEvent(nil), s.PlaybackEvents...)
}

func workflowTargetHost(s *session.Session, workflow *WorkflowConfig) (string, error) {
	if workflow != nil && workflow.Host != "" {
		host := workflow.Host
		if workflow.Port > 0 {
			host = fmt.Sprintf("%s:%d", host, workflow.Port)
		}
		return host, nil
	}
	if s != nil {
		var host string
		var port int
		withSessionLock(s, func() {
			host = s.TargetHost
			port = s.TargetPort
		})
		if host != "" {
			if port > 0 {
				host = fmt.Sprintf("%s:%d", host, port)
			}
			return host, nil
		}
	}
	return "", errors.New("workflow host not provided")
}

func (app *App) resetSessionHost(s *session.Session, hostname string) error {
	if s == nil {
		return errors.New("missing session")
	}
	var existing host.Host
	withSessionLock(s, func() {
		existing = s.Host
	})
	if existing != nil {
		_ = existing.Stop()
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
	withSessionLock(s, func() {
		s.Host = h
		s.TargetHost = hostName
		s.TargetPort = port
		// Apply verbose logging preference
		if logger, ok := h.(interface{ SetVerboseLogging(bool) }); ok {
			logger.SetVerboseLogging(s.Prefs.VerboseLogging)
		}
	})
	return nil
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
	if !isValidHostname(hostname) {
		return fmt.Errorf("invalid hostname format: %q", hostname)
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

	sess := app.SessionManager.CreateSession(h)
	sess.TargetHost, sess.TargetPort = parseHostPort(hostname)
	app.applyDefaultPrefs(sess)
	setSessionCookie(c, "3270Web_session", sess.ID)
	return nil
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
	if s == nil {
		return
	}
	withSessionLock(s, func() {
		app.ensurePrefs(s)
	})
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
	// ⚡ Bolt: Memoize theme CSS generation to improve performance on screen updates
	key := p.ColorScheme + "|" + p.FontName
	app.themeCacheMu.RLock()
	if css, ok := app.themeCache[key]; ok {
		app.themeCacheMu.RUnlock()
		return css
	}
	app.themeCacheMu.RUnlock()

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

	css := sb.String()
	app.themeCacheMu.Lock()
	app.themeCache[key] = css
	app.themeCacheMu.Unlock()
	return css
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

func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' https://unpkg.com; style-src 'self' 'unsafe-inline' https://unpkg.com; img-src 'self' data:; font-src 'self' data:; connect-src 'self' ws: wss:;")
		c.Header("Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()")
		c.Next()
	}
}
