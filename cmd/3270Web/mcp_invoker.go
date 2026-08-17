// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jnnngs/3270Web/internal/mcptools"
)

// httpInvoker performs tool calls against a 3270Web instance over HTTP.
type httpInvoker struct {
	baseURL string
	token   string
	// conversation identifies this MCP connection to the server, so
	// "have I already loaded that skill?" has an answer. An MCP client has
	// no session cookie, and its conversation outlives any one 3270 session.
	conversation string
	client       *http.Client
}

// maxToolResponseBytes caps what one tool call can return.
//
// A chaos mind map or an application overview on a large estate can be very
// large, and the model pays for every byte. Truncating with a message that
// names a narrower tool is more useful than either silently dropping the tail
// or spending a context window on one call.
const maxToolResponseBytes = 256 * 1024

func newHTTPInvoker(baseURL, token, conversation string) *httpInvoker {
	return &httpInvoker{
		baseURL:      strings.TrimRight(baseURL, "/"),
		token:        token,
		conversation: conversation,
		client: &http.Client{
			// Generous, because a host transaction behind wait_for_unlock can
			// legitimately take a while; bounded, because a wedged request
			// would otherwise hang the conversation with no explanation.
			Timeout: 120 * time.Second,
		},
	}
}

func (h *httpInvoker) Do(ctx context.Context, method, path string, query url.Values, body any) (mcptools.Response, error) {
	target := h.baseURL + path
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

	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return mcptools.Response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	if h.conversation != "" {
		req.Header.Set(ConversationHeader, h.conversation)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return mcptools.Response{}, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxToolResponseBytes+1))
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
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        data,
	}, nil
}
