// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// operatorSettings are the names of server settings. They mean something to
// whoever runs the instance and nothing to the person signing in.
var operatorSettings = []string{"TRUST_PROXY_HEADERS", "TLS_TERMINATED_UPSTREAM"}

func loginPageWith(t *testing.T, r *gin.Engine, headers map[string]string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, loginPath, nil)
	req.RemoteAddr = "10.0.0.1:40000"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", loginPath, w.Code)
	}
	return w.Body.String()
}

// The sign-in page is read by everybody who signs in, and most of them cannot
// change how the server is run. The warning is true and stays, but it is
// written to them: what is wrong and who to ask, not which environment
// variable to set.
func TestSignInPageWarningIsWrittenForThePersonSigningIn(t *testing.T) {
	_, r := newAuthTestApp(t, "local")

	for name, headers := range map[string]map[string]string{
		"plain HTTP":                       nil,
		"a proxy claiming HTTPS (ignored)": {"X-Forwarded-Proto": "https"},
	} {
		t.Run(name, func(t *testing.T) {
			// The template wraps its prose, so read it as a person would.
			body := strings.Join(strings.Fields(loginPageWith(t, r, headers)), " ")

			if !strings.Contains(body, "auth-alert--warn") {
				t.Fatal("the warning is gone; it is true and should stay")
			}
			for _, setting := range operatorSettings {
				if strings.Contains(body, setting) {
					t.Errorf("the sign-in page names the server setting %s", setting)
				}
			}
			if !strings.Contains(body, "Ask your administrator") {
				t.Errorf("the warning does not say who can fix it")
			}
			if !strings.Contains(body, "HTTPS") {
				t.Errorf("the warning does not say what the fix is")
			}
		})
	}
}

// Setup is read by the administrator, who is the person who can act on it, so
// it keeps the settings' names.
func TestSetupPageKeepsTheOperatorDetail(t *testing.T) {
	_, r := newSetupTestApp(t)
	w := getWithAuth(r, setupPath, "", "10.0.0.1")
	body := w.Body.String()
	for _, setting := range operatorSettings {
		if !strings.Contains(body, setting) {
			t.Errorf("the setup page no longer names %s for the administrator reading it", setting)
		}
	}
}
