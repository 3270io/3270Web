package profiler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

// fixedTime returns a deterministic Now function for tests.
func fixedTime(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// newMockHostWith returns a connected MockHost with a 24x80 blank screen and
// the given canned Query responses.
func newMockHostWith(queries map[string]string) *host.MockHost {
	m, _ := host.NewMockHost("")
	m.Connected = true
	if queries != nil {
		m.QueryResponses = queries
	}
	// Seed the status line so StatusDimensions / StatusKeyboardLocked work.
	if m.Screen != nil {
		m.Screen.Status = "U F P C(localhost) I 4 24 80 0 0 0x0 0.000"
		m.Screen.IsFormatted = true
		// Drop a recognisable banner into the first row so the screen
		// fingerprint is non-empty.
		if len(m.Screen.Buffer) > 0 {
			banner := []rune("MAINFRAME LOGON SCREEN")
			for i, r := range banner {
				if i < len(m.Screen.Buffer[0]) {
					m.Screen.Buffer[0][i] = r
				}
			}
		}
	}
	return m
}

func TestProbe_RichZOSLikeHost(t *testing.T) {
	mh := newMockHostWith(map[string]string{
		"Host":             "host mvs01.example.com 992 tls",
		"ConnectionState":  "tn3270e mvs01.example.com",
		"Bind":             "rows 32 cols 80 alt color extended",
		"Model":            "IBM-3279-2-E",
		"BindPluName":      "LU01",
		"Tn3270eFunctions": "BIND-IMAGE RESPONSES SYSREQ",
		"ScreenCurSize":    "24 80",
		"Cursor":           "1 1",
	})

	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	p, err := Probe(context.Background(), mh, ProbeOptions{
		CollectRaw: true,
		Tool:       "3270Web",
		Version:    "test",
		RunID:      "test-run-1",
		Now:        fixedTime(now),
	})
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if p.SchemaVersion != SchemaVersion {
		t.Fatalf("schema_version: got %q want %q", p.SchemaVersion, SchemaVersion)
	}
	if p.RunID != "test-run-1" {
		t.Errorf("RunID: got %q", p.RunID)
	}
	if !p.Timestamp.Equal(now.UTC()) {
		t.Errorf("Timestamp: got %v want %v", p.Timestamp, now.UTC())
	}
	if p.Profiler.Tool != "3270Web" || p.Profiler.Version != "test" {
		t.Errorf("Profiler meta: %+v", p.Profiler)
	}
	if p.Host.Host != "mvs01.example.com" {
		t.Errorf("Host.Host: %q", p.Host.Host)
	}
	if p.Host.Port != 992 {
		t.Errorf("Host.Port: %d", p.Host.Port)
	}
	if !p.Host.TLS {
		t.Errorf("Host.TLS should be true")
	}
	if p.Host.BannerSignature == "" || p.Host.BannerPreview == "" {
		t.Errorf("expected banner_signature and banner_preview, got sig=%q preview=%q", p.Host.BannerSignature, p.Host.BannerPreview)
	}
	if p.Device.TerminalType != "IBM-3279-2-E" {
		t.Errorf("Device.TerminalType: %q", p.Device.TerminalType)
	}
	if p.Device.Rows != 32 { // Bind value wins over ScreenCurSize because it's parsed first.
		t.Errorf("Device.Rows: %d", p.Device.Rows)
	}
	if p.Device.Cols != 80 {
		t.Errorf("Device.Cols: %d", p.Device.Cols)
	}
	if !p.Device.Color {
		t.Errorf("Device.Color should be true (3279 + bind color)")
	}
	if !p.Device.ExtendedAttributes {
		t.Errorf("Device.ExtendedAttributes should be true (-E suffix + bind extended)")
	}
	if !p.Device.AltScreen {
		t.Errorf("Device.AltScreen should be true (bind alt)")
	}
	if !p.Protocol.TN3270E || !p.Protocol.BindImagePresent || !p.Protocol.StructuredFields {
		t.Errorf("Protocol flags: %+v", p.Protocol)
	}
	if p.Protocol.LUName != "LU01" {
		t.Errorf("Protocol.LUName: %q", p.Protocol.LUName)
	}
	want := []string{"BIND-IMAGE", "RESPONSES", "SYSREQ"}
	if !equalStrings(p.Protocol.NegotiatedFunctions, want) {
		t.Errorf("Protocol.NegotiatedFunctions: got %v want %v", p.Protocol.NegotiatedFunctions, want)
	}
	if len(p.Capabilities.Unknown) != 0 {
		t.Errorf("expected no Unknown queries, got %v", p.Capabilities.Unknown)
	}
	if p.Capabilities.INDFile != TriUnknown { // opts.INDFileProbe defaulted to false
		t.Errorf("Capabilities.INDFile: %q (expected unknown)", p.Capabilities.INDFile)
	}
	if p.Raw == nil || p.Raw["Bind"] == "" {
		t.Errorf("Raw should contain Bind response when CollectRaw=true")
	}
}

func TestProbe_MuteHostMarksEverythingUnknown(t *testing.T) {
	mh := newMockHostWith(nil) // no canned responses → empty string for every query
	p, err := Probe(context.Background(), mh, ProbeOptions{
		Tool:    "3270Web",
		Version: "test",
		RunID:   "mute-1",
		Now:     fixedTime(time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if len(p.Capabilities.Unknown) != len(probeSequence()) {
		t.Errorf("expected all %d queries unknown, got %d: %v",
			len(probeSequence()), len(p.Capabilities.Unknown), p.Capabilities.Unknown)
	}
	if !sortedAscending(p.Capabilities.Unknown) {
		t.Errorf("Unknown should be sorted: %v", p.Capabilities.Unknown)
	}
	// Fallbacks from status line should still populate rows/cols (24x80 in fixture).
	if p.Device.Rows != 24 || p.Device.Cols != 80 {
		t.Errorf("status fallback: rows=%d cols=%d (want 24x80)", p.Device.Rows, p.Device.Cols)
	}
	// Model fallback: status idx 5 is "4" → "Model-4".
	if p.Device.TerminalType != "Model-4" {
		t.Errorf("status TerminalType fallback: got %q want %q", p.Device.TerminalType, "Model-4")
	}
}

func TestProbe_NotConnectedReturnsError(t *testing.T) {
	m, _ := host.NewMockHost("")
	m.Connected = false
	_, err := Probe(context.Background(), m, ProbeOptions{})
	if err == nil {
		t.Fatal("expected error when host is not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("error message should mention 'not connected', got %q", err.Error())
	}
}

func TestProbe_ContextCancellationPropagates(t *testing.T) {
	mh := newMockHostWith(map[string]string{"Host": "host x.example 23"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Probe(ctx, mh, ProbeOptions{})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestProbe_JSONShapeIsStableAcrossRuns(t *testing.T) {
	mh := newMockHostWith(map[string]string{
		"Host":  "host h.example 23 tls",
		"Model": "3279-2",
	})
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	opts := ProbeOptions{Tool: "t", Version: "v", RunID: "stable", Now: fixedTime(now)}
	a, _ := Probe(context.Background(), mh, opts)
	b, _ := Probe(context.Background(), mh, opts)
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	if string(aJSON) != string(bJSON) {
		t.Errorf("two probes with identical inputs produced different JSON:\nA: %s\nB: %s", aJSON, bJSON)
	}
}

func TestApplyQueryHost_VariousFormats(t *testing.T) {
	cases := []struct {
		name string
		resp string
		host string
		port int
		tls  bool
	}{
		{"verb-host-port", "host mvs.example.com 23", "mvs.example.com", 23, false},
		{"verb-host-port-tls", "host mvs.example.com 992 tls", "mvs.example.com", 992, true},
		{"connect-verb", "Connect mvs.example.com 23", "mvs.example.com", 23, false},
		{"colon-form", "rocket.example.com:992 ssl", "rocket.example.com", 992, true},
		{"bare-host", "h1", "h1", 0, false},
		{"empty", "", "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &CompatibilityProfile{}
			applyQueryHost(p, tc.resp)
			if p.Host.Host != tc.host {
				t.Errorf("host: got %q want %q", p.Host.Host, tc.host)
			}
			if p.Host.Port != tc.port {
				t.Errorf("port: got %d want %d", p.Host.Port, tc.port)
			}
			if p.Host.TLS != tc.tls {
				t.Errorf("tls: got %v want %v", p.Host.TLS, tc.tls)
			}
		})
	}
}

func TestNormaliseScreenText(t *testing.T) {
	in := "  hello\x00\x01 \tworld   here  \n  "
	out := normaliseScreenText(in)
	if out != "hello world here" {
		t.Errorf("normaliseScreenText: got %q", out)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortedAscending(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}
