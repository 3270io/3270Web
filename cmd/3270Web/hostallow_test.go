// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/authz"
)

const fencedTo = "*.allowed.test"

// Unset must stay unrestricted. An allowlist that has to be configured before
// the product works is one people switch off.
func TestNoAllowlistAllowsEverything(t *testing.T) {
	t.Setenv(allowedHostsEnv, "")
	for _, host := range []string{"anything.example.test", "10.0.0.5:23", "[2001:db8::1]:992"} {
		if err := checkHostAllowed(host); err != nil {
			t.Errorf("checkHostAllowed(%q) = %v, want nil", host, err)
		}
	}
}

func TestAllowlistRefusesEverythingElse(t *testing.T) {
	t.Setenv(allowedHostsEnv, fencedTo)

	if err := checkHostAllowed("tso.allowed.test:992"); err != nil {
		t.Errorf("a permitted host was refused: %v", err)
	}
	err := checkHostAllowed("intranet.corp.test:80")
	if err == nil {
		t.Fatal("a host outside the allowlist was permitted")
	}
	// The refusal names the setting, not the list — the list describes the
	// shape of the network.
	if !strings.Contains(err.Error(), allowedHostsEnv) {
		t.Errorf("the refusal does not say what to look at: %v", err)
	}
	if strings.Contains(err.Error(), fencedTo) {
		t.Errorf("the refusal leaks the allowlist: %v", err)
	}
}

// The sample apps are this process talking to itself, already behind
// ALLOW_SAMPLE_APPS. Requiring them in the allowlist would break the demo on
// a fenced instance for no gain.
func TestSampleAppsAreNotFencedOut(t *testing.T) {
	t.Setenv(allowedHostsEnv, fencedTo)
	for _, host := range []string{"mock", "demo", "sampleapp:app1"} {
		if err := checkHostAllowed(host); err != nil {
			t.Errorf("checkHostAllowed(%q) = %v, want nil", host, err)
		}
	}
}

// A terminal is a client for arbitrary TCP, so every way of pointing one at a
// host has to be fenced. Two functions reach a host — opening a session and
// re-pointing an existing one — and this drives both.
func TestEveryConnectionPathHonoursTheAllowlist(t *testing.T) {
	t.Setenv(allowedHostsEnv, fencedTo)
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	token := issueToken(t, app, alice.ID, "ci", nil)

	const forbidden = "intranet.corp.test:23"

	// 1. Opening a session over the API. A refusal by policy is 403 rather
	// than the 502 a failed dial gets: 502 says the far end failed and the
	// request is worth repeating, which for a host that will never be
	// permitted is both wrong and an invitation to retry in a loop.
	w := apiCall(r, http.MethodPost, "/api/v1/sessions", token, `{"host":"`+forbidden+`"}`)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), allowedHostsEnv) {
		t.Errorf("the API refusal does not explain itself: %s", w.Body.String())
	}

	// 2. Re-pointing an existing session, which workflow playback and the MCP
	// connect tool both use. It keeps the session and swaps the host, so it
	// never passes through the creation path.
	// Asserting on the *reason*, not merely that it failed. Dialling a host
	// that is not there fails anyway, so "returned an error" would have passed
	// with no allowlist check at all — as it did, the first time this was
	// written.
	s := app.SessionManager.CreateSessionFor(alice.ID, mustMockHost(t))
	err := app.resetSessionHost(context.Background(), s, forbidden)
	if err == nil {
		t.Error("an existing session was re-pointed at a host outside the allowlist")
	} else if !strings.Contains(err.Error(), allowedHostsEnv) {
		t.Errorf("re-pointing failed for an unrelated reason, so the allowlist "+
			"is not being checked on this path: %v", err)
	}
	// A permitted host gets past the allowlist. It will still fail to dial —
	// nothing is listening — so this asserts only that the refusal is not the
	// allowlist's.
	if err := app.resetSessionHost(context.Background(), s, "tso.allowed.test:992"); err != nil &&
		strings.Contains(err.Error(), allowedHostsEnv) {
		t.Errorf("a permitted host was refused by the allowlist: %v", err)
	}
}

// A refusal is worth a line in the trail: it is what somebody probing the
// network from a session looks like.
func TestRefusedHostIsAudited(t *testing.T) {
	t.Setenv(allowedHostsEnv, fencedTo)
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	token := issueToken(t, app, alice.ID, "ci", nil)

	apiCall(r, http.MethodPost, "/api/v1/sessions", token, `{"host":"intranet.corp.test:23"}`)

	entry := mustFind(t, app, audit.EventSessionDenied)
	if entry.Detail["reason"] != "host not allowed" {
		t.Errorf("reason = %q, want the allowlist refusal", entry.Detail["reason"])
	}
	if entry.Target != "intranet.corp.test:23" {
		t.Errorf("target = %q, want the host that was asked for", entry.Target)
	}
	if entry.Actor.UserID != alice.ID {
		t.Errorf("actor = %+v", entry.Actor)
	}
}

// The browser's own connect form is the third way in, and the one a person
// actually uses.
func TestBrowserConnectHonoursTheAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(allowedHostsEnv, fencedTo)
	app, _ := newAuthTestApp(t, "local")

	ctx := asPrincipal(userPrincipal("aaaa1111"))
	if _, err := app.startHostSession(ctx, "intranet.corp.test:23"); err == nil {
		t.Error("the connect path opened a session outside the allowlist")
	} else if !strings.Contains(err.Error(), allowedHostsEnv) {
		t.Errorf("refused for the wrong reason: %v", err)
	}
}
