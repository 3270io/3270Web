package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// TestAPICreateSessionRejectsInvalidHostnameWith400 guards a status-code fix:
// an invalid hostname is a client error, but APICreateSession used to map
// every startHostSession failure — including this one — to 502, indistinguishable
// from a real connection failure to a validly-formed host.
func TestAPICreateSessionRejectsInvalidHostnameWith400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager()}
	r := gin.New()
	r.POST("/api/v1/sessions", app.APICreateSession)

	body, _ := json.Marshal(map[string]string{"host": "169.254.169.254:80"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// TestAPIDeleteSessionIsIdempotent guards a status-code fix: deleting a
// session id that doesn't exist (never existed, or a repeat of an earlier
// delete) used to 404. DELETE is supposed to be idempotent — a client
// retrying a dropped response shouldn't see an error for a delete that
// already took effect.
func TestAPIDeleteSessionIsIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager()}
	r := gin.New()
	r.DELETE("/api/v1/sessions/:id", app.APIDeleteSession)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/does-not-exist", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	// A second delete of the same (never-existent) id must also succeed.
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/does-not-exist", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second delete status = %d, want %d, body=%s", w2.Code, http.StatusOK, w2.Body.String())
	}
}

// A bare Query() response as s3270 v4.5 actually emits one, including the "..."
// it uses for the fields it wants asked for by name.
const fakeBareQuery = `ConnectionState: connected-3270
Copyright: ...
Host: host 127.0.0.1 3270
LuName: 
ScreenSizeCurrent: rows 24 columns 80
TelnetHostOptions: BINARY END OF RECORD
TerminalName: IBM-3278-4-E
Tls: not secure
Tn3270eOptions: `

func newQueryTestApp(t *testing.T) (*gin.Engine, string, *host.MockHost) {
	t.Helper()
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	mh.QueryResponses = map[string]string{
		"":          fakeBareQuery,
		"Copyright": "Copyright 1989-2026 by Paul Mattes.",
	}
	app := &App{SessionManager: session.NewManager()}
	sess := app.SessionManager.CreateSession(mh)
	r := gin.New()
	r.GET("/api/v1/sessions/:id/query", app.APIQuery)
	return r, sess.ID, mh
}

func getQuery(t *testing.T, r *gin.Engine, url string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v (body=%s)", url, err, w.Body.String())
	}
	return w.Code, body
}

// The point of the endpoint: s3270's Query is the connection's own account of
// itself, and none of it is derivable from the screen.
func TestAPIQueryReturnsEveryFieldS3270Reports(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, id, _ := newQueryTestApp(t)

	code, body := getQuery(t, r, "/api/v1/sessions/"+id+"/query")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%v", code, body)
	}
	queries, _ := body["queries"].(map[string]any)
	if queries["ConnectionState"] != "connected-3270" {
		t.Errorf("ConnectionState = %v", queries["ConnectionState"])
	}
	if queries["TelnetHostOptions"] != "BINARY END OF RECORD" {
		t.Errorf("TelnetHostOptions = %v — the negotiated telnet options are exactly what a screen scrape cannot tell you", queries["TelnetHostOptions"])
	}
	// A field s3270 reports as empty is still a field it reports. Dropping it
	// would make "the LU name is blank" indistinguishable from "this s3270 has
	// no LuName query".
	if v, present := queries["LuName"]; !present || v != "" {
		t.Errorf("LuName = %v, present = %v; want present and empty", v, present)
	}
	// available preserves s3270's own ordering, which is its grouping and reads
	// better than alphabetical.
	available, _ := body["available"].([]any)
	if len(available) == 0 || available[0] != "ConnectionState" {
		t.Errorf("available = %v, want s3270's order starting at ConnectionState", available)
	}
}

// The names are derived from s3270's own answer, never taken from the caller.
// A keyword this s3270 does not handle can BLOCK rather than error — the real
// binary does that for Tn3270eFunctions against a plain TN3270 server — and a
// blocked command on the s3270 pipe costs the whole session, because the only
// way out is to kill the subprocess. So an unknown name must be refused here,
// not forwarded.
func TestAPIQueryRefusesANameS3270DidNotOffer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, id, mh := newQueryTestApp(t)

	code, body := getQuery(t, r, "/api/v1/sessions/"+id+"/query?name=Tn3270eFunctions")
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%v", code, body)
	}
	if _, ok := body["available"]; !ok {
		t.Error("the refusal does not say which names would have worked")
	}
	for _, cmd := range mh.Commands {
		if strings.Contains(cmd, "Tn3270eFunctions") {
			t.Fatalf("the unknown name was forwarded to s3270 anyway: %q", cmd)
		}
	}
}

