// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
)

// decodeRC pulls the return code and body out of a response.
func decodeBody(t *testing.T, raw []byte) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v (body %s)", err, raw)
	}
	return out
}

func rcOf(t *testing.T, raw []byte) int {
	t.Helper()
	body := decodeBody(t, raw)
	rc, ok := body["rc"].(float64)
	if !ok {
		t.Fatalf("no rc in response: %s", raw)
	}
	return int(rc)
}

// A position is one-based and linear, because that is the arithmetic every
// HLLAPI program is built around. Changing the origin would mean auditing
// every offset in the code being ported, which is the rewrite this endpoint
// exists to avoid.
func TestHLLAPIPositionsAreOneBasedAndLinear(t *testing.T) {
	if row, col := positionToRowCol(1, 80); row != 0 || col != 0 {
		t.Errorf("position 1 = row %d col %d, want the top-left cell", row, col)
	}
	if row, col := positionToRowCol(81, 80); row != 1 || col != 0 {
		t.Errorf("position 81 on an 80-column screen = row %d col %d, want row 2 column 1", row, col)
	}
	if row, col := positionToRowCol(1920, 80); row != 23 || col != 79 {
		t.Errorf("position 1920 = row %d col %d, want the bottom-right cell", row, col)
	}
	for _, pos := range []int{1, 81, 160, 1920} {
		row, col := positionToRowCol(pos, 80)
		if got := rowColToPosition(row, col, 80); got != pos {
			t.Errorf("round trip of position %d gave %d", pos, got)
		}
	}
}

func TestParseHLLAPIKeysSplitsTextFromMnemonics(t *testing.T) {
	tokens, err := parseHLLAPIKeys("SMITH@E")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tokens) != 2 || tokens[0].Text != "SMITH" || tokens[1].Key != "Enter" {
		t.Fatalf("tokens = %+v, want SMITH then Enter", tokens)
	}

	// Order is the whole meaning of the call.
	tokens, err = parseHLLAPIKeys("@CLOGON@Tuser@E")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{"key:Clear", "text:LOGON", "key:Tab", "text:user", "key:Enter"}
	if len(tokens) != len(want) {
		t.Fatalf("tokens = %+v, want %v", tokens, want)
	}
	for i, w := range want {
		got := "text:" + tokens[i].Text
		if tokens[i].Key != "" {
			got = "key:" + tokens[i].Key
		}
		if got != w {
			t.Errorf("token %d = %s, want %s", i, got, w)
		}
	}

	// "@@" is a literal at-sign — how a program types an email address.
	tokens, err = parseHLLAPIKeys("a@@b.com")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tokens) != 1 || tokens[0].Text != "a@b.com" {
		t.Errorf("tokens = %+v, want the literal a@b.com", tokens)
	}
}

// A mnemonic mapped to the wrong key is worse than one that is not mapped:
// the wrong key reaches the host and the program carries on believing it did
// what it asked. Past the common core the vendor tables differ, so anything
// outside the table is refused rather than guessed at.
func TestParseHLLAPIKeysRefusesMnemonicsItDoesNotKnow(t *testing.T) {
	for _, in := range []string{"@q", "@", "SMITH@", "@Z"} {
		if _, err := parseHLLAPIKeys(in); err == nil {
			t.Errorf("parseHLLAPIKeys(%q) = nil error, want a refusal", in)
		}
	}
	// And the refusal points at the way out.
	_, err := parseHLLAPIKeys("@q")
	if err == nil || !strings.Contains(err.Error(), "PF13") {
		t.Errorf("refusal should say a key can be named instead, got %v", err)
	}
}

func TestResolveHLLAPIFunctionByNumberAndName(t *testing.T) {
	for _, raw := range []interface{}{float64(8), "8", "CopyPresentationSpaceToString", "copypresentationspacetostring"} {
		fn, ok := resolveHLLAPIFunction(raw)
		if !ok || fn.Number != 8 {
			t.Errorf("resolveHLLAPIFunction(%v) = (%+v, %v), want function 8", raw, fn, ok)
		}
	}
	for _, raw := range []interface{}{float64(99), "Teleport", ""} {
		if fn, ok := resolveHLLAPIFunction(raw); ok {
			t.Errorf("resolveHLLAPIFunction(%v) resolved to %+v", raw, fn)
		}
	}
}

// hllapiApp gives a session with known text on the screen.
func hllapiSetup(t *testing.T) (string, string, *host.MockHost, func(string) []byte) {
	t.Helper()
	_, r, sess, mock, token := snapshotTestApp(t)
	_ = mock.WriteStringAt(0, 0, "ACCOUNT ENQUIRY")
	_ = mock.WriteStringAt(2, 0, "CUSTOMER NUMBER")

	base := "/api/v1/sessions/" + sess.ID + "/hllapi"
	call := func(body string) []byte {
		w := doToken(t, r, http.MethodPost, base, token, body)
		if w.Code != http.StatusOK {
			t.Fatalf("HTTP %d for %s (body %s)", w.Code, body, w.Body.String())
		}
		return w.Body.Bytes()
	}
	return base, token, mock, call
}

