// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/printer"
	"github.com/jnnngs/3270Web/internal/session"
)

// A 3287 printer session, bound beside the terminal session.
//
// Batch output in a 3270 shop does not come back on the display: the host
// binds a separate printer LU and sends the job there. A terminal that cannot
// bind one cannot replace the emulator on the desk, whatever else it does.
//
// See internal/printer for the process and the spool. What this file adds is
// the part that has to know about 3270Web: which terminal session a printer
// belongs to, who is allowed to see its output, and where that output goes on
// a server that has no printer attached to it.
//
// Two decisions worth stating, because both are restrictions:
//
//   - A printer session always connects to the same host, port and TLS terms
//     as the terminal session it belongs to. The caller chooses the LU and the
//     print options and nothing else. An endpoint that accepted a host would
//     be a way to make the server open connections wherever it can reach, and
//     the real case is always the printer LU on the mainframe the operator is
//     already signed in to.
//   - The jobs live as long as the terminal session. They are a working
//     output taken during a run, in the same way snapshots are, and they go
//     when the run does — which is also what keeps a forgotten session from
//     leaving a spool directory nobody can reach behind it.

// printerSpoolDirName holds one directory per session, under the data
// directory beside chaos-runs and screen-traces.
const printerSpoolDirName = "printer-jobs"

// maxPrinterJobDownloadBytes bounds one download. It is above
// printer.MaxJobBytes on purpose: every job on disk is servable, and the check
// exists for a file that grew some other way.
const maxPrinterJobDownloadBytes = printer.MaxJobBytes + (1 << 20)

// printerSessionIDPattern is what may become part of a spool path. The server
// is the only thing that mints a session ID, and it is checked anyway.
var printerSessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// pr3287Names is what the binary is called, most specific first. wpr3287 is
// the Windows build's name; the plain one is checked on every platform because
// a hand-built or cross-installed copy may use it.
func pr3287Names() []string {
	if runtime.GOOS == "windows" {
		return []string{"wpr3287.exe", "pr3287.exe"}
	}
	return []string{"pr3287"}
}

