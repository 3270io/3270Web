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
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	webassets "github.com/jnnngs/3270Web"
	"github.com/jnnngs/3270Web/internal/aiprovider"
	"github.com/jnnngs/3270Web/internal/apitoken"
	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/authsession"
	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/config"
	"github.com/jnnngs/3270Web/internal/copilot"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/render"
	"github.com/jnnngs/3270Web/internal/session"
	"github.com/jnnngs/3270Web/internal/task"
	"github.com/jnnngs/3270Web/internal/users"
)

type App struct {
	SessionManager *session.Manager
	Renderer       render.Renderer
	Config         *config.Config
	// authMode selects how callers are identified. Validated at startup, so
	// handlers can treat it as a known value.
	authMode authz.Mode
	// users holds local accounts; authSessions holds live logins. Both are
	// only consulted when authMode requires authentication.
	users        *users.Store
	usersOnce    sync.Once
	usersPath    string
	authSessions *authsession.Store
	// apiTokens holds the credentials automated clients present. Consulted
	// only where users are separated; see authenticateAPIToken.
	apiTokens     *apitoken.Store
	apiTokensOnce sync.Once
	// audit is the security trail, kept apart from the debug log; see
	// internal/audit.
	audit     *audit.Recorder
	auditOnce sync.Once
	// rates throttles the routes where one caller can consume something the
	// whole instance shares; see ratelimit.go.
	rates        *rateLimiter
	rateOnce     sync.Once
	loginLimiter *loginLimiter
	// authBindIP is "auto", "true" or "false"; see App.bindSessionIP.
	authBindIP          string
	authIdleTimeout     time.Duration
	authAbsoluteTimeout time.Duration
	// setup tracks whether the instance still needs its first administrator.
	setup setupState
	// ownerStores caches each account's own file stores.
	ownerStores *ownerStores
	storesOnce  sync.Once

	themeCache   map[string]string
	themeCacheMu sync.RWMutex
	logFilePath  string
	envPath      string
	// baseDir is where state is written; installDir is where the program and
	// its web assets live. The same directory unless DATA_DIR says otherwise.
	baseDir      string
	installDir   string
	shutdown     func()
	chaosEngines *chaosEngineStore
	// chaosSync counts the running chaos status goroutines so shutdown — and
	// a test's cleanup — can wait for them rather than racing whatever they
	// are still writing to. See startChaosSync.
	chaosSync    sync.WaitGroup
	chaosHintsMu sync.Mutex
	profiles     *profileCache
	// connProfiles holds named CONNECTION profiles (host, TLS, LU, model).
	// Distinct from `profiles` above, which caches host COMPATIBILITY
	// profiler results — unrelated feature, unfortunately similar word.
	connProfiles     *connectionProfileStore
	connProfilesOnce sync.Once
	copilotAuthStore *copilot.Store
	// tasks holds the Guided Business Task catalogue; taskRunStore holds the
	// in-flight run per session. See tasks.go.
	tasks         *task.Store
	taskStoreOnce sync.Once
	taskRunStore  *taskRunStore
	taskRunsOnce  sync.Once
	// extensionTaskList holds tasks contributed by installed extensions,
	// which are read-only and live outside the store's file.
	extensionTaskList  []task.Task
	extensionTasksOnce sync.Once
	// aiConfigStore holds each browser's AI provider choice and credentials
	// (Claude, OpenAI, Ollama, ...), keyed by the same identity cookie as
	// copilotAuthStore.
	aiConfigStore *aiprovider.ConfigStore
	// snapshotStore holds the named point-in-time screen copies a session has
	// taken; screenTraceStore holds its running or finished screen capture.
	// See snapshots.go and screen_trace.go.
	snapshotStore     *snapshotStore
	snapshotStoreOnce sync.Once
	screenTraceStore  *screenTraceStore
	screenTraceOnce   sync.Once
	// catalogueFields holds the skills/instructions/extensions catalogue and
	// the per-conversation load trackers. See skills.go.
	catalogueFields
}

// connectionProfiles lazily opens the connection-profile store, which lives
// beside the themes directory under the app's base directory.
func (app *App) connectionProfiles() *connectionProfileStore {
	app.connProfilesOnce.Do(func() {
		base := strings.TrimSpace(app.baseDir)
		if base == "" {
			base = resolveStateDir()
		}
		app.connProfiles = newConnectionProfileStore(base)
	})
	return app.connProfiles
}

type WorkflowConfig struct {
	Host             string                      `json:"Host"`
	Port             int                         `json:"Port"`
	Name             string                      `json:"Name,omitempty"`
	Description      string                      `json:"Description,omitempty"`
	BusinessFunction string                      `json:"BusinessFunction,omitempty"`
	Parameters       []session.WorkflowParameter `json:"Parameters,omitempty"`
	EveryStepDelay   *session.WorkflowDelayRange `json:"EveryStepDelay,omitempty"`
	OutputFilePath   string                      `json:"OutputFilePath,omitempty"`
	RampUpBatchSize  int                         `json:"RampUpBatchSize,omitempty"`
	RampUpDelay      float64                     `json:"RampUpDelay,omitempty"`
	EndOfTaskDelay   *session.WorkflowDelayRange `json:"EndOfTaskDelay,omitempty"`
	Steps            []session.WorkflowStep      `json:"Steps"`
}

type workflowJSONPayload struct {
	Name    string `json:"name"`
	Content string `json:"content"`
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
	{ID: "app1", Name: "Sample App 1 - Name Entry & Validation"},
	{ID: "app2", Name: "Sample App 2 - RSS Newsreader"},
	{ID: "app3", Name: "Sample App 3 - BMS Field Attribute Test Matrix"},
}

const defaultSampleAppPort = 3270

// appVersion can be overridden at build time with:
// go build -ldflags "-X main.appVersion=v1.2.3"
var appVersion = "0.3.2"

// newApp constructs the application with its program files and its state in
// the same directory, which is what a desktop install and `go run` both have.
func newApp(baseDir string) *App {
	return newAppAt(baseDir, baseDir)
}

// newAppAt separates where the program lives from where its state goes.
//
// They are the same directory almost everywhere, and deliberately not in a
// container: the image holds the binary and web/, and a volume holds the
// accounts, tokens, audit trail and everyone's saved work. Mounting a volume
// over the program's own directory would hide the program, so the two have to
// be nameable separately.
//
// It is separate from main() so that other entry points into the same binary —
// the `mcp` subcommand — boot an identical App without duplicating the .env,
// config-fallback and directory conventions.
func newAppAt(installDir, dataDir string) *App {
	baseDir := dataDir
	envPath := filepath.Join(baseDir, ".env")
	if err := config.EnsureDotEnv(envPath); err != nil {
		log.Printf("Warning: could not ensure .env file: %v", err)
	}
	if err := config.LoadDotEnv(envPath); err != nil {
		log.Printf("Warning: could not load .env file: %v", err)
	}
	// Looked for beside the state first, so an operator can supply one on the
	// volume, then beside the program, where the shipped copy lives.
	configPath := filepath.Join(baseDir, "webapp", "WEB-INF", "3270Web-config.xml")
	if !fileExists(configPath) {
		if beside := filepath.Join(installDir, "webapp", "WEB-INF", "3270Web-config.xml"); fileExists(beside) {
			configPath = beside
		}
	}
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

	return &App{
		SessionManager: session.NewManager(),
		Renderer:       render.NewHtmlRenderer(),
		Config:         cfg,
		// Overwritten by buildRouter once AUTH_MODE has been validated. The
		// default matches the historical behaviour: a single local operator.
		authMode:            authz.ModeNone,
		usersPath:           filepath.Join(baseDir, "users.json"),
		loginLimiter:        newLoginLimiter(),
		authIdleTimeout:     authsession.DefaultIdleTimeout,
		authAbsoluteTimeout: authsession.DefaultAbsoluteTimeout,
		themeCache:          make(map[string]string),
		logFilePath:         filepath.Join(baseDir, "3270Web.log"),
		envPath:             envPath,
		baseDir:             baseDir,
		installDir:          installDir,
		chaosEngines:        newChaosEngineStore(),
		profiles:            newProfileCache(),
	}
}

