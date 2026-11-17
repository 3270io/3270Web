// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/gin-gonic/gin"
)

var connectHostValue = regexp.MustCompile(`id="hostname-input"[^>]*\svalue="([^"]*)"`)

// connectPageHostValue renders the connect page the way a browser asks for it
// and returns what the host field was pre-filled with.
func connectPageHostValue(t *testing.T, r *gin.Engine, cookies ...*http.Cookie) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:40000"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	m := connectHostValue.FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatalf("host input not found in connect page:\n%s", w.Body.String())
	}
	return html.UnescapeString(m[1])
}

// The field used to open pre-filled with "localhost:3270", which this
// instance's own loopback guard refuses on Connect: the first thing a new
// operator did was press the button and be told no. An empty field shows the
// placeholder example instead and asks for what is actually wanted.
func TestConnectPage_HostFieldStartsEmpty(t *testing.T) {
	_, r := newAuthTestApp(t, "none")
	if got := connectPageHostValue(t, r); got != "" {
		t.Errorf("host field = %q on a first visit, want it empty", got)
	}
}

func TestConnectPage_OffersTheLastTargetWhenItIsUsable(t *testing.T) {
	_, r := newAuthTestApp(t, "none")
	got := connectPageHostValue(t, r, &http.Cookie{Name: lastTargetCookieName, Value: "mainframe.example.com:992"})
	if got != "mainframe.example.com:992" {
		t.Errorf("host field = %q, want the last target offered back", got)
	}
}

// A last target the instance would refuse is not worth offering back. Older
// browsers carry the cookie from when the pre-filled default was submitted.
func TestConnectPage_DoesNotOfferALastTargetTheGuardRefuses(t *testing.T) {
	_, r := newAuthTestApp(t, "none")
	for _, stale := range []string{"localhost:3270", "127.0.0.1:3270", "[::1]:3270"} {
		if got := connectPageHostValue(t, r, &http.Cookie{Name: lastTargetCookieName, Value: stale}); got != "" {
			t.Errorf("last target %q: host field = %q, want it empty", stale, got)
		}
	}
}

// The bundled sample apps are served from this process on loopback but are
// not a loopback refusal; the guard exempts them and so must this.
func TestConnectPage_OffersASampleAppAsLastTarget(t *testing.T) {
	_, r := newAuthTestApp(t, "none")
	got := connectPageHostValue(t, r, &http.Cookie{Name: lastTargetCookieName, Value: "sampleapp:petstore:3271"})
	if got != "sampleapp:petstore:3271" {
		t.Errorf("host field = %q, want the sample app target offered back", got)
	}
}
