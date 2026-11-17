// SPDX-License-Identifier: AGPL-3.0-or-later

package aipolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"strings"

	"github.com/jnnngs/3270Web/internal/egress"
)

// What a browser is told when a provider call fails.
//
// The chat proxy used to hand the browser whatever went wrong, verbatim: the
// dial error, with the address the name resolved to; the upstream's response
// body, up to 64 KiB of it. For a real provider that is a helpful error
// message. For an endpoint a user aimed at something else, it is the read
// half of a request-forgery primitive — point the base URL at an internal
// service, read its answer out of the error. So the detail goes to the server
// log, where the operator can see it, and the browser gets a sentence this
// program wrote.

// ErrStreamStalled is returned when an upstream stops sending for longer than
// the idle window.
var ErrStreamStalled = errors.New("upstream stalled (no data received for 60s)")

// ErrStreamIncomplete is returned when an upstream closes a stream without
// ending the message it was sending.
var ErrStreamIncomplete = errors.New("the provider closed the stream before the response was complete")

// ErrUnexpectedContent marks a 2xx response that is not the kind of document
// a chat API returns.
type ErrUnexpectedContent struct {
	Want string
	Got  string
}

func (e *ErrUnexpectedContent) Error() string {
	return fmt.Sprintf("unexpected content type %q (want %s)", e.Got, e.Want)
}

// Content types a provider call may be answered with.
const (
	ContentEventStream = "text/event-stream"
	ContentJSON        = "application/json"
)

// CheckContentType refuses a successful response whose body is not of the
// expected kind, before any of it is read.
//
// A chat completion is an event stream and a model list is JSON. An endpoint
// that answers 200 with an HTML page, a file, or a metrics dump is not a chat
// API, and relaying its body to the browser would make the proxy a way to read
// arbitrary HTTP resources the server can reach.
func CheckContentType(resp *http.Response, want string) error {
	got := resp.Header.Get("Content-Type")
	media, _, err := mime.ParseMediaType(got)
	if err == nil && strings.EqualFold(media, want) {
		return nil
	}
	return &ErrUnexpectedContent{Want: want, Got: got}
}

// UpstreamError is implemented by the errors that carry a provider's non-2xx
// answer.
type UpstreamError interface {
	error
	// Upstream returns who answered, with what status, and the body.
	Upstream() (provider string, status int, body string)
}

// statusHint says what a status usually means for someone configuring a
// provider.
func statusHint(status int) string {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "Check the API key or sign-in for this provider."
	case status == http.StatusNotFound:
		return "Check the base URL and the model name."
	case status == http.StatusPaymentRequired:
		return "The account has no remaining quota or credit."
	case status == http.StatusRequestEntityTooLarge:
		return "The conversation is too long for this model; clear it or start a new one."
	case status == http.StatusTooManyRequests:
		return "The provider is rate-limiting this account; wait and try again."
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		return "The provider rejected the request; the model may not support tool calling, or the conversation may be too long."
	case status >= 500:
		return "The provider is having trouble; try again shortly."
	}
	return ""
}

// upstreamMessage pulls the human-readable message out of a provider's JSON
// error body, in either of the two shapes providers use.
func upstreamMessage(body string) string {
	var doc struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal([]byte(body), &doc) != nil {
		return ""
	}
	var nested struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(doc.Error, &nested) == nil && nested.Message != "" {
		return nested.Message
	}
	var plain string
	if json.Unmarshal(doc.Error, &plain) == nil && plain != "" {
		return plain
	}
	return doc.Message
}

const maxUpstreamMessageRunes = 240

// PublicError turns a failed provider call into an HTTP status and a message
// that is safe to show, and writes the detail to the server log.
//
// trusted says the request went to a provider's own published endpoint rather
// than to an address a user typed. Only then is the provider's own error
// message quoted, since only then is it known to be an error message.
func PublicError(err error, trusted bool) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	log.Printf("[ai] provider call failed: %v", err)

	var upstream UpstreamError
	if errors.As(err, &upstream) {
		provider, status, body := upstream.Upstream()
		if provider == "" {
			provider = "The provider"
		}
		msg := fmt.Sprintf("%s answered with status %d.", provider, status)
		if hint := statusHint(status); hint != "" {
			msg += " " + hint
		}
		if trusted {
			if detail := strings.Join(strings.Fields(upstreamMessage(body)), " "); detail != "" {
				if r := []rune(detail); len(r) > maxUpstreamMessageRunes {
					detail = string(r[:maxUpstreamMessageRunes]) + "…"
				}
				msg += " It said: " + detail
			}
		}
		code := http.StatusBadGateway
		if status >= 400 && status < 600 {
			code = status
		}
		return code, msg
	}

	var refused *ErrEndpointRefused
	if errors.As(err, &refused) {
		return http.StatusForbidden, refused.Reason
	}
	var blocked *egress.ErrBlocked
	if errors.As(err, &blocked) {
		// The reason, not the address: where a name resolved to is a fact
		// about the server's network.
		return http.StatusForbidden, "This instance will not call that endpoint: " + blocked.Reason
	}
	var content *ErrUnexpectedContent
	if errors.As(err, &content) {
		return http.StatusBadGateway, "The endpoint did not answer like a chat API. Check the base URL."
	}
	switch {
	case errors.Is(err, ErrDisabled):
		return http.StatusForbidden, DisabledMessage
	case errors.Is(err, ErrStreamStalled):
		return http.StatusGatewayTimeout, "The provider stopped sending (no data for 60 seconds). Try again."
	case errors.Is(err, ErrStreamIncomplete):
		return http.StatusBadGateway, "The provider closed the connection before the response was complete. Nothing from the partial response was acted on; try again."
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "The provider took too long to answer."
	case errors.Is(err, context.Canceled):
		return http.StatusBadGateway, "The request was cancelled."
	}
	return http.StatusBadGateway, "Could not reach the AI provider. The server log has the details."
}
