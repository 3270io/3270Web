// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// inputValue returns the value attribute of the input with this id in a
// rendered page, and whether the input was found at all.
func inputValue(page, id string) (string, bool) {
	tag := regexp.MustCompile(`<input[^>]*\sid="` + regexp.QuoteMeta(id) + `"[^>]*>`).FindString(page)
	if tag == "" {
		return "", false
	}
	m := regexp.MustCompile(`\svalue="([^"]*)"`).FindStringSubmatch(tag)
	if m == nil {
		return "", true
	}
	return html.UnescapeString(m[1]), true
}

func postFormTo(r *gin.Engine, path string, form url.Values, clientIP string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test"+path)
	req.Host = "example.test"
	req.RemoteAddr = clientIP + ":40000"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// A failed sign-in used to hand back an empty form, so a typo in the password
// cost the username as well.
func TestFailedSignInKeepsTheUsernameAndNeverThePassword(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", "user", false)

	w := doLogin(t, r, "alice", "not-the-password", "10.0.0.1")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	body := w.Body.String()
	if got, found := inputValue(body, "username"); !found || got != "alice" {
		t.Errorf("username field = %q (found %v), want %q kept", got, found, "alice")
	}
	if got, _ := inputValue(body, "password"); got != "" {
		t.Errorf("password field came back filled: %q", got)
	}
	if strings.Contains(body, "not-the-password") {
		t.Error("the submitted password was written back into the page")
	}
}

// A fresh visit has nothing to keep.
func TestSignInPageStartsEmpty(t *testing.T) {
	_, r := newAuthTestApp(t, "local")
	w := getWithAuth(r, loginPath, "", "10.0.0.1")
	if got, found := inputValue(w.Body.String(), "username"); !found || got != "" {
		t.Errorf("username field = %q (found %v), want it empty", got, found)
	}
}

// Markup in the username is text, not markup.
func TestKeptUsernameIsEscaped(t *testing.T) {
	_, r := newAuthTestApp(t, "local")
	w := doLogin(t, r, `"><script>alert(1)</script>`, "x", "10.0.0.1")
	if strings.Contains(w.Body.String(), "<script>alert(1)</script>") {
		t.Error("the username was written into the page unescaped")
	}
}

// When there is a username to keep, the cursor belongs in the password box:
// that is the thing to retype.
func TestFailedSignInFocusesThePasswordWhenTheUsernameIsKept(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", "user", false)

	body := doLogin(t, r, "alice", "wrong", "10.0.0.1").Body.String()

	username := regexp.MustCompile(`<input[^>]*\sid="username"[^>]*>`).FindString(body)
	password := regexp.MustCompile(`<input[^>]*\sid="password"[^>]*>`).FindString(body)
	if strings.Contains(username, "autofocus") {
		t.Errorf("focus is on the username that was kept: %s", username)
	}
	if !strings.Contains(password, "autofocus") {
		t.Errorf("focus is not on the password: %s", password)
	}
}

// "Try again shortly" left somebody deciding how long to wait. The limiter
// knows exactly how long, and says so.
func TestLockoutSaysHowLongToWait(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", "user", false)
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	app.loginLimiter.now = func() time.Time { return clock }

	keys := loginLimitKeys("alice", "10.0.0.1")
	// Twelve failures: 2s doubled seven times is 256s, a little over four
	// minutes, which reads as five.
	for i := 0; i < loginFreeAttempts+7; i++ {
		app.loginLimiter.RecordFailure(keys...)
	}

	w := doLogin(t, r, "alice", loginTestPassword, "10.0.0.1")

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Try again in 5 minutes.") {
		t.Errorf("the lockout message does not say how long:\n%s", body)
	}
	if strings.Contains(body, "shortly") {
		t.Error("the lockout message still says \"shortly\"")
	}
	if got := w.Header().Get("Retry-After"); got != "256" {
		t.Errorf("Retry-After = %q, want 256", got)
	}
	if got, _ := inputValue(body, "username"); got != "alice" {
		t.Errorf("a locked-out sign-in lost the username: %q", got)
	}
}