// buildRouter wires middleware, templates, static assets and every route
// onto a fresh engine. Returning an error rather than calling showFatalError
// keeps it usable from the `mcp` subcommand, where a modal dialog would be
// the wrong way to fail.
func buildRouter(app *App) (*gin.Engine, error) {
	// Validate before wiring anything. An AUTH_MODE this build cannot honour
	// must stop startup rather than quietly fall back to no authentication —
	// an operator who asked for a mode and got silence would reasonably
	// believe the instance was protected.
	if err := app.configureAuth(); err != nil {
		return nil, err
	}

	r := gin.Default()
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Printf("Warning: could not set trusted proxies: %v", err)
	}
	r.Use(MaxBodySizeMiddleware(maxRequestBodyBytes))
	r.Use(SecurityHeadersMiddleware())
	r.Use(OriginRefererCheckMiddleware())
	// Resolve the caller before any handler runs, so every request has exactly
	// one answer to "who is this", then refuse anonymous callers when the mode
	// requires a login. RequireLogin is a no-op under AUTH_MODE=none.
	r.Use(app.Authenticate())
	r.Use(app.RequireSetup())
	r.Use(app.RequireLogin())
	// After authentication, so a limit applies to the account rather than to
	// whoever happens to share an address with them.
	r.Use(app.RateLimit())
	templatesGlob, tmplErr := resolveTemplatesGlob(app.assetDir())
	if tmplErr == nil {
		r.LoadHTMLGlob(templatesGlob)
	} else {
		log.Printf("Warning: %v", tmplErr)
		tmplFS, err := fs.Sub(webassets.FS, "web/templates")
		if err != nil {
			return nil, fmt.Errorf("load embedded templates: %w", err)
		}
		r.LoadHTMLFS(http.FS(tmplFS), "*")
	}

	staticDir, staticErr := resolveStaticDir(app.assetDir())
	if staticErr == nil {
		r.Static("/static", staticDir)
	} else {
		log.Printf("Warning: %v", staticErr)
		staticFS, err := fs.Sub(webassets.FS, "web/static")
		if err != nil {
			return nil, fmt.Errorf("load embedded static assets: %w", err)
		}
		r.StaticFS("/static", http.FS(staticFS))
	}

	r.GET("/", app.HomeHandler)
	r.GET("/healthz", app.HealthHandler)
	r.GET(loginPath, app.LoginPageHandler)
	r.POST(loginPath, app.LoginHandler)
	r.POST(logoutPath, app.LogoutHandler)
	r.GET(logoutPath, app.LogoutHandler)
	r.GET(changePasswordPath, app.ChangePasswordPageHandler)
	r.POST(changePasswordPath, app.ChangePasswordHandler)
	r.GET("/api/whoami", app.WhoAmIHandler)
	r.GET(setupPath, app.SetupPageHandler)
	r.POST(setupPath, app.SetupHandler)

	// Account administration.
	admin := r.Group("", app.RequireAdmin())
	admin.GET(adminUsersPath, app.AdminUsersPageHandler)
	admin.GET("/api/admin/users", app.AdminListUsersHandler)
	admin.POST("/api/admin/users", app.AdminCreateUserHandler)
	admin.PATCH("/api/admin/users/:id", app.AdminUpdateUserHandler)
	admin.DELETE("/api/admin/users/:id", app.AdminDeleteUserHandler)
	r.POST("/connect", app.ConnectHandler)
	r.GET("/screen", app.ScreenHandler)
	r.GET("/screen/content", app.ScreenContentHandler)
	r.GET("/screen/stream", app.ScreenStreamHandler)
	r.GET("/screen/print", app.PrintScreenHandler)
	r.GET("/screen/history", app.ScreenHistoryHandler)

	// IND$FILE file transfer. See transfer.go.
	r.POST("/transfer/send", app.TransferSendHandler)
	r.POST("/transfer/receive", app.TransferReceiveHandler)
	r.POST("/submit", app.SubmitHandler)
	r.POST("/submit/async", app.SubmitAsyncHandler)
	r.POST("/prefs", app.PrefsHandler)
	r.POST("/prefs/keypad", app.KeypadPrefHandler)
	r.POST("/record/start", app.RecordStartHandler)
	r.POST("/record/stop", app.RecordStopHandler)
	r.GET("/record/download", app.RecordDownloadHandler)
	r.POST("/workflow/load", app.LoadWorkflowHandler)
	r.POST("/workflow/load-json", app.LoadWorkflowJSONHandler)
	r.POST("/workflow/play", app.PlayWorkflowHandler)
	r.POST("/workflow/debug", app.DebugWorkflowHandler)
	r.POST("/workflow/pause", app.PauseWorkflowHandler)
	r.POST("/workflow/step", app.StepWorkflowHandler)
	r.POST("/workflow/stop", app.StopWorkflowHandler)
	r.POST("/workflow/remove", app.RemoveWorkflowHandler)
	r.GET("/workflow/status", app.WorkflowStatusHandler)
	// Instance-wide administration. These change or expose state that belongs
	// to the whole deployment, not to one person's terminal: settings are read
	// by every session, the log holds every user's activity, and a restart ends
	// everyone's sessions at once. Before this they needed only *a* session,
	// which any visitor could mint by connecting.
	adminOnly := r.Group("", app.RequireAdmin())
	adminOnly.GET("/api/settings", app.SettingsHandler)
	adminOnly.POST("/api/settings", app.SettingsHandler)
	r.GET("/api/themes", app.ThemeListHandler)
	r.POST("/api/themes/save", app.ThemeSaveHandler)
	adminOnly.POST("/app/restart", app.RestartHandler)

	// Logging handlers
	adminOnly.GET("/logs", app.LogsHandler)
	adminOnly.GET("/logs/access", app.LogsAccessHandler)
	adminOnly.POST("/logs/access", app.LogsAccessHandler)
	adminOnly.POST("/logs/toggle", app.LogsToggleHandler)
	adminOnly.POST("/logs/clear", app.LogsClearHandler)
	adminOnly.GET("/logs/download", app.LogsDownloadHandler)

	// The audit trail. Admin-only for the same reason the debug log is: it
	// names accounts, the addresses they came from and the hosts they reached.
	// Unlike the debug log it has no on/off switch — a trail somebody can turn
	// off before acting is not a trail.
	adminOnly.GET("/admin/audit", app.AdminAuditPageHandler)
	adminOnly.GET("/api/admin/audit", app.AdminAuditHandler)
	adminOnly.GET("/api/admin/audit/download", app.AdminAuditDownloadHandler)

	// Disconnect handler
	r.POST("/disconnect", app.DisconnectHandler)
	r.POST("/reconnect", app.ReconnectHandler)

	// Concurrent sessions (tab bar). See sessions.go.
	// Connection profiles (host, TLS, LU, model, code page). See profiles.go.
	r.GET("/api/profiles", app.ProfilesListHandler)
	r.POST("/api/profiles", app.ProfilesSaveHandler)
	r.POST("/api/profiles/delete", app.ProfilesDeleteHandler)

	// Guided Business Tasks. See tasks.go.
	r.GET("/tasks", app.TasksListHandler)
	r.POST("/tasks/save", app.TasksSaveHandler)
	r.POST("/tasks/delete", app.TasksDeleteHandler)
	r.POST("/tasks/run", app.TasksRunHandler)
	r.GET("/tasks/status", app.TasksStatusHandler)
	r.GET("/tasks/draft", app.TasksDraftHandler)
	r.POST("/tasks/cancel", app.TasksCancelHandler)

	r.GET("/sessions", app.SessionsListHandler)
	r.POST("/sessions/new", app.SessionsNewHandler)
	r.POST("/sessions/switch", app.SessionsSwitchHandler)
	r.POST("/sessions/close", app.SessionsCloseHandler)

	// Host compatibility profile (cookie-auth, current session)
	r.POST("/profile", app.ProfileHandler)
	r.GET("/profile", app.ProfileGetHandler)
	r.GET("/embed/config", app.EmbedConfigHandler)
	r.GET("/host/query", app.HostQueryHandler)
	r.GET("/host/toggles", app.HostTogglesHandler)
	r.POST("/host/toggles", app.HostToggleSetHandler)

	// Chaos exploration handlers
	r.POST("/chaos/start", app.ChaosStartHandler)
	r.POST("/chaos/stop", app.ChaosStopHandler)
	r.POST("/chaos/remove", app.ChaosRemoveHandler)
	r.GET("/chaos/status", app.ChaosStatusHandler)
	r.POST("/chaos/export", app.ChaosExportHandler)
	r.POST("/chaos/report", app.ChaosReportHandler)
	r.GET("/chaos/mindmap/export", app.ChaosMindMapExportHandler)
	r.POST("/chaos/mindmap/import", app.ChaosMindMapImportHandler)
	r.POST("/chaos/mindmap/compare", app.ChaosMindMapCompareHandler)
	r.GET("/chaos/runs", app.ChaosListRunsHandler)
	r.POST("/chaos/load", app.ChaosLoadHandler)
	r.POST("/chaos/runs/delete", app.ChaosDeleteRunHandler)
	r.POST("/chaos/load-recording", app.ChaosLoadRecordingHandler)
	r.POST("/chaos/resume", app.ChaosResumeHandler)
	r.GET("/chaos/hints", app.ChaosHintsGetHandler)
	r.POST("/chaos/hints", app.ChaosHintsSaveHandler)
	r.POST("/chaos/hints/extract-recording", app.ChaosHintsExtractHandler)
	r.GET("/chaos/screen-hints", app.ChaosScreenHintsGetHandler)
	r.POST("/chaos/screen-hints", app.ChaosScreenHintsSaveHandler)

	// Business-understanding handlers (AI screen annotations + function catalog)
	r.GET("/chaos/screens", app.ChaosScreensListHandler)
	r.POST("/chaos/screens/annotate", app.ChaosScreenAnnotateHandler)
	r.GET("/chaos/business/functions", app.ChaosBusinessFunctionsListHandler)
	r.POST("/chaos/business/functions", app.ChaosBusinessFunctionSaveHandler)
	r.POST("/chaos/business/generate-workflow", app.ChaosBusinessGenerateWorkflowHandler)
	r.GET("/chaos/business/overview", app.ChaosBusinessOverviewHandler)
	// Convert a discovered function into a business task draft.
	r.GET("/chaos/business/task-draft", app.ChaosBusinessToTaskHandler)
	r.GET("/chaos/insights", app.ChaosInsightsHandler)

	// Skills, instructions and extensions. Registered before initCopilot so
	// the skill index is available to the system prompt it serves.
	app.registerSkillRoutes(r)
	copilot.SetSkillIndex(app.skillIndexSection)

	// AI chat side panel + screen JSON tool endpoints
	app.initCopilot(r)

	// Public REST/JSON API (gated by API_TOKEN env var)
	app.registerAPIv1(r)

	// MCP over HTTP, for clients that cannot launch a local process. The
	// usual route is the stdio subcommand; see docs/mcp.md.
	app.registerMCPHTTP(r)

	return r, nil
}

