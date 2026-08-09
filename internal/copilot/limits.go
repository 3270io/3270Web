package copilot

import (
	"fmt"
	"io"
	"net/http"
)

// Byte ceilings for the upstream responses this package buffers whole.
//
// Every one of these is small JSON — a device code, an OAuth token, a model
// list, an error body quoted back into a message. None of it is streamed, so
// all of it is read into memory before it is looked at, and until these limits
// existed "how much memory" was a number chosen by whoever was on the other
// end of the connection.
//
// That end is not necessarily GitHub. A caller sets its own GitHub Enterprise
// hostname (Handlers.SetEnterprise) and every control-plane request then goes
// to a host they nominated, so an endpoint that answers a device-code request
// with an endless body is a plain request away. http.Client.Timeout bounds how
// long that takes and says nothing about how large it gets: 30 seconds of a
// fast response is gigabytes. A handful of those concurrently is the process,
// and with it every terminal session it was holding.
const (
	// maxControlResponseBytes covers the OAuth and token endpoints, whose
	// real payloads are a few hundred bytes.
	maxControlResponseBytes = 256 << 10
	// maxCatalogueResponseBytes covers /models, which is a list rather than a
	// single record and so is given more room.
	maxCatalogueResponseBytes = 2 << 20
	// maxErrorBodyBytes is how much of a non-2xx body is read in order to
	// quote it back in an error message. The messages themselves truncate to a
	// couple of hundred runes, so reading more than this is never useful.
	maxErrorBodyBytes = 64 << 10
)

// readCapped reads at most max bytes from an upstream response and reports a
// body that runs past the limit as an error rather than truncating it.
//
// Truncation would be the worse failure: a body cut off mid-JSON decodes to
// something arbitrary, and the caller cannot tell that from a genuine
// malformed reply. Refusing says what happened.
func readCapped(resp *http.Response, max int64, what string) ([]byte, error) {
	if resp.ContentLength > max {
		return nil, fmt.Errorf("%s: response too large (%d bytes, limit %d)", what, resp.ContentLength, max)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("%s: response too large (over %d bytes)", what, max)
	}
	return body, nil
}

// readErrorBody reads as much of a failed response as is worth quoting. A
// truncated error body is fine — it only ever ends up inside a message that
// truncates further — so this returns what it got rather than failing.
func readErrorBody(resp *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	return string(body)
}