// resolvePr3287Path finds the printer binary, or returns "".
//
// Unlike s3270 there is no embedded copy to fall back on, so "not installed"
// is a normal answer and the surface says so rather than failing obscurely at
// launch. PR3287_PATH names it for an install that keeps it somewhere of its
// own.
func resolvePr3287Path(installDir string) string {
	if configured := strings.TrimSpace(os.Getenv("PR3287_PATH")); configured != "" {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured
		}
		if path, err := exec.LookPath(configured); err == nil {
			return path
		}
		// A configured path that does not resolve is a mistake worth
		// surfacing rather than papering over with a search.
		return ""
	}
	for _, name := range pr3287Names() {
		// Beside s3270, which is where an install that unpacked the x3270
		// suite together puts it.
		local := filepath.Join(".", "s3270-bin", name)
		if fileExists(local) {
			return local
		}
		if installDir != "" {
			beside := filepath.Join(installDir, name)
			if fileExists(beside) {
				return beside
			}
		}
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// printerSession is one session's printer, as the store holds it and the API
// reports it.
type printerSession struct {
	Session   string    `json:"session"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	LU        string    `json:"lu,omitempty"`
	Associate string    `json:"associate,omitempty"`
	TLS       bool      `json:"tls"`
	StartedAt time.Time `json:"started_at"`
	Running   bool      `json:"running"`
	// Error is why a printer that is no longer running stopped. Empty when it
	// was stopped deliberately.
	Error string `json:"error,omitempty"`

	proc *printer.Session
	dir  string
}

// printerStore holds at most one printer per terminal session.
//
// One is the right number for the same reason as a screen trace: a second
// printer on the same session would bind a second LU nobody asked for, and
// the panel has one place to show a status.
type printerStore struct {
	mu       sync.Mutex
	printers map[string]*printerSession
}

func newPrinterStore() *printerStore {
	return &printerStore{printers: make(map[string]*printerSession)}
}

func (s *printerStore) get(sessionID string) (*printerSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.printers[sessionID]
	return p, ok
}

// begin registers a printer, refusing if one is already running.
func (s *printerStore) begin(p *printerSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.printers[p.Session]; ok && existing.proc != nil && existing.proc.State().Running {
		return fmt.Errorf("this session already has a printer session running")
	}
	s.printers[p.Session] = p
	return nil
}

func (s *printerStore) forget(sessionID string) (*printerSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.printers[sessionID]
	delete(s.printers, sessionID)
	return p, ok
}

// printers lazily creates the store.
func (app *App) printers() *printerStore {
	app.printerOnce.Do(func() {
		if app.printerStore == nil {
			app.printerStore = newPrinterStore()
		}
	})
	return app.printerStore
}

// printerSpoolDir is where one session's jobs are written.
func (app *App) printerSpoolDir(sessionID string) (string, error) {
	if !printerSessionIDPattern.MatchString(sessionID) {
		return "", fmt.Errorf("session id is not in a form that can name a directory")
	}
	base := strings.TrimSpace(app.baseDir)
	if base == "" {
		base = resolveStateDir()
	}
	return filepath.Join(base, printerSpoolDirName, sessionID), nil
}

// stopPrinterFor ends a session's printer and removes its spool.
//
// Called from cleanupSession. Both halves matter: the process holds an LU
// bound on the host, and the directory holds output nobody can reach once the
// session it belonged to is gone.
func (app *App) stopPrinterFor(sessionID string) {
	if app.printerStore == nil {
		return
	}
	p, ok := app.printerStore.forget(sessionID)
	if !ok {
		return
	}
	if p.proc != nil {
		if err := p.proc.Stop(); err != nil {
			log.Printf("printer: stopping the printer for session %s: %v", sessionID, err)
		}
	}
	if p.dir != "" {
		if err := (printer.Spool{Dir: p.dir}).Remove(); err != nil {
			log.Printf("printer: removing the spool for session %s: %v", sessionID, err)
		}
	}
}

// snapshot fills in the live state a stored printer cannot hold: whether the
// process is still there, and what it said if it is not.
func (p *printerSession) snapshot() printerSession {
	out := *p
	out.proc = nil
	if p.proc != nil {
		state := p.proc.State()
		out.Running = state.Running
		out.Error = state.Error
	}
	return out
}

// printerRequest is what a caller may choose. Host, port and TLS are absent
// deliberately — see the note at the top of this file.
type printerRequest struct {
	// LU names the printer LU to bind. Associate instead binds the printer
	// paired with this session's own display LU, which the terminal is asked
	// for rather than the caller.
	LU         string `json:"lu"`
	Associate  bool   `json:"associate"`
	CodePage   string `json:"code_page"`
	MPP        int    `json:"mpp"`
	CRLF       bool   `json:"crlf"`
	BlankLines bool   `json:"blank_lines"`
	FFSkip     bool   `json:"ff_skip"`
	FFThru     bool   `json:"ff_thru"`
	IgnoreEOJ  bool   `json:"ignore_eoj"`
	EOJTimeout int    `json:"eoj_timeout"`
	SkipCC     bool   `json:"skip_cc"`
}

// terminalTLS asks the terminal whether its own connection is encrypted.
//
// The printer has to agree with the display about this: a host that expects
// TLS on 992 will not talk to a printer session in the clear, and the operator
// who chose a TLS profile did not choose it for half the connection. Query is
// the terminal's own account of what it negotiated, which is better than
// re-deriving it from the profile that was asked for.
func terminalTLS(h interface{ Query(string) (string, error) }) bool {
	if h == nil {
		return false
	}
	answer, err := h.Query("Tls")
	if err != nil {
		return false
	}
	// The answer is "true host" / "false", so the first word is the fact.
	fields := strings.Fields(strings.TrimSpace(answer))
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "true", "yes", "on", "1":
		return true
	}
	return false
}

// terminalLU is the LU the host bound this display session as, for the
// associated-printer case.
func terminalLU(h interface{ Query(string) (string, error) }) string {
	if h == nil {
		return ""
	}
	answer, err := h.Query("LuName")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(answer)
}

// printerVerifiesCertificate mirrors the terminal's certificate policy.
func printerVerifiesCertificate() bool {
	return !isTruthyParam(os.Getenv("S3270_NO_VERIFY_CERT"))
}

// startPrinterFor does the work behind both doors. It returns the printer as
// the caller should see it, or an HTTP status and an error to report.
func (app *App) startPrinterFor(s *session.Session, req printerRequest) (printerSession, int, error) {
	execPath := resolvePr3287Path(app.installDir)
	if execPath == "" {
		return printerSession{}, http.StatusNotImplemented, fmt.Errorf(
			"this installation has no pr3287 binary, so it cannot bind a printer LU. " +
				"Install the pr3287 package that ships with the x3270 suite, or set PR3287_PATH to it")
	}

	h := app.sessionHost(s)
	if h == nil {
		return printerSession{}, http.StatusConflict, errSessionHostMissing
	}
	if !h.IsConnected() {
		return printerSession{}, http.StatusConflict, errSessionNotConnected
	}

	var targetHost string
	var targetPort int
	withSessionLock(s, func() {
		targetHost = s.TargetHost
		targetPort = s.TargetPort
	})
	if targetPort <= 0 {
		targetPort = 3270
	}
	// A bundled sample app is a listener this process opened to demonstrate
	// the terminal, and its target is a pseudo-host ("sampleapp:petstore")
	// rather than an address a printer could dial. Refused by name, because
	// the alternative is a syntax error out of pr3287 about a hostname the
	// operator never typed.
	if _, _, ok := parseSampleAppHost(targetHost); ok {
		return printerSession{}, http.StatusConflict, fmt.Errorf(
			"the bundled sample applications are display-only: there is no printer LU to bind against one")
	}

	opts := printer.Options{
		Host:       targetHost,
		Port:       targetPort,
		TLS:        terminalTLS(h),
		CodePage:   strings.TrimSpace(req.CodePage),
		MPP:        req.MPP,
		CRLF:       req.CRLF,
		BlankLines: req.BlankLines,
		FFSkip:     req.FFSkip,
		FFThru:     req.FFThru,
		IgnoreEOJ:  req.IgnoreEOJ,
		EOJTimeout: req.EOJTimeout,
		SkipCC:     req.SkipCC,
	}
	opts.Verify = opts.TLS && printerVerifiesCertificate()

	switch {
	case req.Associate:
		lu := terminalLU(h)
		if lu == "" {
			return printerSession{}, http.StatusConflict, fmt.Errorf(
				"the host has not bound this session to a named LU, so there is no associated printer to ask for. " +
					"Name the printer LU instead")
		}
		opts.Associate = lu
	default:
		opts.LU = strings.TrimSpace(req.LU)
	}

	dir, err := app.printerSpoolDir(s.ID)
	if err != nil {
		return printerSession{}, http.StatusInternalServerError, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return printerSession{}, http.StatusInternalServerError, fmt.Errorf("could not create the print spool directory: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return printerSession{}, http.StatusInternalServerError, fmt.Errorf("could not work out where print jobs should go: %w", err)
	}
	opts.SpoolCommand, err = printer.SpoolCommandFor(self, dir)
	if err != nil {
		return printerSession{}, http.StatusInternalServerError, err
	}

	// Validated before anything is registered or launched, so a bad LU name
	// is a 400 rather than a process that starts and dies.
	if err := opts.Validate(); err != nil {
		return printerSession{}, http.StatusBadRequest, err
	}

	entry := &printerSession{
		Session:   s.ID,
		Host:      opts.Host,
		Port:      opts.Port,
		LU:        opts.LU,
		Associate: opts.Associate,
		TLS:       opts.TLS,
		StartedAt: time.Now(),
		Running:   true,
		proc:      printer.New(execPath, opts),
		dir:       dir,
	}
	// Registered before the process is started, so two concurrent starts
	// cannot both get past the check and leave one of them unreachable.
	if err := app.printers().begin(entry); err != nil {
		return printerSession{}, http.StatusConflict, err
	}
	if err := entry.proc.Start(); err != nil {
		app.printers().forget(s.ID)
		return printerSession{}, http.StatusBadGateway, err
	}
	return entry.snapshot(), http.StatusCreated, nil
}

// writePrinterStart answers a start request for one session.
func (app *App) writePrinterStart(c *gin.Context, s *session.Session) {
	var req printerRequest
	if c.Request.Body != nil {
		// Optional: a start with no body binds by association if it can, and
		// is refused with a readable message if it cannot.
		_ = c.ShouldBindJSON(&req)
	}
	if !req.Associate && strings.TrimSpace(req.LU) == "" {
		req.Associate = true
	}
	entry, status, err := app.startPrinterFor(s, req)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	app.auditRequest(c, audit.EventPrinterStarted, audit.Success, entry.Session,
		map[string]string{"printer": printerAuditDetail(entry)})
	c.JSON(status, entry)
}

// printerAuditDetail says which LU was bound where, which is the part of a
// printer session worth having in the trail.
func printerAuditDetail(entry printerSession) string {
	lu := entry.LU
	if lu == "" {
		lu = entry.Associate + " (associated)"
	}
	return fmt.Sprintf("printer LU %s on %s:%d", lu, entry.Host, entry.Port)
}

// writePrinterStatus reports a session's printer and its jobs.
//
// The jobs are listed whether or not a printer is bound. A printer stopped an
// hour ago may still have the report somebody opened this panel to collect,
// and a status that answered "no jobs" because nothing is running would be
// describing the wrong thing.
func (app *App) writePrinterStatus(c *gin.Context, s *session.Session) {
	body := gin.H{
		"available": resolvePr3287Path(app.installDir) != "",
		"running":   false,
		"jobs":      []printer.Job{},
	}
	if spool, err := app.sessionSpool(s); err == nil {
		jobs, err := spool.List()
		if err != nil {
			log.Printf("printer: listing jobs for session %s: %v", s.ID, err)
		}
		if jobs != nil {
			body["jobs"] = jobs
		}
	}
	if entry, ok := app.printers().get(s.ID); ok {
		snapshot := entry.snapshot()
		body["printer"] = snapshot
		body["running"] = snapshot.Running
	}
	c.JSON(http.StatusOK, body)
}

// writePrinterStop ends a session's printer, leaving the jobs in place.
//
// The spool survives a stop and not a disconnect: stopping the printer is
// something an operator does on purpose, often to start it again against a
// different LU, and losing the output they just collected would be a surprise.
func (app *App) writePrinterStop(c *gin.Context, s *session.Session) {
	entry, ok := app.printers().forget(s.ID)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"ok": true, "running": false})
		return
	}
	if entry.proc != nil {
		if err := entry.proc.Stop(); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
	}
	app.auditRequest(c, audit.EventPrinterStopped, audit.Success, entry.Session,
		map[string]string{"printer": printerAuditDetail(entry.snapshot())})
	c.JSON(http.StatusOK, gin.H{"ok": true, "running": false})
}

// sessionSpool resolves the spool a request is asking about.
//
// A session that has no printer registered still has a spool directory if one
// ever ran, so the directory is derived rather than read from the store: a job
// stays downloadable after the printer that produced it was stopped.
func (app *App) sessionSpool(s *session.Session) (printer.Spool, error) {
	dir, err := app.printerSpoolDir(s.ID)
	if err != nil {
		return printer.Spool{}, err
	}
	return printer.Spool{Dir: dir}, nil
}

// writePrinterJobs lists what has arrived.
func (app *App) writePrinterJobs(c *gin.Context, s *session.Session) {
	spool, err := app.sessionSpool(s)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	jobs, err := spool.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read the print spool: " + err.Error()})
		return
	}
	if jobs == nil {
		jobs = []printer.Job{}
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

// writePrinterJobDownload serves one job.
//
// The name is checked against the shape this server generates and joined to
// the session's own directory, so the endpoint reaches exactly the files this
// session's printer wrote and nothing else.
func (app *App) writePrinterJobDownload(c *gin.Context, s *session.Session) {
	spool, err := app.sessionSpool(s)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(c.Query("name"))
	path, err := spool.Path(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("no print job is named %q", name)})
		return
	}
	if info.Size() > maxPrinterJobDownloadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": fmt.Sprintf("print job %s is %d bytes, larger than the %d-byte download limit",
				name, info.Size(), maxPrinterJobDownloadBytes),
		})
		return
	}
	// An attachment, and octet-stream rather than text/plain: a print job is
	// whatever the host sent, and it is not this origin's business to render
	// it.
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	c.Header("Content-Type", "application/octet-stream")
	c.File(path)
}

// writePrinterJobDelete removes one job.
func (app *App) writePrinterJobDelete(c *gin.Context, s *session.Session) {
	spool, err := app.sessionSpool(s)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		name = strings.TrimSpace(c.PostForm("name"))
	}
	if err := spool.Delete(name); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// The browser door. Each of these resolves the session from the cookie and
// hands off to the shared implementation above, so the two surfaces cannot
// drift apart in what they call a field or when they refuse.

func (app *App) PrinterStatusHandler(c *gin.Context) {
	if s := app.browserSessionOr404(c); s != nil {
		app.writePrinterStatus(c, s)
	}
}

func (app *App) PrinterStartHandler(c *gin.Context) {
	if s := app.browserSessionOr404(c); s != nil {
		app.writePrinterStart(c, s)
	}
}

func (app *App) PrinterStopHandler(c *gin.Context) {
	if s := app.browserSessionOr404(c); s != nil {
		app.writePrinterStop(c, s)
	}
}

func (app *App) PrinterJobsHandler(c *gin.Context) {
	if s := app.browserSessionOr404(c); s != nil {
		app.writePrinterJobs(c, s)
	}
}

func (app *App) PrinterJobDownloadHandler(c *gin.Context) {
	if s := app.browserSessionOr404(c); s != nil {
		app.writePrinterJobDownload(c, s)
	}
}

func (app *App) PrinterJobDeleteHandler(c *gin.Context) {
	if s := app.browserSessionOr404(c); s != nil {
		app.writePrinterJobDelete(c, s)
	}
}

// browserSessionOr404 resolves the cookie's session or answers the request.
func (app *App) browserSessionOr404(c *gin.Context) *session.Session {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no active session"})
		return nil
	}
	return s
}

// The API door.

func (app *App) APIPrinterStatus(c *gin.Context) {
	if s, ok := app.apiSessionFromPath(c); ok {
		app.writePrinterStatus(c, s)
	}
}

func (app *App) APIPrinterStart(c *gin.Context) {
	if s, ok := app.apiSessionFromPath(c); ok {
		app.writePrinterStart(c, s)
	}
}

func (app *App) APIPrinterStop(c *gin.Context) {
	if s, ok := app.apiSessionFromPath(c); ok {
		app.writePrinterStop(c, s)
	}
}

func (app *App) APIPrinterJobs(c *gin.Context) {
	if s, ok := app.apiSessionFromPath(c); ok {
		if isTruthyParam(c.Query("download")) || strings.TrimSpace(c.Query("name")) != "" {
			app.writePrinterJobDownload(c, s)
			return
		}
		app.writePrinterJobs(c, s)
	}
}

func (app *App) APIPrinterJobDelete(c *gin.Context) {
	if s, ok := app.apiSessionFromPath(c); ok {
		app.writePrinterJobDelete(c, s)
	}
}