func main() {
	// The MCP subcommand owns stdout from its first statement, so it must be
	// dispatched before the startup log, the config load, or the deferred
	// recover below — which shows a modal dialog on Windows, in front of a
	// process no one is looking at.
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		runMCP(os.Args[2:])
		return
	}

	// Account management is a console tool: it writes to stdout and must not
	// start a server or raise a window, so it is dispatched here too.
	if len(os.Args) > 1 && os.Args[1] == "user" {
		os.Exit(runUserCLI(os.Args[2:], os.Stdout, os.Stderr, os.Stdin))
	}
	if len(os.Args) > 1 && os.Args[1] == "token" {
		os.Exit(runTokenCLI(os.Args[2:], os.Stdout, os.Stderr))
	}

	installDir := resolveBaseDir()
	baseDir, dataErr := resolveDataDir(installDir)
	if dataErr != nil {
		// Before the log is opened, because the log lives in the directory
		// that just turned out to be unusable.
		fmt.Fprintf(os.Stderr, "3270Web: %v\n", dataErr)
		showFatalError(dataErr.Error())
		os.Exit(1)
	}
	// Before the log is opened, so an older installation's log file is moved
	// rather than being written to in the place it is about to leave.
	if err := migrateStateToDataDir(installDir, baseDir); err != nil {
		fmt.Fprintf(os.Stderr, "3270Web: %v\n", err)
		showFatalError(err.Error())
		os.Exit(1)
	}
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
	app := newAppAt(installDir, baseDir)

	r, err := buildRouter(app)
	if err != nil {
		showFatalError(err.Error())
		return
	}
	// Idle sessions (browser tab closed without disconnecting, network drop)
	// otherwise live forever: their s3270 subprocess, chaos engine, and
	// session-status poller keep running until the server restarts. Reap
	// them periodically; see reapIdleSessions for what's skipped/cleaned up.
	stopSessionReaper := app.startSessionReaper(sessionReapInterval, sessionIdleTimeout)
	defer stopSessionReaper()

	shutdownCh := make(chan struct{})
	requestShutdown := func() {
		select {
		case <-shutdownCh:
			return
		default:
			close(shutdownCh)
		}
	}
	app.shutdown = requestShutdown

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		requestShutdown()
	}()

	// Bind to loopback by default on all platforms to avoid exposing the admin UI
	// and host-connection features on the local network unintentionally. WEBUI_BIND
	// overrides the interface; the Docker image sets it to 0.0.0.0 (see Dockerfile).
	// If the default port is already in use, fall back to an OS-assigned free port
	// on the same interface.
	bindHost := os.Getenv("WEBUI_BIND")
	preferredAddr := resolveListenAddr(bindHost, os.Getenv("WEBUI_PORT"))
	ln, err := net.Listen("tcp", preferredAddr)
	if err != nil {
		log.Printf("Port %s unavailable (%v), falling back to OS-assigned port", preferredAddr, err)
		ln, err = net.Listen("tcp", resolveListenAddr(bindHost, "0"))
		if err != nil {
			msg := fmt.Sprintf("3270Web failed to start: could not bind to any port: %v", err)
			log.Print(msg)
			showFatalError(msg)
			return
		}
	}
	addr := ln.Addr().String()

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
		app.stopAllSessions()
	}()

	startServer := func(errCh chan<- error) {
		log.Printf("Starting server on %s", addr)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

const (
	// defaultBindHost is the interface the server listens on when WEBUI_BIND is
	// unset. Loopback keeps the UI — which has no password of its own — off the
	// local network for desktop and `go run` users.
	//
	// Containers must override this. A published port (`-p 3270:8080`) forwards
	// to the container's external interface, so a loopback-only listener inside
	// the container is unreachable from the host no matter how the ports are
	// mapped: the connection is refused even though the container is running and
	// its healthcheck (which curls 127.0.0.1 from inside) passes. The Dockerfile
	// therefore sets WEBUI_BIND=0.0.0.0 and leaves exposure to the port mapping,
	// which is where Docker users expect to control it.
	defaultBindHost  = "127.0.0.1"
	defaultWebUIPort = "8080"
)

// resolveListenAddr builds the TCP address to listen on from the WEBUI_BIND and
// WEBUI_PORT values, falling back to the loopback default and port 8080 when
// either is empty. net.JoinHostPort brackets IPv6 hosts, so WEBUI_BIND="::"
// yields "[::]:8080".
func resolveListenAddr(bind, port string) string {
	bind = strings.TrimSpace(bind)
	if bind == "" {
		bind = defaultBindHost
	}
	port = strings.TrimSpace(port)
	if port == "" {
		port = defaultWebUIPort
	}
	return net.JoinHostPort(bind, port)
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

// resolveDataDir says where state is written, and refuses rather than guesses.
//
// DATA_DIR exists for containers, where the program's own directory is part of
// an image that is replaced on every deploy. Without it, upgrading a
// containerised instance destroys every account, every issued token, the audit
// trail and everyone's saved work — and, with accounts enabled, brings the
// instance back up in first-run setup for whoever reaches it first.
//
// Unset means "beside the program", which is right for a desktop install and
// for `go run`.
//
// Set but unusable is an error, not something to work around. The usual cause
// is a bind-mounted host directory the container's user cannot write: falling
// back to the program's directory would put the state exactly where a deploy
// deletes it, and the instance would look fine until the day it lost
// everything. Saying so at startup costs one restart; the alternative costs
// the accounts.
func resolveDataDir(installDir string) (string, error) {
	dir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dir == "" {
		return installDir, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("DATA_DIR %s cannot be created: %w%s", dir, err, dataDirHint())
	}
	// MkdirAll succeeds on a directory that already exists and belongs to
	// somebody else, which is the bind-mount case exactly. Only a write says
	// whether this will work.
	probe, err := os.CreateTemp(dir, ".writable-*")
	if err != nil {
		return "", fmt.Errorf("DATA_DIR %s is not writable: %w%s", dir, err, dataDirHint())
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return dir, nil
}

// dataDirHint names the fix for the failure people actually hit: a host
// directory bind-mounted into a container that runs as an unprivileged user.
func dataDirHint() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return fmt.Sprintf("\nThe server runs as uid %d. For a bind-mounted host directory:\n"+
		"  mkdir -p ./data && sudo chown -R %d:%d ./data", os.Getuid(), os.Getuid(), os.Getgid())
}

// resolveStateDir is where state lives, for the callers that have no App to
// ask — the account and token CLIs, run inside the same container as the
// server. They must reach the same files the server does, or `3270Web user
// add` writes an account into the image layer that the running server, reading
// its data directory, never sees.
//
// A CLI that cannot reach the right directory must not fall back to the wrong
// one and report success, so this ends the process instead.
func resolveStateDir() string {
	dir, err := resolveDataDir(resolveBaseDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "3270Web: %v\n", err)
		os.Exit(1)
	}
	return dir
}

// assetDir is where web/templates and web/static are read from: beside the
// program, never from the state directory, which an untrusted volume could
// otherwise use to replace the pages this server serves.
func (app *App) assetDir() string {
	if app.installDir != "" {
		return app.installDir
	}
	return app.baseDir
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

type recordingStatus struct {
	Active    bool
	StartedAt string
	Steps     int
	File      string
}

func recordingStatusSnapshot(s *session.Session) recordingStatus {
	var status recordingStatus
	if s == nil {
		return status
	}
	s.Lock()
	defer s.Unlock()
	if s.Recording == nil {
		return status
	}
	status.Active = s.Recording.Active
	status.Steps = len(s.Recording.Steps)
	if !s.Recording.StartedAt.IsZero() {
		status.StartedAt = s.Recording.StartedAt.Format(time.RFC3339)
	}
	if s.Recording.FilePath != "" {
		status.File = filepath.Base(s.Recording.FilePath)
	}
	return status
}

type sessionSnapshot struct {
	Prefs                   session.Preferences
	RecordingActive         bool
	RecordingFile           string
	PlaybackActive          bool
	PlaybackPaused          bool
	PlaybackCompleted       bool
	PlaybackMode            string
	PlaybackStep            int
	PlaybackStepType        string
	PlaybackStepTotal       int
	PlaybackDelayRange      string
	PlaybackDelayApplied    string
	PlaybackEvents          []session.WorkflowEvent
	LoadedWorkflow          bool
	LoadedWorkflowName      string
	LoadedWorkflowPreview   string
	LoadedWorkflowSize      int
	LoadedWorkflowStepTotal int
	TargetHost              string
	TargetPort              int
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
		snap.LoadedWorkflowStepTotal = s.LoadedWorkflow.StepTotal
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

// sessionHost returns a stable snapshot of the session's current Host, read
// under the session lock. resetSessionHost (called on reconnect/workflow
// load) swaps s.Host under this same lock; reading s.Host directly from a
// handler without going through this helper is a data race with that swap,
// and can observe a Host that has already been Stop()ped. Callers should
// read the host once per request and reuse the local value rather than
// re-reading s.Host on every call.
func (app *App) sessionHost(s *session.Session) host.Host {
	var h host.Host
	withSessionLock(s, func() { h = s.Host })
	return h
}

const (
	// sessionIdleTimeout is how long a session may go without an HTTP
	// request (LastAccess) before the reaper considers it abandoned. Kept
	// comfortably longer than the session cookie's 1-hour Max-Age (see
	// setSessionCookie) so a session isn't reaped while its cookie is still
	// plausibly in use.
	sessionIdleTimeout = 2 * time.Hour
	// sessionReapInterval is how often the reaper sweeps for idle sessions.
	sessionReapInterval = 10 * time.Minute
	// copilotIdentityIdleTimeout bounds the in-memory Copilot auth store
	// across a long-running server that many different browsers connect to
	// over time. It is far longer than sessionIdleTimeout since evicting an
	// identity only drops its in-memory cache — the on-disk token file is
	// untouched and reloaded lazily the next time that browser is seen.
	copilotIdentityIdleTimeout = 24 * time.Hour
)

// startSessionReaper periodically removes sessions that have had no HTTP
// activity for maxIdle, along with their s3270 subprocess, chaos engine,
// recording file, and cached compatibility profile. Without this, a session
// whose browser tab is closed (or that loses network) without an explicit
// /disconnect lives forever: its s3270 subprocess keeps running and its
// chaos-status poller keeps ticking until the server itself restarts. The
// returned stop func halts the sweep goroutine.
func (app *App) startSessionReaper(interval, maxIdle time.Duration) (stop func()) {
	done := make(chan struct{})
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				app.reapIdleSessions(maxIdle)
				if app.copilotAuthStore != nil {
					app.copilotAuthStore.EvictIdle(copilotIdentityIdleTimeout)
				}
				if app.aiConfigStore != nil {
					app.aiConfigStore.EvictIdle(copilotIdentityIdleTimeout)
				}
			}
		}
	}()
	return func() { close(done) }
}

// reapIdleSessions removes sessions idle for at least maxIdle, skipping any
// with an in-progress chaos exploration or workflow playback so a long,
// unattended automated run isn't killed out from under itself just because
// nobody touched the browser.
func (app *App) reapIdleSessions(maxIdle time.Duration) {
	for _, id := range app.SessionManager.IdleSessionIDs(maxIdle) {
		s, ok := app.SessionManager.PeekSession(id)
		if !ok {
			continue
		}
		if eng, ok := app.chaosEngines.get(id); ok && eng != nil && eng.Active() {
			continue
		}
		playbackActive := false
		withSessionLock(s, func() {
			playbackActive = s.Playback != nil && s.Playback.Active
		})
		if playbackActive {
			continue
		}
		log.Printf("Reaping idle session %s (no activity for %s)", id, maxIdle)
		app.cleanupSession(s)
	}
}

// cleanupSession releases everything owned by a session: its s3270
// subprocess (via RemoveSession), any in-progress recording file, and the
// per-session chaos engine, loaded-run, and profile caches. Shared by the
// explicit /disconnect handler and the idle-session reaper so the two paths
// can't drift out of sync.
func (app *App) cleanupSession(s *session.Session) {
	if s == nil {
		return
	}
	cleanupRecordingFile(s)
	// Told to stop before the engine is unregistered and the removed mark is
	// cleared below. Those two together used to leave the status goroutine
	// with no exit condition at all: it polled a detached engine, and wrote
	// the run file whenever it eventually finished — by which time the
	// session, and in tests the directory it wrote into, had gone.
	app.chaosEngines.stopSync(s.ID)
	app.chaosEngines.delete(s.ID)
	app.chaosEngines.deleteLoadedRun(s.ID)
	app.chaosEngines.clearRemoved(s.ID)
	if app.profiles != nil {
		app.profiles.delete(s.ID)
	}
	// A finished task run stays readable so the browser can fetch its result
	// after the fact; the session ending is what makes it collectable. Without
	// this the run map grows by one entry for every session that ever ran a
	// task, for the life of the process.
	if app.taskRunStore != nil {
		app.taskRunStore.cancel(s.ID)
		app.taskRunStore.forget(s.ID)
	}
	// Snapshots are a working note taken during a run, so they end with the
	// run. A screen trace holds a file handle inside the terminal, which the
	// subprocess exiting closes for us; what is dropped here is only this
	// server's record of where the file went — the file itself stays on disk
	// for whoever asked for it.
	if app.snapshotStore != nil {
		app.snapshotStore.forget(s.ID)
	}
	if app.screenTraceStore != nil {
		app.screenTraceStore.forget(s.ID)
	}
	// Which skills a conversation has already been given is only meaningful
	// while that conversation exists, and the map would otherwise grow by one
	// entry per session for the life of the process.
	app.forgetLoadSession(s.ID)
	app.SessionManager.RemoveSession(s.ID)
}

// stopAllSessions cleans up every active session, killing each one's s3270
// subprocess. Called on graceful shutdown — without it, a clean server exit
// (SIGINT, /app/restart) left every connected session's s3270 child running,
// orphaned rather than killed, relying only on the OS being asked nicely to
// cascade the exit (which it does not do on its own).
func (app *App) stopAllSessions() {
	for _, s := range app.SessionManager.ListSessions() {
		app.cleanupSession(s)
	}
	// cleanupSession has told each status goroutine to stop; this is where we
	// wait for them. Without it a chaos run's saved-run file could still be
	// half-written when the process exits, which is a truncated file on disk
	// rather than a missing one.
	if app.chaosEngines != nil {
		app.chaosEngines.stopAllSync()
	}
	if !app.waitForChaosSync(chaosSyncShutdownTimeout) {
		log.Printf("Warning: chaos status goroutines did not finish within %s", chaosSyncShutdownTimeout)
	}
}

// chaosSyncShutdownTimeout bounds how long a shutdown waits for the chaos
// status goroutines. They stop within one poll interval of being told to, so
// this is a backstop rather than a budget.
const chaosSyncShutdownTimeout = 5 * time.Second

// sessionCookieName is the cookie app.getSession reads to identify the
// active host session. The /api/v1 session-scoped routes set it from the
// path parameter, so it is named once rather than spelled out per call.
const sessionCookieName = "3270Web_session"

const lastTargetCookieName = "3270Web_last_target"