// The endpoint answers 200 with a return code in the body. A caller ported
// from HLLAPI branches on the return code, and turning "string not found"
// into an HTTP error would make it a different program.
func TestHLLAPIAnswersWithReturnCodesNotHTTPErrors(t *testing.T) {
	_, _, _, call := hllapiSetup(t)

	if rc := rcOf(t, call(`{"function":6,"text":"NOT ON THIS SCREEN"}`)); rc != hllapiNotFound {
		t.Errorf("search for absent text: rc = %d, want %d", rc, hllapiNotFound)
	}
	if rc := rcOf(t, call(`{"function":40,"position":99999}`)); rc != hllapiInvalidPosition {
		t.Errorf("position past the screen: rc = %d, want %d", rc, hllapiInvalidPosition)
	}
	if rc := rcOf(t, call(`{"function":3,"text":"@q"}`)); rc != hllapiParameterError {
		t.Errorf("unknown mnemonic: rc = %d, want %d", rc, hllapiParameterError)
	}
}

func TestHLLAPISearchReturnsAOneBasedPosition(t *testing.T) {
	_, _, _, call := hllapiSetup(t)

	body := decodeBody(t, call(`{"function":6,"text":"ACCOUNT"}`))
	if int(body["rc"].(float64)) != hllapiOK {
		t.Fatalf("search: %v", body)
	}
	if pos := int(body["position"].(float64)); pos != 1 {
		t.Errorf("position = %d, want 1 — the text starts at the top-left cell", pos)
	}

	// Row 3, column 1 on an 80-column screen.
	body = decodeBody(t, call(`{"function":6,"text":"CUSTOMER"}`))
	if pos := int(body["position"].(float64)); pos != 161 {
		t.Errorf("position = %d, want 161 (row 3, column 1)", pos)
	}
}

func TestHLLAPICopyPresentationSpaceToString(t *testing.T) {
	_, _, _, call := hllapiSetup(t)

	body := decodeBody(t, call(`{"function":8,"position":1,"length":7}`))
	if got := body["data"].(string); got != "ACCOUNT" {
		t.Errorf("data = %q, want ACCOUNT", got)
	}

	// A length past the end is clamped rather than refused: the screen is a
	// fixed rectangle and "the rest of it" is a reasonable thing to ask for.
	body = decodeBody(t, call(`{"function":8,"position":1,"length":99999}`))
	if int(body["rc"].(float64)) != hllapiOK {
		t.Fatalf("clamped read: %v", body)
	}
	rows := int(body["rows"].(float64))
	cols := int(body["cols"].(float64))
	if got := len([]rune(body["data"].(string))); got != rows*cols {
		t.Errorf("read %d cells, want the whole %dx%d screen", got, rows, cols)
	}
}

func TestHLLAPIQueryCursorAndSetCursor(t *testing.T) {
	_, _, mock, call := hllapiSetup(t)

	if rc := rcOf(t, call(`{"function":40,"position":161}`)); rc != hllapiOK {
		t.Fatalf("SetCursor: rc = %d", rc)
	}
	if mock.Screen.CursorY != 2 || mock.Screen.CursorX != 0 {
		t.Errorf("cursor at row %d col %d, want row 2 col 0 (position 161)", mock.Screen.CursorY, mock.Screen.CursorX)
	}

	body := decodeBody(t, call(`{"function":7}`))
	if pos := int(body["position"].(float64)); pos != 161 {
		t.Errorf("QueryCursorLocation = %d, want the 161 just set", pos)
	}
	if row := int(body["row"].(float64)); row != 3 {
		t.Errorf("row = %d, want 3 — reported one-based like the position", row)
	}
}

// "SMITH@E" is the shape the whole endpoint exists for: type into the screen,
// then press the key, in that order.
func TestHLLAPISendKeyTypesThenPresses(t *testing.T) {
	_, _, mock, call := hllapiSetup(t)

	if rc := rcOf(t, call(`{"function":3,"text":"SMITH@E"}`)); rc != hllapiOK {
		t.Fatalf("SendKey: rc = %d", rc)
	}
	joined := strings.Join(mock.Commands, ",")
	writeAt := strings.Index(joined, "write")
	submitAt := strings.Index(joined, "submit")
	keyAt := strings.Index(joined, "key:Enter")
	if writeAt < 0 || keyAt < 0 {
		t.Fatalf("commands = %v, want a write and an Enter", mock.Commands)
	}
	if writeAt > keyAt {
		t.Errorf("commands = %v, want the text typed before the key", mock.Commands)
	}
	// A field write is local until an AID key is sent, so the pending write
	// has to be submitted with it — otherwise SMITH never reaches the host.
	if submitAt < 0 || submitAt > keyAt {
		t.Errorf("commands = %v, want the pending write submitted before Enter", mock.Commands)
	}
}

