package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
)

func TestTogglesListAndSet(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	base := "/api/v1/sessions/" + sess.ID

	w := doToken(t, r, http.MethodGet, base+"/toggles", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: status = %d (body %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "monoCase") {
		t.Errorf("list should name the toggles, got %s", w.Body.String())
	}
	// Each toggle carries what it does — the list is shown to an operator, and
	// "overlayPaste" on its own does not tell anyone anything.
	if !strings.Contains(w.Body.String(), "upper case") {
		t.Errorf("list should describe each toggle, got %s", w.Body.String())
	}

	w = doToken(t, r, http.MethodPost, base+"/toggles", token, `{"name":"monoCase","value":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("set: status = %d (body %s)", w.Code, w.Body.String())
	}
	if !mock.ToggleValues["monoCase"] {
		t.Error("the change did not reach the terminal")
	}
	if !strings.Contains(w.Body.String(), `"value":true`) {
		t.Errorf("set should answer with the toggle as it now stands, got %s", w.Body.String())
	}
}

// Set() also reaches trace files, printer sessions and the proxy
// configuration. Only the display toggles may be named, and the refusal says
// which names would have worked.
func TestTogglesRefuseNamesOutsideTheAllowlist(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	base := "/api/v1/sessions/" + sess.ID

	for _, name := range []string{"traceFile", "printerLu", "proxy", "oversize", "monoCase,traceFile"} {
		w := doToken(t, r, http.MethodPost, base+"/toggles", token, `{"name":"`+name+`","value":true}`)
		if w.Code != http.StatusBadRequest {
			t.Errorf("set %q: status = %d, want 400 (body %s)", name, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "available") {
			t.Errorf("set %q: refusal should carry the names that would have worked, got %s", name, w.Body.String())
		}
	}
	if len(mock.ToggleValues) != 0 {
		t.Errorf("a refused name still changed something: %v", mock.ToggleValues)
	}
}

// A body without "value" would otherwise be indistinguishable from asking for
// the toggle to be turned off, which is half of the possible requests.
func TestToggleSetRequiresAnExplicitValue(t *testing.T) {
	_, r, sess, _, token := snapshotTestApp(t)
	base := "/api/v1/sessions/" + sess.ID

	for _, body := range []string{`{"name":"monoCase"}`, `{"value":true}`, `{}`} {
		if w := doToken(t, r, http.MethodPost, base+"/toggles", token, body); w.Code != http.StatusBadRequest {
			t.Errorf("POST %s: status = %d, want 400", body, w.Code)
		}
	}
}

func TestTogglesOnDisconnectedSessionIs409(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	mock.Connected = false

	if w := doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/toggles", token, ""); w.Code != http.StatusConflict {
		t.Errorf("list: status = %d, want 409 (body %s)", w.Code, w.Body.String())
	}
}

// The browser reaches the same implementation with a cookie instead of a
// token — the point of one implementation behind two doors is that the two
// cannot disagree about what a toggle is called.
func TestToggleCookieDoorServesTheSameThing(t *testing.T) {
	app, _, sess, _, _ := snapshotTestApp(t)

	r := gin.New()
	r.GET("/host/toggles", app.HostTogglesHandler)
	r.POST("/host/toggles", app.HostToggleSetHandler)

	w := doJSON(r, http.MethodGet, "/host/toggles", sess.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /host/toggles: status = %d (body %s)", w.Code, w.Body.String())
	}
	for _, name := range host.SafeToggleNames() {
		if !strings.Contains(w.Body.String(), name) {
			t.Errorf("cookie door omits %q, which the token door reports", name)
		}
	}

	if w := doJSON(r, http.MethodGet, "/host/toggles", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("no session: status = %d, want 401", w.Code)
	}
}