func (app *App) renderConnectPage(c *gin.Context, status int, hostname string, connectError string) {
	defaultHost := strings.TrimSpace(hostname)
	if app.Config.TargetHost.Value != "" {
		defaultHost = app.Config.TargetHost.Value
	}
	if defaultHost == "" {
		defaultHost = strings.TrimSpace(getCookieValue(c, lastTargetCookieName))
	}
	if defaultHost == "" {
		defaultHost = "localhost:3270"
	}
	samplePorts := allowedSampleAppPorts()
	// The connect page carries the same theme picker as the terminal, but no
	// session exists yet, so it cannot use the session-gated /api/themes.
	// Embedding the list is what makes custom themes actually appear here;
	// fetching it was silently 401ing and falling back to an empty list.
	themesJSON := "[]"
	if items, err := app.listFileThemes(c); err != nil {
		log.Printf("Connect page: failed to read themes folder: %v", err)
	} else if encoded, err := json.Marshal(items); err != nil {
		log.Printf("Connect page: failed to encode themes: %v", err)
	} else {
		themesJSON = string(encoded)
	}
	rememberEmbedMode(c)
	c.HTML(status, "connect.html", gin.H{
		"Embedded":     embedRequested(c),
		"EmbedOrigins": embedOriginsAttr(),
		"DefaultHost":  defaultHost,
		"SampleApps":   availableSampleApps(),
		"SamplePorts":  samplePorts,
		"ConnectError": connectError,
		"Version":      appVersion,
		// Passed as a plain string into an attribute, not template.JS into a
		// <script>: theme names come from files on disk and html/template
		// escapes attribute values for us, so a name containing markup
		// cannot break out.
		"ThemesJSON": themesJSON,
		"Auth":       app.authView(c),
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
	// A named connection profile carries TLS, LU, model and code page, which
	// a bare "hostname:port" cannot express.
	if name := strings.TrimSpace(c.PostForm("profile")); name != "" {
		profile, ok := app.findVisibleProfile(c, name)
		if !ok {
			app.renderConnectPage(c, http.StatusBadRequest, "", fmt.Sprintf("Connection profile %q no longer exists.", name))
			return
		}
		if err := app.connectWithProfile(c, profile); err != nil {
			log.Printf("Connect failed for profile %q: %v", name, err)
			app.renderConnectPage(c, http.StatusServiceUnavailable, profile.displayTarget(),
				connectErrorMessage(profile.displayTarget(), err))
			return
		}
		c.Redirect(http.StatusFound, "/screen")
		return
	}

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

// screenStatusLabels formats the OIA status line's four fields (keyboard
// lock, model, dimensions, cursor) the same way for both the full-page
// render (ScreenHandler) and the async refresh payload
// (ScreenContentHandler) — the latter needs these too so the client can
// actually update the aria-live status line instead of leaving it frozen
// at whatever the last full page load rendered.
func screenStatusLabels(screen *host.Screen) (keyboard, model, dimensions, cursor string) {
	keyboard = "UNKNOWN"
	if state, ok := screen.StatusKeyboardState(); ok {
		switch state {
		case "U":
			keyboard = "UNLOCKED"
		case "L":
			keyboard = "LOCKED"
		case "E":
			keyboard = "ERROR"
		default:
			keyboard = state
		}
	}
	model = "--"
	if m, ok := screen.StatusModel(); ok {
		model = m
	}
	dimensions = "--x--"
	if rows, cols, ok := screen.StatusDimensions(); ok {
		dimensions = fmt.Sprintf("%dx%d", rows, cols)
	}
	cursor = "--,--"
	if row, col, ok := screen.StatusCursor(); ok {
		cursor = fmt.Sprintf("%d,%d", row+1, col+1)
	}
	return keyboard, model, dimensions, cursor
}

// screenOIA builds the Operator Information Area fields. The status line used
// to carry only KB/MODEL/SIZE/CURSOR, which left an operator unable to tell
// "the host is thinking" from "the app is broken" — the one thing the real
// OIA exists to communicate. These fields add the indicators operators
// actually read: the online/application block on the left and the
// input-inhibit indicator ("X SYSTEM", "X -f") in the middle.
func screenOIA(screen *host.Screen) gin.H {
	indicator, explanation := screen.OIAIndicator()
	connected, peer, known := screen.StatusConnected()
	// The classic left-hand block: "4" means online, "4-A" means online and
	// owned by an application (which, for a TN3270 session showing a
	// formatted screen, it is).
	online := "4"
	if !known {
		online = "?"
	} else if !connected {
		online = "–"
	} else if screen.IsFormatted {
		online = "4-A"
	}
	return gin.H{
		"oiaOnline":      online,
		"oiaConnected":   connected && known,
		"oiaPeer":        peer,
		"oiaIndicator":   indicator,
		"oiaExplanation": explanation,
	}
}

// recordScreenHistory stores the screen the operator is about to be shown.
// Called from every display path (full page render, async refresh, SSE push);
// Session.RecordScreen drops repeats, so calling it more than once for the
// same screen is harmless.
func recordScreenHistory(s *session.Session, screen *host.Screen) {
	if s == nil || screen == nil {
		return
	}
	rows := screen.Height
	cols := screen.Width
	if rows <= 0 || cols <= 0 {
		return
	}
	cursor := ""
	if row, col, ok := screen.StatusCursor(); ok {
		cursor = fmt.Sprintf("%d,%d", row+1, col+1)
	}
	s.RecordScreen(session.ScreenHistoryEntry{
		Text:   screen.Text(),
		Rows:   rows,
		Cols:   cols,
		Cursor: cursor,
		At:     time.Now(),
	})
}

// ScreenHistoryHandler returns the session's recent screens, oldest first.
// Answers "what did the previous screen say?", which a terminal without
// scrollback simply cannot.
func (app *App) ScreenHistoryHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}
	entries := s.ScreenHistorySnapshot()
	if entries == nil {
		entries = []session.ScreenHistoryEntry{}
	}
	c.JSON(http.StatusOK, gin.H{
		"screens": entries,
		"limit":   session.ScreenHistoryLimit,
	})
}

// mergeOIA copies the OIA fields into an existing payload map.
func mergeOIA(payload gin.H, screen *host.Screen) gin.H {
	for k, v := range screenOIA(screen) {
		payload[k] = v
	}
	return payload
}

func (app *App) ScreenHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}

	h := app.sessionHost(s)
	if err := h.UpdateScreen(); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("Update screen failed: %v", err)})
		return
	}

	screen := hostScreenSnapshot(h)
	if screen == nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": "Screen unavailable"})
		return
	}
	if rows, cols, ok := app.modelDimensions(); ok {
		screen = limitScreenForDisplay(screen, rows, cols)
	}
	recordScreenHistory(s, screen)
	rendered := app.Renderer.Render(screen, "/submit", s.ID)
	snap := app.snapshotSession(s)
	themeCSS := app.buildThemeCSS(snap.Prefs)

	keyboardLabel, modelLabel, dimensionLabel, cursorLabel := screenStatusLabels(screen)
	oia := screenOIA(screen)

	chaosRunActive := false
	if app.chaosEngines != nil {
		if eng, ok := app.chaosEngines.get(s.ID); ok && eng != nil {
			chaosRunActive = eng.Active()
		}
	}

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
	rememberEmbedMode(c)
	c.HTML(http.StatusOK, "screen.html", gin.H{
		"Embedded":                embedRequested(c),
		"EmbedOrigins":            embedOriginsAttr(),
		"ScreenContent":           template.HTML(rendered),
		"SessionID":               s.ID,
		"Auth":                    app.authView(c),
		"ColorSchemes":            app.Config.ColorSchemes.Schemes,
		"Fonts":                   app.Config.Fonts.Fonts,
		"SelectedColorScheme":     snap.Prefs.ColorScheme,
		"SelectedFont":            snap.Prefs.FontName,
		"UseKeypad":               snap.Prefs.UseKeypad,
		"ThemeCSS":                template.CSS(themeCSS),
		"RecordingActive":         snap.RecordingActive,
		"RecordingFile":           snap.RecordingFile,
		"PlaybackActive":          snap.PlaybackActive,
		"PlaybackPaused":          snap.PlaybackPaused,
		"PlaybackCompleted":       snap.PlaybackCompleted,
		"PlaybackMode":            snap.PlaybackMode,
		"PlaybackStep":            snap.PlaybackStep,
		"PlaybackStepType":        snap.PlaybackStepType,
		"PlaybackStepTotal":       snap.PlaybackStepTotal,
		"PlaybackDelayRange":      snap.PlaybackDelayRange,
		"PlaybackDelayApplied":    snap.PlaybackDelayApplied,
		"PlaybackEvents":          snap.PlaybackEvents,
		"LoadedWorkflow":          snap.LoadedWorkflow,
		"LoadedWorkflowName":      snap.LoadedWorkflowName,
		"LoadedWorkflowPreview":   snap.LoadedWorkflowPreview,
		"LoadedWorkflowSize":      snap.LoadedWorkflowSize,
		"LoadedWorkflowStepTotal": snap.LoadedWorkflowStepTotal,
		"StatusKeyboard":          keyboardLabel,
		"StatusModel":             modelLabel,
		"StatusDimensions":        dimensionLabel,
		"StatusCursor":            cursorLabel,
		// The run-status widget is only rendered when something is actually
		// running, so a page load with no run in progress does not flash a
		// 360x320 panel saying "Playback has not started yet".
		"ShowRunStatus":  snap.RecordingActive || snap.PlaybackActive || chaosRunActive,
		"SessionTabs":    app.sessionTabs(c),
		"MaxSessions":    maxConcurrentSessions,
		"OIAOnline":      oia["oiaOnline"],
		"OIAIndicator":   oia["oiaIndicator"],
		"OIAExplanation": oia["oiaExplanation"],
		"OIAPeer":        oia["oiaPeer"],
		"SampleAppName":  sampleAppName,
		"SampleAppPort":  sampleAppPort,
		"TargetHost":     snap.TargetHost,
		"TargetPort":     snap.TargetPort,
		"Version":        appVersion,
	})
}

func (app *App) ScreenContentHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	h := app.sessionHost(s)
	if err := h.UpdateScreen(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Update screen failed: %v", err)})
		return
	}
	screen := hostScreenSnapshot(h)
	if screen == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "screen unavailable"})
		return
	}
	if rows, cols, ok := app.modelDimensions(); ok {
		screen = limitScreenForDisplay(screen, rows, cols)
	}
	recordScreenHistory(s, screen)
	rendered := app.Renderer.Render(screen, "/submit", s.ID)
	keyboardLabel, modelLabel, dimensionLabel, cursorLabel := screenStatusLabels(screen)
	payload := mergeOIA(gin.H{
		"html":             rendered,
		"statusKeyboard":   keyboardLabel,
		"statusModel":      modelLabel,
		"statusDimensions": dimensionLabel,
		"statusCursor":     cursorLabel,
	}, screen)
	if cursorRow, cursorCol, ok := screen.StatusCursor(); ok {
		payload["cursorRow"] = cursorRow
		payload["cursorCol"] = cursorCol
	}
	c.JSON(http.StatusOK, payload)
}

func hostScreenSnapshot(h host.Host) *host.Screen {
	if h == nil {
		return nil
	}
	if provider, ok := h.(interface{ GetScreenSnapshot() *host.Screen }); ok {
		return provider.GetScreenSnapshot()
	}
	screen := h.GetScreen()
	if screen == nil {
		return nil
	}
	return screen.Clone()
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
	if err := app.processSubmit(c, s); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": err.Error()})
		return
	}

	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) SubmitAsyncHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	if err := app.processSubmit(c, s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (app *App) processSubmit(c *gin.Context, s *session.Session) error {
	key := c.PostForm("key")
	cursorRow := strings.TrimSpace(c.PostForm("cursor_row"))
	cursorCol := strings.TrimSpace(c.PostForm("cursor_col"))

	h := app.sessionHost(s)
	// Capture the screen for the recording BEFORE updateFields writes the
	// operator's input into the buffer. A guard derived from this has to
	// match the screen as the host paints it, not as it looks once someone
	// has typed into it.
	recordScreenForStep(s, h)
	if h.GetScreen().IsFormatted {
		// 1. Update fields from form data
		app.updateFields(c, h)
		recordFieldUpdates(s, h)

		// 2. Submit changes to host
		if err := h.SubmitScreen(); err != nil {
			return fmt.Errorf("submit failed: %w", err)
		}
	} else {
		data := c.PostForm("field")
		if err := h.SubmitUnformatted(data); err != nil {
			return fmt.Errorf("submit failed: %w", err)
		}
	}

	if cursorRow != "" && cursorCol != "" {
		if row, err := strconv.Atoi(cursorRow); err == nil {
			if col, err := strconv.Atoi(cursorCol); err == nil {
				if row >= 0 && col >= 0 {
					_ = h.MoveCursor(row, col)
				}
			}
		}
	}

	// 3. Send action key
	actionKey := "Enter"
	if key != "" {
		normalized, ok := normalizeKey(key)
		if !ok {
			return fmt.Errorf("unrecognized key %q", key)
		}
		actionKey = normalized
	}
	log.Printf("Submit: normalized key=%q", actionKey)
	recordActionKey(s, actionKey)

	if err := h.SendKey(actionKey); err != nil {
		return fmt.Errorf("send key failed: %w", err)
	}
	return nil
}

// DisconnectHandler ends the ACTIVE session. With tabs open, the others keep
// running and the browser lands on one of them rather than back at the
// connect page — disconnecting one host should not close the rest.
func (app *App) DisconnectHandler(c *gin.Context) {
	closedID := ""
	if s := app.getSession(c); s != nil {
		closedID = s.ID
		if lastTarget := formatSessionTarget(s); lastTarget != "" {
			setSessionCookie(c, lastTargetCookieName, lastTarget)
		}
		app.cleanupSession(s)
	}
	remaining := []string(nil)
	if closedID != "" {
		remaining = app.forgetSession(c, closedID)
	} else {
		setSessionCookie(c, sessionCookieName, "")
	}
	if len(remaining) > 0 {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	setSessionCookie(c, sessionCookieName, "")
	c.Redirect(http.StatusFound, "/")
}

// ReconnectHandler re-dials the current session's target host after a drop.
// Mainframe sessions end for routine reasons — LPAR maintenance, VTAM
// timeouts, network blips — and before this the only recovery was to go back
// to the connect page and retype the host, losing your place several times a
// day.
//
// The old session's s3270 subprocess is torn down and a fresh one started,
// which means recording and chaos state for the dead session does not
// survive. That is the honest outcome: the host-side conversation those
// features were tracking ended when the connection did.
func (app *App) ReconnectHandler(c *gin.Context) {
	target := ""
	previousID := ""
	if s := app.getSession(c); s != nil {
		target = formatSessionTarget(s)
		previousID = s.ID
		app.cleanupSession(s)
	}
	if target == "" {
		target = strings.TrimSpace(getCookieValue(c, lastTargetCookieName))
	}
	if target == "" {
		setSessionCookie(c, sessionCookieName, "")
		c.JSON(http.StatusBadRequest, gin.H{"error": "no previous host to reconnect to"})
		return
	}

	if err := app.connectToHost(c, target); err != nil {
		log.Printf("Reconnect failed for %q: %v", target, err)
		setSessionCookie(c, sessionCookieName, "")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":  connectErrorMessage(target, err),
			"target": target,
		})
		return
	}
	// Keep the reconnected session where it was on the tab bar.
	if previousID != "" {
		if newID := getCookieValue(c, sessionCookieName); newID != "" {
			app.replaceSessionInRoster(c, previousID, newID)
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "target": target})
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
	storeLoadedWorkflow(s, upload.Name, upload.Payload, len(upload.Config.Steps), preview)
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) LoadWorkflowJSONHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	playing := false
	withSessionLock(s, func() {
		playing = s.Playback != nil && s.Playback.Active
	})
	if playing {
		c.JSON(http.StatusConflict, gin.H{"error": "cannot edit workflow during playback"})
		return
	}

	var payload workflowJSONPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	content := strings.TrimSpace(payload.Content)
	if content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow content is required"})
		return
	}
	raw := []byte(content)
	workflow, err := parseWorkflowPayload(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid workflow JSON: %v", err)})
		return
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		name = "edited-workflow.json"
	}
	preview := prettyWorkflowPayload(raw)
	storeLoadedWorkflow(s, name, raw, len(workflow.Steps), preview)
	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"name":      name,
		"size":      len(raw),
		"stepTotal": len(workflow.Steps),
		"preview":   preview,
	})
}

