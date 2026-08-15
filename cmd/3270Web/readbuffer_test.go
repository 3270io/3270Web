package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
)

// seedBufferScreen puts a sign-on screen on the mock: a label, a visible user
// ID field, and a hidden password field holding a password, so the tests below
// can ask what the endpoints do with each.
//
//	row 0:  USERID SMITH
//	row 1:  PASSWD HUNTER2   <- hidden
func seedBufferScreen(t *testing.T, mock *host.MockHost) {
	t.Helper()
	if err := mock.WriteStringAt(0, 0, "USERID SMITH"); err != nil {
		t.Fatalf("seed row 0: %v", err)
	}
	if err := mock.WriteStringAt(1, 0, "PASSWD HUNTER2"); err != nil {
		t.Fatalf("seed row 1: %v", err)
	}
	screen := mock.GetScreen()
	// FieldCode 0x0c sets both display bits, which is what makes a field
	// hidden; 0x00 is an ordinary unprotected one.
	screen.Fields = []*host.Field{
		host.NewField(screen, 0x00, 7, 0, 11, 0, 0, 0),
		host.NewField(screen, 0x0c, 7, 1, 13, 1, 0, 0),
	}
}

func TestReadBufferServesARegionOfTheDisplay(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	seedBufferScreen(t, mock)

	w := doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/buffer?row=1&col=1&length=6", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", w.Code, w.Body.String())
	}
	var got struct {
		Row      int    `json:"row"`
		Col      int    `json:"col"`
		Length   int    `json:"length"`
		Encoding string `json:"encoding"`
		Text     string `json:"text"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, w.Body.String())
	}
	if got.Text != "USERID" {
		t.Errorf("text = %q, want USERID", got.Text)
	}
	// Quoted back the way it was asked for. A response that answered a question
	// about row 1 with "row 0" would be correct and useless.
	if got.Row != 1 || got.Col != 1 {
		t.Errorf("position = row %d col %d, want row 1 col 1", got.Row, got.Col)
	}
	if got.Encoding != "ascii" {
		t.Errorf("encoding = %q, want ascii", got.Encoding)
	}
}

// One-based at the boundary, zero-based at the terminal. The conversion happens
// in exactly one place, and this is what pins it: row 1 column 8 is the "S" of
// SMITH, which the terminal knows as row 0 column 7.
func TestReadBufferPositionsAreOneBased(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	seedBufferScreen(t, mock)

	w := doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/buffer?row=1&col=8&length=5", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"text":"SMITH"`) {
		t.Errorf("body = %s, want the region starting at the S of SMITH", w.Body.String())
	}

	for _, query := range []string{
		"row=0&col=1&length=4", "row=1&col=0&length=4", "row=1&col=1&length=0",
		"row=x&col=1&length=4", "col=1&length=4", "row=1&length=4", "row=1&col=1",
		fmt.Sprintf("row=1&col=1&length=%d", host.MaxRegionLength+1),
	} {
		w := doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/buffer?"+query, token, "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %s)", query, w.Code, w.Body.String())
		}
	}
}

// 3270 "hidden" only suppresses local echo on a real terminal — the characters
// are still in the buffer. A read that goes straight to the terminal would be
// the one way around the redaction every other serializer applies, so it is
// applied here too.
func TestReadBufferRedactsACellInsideAHiddenField(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	seedBufferScreen(t, mock)

	w := doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/buffer?row=2&col=1&length=14", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "HUNTER2") {
		t.Fatalf("the password crossed the wire: %s", body)
	}
	// The label is not in the hidden field and should still be readable — a
	// mask that blanked the whole region would be safe and would also make the
	// endpoint useless.
	if !strings.Contains(body, `"text":"PASSWD *******"`) {
		t.Errorf("body = %s, want the label intact and the hidden field masked", body)
	}
}