func TestAPIQueryNamedIsCaseInsensitiveAndEchoesS3270sSpelling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, id, _ := newQueryTestApp(t)

	code, body := getQuery(t, r, "/api/v1/sessions/"+id+"/query?name=terminalname")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%v", code, body)
	}
	if body["name"] != "TerminalName" {
		t.Errorf("name = %v, want s3270's own spelling TerminalName", body["name"])
	}
	if body["value"] != "IBM-3278-4-E" {
		t.Errorf("value = %v", body["value"])
	}
}

// s3270 abbreviates its longest fields to "..." in the bare response. Handing
// that back as the value would be reporting s3270's placeholder as the answer.
func TestAPIQueryFetchesElidedFieldsInFull(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, id, _ := newQueryTestApp(t)

	code, body := getQuery(t, r, "/api/v1/sessions/"+id+"/query?name=Copyright")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%v", code, body)
	}
	if body["value"] == "..." {
		t.Fatal("returned s3270's elision placeholder instead of the value")
	}
	if !strings.Contains(body["value"].(string), "Copyright") {
		t.Errorf("value = %v", body["value"])
	}
}

// A disconnected session cannot be queried, and saying so as a 409 is the
// difference between "you asked at the wrong time" and "the host is broken".
func TestAPIQueryOnDisconnectedSessionIs409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = false
	app := &App{SessionManager: session.NewManager()}
	sess := app.SessionManager.CreateSession(mh)

	r := gin.New()
	r.GET("/api/v1/sessions/:id/query", app.APIQuery)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/query", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", w.Code, w.Body.String())
	}
}

func TestParseQueryFields(t *testing.T) {
	fields, order := parseQueryFields("A: one\n\nB:\nC: two: three\nD\n")
	want := map[string]string{"A": "one", "B": "", "C": "two: three", "D": ""}
	for k, v := range want {
		if fields[k] != v {
			t.Errorf("%s = %q, want %q", k, fields[k], v)
		}
	}
	if len(order) != 4 {
		t.Errorf("order = %v, want four names", order)
	}
	// A value containing a colon must not be cut at the second one.
	if fields["C"] != "two: three" {
		t.Errorf("C = %q", fields["C"])
	}
	// A line with no colon at all is kept rather than dropped, so an s3270 that
	// reports something in an unexpected shape still shows up.
	if _, ok := fields["D"]; !ok {
		t.Error("a line with no colon was dropped")
	}
}

// The browser panel and the API read the same implementation through different
// doors. Two handlers would be how the two surfaces end up disagreeing about
// what a field is called, so this pins that they agree on a real session.
func TestHostQueryHandlerMatchesTheAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	mh.QueryResponses = map[string]string{"": fakeBareQuery}
	app := &App{SessionManager: session.NewManager()}
	sess := app.SessionManager.CreateSession(mh)

	r := gin.New()
	r.GET("/host/query", app.HostQueryHandler)
	r.GET("/api/v1/sessions/:id/query", app.APIQuery)

	cookieReq := httptest.NewRequest(http.MethodGet, "/host/query", nil)
	cookieReq.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	cookieW := httptest.NewRecorder()
	r.ServeHTTP(cookieW, cookieReq)
	if cookieW.Code != http.StatusOK {
		t.Fatalf("cookie route status = %d, want 200, body=%s", cookieW.Code, cookieW.Body.String())
	}

	_, apiBody := getQuery(t, r, "/api/v1/sessions/"+sess.ID+"/query")
	var browserBody map[string]any
	if err := json.Unmarshal(cookieW.Body.Bytes(), &browserBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(browserBody, apiBody) {
		t.Errorf("the two doors disagree:\n browser = %v\n api     = %v", browserBody, apiBody)
	}
}

// No session cookie is 401, not an empty panel: "you are not signed in" and
// "this connection reports nothing" are different answers.
func TestHostQueryHandlerWithoutASessionIs401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager()}
	r := gin.New()
	r.GET("/host/query", app.HostQueryHandler)

	req := httptest.NewRequest(http.MethodGet, "/host/query", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body=%s", w.Code, w.Body.String())
	}
}