func storeLoadedWorkflow(s *session.Session, name string, raw []byte, stepTotal int, preview string) {
	if s == nil {
		return
	}
	withSessionLock(s, func() {
		s.LoadedWorkflow = &session.LoadedWorkflow{
			Name:      name,
			Payload:   append([]byte(nil), raw...),
			Preview:   preview,
			StepTotal: stepTotal,
			LoadedAt:  time.Now(),
		}
	})
	clearWorkflowStatus(s)
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
		s.Recording = nil
		s.LoadedWorkflow = nil
		s.Playback = nil
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
	recStatus := recordingStatusSnapshot(s)
	eventList := playbackEvents(s)
	events := make([]gin.H, 0, len(eventList))
	for _, event := range eventList {
		events = append(events, gin.H{
			"time":    event.Time.Format("15:04:05"),
			"message": event.Message,
		})
	}
	chaosState := chaosStateSnapshot(s)
	chaosEvents := make([]gin.H, 0)
	chaosAttempts := make([]gin.H, 0)
	var chaosLastAttempt gin.H
	chaosStepLabel := ""
	chaosCompleted := false
	chaosStoppedAt := ""
	if chaosState != nil {
		chaosCompleted = !chaosState.Active && chaosState.StepsRun > 0
		if !chaosState.StoppedAt.IsZero() {
			chaosStoppedAt = chaosState.StoppedAt.Format(time.RFC3339)
		}
		if len(chaosState.RecentAttempts) > 0 {
			chaosEvents = make([]gin.H, 0, len(chaosState.RecentAttempts))
			chaosAttempts = make([]gin.H, 0, len(chaosState.RecentAttempts))
			for _, attempt := range chaosState.RecentAttempts {
				chaosEvents = append(chaosEvents, gin.H{
					"time":    attempt.Time.Format("15:04:05"),
					"message": formatChaosAttemptMessage(attempt),
				})
				chaosAttempts = append(chaosAttempts, sessionChaosAttemptToJSON(attempt))
			}
		}
		if chaosState.LastAttempt != nil {
			chaosLastAttempt = sessionChaosAttemptToJSON(*chaosState.LastAttempt)
		} else if n := len(chaosState.RecentAttempts); n > 0 {
			chaosLastAttempt = sessionChaosAttemptToJSON(chaosState.RecentAttempts[n-1])
		}
		if chaosState.StepsRun > 0 {
			if chaosCompleted {
				chaosStepLabel = fmt.Sprintf("Chaos completed after %d attempts", chaosState.StepsRun)
			} else {
				chaosStepLabel = fmt.Sprintf("Chaos attempt %d", chaosState.StepsRun)
			}
			if !chaosCompleted && chaosState.LastAttempt != nil && chaosState.LastAttempt.AIDKey != "" {
				chaosStepLabel = fmt.Sprintf("%s: %s", chaosStepLabel, chaosState.LastAttempt.AIDKey)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"recordingActive":         recStatus.Active,
		"recordingStartedAt":      recStatus.StartedAt,
		"recordingSteps":          recStatus.Steps,
		"recordingFile":           recStatus.File,
		"loadedWorkflowStepTotal": loadedWorkflowStepTotal(s),
		"playbackActive":          playbackActive(s),
		"playbackPaused":          playbackPaused(s),
		"playbackCompleted":       playbackCompleted(s),
		"playbackStartedAt":       playbackStartedAt(s),
		"playbackMode":            playbackMode(s),
		"playbackStep":            playbackStepIndex(s),
		"playbackStepTotal":       playbackStepTotal(s),
		"playbackStepType":        playbackStepType(s),
		"playbackStepLabel":       playbackStepLabel(s),
		"playbackDelayRange":      playbackDelayRangeLabel(s),
		"playbackDelayApplied":    playbackDelayAppliedLabel(s),
		"playbackEvents":          events,
		"chaosActive":             chaosState != nil && chaosState.Active,
		"chaosStepsRun":           chaosStateStepsRun(chaosState),
		"chaosMaxSteps":           chaosStateMaxSteps(chaosState),
		"chaosTimeBudgetMs":       chaosStateTimeBudgetMs(chaosState),
		"chaosTransitions":        chaosStateTransitions(chaosState),
		"chaosUniqueScreens":      chaosStateUniqueScreens(chaosState),
		"chaosUniqueInputs":       chaosStateUniqueInputs(chaosState),
		"chaosUniqueKeys":         chaosStateUniqueKeys(chaosState),
		"chaosLoadedRunID":        chaosStateLoadedRunID(chaosState),
		"chaosError":              chaosStateError(chaosState),
		"chaosStartedAt":          chaosStateStartedAt(chaosState),
		"chaosStepLabel":          chaosStepLabel,
		"chaosLastAttempt":        chaosLastAttempt,
		"chaosAttempts":           chaosAttempts,
		"chaosEvents":             chaosEvents,
		"chaosMindMap":            chaosStateMindMap(chaosState),
		"chaosCompleted":          chaosCompleted,
		"chaosStoppedAt":          chaosStoppedAt,
	})
}

func formatChaosAttemptMessage(attempt session.ChaosAttempt) string {
	base := fmt.Sprintf(
		"Attempt %d AID %s writes %d/%d",
		attempt.Attempt,
		defaultChaosAIDLabel(attempt.AIDKey),
		attempt.FieldsWritten,
		attempt.FieldsTargeted,
	)
	if attempt.Transitioned {
		base += " transitioned"
	}
	if attempt.Error != "" {
		base += fmt.Sprintf(" error: %s", attempt.Error)
	}
	return base
}

func defaultChaosAIDLabel(aid string) string {
	aid = strings.TrimSpace(aid)
	if aid == "" {
		return "?"
	}
	return aid
}

func chaosStateStepsRun(state *session.ChaosState) int {
	if state == nil {
		return 0
	}
	return state.StepsRun
}

func chaosStateTransitions(state *session.ChaosState) int {
	if state == nil {
		return 0
	}
	return state.Transitions
}

func chaosStateMaxSteps(state *session.ChaosState) int {
	if state == nil {
		return 0
	}
	return state.MaxSteps
}

func chaosStateTimeBudgetMs(state *session.ChaosState) int64 {
	if state == nil || state.TimeBudget <= 0 {
		return 0
	}
	return state.TimeBudget.Milliseconds()
}

func chaosStateUniqueScreens(state *session.ChaosState) int {
	if state == nil {
		return 0
	}
	return state.UniqueScreens
}

func chaosStateUniqueInputs(state *session.ChaosState) int {
	if state == nil {
		return 0
	}
	return state.UniqueInputs
}

func chaosStateUniqueKeys(state *session.ChaosState) int {
	if state == nil {
		return 0
	}
	return len(state.AIDKeyCounts)
}

func chaosStateLoadedRunID(state *session.ChaosState) string {
	if state == nil {
		return ""
	}
	return state.LoadedRunID
}

func chaosStateError(state *session.ChaosState) string {
	if state == nil {
		return ""
	}
	return state.Error
}

func chaosStateStartedAt(state *session.ChaosState) string {
	if state == nil || state.StartedAt.IsZero() {
		return ""
	}
	return state.StartedAt.Format(time.RFC3339)
}

func chaosStateMindMap(state *session.ChaosState) interface{} {
	if state == nil || len(state.MindMap) == 0 {
		return nil
	}
	var decoded interface{}
	if err := json.Unmarshal(state.MindMap, &decoded); err != nil {
		return nil
	}
	return decoded
}

type settingsPayload struct {
	Settings map[string]string `json:"settings"`
}

type settingsResponse struct {
	Settings map[string]string `json:"settings"`
	Masked   []string          `json:"masked,omitempty"`
}

type themeSavePayload struct {
	Name        string            `json:"name"`
	CustomTheme map[string]string `json:"customTheme"`
	// Publish asks for the theme to go into the set every account sees,
	// rather than the caller's own. Administrators only.
	Publish bool `json:"publish"`
}

type themeListItem struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	FileName    string            `json:"fileName"`
	CustomTheme map[string]string `json:"customTheme"`
}

func (app *App) SettingsHandler(c *gin.Context) {
	if !app.hasSession(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}
	// A valid session is already required above. Sensitive values (e.g.
	// S3270_KEY_PASSWORD) must never be gated on "the request came from
	// loopback" alone. That check was worthless when the server always bound
	// to 127.0.0.1 (every request satisfied it by construction), and it is
	// worse than worthless now that WEBUI_BIND can expose other interfaces:
	// behind Docker's port forwarding the peer address is the bridge gateway,
	// so a loopback test would either pass for every remote client or fail for
	// every one, depending on the network mode. The session check is the real
	// authorization signal.
	switch c.Request.Method {
	case http.MethodGet:
		app.writeSettingsResponse(c)
	case http.MethodPost:
		app.updateSettings(c)
	default:
		c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "method not allowed"})
	}
}

// hasSession reports whether the request carries a valid session cookie.
// The session ID is a 128-bit crypto/rand value (see session.generateID),
// so this is a real authorization signal — unlike the loopback check this
// used to be OR'd with, which gated nothing: it was always true back when the
// server only ever bound to 127.0.0.1, and it is not a trustworthy signal now
// that WEBUI_BIND can put the listener on another interface.
func (app *App) hasSession(c *gin.Context) bool {
	return app.getSession(c) != nil
}

func (app *App) writeSettingsResponse(c *gin.Context) {
	settings, masked, err := app.settingsSnapshot()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load settings"})
		return
	}
	c.JSON(http.StatusOK, settingsResponse{
		Settings: settings,
		Masked:   masked,
	})
}

