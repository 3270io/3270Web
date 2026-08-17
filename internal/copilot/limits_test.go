// SPDX-License-Identifier: AGPL-3.0-or-later

package copilot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The enterprise hostname is caller-supplied, so the device-code endpoint is
// an origin the caller chose. A response that never ends must not be read
// until the process runs out of memory.
func TestStartDeviceLoginRefusesAnOversizedResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Just past the ceiling, written in chunks so the test does not depend
		// on Content-Length being set.
		chunk := strings.Repeat("a", 64<<10)
		for written := 0; written <= maxControlResponseBytes; written += len(chunk) {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()

	m := NewAuthManager(t.TempDir() + "/auth.json")
	m.githubHost = upstream.URL

	_, err := m.StartDeviceLogin(context.Background())
	if err == nil {
		t.Fatal("an unbounded device-code response was accepted")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %v, want it to say the response was too large", err)
	}
}

// The cap must not be so tight that a real payload trips it — a limit that
// breaks the ordinary case is a limit somebody removes.
func TestStartDeviceLoginAcceptsAnOrdinaryResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"ABCD-1234",` +
			`"verification_uri":"https://github.com/login/device","expires_in":900,"interval":5}`))
	}))
	defer upstream.Close()

	m := NewAuthManager(t.TempDir() + "/auth.json")
	m.githubHost = upstream.URL

	dc, err := m.StartDeviceLogin(context.Background())
	if err != nil {
		t.Fatalf("StartDeviceLogin = %v, want success", err)
	}
	if dc.UserCode != "ABCD-1234" {
		t.Errorf("user code = %q, want ABCD-1234", dc.UserCode)
	}
}

// A failed request still quotes some of the body back, and that read is capped
// too — but it truncates rather than failing, because the message it feeds
// truncates further anyway.
func TestErrorBodyIsBoundedButStillReported(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream is unwell" + strings.Repeat("!", 1<<20)))
	}))
	defer upstream.Close()

	m := NewAuthManager(t.TempDir() + "/auth.json")
	m.githubHost = upstream.URL

	_, err := m.StartDeviceLogin(context.Background())
	if err == nil {
		t.Fatal("a 502 was treated as success")
	}
	if !strings.Contains(err.Error(), "upstream is unwell") {
		t.Errorf("error = %v, want it to quote what the upstream said", err)
	}
}