func TestHLLAPIFieldFunctions(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	mock.Screen.Fields = []*host.Field{
		// An unprotected field on row 3 (0-indexed row 2), columns 21-30.
		host.NewField(mock.Screen, 0x00, 20, 2, 29, 2, 0, 0),
	}
	base := "/api/v1/sessions/" + sess.ID + "/hllapi"
	call := func(body string) []byte {
		return doToken(t, r, http.MethodPost, base, token, body).Body.Bytes()
	}

	// Position 185 is row 3, column 25 — inside the field, but not its start.
	body := decodeBody(t, call(`{"function":31,"position":185}`))
	if int(body["rc"].(float64)) != hllapiOK {
		t.Fatalf("FindFieldPosition: %v", body)
	}
	if pos := int(body["position"].(float64)); pos != 181 {
		t.Errorf("field starts at %d, want 181 (row 3, column 21)", pos)
	}
	if length := int(body["length"].(float64)); length != 10 {
		t.Errorf("field length = %d, want 10", length)
	}

	// CopyStringToField writes from the start of the field, not from the
	// position given — that is what makes it a different function.
	if rc := rcOf(t, call(`{"function":33,"position":185,"text":"AB"}`)); rc != hllapiOK {
		t.Fatalf("CopyStringToField rc")
	}
	if got := string(mock.Screen.Buffer[2][20:22]); got != "AB" {
		t.Errorf("wrote %q at the field start, want AB", got)
	}

	// A position with no input field is not found, rather than silently
	// succeeding somewhere else.
	if rc := rcOf(t, call(`{"function":31,"position":1}`)); rc != hllapiNotFound {
		t.Errorf("FindFieldPosition off any field: rc = %d, want %d", rc, hllapiNotFound)
	}
}

// A hidden field is a password field, redacted everywhere else this
// application hands a screen out. This endpoint must not become the way
// around that.
func TestHLLAPICopyFieldToStringRedactsHiddenFields(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	hidden := host.NewField(mock.Screen, host.AttrDisp1|host.AttrDisp2, 20, 2, 29, 2, 0, 0)
	hidden.Value = "hunter2"
	mock.Screen.Fields = []*host.Field{hidden}
	if !hidden.IsHidden() {
		t.Fatalf("test fixture is not a hidden field; DisplayMode = %d", hidden.DisplayMode())
	}

	body := decodeBody(t, doToken(t, r, http.MethodPost,
		"/api/v1/sessions/"+sess.ID+"/hllapi", token, `{"function":34,"position":181}`).Body.Bytes())
	if data, _ := body["data"].(string); strings.Contains(data, "hunter2") {
		t.Fatalf("a hidden field's contents were returned: %v", body)
	}
	if hiddenFlag, _ := body["hidden"].(bool); !hiddenFlag {
		t.Errorf("the response should say the field was hidden, got %v", body)
	}
}

// Connect and Disconnect have nothing to do — the session is named in the
// path — but they answer 0 so ported code's opening and closing calls do not
// have to be deleted before it will run.
func TestHLLAPIConnectAndDisconnectSucceedAsNoOps(t *testing.T) {
	_, _, _, call := hllapiSetup(t)
	for _, fn := range []string{`{"function":1}`, `{"function":2}`} {
		if rc := rcOf(t, call(fn)); rc != hllapiOK {
			t.Errorf("%s: rc = %d, want 0", fn, rc)
		}
	}
}

// An unimplemented function is refused by number, with the list of what is
// implemented. Answering 0 for something that did not happen is the one thing
// a compatibility layer must never do.
func TestHLLAPIUnknownFunctionIsRefusedWithTheList(t *testing.T) {
	_, _, _, call := hllapiSetup(t)
	raw := call(`{"function":99}`)
	if rc := rcOf(t, raw); rc != hllapiUnsupported {
		t.Errorf("rc = %d, want %d", rc, hllapiUnsupported)
	}
	if !strings.Contains(string(raw), "CopyPresentationSpaceToString") {
		t.Errorf("refusal should list what is implemented, got %s", raw)
	}
}

func TestHLLAPIOnDisconnectedSessionReportsNotConnected(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	mock.Connected = false

	raw := doToken(t, r, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/hllapi", token, `{"function":7}`).Body.Bytes()
	if rc := rcOf(t, raw); rc != hllapiNotConnected {
		t.Errorf("rc = %d, want %d", rc, hllapiNotConnected)
	}
}