// settingsDefaults enumerates every key the settings API understands, with the
// value used when .env does not define it. It doubles as the source of the
// write allowlist, so a key added here becomes writable and a key removed here
// stops being writable — the two cannot drift apart.
func settingsDefaults() map[string]string {
	defaults := make(map[string]string)
	for _, spec := range config.S3270OptionSpecs() {
		defaults[spec.EnvVar] = spec.DefaultVal
	}
	defaults["ALLOW_LOG_ACCESS"] = "false"
	defaults["APP_USE_KEYPAD"] = "false"
	defaults["CHAOS_MAX_STEPS"] = "100"
	defaults["CHAOS_TIME_BUDGET_SEC"] = "300"
	defaults["CHAOS_STEP_DELAY_SEC"] = "0.5"
	defaults["CHAOS_SEED"] = "0"
	defaults["CHAOS_MAX_FIELD_LENGTH"] = "40"
	defaults["CHAOS_SCREEN_DEDUP_SIMILARITY"] = "0.95"
	defaults["CHAOS_LEARNED_INPUT_REUSE_BIAS"] = "1.0"
	defaults["CHAOS_LEARNED_KEY_REUSE_BIAS"] = "1.0"
	defaults["CHAOS_EXPORT_SUCCESS_BALANCE"] = "1.0"
	defaults["CHAOS_OUTPUT_FILE"] = ""
	defaults["CHAOS_TRANSITION_LOG_PATH"] = ""
	defaults["CHAOS_FORCE_OVERRIDE_EXISTING_INPUTS"] = "true"
	defaults["CHAOS_EXCLUDE_NO_PROGRESS_EVENTS"] = "true"
	return defaults
}

// settingsSnapshot returns the effective settings with every secret value
// replaced by settingsSecretPlaceholder, plus the names of the secrets that
// are actually set.
//
// There is deliberately no way to read a secret back out. The UI only needs to
// know whether one is configured, and the previous includeSensitive=true
// escape hatch handed S3270_KEY_PASSWORD in cleartext to anyone who could
// reach the endpoint.
func (app *App) settingsSnapshot() (map[string]string, []string, error) {
	settings := make(map[string]string)
	for key, value := range settingsDefaults() {
		settings[key] = value
	}

	if app.envPath != "" {
		values, err := config.ReadDotEnv(app.envPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, nil, err
			}
		} else {
			for key, value := range values {
				settings[key] = value
			}
		}
	}

	masked := []string{}
	for key := range settingsSecretKeys {
		value, ok := settings[key]
		if !ok || value == "" {
			continue
		}
		settings[key] = settingsSecretPlaceholder
		masked = append(masked, key)
	}
	sort.Strings(masked)

	return settings, masked, nil
}

func (app *App) updateSettings(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request"})
		return
	}

	var payload settingsPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	settings := payload.Settings
	if settings == nil {
		var raw map[string]string
		if err := json.Unmarshal(data, &raw); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid settings payload"})
			return
		}
		settings = raw
	}

	if len(settings) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no settings provided"})
		return
	}

	specs := make(map[string]config.S3270OptionSpec)
	for _, spec := range config.S3270OptionSpecs() {
		specs[spec.EnvVar] = spec
	}

	allowed := settingsWritableKeys()
	errorsByKey := make(map[string]string)
	for key, value := range settings {
		if !settingsKeyWritable(key, allowed) {
			errorsByKey[key] = "unknown setting"
			continue
		}
		if err := validateSettingValue(key, value, specs); err != nil {
			errorsByKey[key] = err.Error()
		}
	}
	if len(errorsByKey) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid settings",
			"details": errorsByKey,
		})
		return
	}

	changed := make([]string, 0, len(settings))
	for key, value := range settings {
		// The client echoes back the placeholder it was served for a secret it
		// did not change. Writing it would replace the secret with the mask.
		if settingsSecretKeys[key] && value == settingsSecretPlaceholder {
			continue
		}
		if err := config.UpsertDotEnvValue(app.envPath, key, value); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to persist %s", key)})
			return
		}
		app.applyRuntimeSetting(key, value)
		changed = append(changed, key)
	}

	// The keys, never the values: a setting can hold a proxy string or a
	// keyfile password, and the trail is read by every administrator.
	sort.Strings(changed)
	app.auditRequest(c, audit.EventSettingsSaved, audit.Success, "",
		map[string]string{"keys": strings.Join(changed, ",")})

	app.writeSettingsResponse(c)
}

func (app *App) RestartHandler(c *gin.Context) {
	if !app.hasSession(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}
	if err := scheduleSelfRestart(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to restart: %v", err)})
		return
	}
	// Recorded before the process goes: a restart ends every other account's
	// live mainframe sessions, so who asked for it is worth knowing.
	app.auditRequest(c, audit.EventAppRestarted, audit.Success, "", nil)
	c.JSON(http.StatusOK, gin.H{"restarting": true})

	go func() {
		time.Sleep(250 * time.Millisecond)
		if app.shutdown != nil {
			app.shutdown()
			return
		}
		os.Exit(0)
	}()
}

func (app *App) ThemeSaveHandler(c *gin.Context) {
	if !app.hasSession(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}
	var payload themeSavePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "theme name is required"})
		return
	}

	if len(payload.CustomTheme) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "customTheme is required"})
		return
	}

	fileStem := sanitizeThemeFileName(name)
	if fileStem == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "theme name is invalid"})
		return
	}

	dir := app.themesDirFor(c)
	if payload.Publish {
		shared := app.sharedThemesDirFor(c)
		if shared != "" {
			if !principalFrom(c).IsAdmin() {
				c.JSON(http.StatusForbidden, gin.H{
					"error": "publishing a theme to everyone requires an administrator account",
				})
				return
			}
			dir = shared
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to create themes folder: %v", err)})
		return
	}

	filePath := filepath.Join(dir, fileStem+".json")
	content := map[string]interface{}{
		"schema":      "3270web.custom-theme.v1",
		"name":        name,
		"theme":       "theme-custom",
		"customTheme": payload.CustomTheme,
		"savedAt":     time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode theme JSON"})
		return
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to write theme file: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"saved":      true,
		"id":         themeIDFromFile(filepath.Base(filePath)),
		"name":       name,
		"path":       filePath,
		"themesDir":  dir,
		"fileName":   filepath.Base(filePath),
		"themeType":  "custom",
		"savedAtUtc": time.Now().UTC().Format(time.RFC3339),
	})
}

// listFileThemes reads the custom themes on disk. Shared by ThemeListHandler
// and by the connect page, which needs the same list before any session
// exists and so cannot go through the (deliberately session-gated) endpoint.
func (app *App) listFileThemes(c *gin.Context) ([]themeListItem, error) {
	// Published first, so a theme of the caller's own with the same name
	// replaces it rather than appearing twice.
	items, err := app.readThemeDir(app.sharedThemesDirFor(c))
	if err != nil {
		return nil, err
	}
	own, err := app.readThemeDir(app.themesDirFor(c))
	if err != nil {
		return nil, err
	}

	byName := make(map[string]themeListItem, len(items)+len(own))
	order := make([]string, 0, len(items)+len(own))
	for _, group := range [][]themeListItem{items, own} {
		for _, item := range group {
			key := strings.ToLower(strings.TrimSpace(item.Name))
			if _, seen := byName[key]; !seen {
				order = append(order, key)
			}
			byName[key] = item
		}
	}
	out := make([]themeListItem, 0, len(order))
	for _, key := range order {
		out = append(out, byName[key])
	}
	return out, nil
}

