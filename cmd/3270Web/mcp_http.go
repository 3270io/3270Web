package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jnnngs/3270Web/internal/mcptools"
)

// registerMCPHTTP mounts the Streamable HTTP MCP transport.
//
// Under /api/v1 rather than at a top-level /mcp for a specific reason:
// OriginRefererCheckMiddleware skips only the /api/v1/ prefix, so a top-level
// route would be origin-checked and reject every non-browser client — which
// is all of them.
//
// This is for clients that cannot launch a local process. Claude Desktop and
// the rest spawn `3270Web mcp` over stdio, which needs no token and no
// exposed port; this path is for a Dockerised instance or a shared server,
// and inherits RequireAPIToken along with the rest of /api/v1.
func (app *App) registerMCPHTTP(r *gin.Engine) {
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		// A server per connection, so one client's current session and
		// loaded-skill history do not leak into another's — and so each
		// carries the credential its own client presented. A token belongs to
		// an account, so replaying the caller's own header is what makes the
		// tools reach that account's sessions and no one else's.
		return buildMCPServer(newEngineInvoker(r, req.Header.Get("Authorization")), mcpHTTPTier())
	}, nil)

	r.Any("/api/v1/mcp", app.RequireAPIToken(), gin.WrapH(handler))
}

// mcpHTTPTier resolves the tier for HTTP-transport sessions.
func mcpHTTPTier() mcptools.Tier {
	tier, ok := mcptools.ParseTier(os.Getenv("MCP_TOOLS"))
	if !ok {
		// Logged rather than silently accepted, so a typo in the deployment
		// is visible instead of quietly changing what clients can do.
		fmt.Fprintf(os.Stderr, "MCP_TOOLS=%q is not a recognised tier; using %s\n",
			os.Getenv("MCP_TOOLS"), tier)
	}
	return tier
}

// engineInvoker performs tool calls against the router in this process.
//
// The stdio server talks to a real listener, because it may be driving an
// instance it did not start. Here the router is right there, so the request
// goes straight to it — no socket, no second copy of the auth, and no
// question of which address the server happens to be bound to.
type engineInvoker struct {
	engine *gin.Engine
	// authorization is the header the client presented to open this MCP
	// connection, replayed on every tool call it makes.
	authorization string
}

func newEngineInvoker(engine *gin.Engine, authorization string) *engineInvoker {
	return &engineInvoker{engine: engine, authorization: strings.TrimSpace(authorization)}
}

func (e *engineInvoker) Do(ctx context.Context, method, path string, query url.Values, body any) (mcptools.Response, error) {
	target := path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return mcptools.Response{}, fmt.Errorf("encode request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req := httptest.NewRequest(method, target, reader).WithContext(ctx)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// The routes still check the token. The caller reached this transport by
	// presenting one, so their header is passed on rather than the check being
	// skipped — which also means a tool call is authorized as the person who
	// made it, not as the instance.
	if e.authorization != "" {
		req.Header.Set("Authorization", e.authorization)
	}

	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()

	data, err := io.ReadAll(io.LimitReader(res.Body, maxToolResponseBytes+1))
	if err != nil {
		return mcptools.Response{}, err
	}
	if len(data) > maxToolResponseBytes {
		data = append(data[:maxToolResponseBytes], []byte(
			"\n\n[truncated — this result exceeded the per-call limit. "+
				"Ask for a narrower slice: chaos_status without verbose, or "+
				"chaos_list_screens with include_previews=false.]")...)
	}

	return mcptools.Response{
		Status:      res.StatusCode,
		ContentType: res.Header.Get("Content-Type"),
		Body:        data,
	}, nil
}
