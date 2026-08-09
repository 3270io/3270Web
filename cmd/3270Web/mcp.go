package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/mcptools"
)

// The `3270Web mcp` subcommand: a Model Context Protocol server on stdin and
// stdout, so an AI client can drive a 3270 session the same way the browser
// chat panel does.

// runMCP is the entry point for `3270Web mcp`. It is called as the first
// statement of main(), before the startup log, the config load and the
// panic handler that shows a modal dialog — all of which either write to
// stdout or, on Windows, would put a dialog in front of a process nobody is
// looking at.
func runMCP(args []string) {
	// Stdout belongs to the protocol from here on.
	//
	// gin logs every request, the config loader warns about a missing .env,
	// and any stray fmt.Println would land in the middle of a JSON-RPC frame
	// and desynchronise the client for the rest of the session. Capturing
	// the real handle and pointing os.Stdout at stderr redirects all of it in
	// one move, including code that has not been written yet.
	realStdout := os.Stdout
	os.Stdout = os.Stderr
	log.SetOutput(os.Stderr)
	log.SetFlags(0)
	gin.DefaultWriter = os.Stderr
	gin.DefaultErrorWriter = os.Stderr

	fs := flag.NewFlagSet("3270Web mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		listTools = fs.Bool("list-tools", false, "print the enabled tool catalogue as JSON and exit")
		targetURL = fs.String("url", "", "drive a running 3270Web at this base URL instead of starting one")
		token     = fs.String("token", "", "token for --url (defaults to the API_TOKEN environment variable)")
		toolsTier = fs.String("tools", "", "tool tier: readonly, interactive (default) or full")
	)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, mcpUsage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	tier := resolveTier(*toolsTier)

	if *listTools {
		// A smoke test that needs no host, no session and no mainframe:
		// if this prints JSON, the binary and the client's command line are
		// right, and anything that fails afterwards is configuration.
		if err := writeToolCatalogue(realStdout, tier); err != nil {
			fmt.Fprintf(os.Stderr, "3270Web mcp: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// One identifier for the life of this process: an MCP client runs one
	// conversation per server process, so this is exactly its span.
	conversation, err := randomToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "3270Web mcp: %v\n", err)
		os.Exit(1)
	}

	target, cleanup, err := resolveTarget(*targetURL, *token, conversation)
	if err != nil {
		fmt.Fprintf(os.Stderr, "3270Web mcp: %v\n", err)
		os.Exit(1)
	}
	if cleanup != nil {
		defer cleanup()
	}

	log.Printf("3270Web MCP server ready — tools: %s, target: %s", tier, target.baseURL)

	server := buildMCPServer(target.invoker, tier)
	transport := &mcp.IOTransport{
		Reader: os.Stdin,
		// Not StdioTransport: it resolves os.Stdout when it connects, which
		// is now stderr. The protocol needs the handle captured above.
		Writer: nopWriteCloser{realStdout},
	}
	if err := server.Run(context.Background(), transport); err != nil {
		fmt.Fprintf(os.Stderr, "3270Web mcp: %v\n", err)
		os.Exit(1)
	}
}

const mcpUsage = `3270Web mcp — drive a 3270 session from an AI client over the Model Context Protocol.

With no options it starts 3270Web on a loopback port and drives that, so the
browser UI is available to watch while the model works. Point --url at an
already-running instance to share a session with one you already have open.

Options:
`

// nopWriteCloser lets a *os.File satisfy the transport's io.WriteCloser
// without the transport being able to close the process's real stdout.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// resolveTier reads the tier from the flag, then the environment, and
// reports an unrecognised value rather than silently choosing one.
func resolveTier(flagValue string) mcptools.Tier {
	raw := strings.TrimSpace(flagValue)
	if raw == "" {
		raw = os.Getenv("MCP_TOOLS")
	}
	tier, ok := mcptools.ParseTier(raw)
	if !ok {
		log.Printf("MCP_TOOLS=%q is not a tier I recognise (readonly, interactive, full); using %s", raw, tier)
	}
	return tier
}

// target is where tool calls go.
type target struct {
	baseURL string
	invoker mcptools.Invoker
}

// resolveTarget decides which 3270Web to drive, in three steps: an explicit
// --url, then an instance already listening locally, then one started here.
//
// Preferring a running instance matters: if the user has 3270Web open in a
// browser, sharing it means they watch the model work rather than being
// handed a second, invisible copy.
func resolveTarget(explicitURL, explicitToken, conversation string) (target, func(), error) {
	if url := strings.TrimSpace(explicitURL); url != "" {
		tok := strings.TrimSpace(explicitToken)
		if tok == "" {
			tok = strings.TrimSpace(os.Getenv("API_TOKEN"))
		}
		if tok == "" {
			return target{}, nil, fmt.Errorf(
				"--url needs a token: set API_TOKEN in the environment, or pass --token.\n" +
					"For an instance with accounts, issue one with `3270Web token add <username> mcp`;\n" +
					"otherwise it is the API_TOKEN that instance was started with")
		}
		return target{baseURL: url, invoker: newHTTPInvoker(url, tok, conversation)}, nil, nil
	}

	if url, tok := findRunningInstance(); url != "" {
		log.Printf("attaching to the 3270Web already running at %s", url)
		return target{baseURL: url, invoker: newHTTPInvoker(url, tok, conversation)}, nil, nil
	}

	return startOwnInstance(conversation)
}

// findRunningInstance looks for a local 3270Web that this process is allowed
// to drive. Without a token in the environment there is nothing to attach
// with, so an instance is left alone rather than probed further.
//
// The token may be either kind: the shared API_TOKEN of a single-operator
// instance, or one issued to an account on an instance that has them. Here it
// is simply the credential to present.
func findRunningInstance() (string, string) {
	tok := strings.TrimSpace(os.Getenv("API_TOKEN"))
	if tok == "" {
		return "", ""
	}
	port := strings.TrimSpace(os.Getenv("WEBUI_PORT"))
	if port == "" {
		port = defaultWebUIPort
	}
	url := "http://127.0.0.1:" + port
	client := &http.Client{Timeout: 750 * time.Millisecond}
	resp, err := client.Get(url + "/healthz")
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}
	return url, tok
}