// readThemeDir reads one themes directory. An empty path or a missing
// directory is an empty list, not an error: a deployment with a single
// operator has no published set, and nobody has to have saved a theme.
func (app *App) readThemeDir(dir string) ([]themeListItem, error) {
	if strings.TrimSpace(dir) == "" {
		return []themeListItem{}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []themeListItem{}, nil
		}
		return nil, err
	}

	items := make([]themeListItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var raw struct {
			Name        string            `json:"name"`
			CustomTheme map[string]string `json:"customTheme"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		if len(raw.CustomTheme) == 0 {
			continue
		}
		displayName := strings.TrimSpace(raw.Name)
		if displayName == "" {
			displayName = strings.TrimSuffix(name, filepath.Ext(name))
		}
		items = append(items, themeListItem{
			ID:          themeIDFromFile(name),
			Name:        displayName,
			FileName:    name,
			CustomTheme: raw.CustomTheme,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	return items, nil
}

func (app *App) ThemeListHandler(c *gin.Context) {
	if !app.hasSession(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}
	items, err := app.listFileThemes(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read themes folder: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"themes": items})
}

func sanitizeThemeFileName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range trimmed {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isDigit := r >= '0' && r <= '9'
		if isLetter || isDigit {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if r == '-' || r == '_' || r == ' ' {
			if !lastUnderscore {
				b.WriteRune('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return ""
	}
	return out
}

func themeIDFromFile(fileName string) string {
	base := strings.TrimSpace(fileName)
	if base == "" {
		return ""
	}
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return "theme-file:" + base
}

// themesDirFor is where this caller's own themes are written.
func (app *App) themesDirFor(c *gin.Context) string {
	if strings.TrimSpace(app.baseDir) == "" {
		return filepath.Join(resolveStateDir(), "themes")
	}
	return app.dataScopeForRequest(c).themesDir()
}

// sharedThemesDirFor is the published set, or empty under a single operator
// where the caller's own directory already is it.
func (app *App) sharedThemesDirFor(c *gin.Context) string {
	if strings.TrimSpace(app.baseDir) == "" {
		return ""
	}
	return app.dataScopeForRequest(c).sharedThemesDir()
}

func scheduleSelfRestart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := os.Args[1:]
	workingDir := filepath.Dir(exe)

	if runtime.GOOS == "windows" {
		script := buildWindowsRestartScript(exe, workingDir, args)
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
		return cmd.Start()
	}

	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(exe))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	commandLine := "sleep 1; " + strings.Join(parts, " ")
	cmd := exec.Command("sh", "-c", commandLine)
	cmd.Dir = workingDir
	return cmd.Start()
}

func buildWindowsRestartScript(exe, workingDir string, args []string) string {
	quotedExe := powerShellSingleQuote(exe)
	quotedDir := powerShellSingleQuote(workingDir)
	if len(args) == 0 {
		return fmt.Sprintf("Start-Sleep -Milliseconds 900; Start-Process -FilePath '%s' -WorkingDirectory '%s'", quotedExe, quotedDir)
	}

	quotedArgs := make([]string, 0, len(args))
	for _, arg := range args {
		quotedArgs = append(quotedArgs, fmt.Sprintf("'%s'", powerShellSingleQuote(arg)))
	}
	return fmt.Sprintf(
		"Start-Sleep -Milliseconds 900; Start-Process -FilePath '%s' -WorkingDirectory '%s' -ArgumentList @(%s)",
		quotedExe,
		quotedDir,
		strings.Join(quotedArgs, ","),
	)
}

func powerShellSingleQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (app *App) applyRuntimeSetting(key, value string) {
	switch key {
	case "ALLOW_LOG_ACCESS":
		if strings.EqualFold(value, "true") {
			_ = os.Setenv(key, "true")
		} else {
			_ = os.Unsetenv(key)
		}
	case "APP_USE_KEYPAD":
		if value == "" {
			_ = os.Unsetenv(key)
		} else {
			_ = os.Setenv(key, strings.ToLower(value))
		}
	}
}

// settingsSecretPlaceholder stands in for a secret's value in every settings
// response. The UI posts the whole form back on save, so a masked field would
// otherwise be written back verbatim and overwrite the real secret with the
// mask. updateSettings treats an incoming value equal to this placeholder as
// "leave it alone", which is what makes masking safe to do unconditionally.
const settingsSecretPlaceholder = "********"

// settingsSecretKeys hold values that are never returned to a client. The
// response's Masked list still names them when they are set, so the UI can
// show that a value exists without being told what it is.
var settingsSecretKeys = map[string]bool{
	"S3270_KEY_PASSWORD": true,
	// Proxy specifications routinely carry credentials in the userinfo part.
	"S3270_PROXY": true,
}

// settingsExtraOptionsPrefix namespaces the per-field option lists the UI
// persists alongside the settings it owns (see extraOptionsPrefix in ui.js).
// The suffix must name a real s3270 option, so these expand the writable set
// without opening it up.
const settingsExtraOptionsPrefix = "APP_SETTINGS_OPTIONS_"

// settingsDeniedKeys can never be written through the settings API, whatever
// else says otherwise.
//
// .env is promoted into the process environment at startup, so a write here is
// a write to the environment of the next run. API_TOKEN is the sharpest case:
// it is unset by default and LoadDotEnv only fills keys that are absent, so
// before this list existed a caller could name their own API token, restart the
// app, and come back holding the instance-wide API credential.
var settingsDeniedKeys = map[string]bool{
	"API_TOKEN":         true,
	"COPILOT_AUTH_PATH": true,
	// The authentication settings themselves. Naming a different account
	// store, or turning the mode off, would be a way to walk out through the
	// gate using a page that sits behind it.
	"AUTH_MODE":       true,
	"USERS_PATH":      true,
	"API_TOKENS_PATH": true,
	"AUDIT_LOG_PATH":  true,
	// The two limits. Both are the kind of setting a caller would like to
	// raise before doing something the limit exists to bound.
	"ALLOWED_HOSTS":        true,
	"RATE_LIMIT_CONNECT":   true,
	"RATE_LIMIT_CHAOS":     true,
	"RATE_LIMIT_TRANSFER":  true,
	"RATE_LIMIT_AI":        true,
	"AUTH_BIND_SESSION_IP": true,
	"AUTH_SESSION_IDLE":    true,
	"AUTH_SESSION_MAX":     true,
	"MCP_TOOLS":            true,
	"MCP_ALLOWED_HOSTS":    true,
	"WEBUI_BIND":           true,
	"WEBUI_PORT":           true,
	"PATH":                 true,
	"LD_PRELOAD":           true,
	"LD_LIBRARY_PATH":      true,
	"GIN_MODE":             true,
	"ALLOW_SAMPLE_APPS":    true,
	"CHAOS_RUNS_DIR":       true,
	"XDG_CONFIG_HOME":      true,
	"HOME":                 true,
	"TMPDIR":               true,
	"GOTRACEBACK":          true,
	"S3270_EXEC_PATH":      true,
	"S3270_TRACE_FILE":     true,
	"S3270_SCRIPT_PORT":    true,
	"S3270_HTTPD":          true,
	"S3270_CHILD_SCRIPT":   true,
}

// settingsWritableKeys is the allowlist for POST /api/settings.
//
// Validation used to check the *value* of keys it recognised and then fall
// through to "no error" for every key it did not, so any key at all could be
// written into .env. The set of keys the app actually understands is already
// enumerated by settingsDefaults, so that is the natural allowlist.
func settingsWritableKeys() map[string]bool {
	allowed := make(map[string]bool)
	for key := range settingsDefaults() {
		if !settingsDeniedKeys[key] {
			allowed[key] = true
		}
	}
	return allowed
}

// settingsKeyWritable reports whether key may be persisted.
func settingsKeyWritable(key string, allowed map[string]bool) bool {
	if settingsDeniedKeys[key] {
		return false
	}
	if allowed[key] {
		return true
	}
	if suffix, ok := strings.CutPrefix(key, settingsExtraOptionsPrefix); ok {
		return allowed[suffix] && !settingsDeniedKeys[suffix]
	}
	return false
}

func validateSettingValue(key, value string, specs map[string]config.S3270OptionSpec) error {
	if value == "" {
		return nil
	}

	if key == "ALLOW_LOG_ACCESS" || key == "APP_USE_KEYPAD" || key == "CHAOS_EXCLUDE_NO_PROGRESS_EVENTS" || key == "CHAOS_FORCE_OVERRIDE_EXISTING_INPUTS" {
		if !isStrictBool(value) {
			return fmt.Errorf("must be true or false")
		}
		return nil
	}
	if key == "CHAOS_SCREEN_DEDUP_SIMILARITY" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || parsed <= 0 || parsed > 1 {
			return fmt.Errorf("must be a number > 0 and <= 1")
		}
		return nil
	}
	if key == "CHAOS_LEARNED_INPUT_REUSE_BIAS" || key == "CHAOS_LEARNED_KEY_REUSE_BIAS" || key == "CHAOS_LEARNED_REUSE_BIAS" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || parsed < 0 || parsed > 1 {
			return fmt.Errorf("must be a number >= 0 and <= 1")
		}
		return nil
	}
	if key == "CHAOS_EXPORT_SUCCESS_BALANCE" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || parsed <= 0 || parsed > 1 {
			return fmt.Errorf("must be a number > 0 and <= 1")
		}
		return nil
	}

	if allowed := s3270EnumValues[key]; len(allowed) > 0 {
		if _, ok := allowed[strings.ToLower(value)]; !ok {
			return fmt.Errorf("must be one of %s", strings.Join(sortedKeys(allowed), ", "))
		}
		return nil
	}

	if numeric, ok := s3270NumericValidators[key]; ok {
		if err := numeric(value); err != nil {
			return err
		}
		return nil
	}

	if spec, ok := specs[key]; ok && spec.Kind == config.S3270OptionBool {
		if !isStrictBool(value) {
			return fmt.Errorf("must be true or false")
		}
	}

	return nil
}

func isStrictBool(value string) bool {
	switch strings.ToLower(value) {
	case "true", "false":
		return true
	default:
		return false
	}
}

var s3270EnumValues = map[string]map[string]struct{}{
	"S3270_CERT_FILE_TYPE": {
		"pem":  {},
		"asn1": {},
	},
	"S3270_KEY_FILE_TYPE": {
		"pem":  {},
		"asn1": {},
	},
	"S3270_TLS_MIN_PROTOCOL": {
		"tls1.0": {},
		"tls1.1": {},
		"tls1.2": {},
		"tls1.3": {},
	},
	"S3270_TLS_MAX_PROTOCOL": {
		"tls1.0": {},
		"tls1.1": {},
		"tls1.2": {},
		"tls1.3": {},
	},
}

var s3270NumericValidators = map[string]func(string) error{
	"S3270_PORT": func(value string) error {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 65535 {
			return fmt.Errorf("must be a valid TCP port")
		}
		return nil
	},
	"S3270_CONNECT_TIMEOUT": func(value string) error {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return fmt.Errorf("must be a non-negative number")
		}
		return nil
	},
	"S3270_TRACE_FILE_SIZE": func(value string) error {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return fmt.Errorf("must be a non-negative number")
		}
		return nil
	},
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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

func (app *App) KeypadPrefHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	enabled := parseBoolFormValue(c.PostForm("keypad"))
	withSessionLock(s, func() {
		app.ensurePrefs(s)
		s.Prefs.UseKeypad = enabled
	})

	_ = os.Setenv("APP_USE_KEYPAD", strconv.FormatBool(enabled))
	persisted := true
	if app.envPath != "" {
		if err := config.UpsertDotEnvValue(app.envPath, "APP_USE_KEYPAD", strconv.FormatBool(enabled)); err != nil {
			persisted = false
			log.Printf("Warning: could not persist APP_USE_KEYPAD to .env: %v", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"useKeypad": enabled,
		"persisted": persisted,
	})
}

func (app *App) LogsAccessHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	if c.Request.Method == http.MethodPost {
		enabled := c.PostForm("enabled") == "true"
		if enabled {
			_ = os.Setenv("ALLOW_LOG_ACCESS", "true")
		} else {
			_ = os.Unsetenv("ALLOW_LOG_ACCESS")
		}
		// The debug log holds every account's activity, so turning access to
		// it on is itself a security-relevant act.
		app.auditRequest(c, audit.EventLogAccessSet, audit.Success, "",
			map[string]string{"enabled": strconv.FormatBool(enabled)})
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled":        os.Getenv("ALLOW_LOG_ACCESS") == "true",
		"verboseLogging": s.Prefs.VerboseLogging,
	})
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

func (app *App) updateFields(c *gin.Context, h host.Host) {
	screen := h.GetScreen()
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

// recordScreenForStep stores the screen the operator is looking at, tagged
// with the index where this screen's group of steps begins. Capped so a long
// recording session cannot grow without bound.
const maxRecordedScreens = 200

func recordScreenForStep(s *session.Session, h host.Host) {
	if s == nil || h == nil {
		return
	}
	screen := h.GetScreen()
	if screen == nil {
		return
	}
	text := screen.Text()
	if strings.TrimSpace(text) == "" {
		return
	}
	withSessionLock(s, func() {
		if s.Recording == nil || !s.Recording.Active {
			return
		}
		if len(s.Recording.Screens) >= maxRecordedScreens {
			return
		}
		s.Recording.Screens = append(s.Recording.Screens, session.RecordedScreen{
			StepIndex: len(s.Recording.Steps),
			Text:      text,
			Rows:      screen.Height,
			Cols:      screen.Width,
		})
	})
}

func recordFieldUpdates(s *session.Session, h host.Host) {
	if s == nil || h == nil {
		return
	}
	screen := h.GetScreen()
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
	step := session.WorkflowStep{Type: "PressEnter"}
	if trimmed != "" && !strings.EqualFold(trimmed, "enter") {
		mapped := workflowStepForKey(key)
		if mapped == nil {
			return
		}
		step = *mapped
	}
	now := time.Now()
	withSessionLock(s, func() {
		if s.Recording == nil || !s.Recording.Active {
			return
		}
		updateRecordingDelayStats(s.Recording, now)
		s.Recording.Steps = append(s.Recording.Steps, step)
	})
}

func updateRecordingDelayStats(r *session.WorkflowRecording, now time.Time) {
	if r == nil {
		return
	}
	if !r.LastStepAt.IsZero() {
		delay := now.Sub(r.LastStepAt).Seconds()
		if delay > 0 {
			if r.DelaySamples == 0 || delay < r.DelayMin {
				r.DelayMin = delay
			}
			if r.DelaySamples == 0 || delay > r.DelayMax {
				r.DelayMax = delay
			}
			r.DelaySamples++
		}
	}
	r.LastStepAt = now
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
	everyStepDelay := &session.WorkflowDelayRange{Min: 0.1, Max: 0.3}
	if s.Recording.DelaySamples > 0 {
		everyStepDelay = &session.WorkflowDelayRange{
			Min: roundDelaySeconds(s.Recording.DelayMin),
			Max: roundDelaySeconds(s.Recording.DelayMax),
		}
	}
	return &WorkflowConfig{
		Host:            host,
		Port:            port,
		EveryStepDelay:  everyStepDelay,
		OutputFilePath:  s.Recording.OutputFilePath,
		RampUpBatchSize: 50,
		RampUpDelay:     1.5,
		EndOfTaskDelay:  &session.WorkflowDelayRange{Min: 60, Max: 120},
		Steps:           s.Recording.Steps,
	}
}

func roundDelaySeconds(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Round(v*1000) / 1000
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
				Name:      upload.Name,
				Payload:   upload.Payload,
				Preview:   preview,
				StepTotal: len(upload.Config.Steps),
				LoadedAt:  time.Now(),
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

func loadedWorkflowStepTotal(s *session.Session) int {
	if s == nil {
		return 0
	}
	s.Lock()
	defer s.Unlock()
	if s.LoadedWorkflow == nil {
		return 0
	}
	return s.LoadedWorkflow.StepTotal
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

func playbackStartedAt(s *session.Session) string {
	if s == nil {
		return ""
	}
	s.Lock()
	defer s.Unlock()
	if s.Playback == nil || s.Playback.StartedAt.IsZero() {
		return ""
	}
	return s.Playback.StartedAt.Format(time.RFC3339)
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
	// This path is reached from workflow-driven reconnects (PlayWorkflowHandler
	// /DebugWorkflowHandler), where hostname comes from an uploaded workflow's
	// "Host" field — unlike startHostSession (used by the manual /connect
	// form), it previously validated only via parseHostPort (a plain string
	// split, no IP restriction at all), so a crafted workflow file could
	// target loopback/link-local/unspecified addresses that isValidHostname
	// exists specifically to block.
	if !isValidHostname(hostname) {
		return fmt.Errorf("invalid hostname format: %q", hostname)
	}
	// The other way to reach a host: keep the session and re-point it. A
	// workflow file names its own host, and so does the MCP connect tool, so
	// the allowlist has to be checked here as well as at creation — a fence
	// with one gate open is not a fence.
	if err := checkHostAllowed(hostname); err != nil {
		return err
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

// getSession resolves the terminal session this request may act on.
//
// Every browser handler goes through here, which is why the ownership check
// belongs here and not in each of them: one edit covers the whole surface, and
// a route added later inherits it without anyone remembering to.
//
// A session the caller does not own is reported as absent rather than
// forbidden. Handlers already treat nil as "no session" and answer accordingly,
// and distinguishing "not yours" from "does not exist" would confirm to a
// caller that a session ID they guessed is real.
func (app *App) getSession(c *gin.Context) *session.Session {
	id := sessionIDFor(c)
	if id == "" {
		return nil
	}
	s, ok := app.SessionManager.GetSession(id)
	if !ok {
		return nil
	}
	if !app.mayUseSession(c, s) {
		return nil
	}
	return s
}

func (app *App) connectToHost(c *gin.Context, hostname string) error {
	sess, err := app.startHostSession(c, hostname)
	if err != nil {
		return err
	}
	setSessionCookie(c, sessionCookieName, sess.ID)
	setSessionCookie(c, lastTargetCookieName, strings.TrimSpace(hostname))
	// Put it on the tab bar. Connecting from the front page is just opening
	// the first tab.
	app.rememberSession(c, sess.ID)
	return nil
}

// connectWithProfile opens a session using a named connection profile and
// makes it the active one.
func (app *App) connectWithProfile(c *gin.Context, profile ConnectionProfile) error {
	sess, err := app.startHostSessionWithProfile(c, profile.displayTarget(), &profile)
	if err != nil {
		return err
	}
	setSessionCookie(c, sessionCookieName, sess.ID)
	setSessionCookie(c, lastTargetCookieName, profile.displayTarget())
	app.rememberSession(c, sess.ID)
	return nil
}

// startHostSession creates and starts a host connection and registers a
// session for it, but does not associate the session with any browser cookie.
// Used by both the cookie-based UI (via connectToHost) and the cookie-free
// REST API.
// startHostSession opens a session owned by whoever made the request.
//
// The context rather than a bare owner ID, because every caller derived the
// owner from it the same way — and because the audit line for "somebody
// connected to that host" wants the address it came from as well as the
// account. Tests that have no request pass nil, which is the unowned case.
func (app *App) startHostSession(c *gin.Context, hostname string) (*session.Session, error) {
	return app.startHostSessionWithProfile(c, hostname, nil)
}

// startHostSessionWithProfile starts a session, optionally applying a
// connection profile's per-host overrides (TLS, LU, model, code page) on top
// of the server-wide s3270 defaults. Passing nil reproduces the previous
// behaviour exactly.
func (app *App) startHostSessionWithProfile(c *gin.Context, hostname string, profile *ConnectionProfile) (*session.Session, error) {
	ownerID := principalFrom(c).UserID
	if !isValidHostname(hostname) {
		app.auditRequest(c, audit.EventSessionDenied, audit.Denied, hostname,
			map[string]string{"reason": "invalid hostname"})
		return nil, fmt.Errorf("invalid hostname format: %q", hostname)
	}
	if err := checkHostAllowed(hostname); err != nil {
		app.auditRequest(c, audit.EventSessionDenied, audit.Denied, hostname,
			map[string]string{"reason": "host not allowed"})
		return nil, err
	}
	// Rate-limited here rather than on the routes, because three of them open
	// sessions — the connect form, the tab bar and the REST API — and each one
	// starts an s3270 subprocess. MAX_SESSIONS_PER_USER bounds how many exist
	// at once; this bounds how fast they can be spawned, which is the part a
	// loop that opens and closes in a tight cycle would otherwise ignore.
	if perMinute := rateLimitFor(rateConnect); perMinute > 0 && c != nil {
		if ok, retryIn := app.rateLimiter().Allow(rateKey(c, rateConnect), perMinute); !ok {
			app.auditRequest(c, audit.EventSessionDenied, audit.Denied, hostname,
				map[string]string{"reason": "rate limit"})
			return nil, refuse(errRateLimited, fmt.Sprintf(
				"too many connection attempts; wait about %ds", int(retryIn.Seconds())+1))
		}
	}
	// Checked before anything is started, so a refused request costs nothing
	// and cannot leave a subprocess behind.
	if err := app.checkSessionCaps(ownerID); err != nil {
		app.auditRequest(c, audit.EventSessionDenied, audit.Denied, hostname,
			map[string]string{"reason": "session limit"})
		return nil, err
	}

	var h host.Host
	var err error

	if sampleID, samplePort, ok := parseSampleAppHost(hostname); ok {
		if samplePort > 0 && !isAllowedSampleAppPort(samplePort) {
			return nil, fmt.Errorf("invalid sample app port %d", samplePort)
		}
		execPath := resolveS3270Path(app.Config.ExecPath)
		h, err = newSampleAppHost(sampleID, samplePort, execPath, app.Config.S3270Options)
	} else if hostname == "mock" || hostname == "demo" {
		execPath := resolveS3270Path(app.Config.ExecPath)
		h, err = newSampleAppHost("app1", defaultSampleAppPort, execPath, app.Config.S3270Options)
	} else {
		execPath := resolveS3270Path(app.Config.ExecPath)
		// A profile's target carries its own TLS ("L:"), skip-verify ("Y:")
		// and LU prefixes, and its overrides are appended after the global
		// flags so the profile's value is the one s3270 ends up using.
		target := hostname
		var overrides []string
		if profile != nil {
			target = profile.s3270Target()
			overrides = profile.overrideArgs()
		}
		args := buildS3270Args(app.Config.S3270Options, "")
		args = append(args, overrides...)
		if strings.TrimSpace(target) != "" {
			args = append(args, target)
		}
		h = host.NewS3270(execPath, args...)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create host: %w", err)
	}
	if err := h.Start(); err != nil {
		return nil, fmt.Errorf("failed to start host connection: %w", err)
	}

	sess := app.SessionManager.CreateSessionFor(ownerID, h)
	sess.TargetHost, sess.TargetPort = parseHostPort(hostname)
	if profile != nil {
		sess.TargetHost = profile.Host
		sess.TargetPort = profile.Port
	}
	app.applyDefaultPrefs(sess)

	// Every creation path — the connect form, the tab bar and the REST API —
	// funnels through here, so recording it once covers all of them and a
	// path added later is covered without anyone remembering.
	app.auditRequest(c, audit.EventSessionOpened, audit.Success,
		net.JoinHostPort(sess.TargetHost, strconv.Itoa(sess.TargetPort)),
		map[string]string{"sessionId": sess.ID})

	return sess, nil
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
	// Both halves come from sessionCookieSameSite: Lax unless this deployment
	// embeds the terminal, and Secure whenever the browser reached the edge
	// over TLS — including through a proxy that terminated it, which is
	// reqsec.IsTLS's job.
	//
	// Setting Lax here directly is the tempting shortcut and it silently
	// disables embedding: a framed terminal is a cross-site context, so a Lax
	// cookie is never sent and the frame shows the connect page forever, with
	// nothing in any log to say why. See sessionCookieSameSite in embedding.go.
	sameSite, secure := sessionCookieSameSite(c)
	c.SetSameSite(sameSite)
	maxAge := 3600
	if value == "" {
		maxAge = -1
	}
	c.SetCookie(name, value, maxAge, "/", "", secure, true)
}

func getCookieValue(c *gin.Context, name string) string {
	if c == nil {
		return ""
	}
	value, err := c.Cookie(name)
	if err != nil {
		return ""
	}
	return value
}

func formatSessionTarget(s *session.Session) string {
	if s == nil {
		return ""
	}
	target := ""
	withSessionLock(s, func() {
		host := strings.TrimSpace(s.TargetHost)
		if host == "" {
			return
		}
		port := s.TargetPort
		if port <= 0 {
			port = 3270
		}
		if ip := net.ParseIP(host); ip != nil && strings.Contains(host, ":") {
			target = net.JoinHostPort(host, strconv.Itoa(port))
			return
		}
		target = fmt.Sprintf("%s:%d", host, port)
	})
	return target
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
		s.Prefs.UseKeypad = parseBoolFormValue(os.Getenv("APP_USE_KEYPAD"))
	})
}

func parseBoolFormValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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

// maxRequestBodyBytes bounds every request body this server accepts. It's
// sized generously enough to comfortably cover the largest legitimate body
// (a multipart /workflow/load upload, itself capped at maxWorkflowUploadBytes
// plus multipart header/boundary overhead) while still bounding endpoints
// that previously had no limit at all (settings, theme save, chaos
// mind-map import, ...) — without this, a request with an arbitrarily large
// body could force the server to buffer gigabytes in memory via
// c.ShouldBindJSON/c.GetRawData before ever validating anything in it.
const maxRequestBodyBytes = 8 * 1024 * 1024

// MaxBodySizeMiddleware rejects any request body larger than maxBytes.
// Gin's JSON binding, GetRawData, and multipart form parsing all read
// through c.Request.Body, so wrapping it here applies the limit uniformly
// regardless of which of those a given handler uses.
func MaxBodySizeMiddleware(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}

func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// X-Frame-Options cannot express "these three origins" — it has
		// SAMEORIGIN and nothing else — and a browser that honours both headers
		// applies whichever refuses. So when embedding is configured the
		// allowlist is stated once, in frame-ancestors, and X-Frame-Options is
		// left off rather than set to something that would contradict it.
		// Without embedding, both are sent, exactly as before.
		if !embeddingEnabled() {
			c.Header("X-Frame-Options", "SAMEORIGIN")
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", baseCSP+" "+frameAncestors())
		c.Header("Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()")
		c.Next()
	}
}

// userStore lazily opens the local account database.
func (app *App) userStore() *users.Store {
	app.usersOnce.Do(func() {
		if app.users == nil {
			path := app.usersPath
			if path == "" {
				path = filepath.Join(app.baseDir, "users.json")
			}
			app.users = users.NewStore(path)
		}
	})
	return app.users
}

// configureAuth validates the authentication settings and prepares the stores.
//
// Called from buildRouter so both entry points get the same checks, and so a
// misconfiguration stops startup instead of surfacing at the first login.
func (app *App) configureAuth() error {
	mode, err := authz.ParseMode(os.Getenv(authz.ModeEnv))
	if err != nil {
		return err
	}
	app.authMode = mode

	app.authIdleTimeout = envDuration("AUTH_SESSION_IDLE", authsession.DefaultIdleTimeout)
	app.authAbsoluteTimeout = envDuration("AUTH_SESSION_MAX", authsession.DefaultAbsoluteTimeout)
	app.authBindIP = strings.TrimSpace(os.Getenv("AUTH_BIND_SESSION_IP"))
	switch strings.ToLower(app.authBindIP) {
	case "", "auto", "true", "false":
	default:
		return fmt.Errorf("unsupported AUTH_BIND_SESSION_IP %q (supported: auto, true, false)", app.authBindIP)
	}

	if app.loginLimiter == nil {
		app.loginLimiter = newLoginLimiter()
	}
	if app.authSessions == nil {
		app.authSessions = authsession.NewStore(app.authIdleTimeout, app.authAbsoluteTimeout)
	}
	if app.authMode == authz.ModeNone {
		return nil
	}

	// A single shared token would undo the mode that was just asked for: one
	// credential, held by every automated client, reaching every account's
	// sessions. Starting anyway would leave an operator believing users were
	// separated while one environment variable said otherwise, so say it
	// plainly and point at what to do instead.
	if strings.TrimSpace(os.Getenv("API_TOKEN")) != "" {
		return fmt.Errorf("API_TOKEN cannot be used with %s=%s: a single shared token would reach every "+
			"account's sessions. Unset it and issue a token per account with `3270Web token add <username> <name>`",
			authz.ModeEnv, authz.ModeLocal)
	}

	// From here on the instance requires accounts, so refuse to start without
	// a usable store rather than presenting a login nobody can pass.
	return app.beginSetupIfNeeded()
}

// envDuration reads a duration setting, falling back when unset or unparseable.
func envDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		log.Printf("Warning: ignoring %s=%q: %v", name, raw, err)
		return fallback
	}
	return d
}
