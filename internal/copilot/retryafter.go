// SPDX-License-Identifier: AGPL-3.0-or-later

package copilot

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MaxRetryAfter is the longest wait the chat proxy will pass on to the
// browser. An upstream that asks for an hour is telling the operator to come
// back later, which a panel that says "try again in about 3600 seconds" does
// not say any better than the plain rate-limit message does.
const MaxRetryAfter = 5 * time.Minute

// ParseRetryAfter reads the delay an upstream asked for in a Retry-After
// header, which is either a whole number of seconds or an HTTP date. It
// returns 0 when the header is absent, unreadable, or names a moment that has
// already passed — none of which is a wait worth quoting.
//
// The chat proxy used to drop this header, so a 429 reached the panel with
// nothing to say about when to try again. It is read here, next to the
// upstream response, and repeated by the handler on the response it sends.
func ParseRetryAfter(h http.Header, now time.Time) time.Duration {
	value := strings.TrimSpace(h.Get("Retry-After"))
	if value == "" {
		return 0
	}
	var wait time.Duration
	if secs, err := strconv.Atoi(value); err == nil {
		wait = time.Duration(secs) * time.Second
	} else if when, err := http.ParseTime(value); err == nil {
		wait = when.Sub(now)
	}
	switch {
	case wait <= 0:
		return 0
	case wait > MaxRetryAfter:
		return MaxRetryAfter
	}
	return wait
}

// SetRetryAfter repeats an upstream's requested delay on a response going back
// to the browser, rounded up to whole seconds as the header requires. A zero
// wait sets nothing.
func SetRetryAfter(h http.Header, wait time.Duration) {
	if wait <= 0 {
		return
	}
	secs := int((wait + time.Second - 1) / time.Second)
	h.Set("Retry-After", strconv.Itoa(secs))
}