func TestRetryAfterText(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{time.Millisecond, "1 second"},
		{time.Second, "1 second"},
		{1500 * time.Millisecond, "2 seconds"},
		{2 * time.Second, "2 seconds"},
		{59 * time.Second, "59 seconds"},
		{60 * time.Second, "1 minute"},
		{61 * time.Second, "2 minutes"},
		{120 * time.Second, "2 minutes"},
		{256 * time.Second, "5 minutes"},
		{15 * time.Minute, "15 minutes"},
		{61 * time.Minute, "2 hours"},
	}
	for _, tt := range tests {
		if got := retryAfterText(tt.in); got != tt.want {
			t.Errorf("retryAfterText(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// First-run setup is a form with four fields and three ways to fail it. Every
// one used to come back empty, including the setup code that is copied out of a
// log and the username that was chosen.
func TestFailedSetupKeepsTheCodeAndUsernameButNeverAPassword(t *testing.T) {
	good := "ok-password-ok-password-1"
	tests := []struct {
		name   string
		form   func(code string) url.Values
		status int
	}{
		{"wrong code", func(string) url.Values {
			return url.Values{"setupCode": {"AAAA-BBBB-CCCC-DDDD"}, "username": {"alice"},
				"password": {good}, "confirmPassword": {good}}
		}, http.StatusUnauthorized},
		{"passwords differ", func(code string) url.Values {
			return url.Values{"setupCode": {code}, "username": {"alice"},
				"password": {good}, "confirmPassword": {good + "x"}}
		}, http.StatusBadRequest},
		{"password too weak", func(code string) url.Values {
			return url.Values{"setupCode": {code}, "username": {"alice"},
				"password": {"short"}, "confirmPassword": {"short"}}
		}, http.StatusBadRequest},
		{"username not allowed", func(code string) url.Values {
			return url.Values{"setupCode": {code}, "username": {"not allowed!"},
				"password": {good}, "confirmPassword": {good}}
		}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, r := newSetupTestApp(t)
			form := tt.form(formatSetupCode(app.setup.code))

			w := postSetup(r, form, "10.0.0.1")

			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d", w.Code, tt.status)
			}
			body := w.Body.String()
			if got, found := inputValue(body, "setupCode"); !found || got != form.Get("setupCode") {
				t.Errorf("setup code field = %q (found %v), want %q kept", got, found, form.Get("setupCode"))
			}
			if got, found := inputValue(body, "username"); !found || got != form.Get("username") {
				t.Errorf("username field = %q (found %v), want %q kept", got, found, form.Get("username"))
			}
			for _, id := range []string{"password", "confirmPassword"} {
				if got, _ := inputValue(body, id); got != "" {
					t.Errorf("%s field came back filled: %q", id, got)
				}
			}
			for _, secret := range []string{good, "short"} {
				if strings.Contains(body, `value="`+secret) {
					t.Errorf("a submitted password was written back into the page")
				}
			}
		})
	}
}

// A refused attempt that was throttled says for how long as well.
func TestSetupLockoutSaysHowLongToWait(t *testing.T) {
	app, r := newSetupTestApp(t)
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	app.loginLimiter.now = func() time.Time { return clock }
	for i := 0; i < loginFreeAttempts; i++ {
		app.loginLimiter.RecordFailure("setup:10.0.0.1")
	}

	w := postSetup(r, url.Values{"setupCode": {"AAAA-BBBB-CCCC-DDDD"}, "username": {"alice"},
		"password": {"x"}, "confirmPassword": {"x"}}, "10.0.0.1")

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Try again in 2 seconds.") {
		t.Errorf("the lockout message does not say how long:\n%s", w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got != strconv.Itoa(2) {
		t.Errorf("Retry-After = %q, want 2", got)
	}
}