// The masking has to survive the encoding, or the way around it is to ask for
// the same region as code points.
func TestReadBufferRedactsTheHostsCodePointsToo(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	seedBufferScreen(t, mock)

	w := doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/buffer?row=2&col=8&length=7&encoding=ebcdic", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", w.Code, w.Body.String())
	}
	var got struct {
		Codes    []string `json:"codes"`
		Encoding string   `json:"encoding"`
		Text     string   `json:"text"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, w.Body.String())
	}
	if got.Encoding != "ebcdic" {
		t.Errorf("encoding = %q, want ebcdic", got.Encoding)
	}
	if got.Text != "" {
		t.Errorf("text = %q, want no text on a code-point read", got.Text)
	}
	if len(got.Codes) != 7 {
		t.Fatalf("codes = %v, want one per cell of the region", got.Codes)
	}
	// One placeholder per cell, so the list still lines up with the region that
	// was asked for, and not hex, so nothing downstream mistakes it for a byte
	// the host sent.
	for i, code := range got.Codes {
		if code != redactedCodePlaceholder {
			t.Errorf("code %d = %q, want every cell of a hidden field masked", i, code)
		}
	}
}

func TestReadBufferRefusesAnUnknownEncoding(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	seedBufferScreen(t, mock)

	w := doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/buffer?row=1&col=1&length=4&encoding=utf8", token, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ebcdic") {
		t.Errorf("refusal should name the encodings that would have worked, got %s", w.Body.String())
	}
}

func TestReadBufferFieldReportsTheFieldAtAPosition(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	seedBufferScreen(t, mock)

	w := doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/buffer/field?row=1&col=9", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", w.Code, w.Body.String())
	}
	var got struct {
		Row       int    `json:"row"`
		Col       int    `json:"col"`
		Text      string `json:"text"`
		Hidden    bool   `json:"hidden"`
		Protected bool   `json:"protected"`
		Formatted bool   `json:"formatted"`
		Attribute string `json:"attribute"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, w.Body.String())
	}
	if got.Text != "SMITH" {
		t.Errorf("text = %q, want SMITH", got.Text)
	}
	if got.Row != 1 || got.Col != 8 {
		t.Errorf("start = row %d col %d, want row 1 col 8 — one-based, like the request", got.Row, got.Col)
	}
	if got.Hidden || got.Protected {
		t.Errorf("the user ID field is neither hidden nor protected, got %+v", got)
	}
	if !got.Formatted {
		t.Error("a screen with fields on it is formatted")
	}
	if got.Attribute == "" {
		t.Error("the attribute byte is the reason to ask the terminal rather than this side")
	}
}

// The hiding is decided by the attribute byte the terminal reported, because
// this endpoint's whole claim is that it reads the buffer directly — and a
// password field is the worst possible place for that claim to hold in one
// direction only.
func TestReadBufferFieldWithholdsAHiddenFieldsContents(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	seedBufferScreen(t, mock)

	w := doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/buffer/field?row=2&col=9", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "HUNTER2") {
		t.Fatalf("the password crossed the wire: %s", body)
	}
	if !strings.Contains(body, `"hidden":true`) {
		t.Errorf("body = %s, want the field reported as hidden", body)
	}
	// The shape of the field is not the secret; its contents are. A caller
	// checking that the password field is seven cells wide has a real question,
	// and answering it discloses nothing.
	if !strings.Contains(body, `"length":7`) {
		t.Errorf("body = %s, want the field's extent still reported", body)
	}
	if strings.Contains(body, `"codes"`) {
		t.Errorf("body = %s, want no code points for a hidden field", body)
	}
}

func TestReadBufferOnDisconnectedSessionIs409(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	seedBufferScreen(t, mock)
	mock.Connected = false

	for _, path := range []string{"/buffer?row=1&col=1&length=4", "/buffer/field?row=1&col=1"} {
		w := doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+path, token, "")
		if w.Code != http.StatusConflict {
			t.Errorf("%s: status = %d, want 409 (body %s)", path, w.Code, w.Body.String())
		}
	}
}

func TestReadBufferRequiresAToken(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	seedBufferScreen(t, mock)
	_ = token

	for _, path := range []string{"/buffer?row=1&col=1&length=4", "/buffer/field?row=1&col=1"} {
		w := doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+path, "", "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401 (body %s)", path, w.Code, w.Body.String())
		}
	}
}

// This side not knowing where the password field is does not make it safe to
// publish the cells it might be in.
func TestRedactRegionRefusesWithoutAUsableParse(t *testing.T) {
	region := &host.RegionRead{Row: 0, Col: 0, Length: 4, Text: "SECRET"}
	if redactRegion(region, nil) {
		t.Error("a nil screen should not be reported as checked")
	}
	if redactRegion(region, &host.Screen{Width: 0, Height: 24}) {
		t.Error("a screen with no width should not be reported as checked")
	}
	// A screen that parsed and simply has no hidden fields is a different
	// answer, and the region is served.
	if !redactRegion(region, &host.Screen{Width: 80, Height: 24}) {
		t.Error("a parsed screen with no hidden fields should serve the region")
	}
	if region.Text != "SECRET" {
		t.Errorf("text = %q, want it untouched when there is nothing to mask", region.Text)
	}
}

// The newlines a wrapped region comes back with are separators, not cells.
// Counting them as cells shifts the mask one place further left for every row
// the region crosses, which quietly unmasks the end of the hidden field.
func TestRedactRegionStepsOverRowSeparators(t *testing.T) {
	screen := &host.Screen{Width: 4, Height: 2}
	screen.Fields = []*host.Field{host.NewField(screen, 0x0c, 0, 1, 3, 1, 0, 0)}
	region := &host.RegionRead{Row: 0, Col: 0, Length: 8, Text: "ABCD\nWXYZ"}
	if !redactRegion(region, screen) {
		t.Fatal("redactRegion reported it could not check a parsed screen")
	}
	if region.Text != "ABCD\n****" {
		t.Errorf("text = %q, want the second row masked and the first intact", region.Text)
	}
}