// startOwnInstance boots 3270Web on a loopback port with a generated token.
//
// The token is generated rather than required because nobody is present to
// configure one: an AI client launches this process with a command line and
// no terminal. It never leaves this process — the invoker is the only thing
// that sees it.
func startOwnInstance(conversation string) (target, func(), error) {
	installDir := resolveBaseDir()
	baseDir, err := resolveDataDir(installDir)
	if err != nil {
		return target{}, nil, err
	}

	// A generated token speaks for the single operator, so it cannot be used
	// on an instance where users are separated — there is nobody it could be.
	// An MCP client has no way to sign in, so the answer is a token issued to
	// an account and an explicit --url, rather than quietly starting a second
	// instance with the authentication turned off over the same files.
	if mode, err := authz.ParseMode(os.Getenv(authz.ModeEnv)); err == nil && mode != authz.ModeNone {
		return target{}, nil, fmt.Errorf(
			"%s=%s is set, so this instance requires an account and cannot be started with a generated token.\n"+
				"Issue one with `3270Web token add <username> mcp`, then run:\n"+
				"  3270Web mcp --url http://127.0.0.1:3270 --token <token>",
			authz.ModeEnv, mode)
	}

	tok, err := randomToken()
	if err != nil {
		return target{}, nil, fmt.Errorf("generate an API token: %w", err)
	}
	os.Setenv("API_TOKEN", tok)
	// The bundled sample apps are how someone tries this without a
	// mainframe. This instance is loopback-only and private to this process,
	// so the reason to keep them behind a flag elsewhere does not apply.
	if os.Getenv("ALLOW_SAMPLE_APPS") == "" {
		os.Setenv("ALLOW_SAMPLE_APPS", "1")
	}

	app := newAppAt(installDir, baseDir)
	router, err := buildRouter(app)
	if err != nil {
		return target{}, nil, fmt.Errorf("build the router: %w", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return target{}, nil, fmt.Errorf("listen on loopback: %w", err)
	}
	srv := &http.Server{Handler: router}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("3270Web server stopped: %v", err)
		}
	}()

	url := "http://" + ln.Addr().String()
	// Stderr, not stdout — and worth printing, because the whole point of
	// running the UI is that someone can watch the session.
	fmt.Fprintf(os.Stderr, "3270Web UI: %s\n", url)

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		app.stopAllSessions()
	}
	return target{baseURL: url, invoker: newHTTPInvoker(url, tok, conversation)}, cleanup, nil
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// writeToolCatalogue prints what the server would expose at a tier.
func writeToolCatalogue(w io.Writer, tier mcptools.Tier) error {
	type entry struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Tier        string         `json:"tier"`
		ReadOnly    bool           `json:"readOnly"`
		Destructive bool           `json:"destructive"`
		InputSchema map[string]any `json:"inputSchema"`
	}

	tools := mcptools.Enabled(tier)
	out := make([]entry, 0, len(tools))
	for _, t := range tools {
		out = append(out, entry{
			Name:        t.Name,
			Description: t.Description,
			Tier:        t.Tier.String(),
			ReadOnly:    t.ReadOnly(),
			Destructive: t.Destructive,
			InputSchema: t.InputSchema,
		})
	}

	// The tools MCP adds on top of the shared catalogue. Without these the
	// check would report a catalogue the server does not actually offer, and
	// the missing four would be the session and task tools — the ones a
	// first-run check most needs to see.
	for _, only := range mcpOnlyTools() {
		if only.Tier > tier {
			continue
		}
		schema, _ := only.Tool.InputSchema.(map[string]any)
		readOnly := only.Tool.Annotations != nil && only.Tool.Annotations.ReadOnlyHint
		destructive := only.Tool.Annotations != nil &&
			only.Tool.Annotations.DestructiveHint != nil && *only.Tool.Annotations.DestructiveHint
		out = append(out, entry{
			Name:        only.Tool.Name,
			Description: only.Tool.Description,
			Tier:        only.Tier.String(),
			ReadOnly:    readOnly,
			Destructive: destructive,
			InputSchema: schema,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
