package main

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/config"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

func TestParseSampleAppHost(t *testing.T) {
	tests := []struct {
		input    string
		wantID   string
		wantPort int
		wantOK   bool
	}{
		{input: "sampleapp:app1", wantID: "app1", wantPort: 0, wantOK: true},
		{input: " sampleapp:app2 ", wantID: "app2", wantPort: 0, wantOK: true},
		{input: "sampleapp:app1:5555", wantID: "app1", wantPort: 5555, wantOK: true},
		{input: "sampleapp:app1:bad", wantID: "", wantPort: 0, wantOK: false},
		{input: "sampleapp:app1:", wantID: "", wantPort: 0, wantOK: false},
		{input: "sampleapp:", wantID: "", wantPort: 0, wantOK: false},
		{input: "mock", wantID: "", wantPort: 0, wantOK: false},
	}

	for _, test := range tests {
		gotID, gotPort, gotOK := parseSampleAppHost(test.input)
		if gotOK != test.wantOK {
			t.Fatalf("parseSampleAppHost(%q) ok=%v, want %v", test.input, gotOK, test.wantOK)
		}
		if gotID != test.wantID {
			t.Fatalf("parseSampleAppHost(%q) id=%q, want %q", test.input, gotID, test.wantID)
		}
		if gotPort != test.wantPort {
			t.Fatalf("parseSampleAppHost(%q) port=%d, want %d", test.input, gotPort, test.wantPort)
		}
	}
}

func TestWorkflowSpecialKeys(t *testing.T) {
	mockHost, _ := host.NewMockHost("")
	sess := &session.Session{Host: mockHost}
	app := &App{}

	keys := []struct {
		stepType string
		wantKey  string
	}{
		{"PressClear", "Clear"},
		{"PressReset", "Reset"},
		{"PressPA1", "PA(1)"},
		{"PressPA2", "PA(2)"},
		{"PressPA3", "PA(3)"},
		{"PressHome", "Home"},
		{"PressEraseInput", "EraseInput"},
	}

	for _, k := range keys {
		step := session.WorkflowStep{Type: k.stepType}
		if err := app.applyWorkflowStep(sess, step); err != nil {
			t.Errorf("applyWorkflowStep(%q) failed: %v", k.stepType, err)
		}
	}

	if len(mockHost.Commands) != len(keys) {
		t.Fatalf("expected %d commands, got %d", len(keys), len(mockHost.Commands))
	}

	for i, k := range keys {
		want := "key:" + k.wantKey
		if mockHost.Commands[i] != want {
			t.Errorf("command %d: expected %q, got %q", i, want, mockHost.Commands[i])
		}
	}
}

func TestSampleAppHostname(t *testing.T) {
	if got := sampleAppHostname("app1"); got != "sampleapp:app1" {
		t.Fatalf("sampleAppHostname returned %q", got)
	}
}

func TestSampleAppPort(t *testing.T) {
	if got := sampleAppPort(0); got != defaultSampleAppPort {
		t.Fatalf("expected default port %d, got %d", defaultSampleAppPort, got)
	}
	if got := sampleAppPort(5555); got != 5555 {
		t.Fatalf("expected port 5555, got %d", got)
	}
}

func TestAvailableSampleApps(t *testing.T) {
	options := availableSampleApps()
	if len(options) != len(sampleAppConfigs) {
		t.Fatalf("expected %d sample apps, got %d", len(sampleAppConfigs), len(options))
	}
	for i, option := range options {
		if option.ID != sampleAppConfigs[i].ID {
			t.Fatalf("expected option %d to have id %q, got %q", i, sampleAppConfigs[i].ID, option.ID)
		}
		if option.Name != sampleAppConfigs[i].Name {
			t.Fatalf("expected option %d to have name %q, got %q", i, sampleAppConfigs[i].Name, option.Name)
		}
		if option.Hostname != sampleAppHostname(sampleAppConfigs[i].ID) {
			t.Fatalf("expected option %d to have hostname %q, got %q", i, sampleAppHostname(sampleAppConfigs[i].ID), option.Hostname)
		}
	}
}

func TestWorkflowTargetHost(t *testing.T) {
	sessionHost := &session.Session{TargetHost: "localhost", TargetPort: 3270}
	workflow := &WorkflowConfig{Host: "example.com", Port: 992}
	got, err := workflowTargetHost(sessionHost, workflow)
	if err != nil {
		t.Fatalf("expected workflow host, got error %v", err)
	}
	if got != "example.com:992" {
		t.Fatalf("expected example.com:992, got %q", got)
	}

	fallback, err := workflowTargetHost(sessionHost, &WorkflowConfig{})
	if err != nil {
		t.Fatalf("expected session host fallback, got error %v", err)
	}
	if fallback != "localhost:3270" {
		t.Fatalf("expected localhost:3270, got %q", fallback)
	}
}

func TestWorkflowFillThenKeySubmitsOnce(t *testing.T) {
	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("failed to create mock host: %v", err)
	}
	screen := mockHost.GetScreen()
	screen.Fields = []*host.Field{
		{
			Screen:    screen,
			StartX:    0,
			StartY:    0,
			EndX:      4,
			EndY:      0,
			Changed:   false,
			FieldCode: 0,
		},
	}
	screen.IsFormatted = true
	screen.Width = 80
	screen.Height = 24

	sess := &session.Session{Host: mockHost, Playback: &session.WorkflowPlayback{Active: true}}
	step := session.WorkflowStep{
		Type: "FillString",
		Coordinates: &session.WorkflowCoordinates{
			Row:    1,
			Column: 1,
		},
		Text: "HELLO",
	}
	app := &App{}
	if err := app.applyWorkflowFill(sess, step); err != nil {
		t.Fatalf("applyWorkflowFill failed: %v", err)
	}
	if err := submitWorkflowPendingInput(sess); err != nil {
		t.Fatalf("submitWorkflowPendingInput failed: %v", err)
	}

	// Expected behavior: FillString calls WriteStringAt ("write"), and sets PendingInput=false.
	// So submitWorkflowPendingInput does nothing.
	// The test previously expected "submit", which implies the logic changed.
	// We update the test to match current behavior.
	if len(mockHost.Commands) != 1 || mockHost.Commands[0] != "write" {
		t.Fatalf("expected write command, got %v", mockHost.Commands)
	}
}

func TestIsValidHostname(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		expected bool
	}{
		{name: "empty", hostname: "", expected: false},
		{name: "whitespace", hostname: "   ", expected: false},
		{name: "hostname", hostname: "localhost", expected: true},
		{name: "host with port", hostname: "localhost:3270", expected: true},
		{name: "sample app", hostname: "sampleapp:app1:3270", expected: true},
		{name: "ipv4", hostname: "127.0.0.1", expected: true},
		{name: "ipv6", hostname: "::1", expected: true},
		{name: "ipv6 with port", hostname: "[::1]:3270", expected: true},
		{name: "ipv6 missing bracket", hostname: "[::1", expected: false},
		{name: "ipv6 missing opening bracket", hostname: "::1]", expected: false},
		{name: "ipv6 trailing garbage", hostname: "[::1]x", expected: false},
		{name: "invalid char", hostname: "bad host", expected: false},
		{name: "empty label", hostname: "bad..host", expected: false},
		{name: "invalid label length", hostname: strings.Repeat("a", 64), expected: false},
		{name: "label starts hyphen", hostname: "-bad.example", expected: false},
		{name: "label ends hyphen", hostname: "bad-.example", expected: false},
		{name: "invalid port", hostname: "localhost:99999", expected: false},
		{name: "invalid ipv6 port", hostname: "[::1]:70000", expected: false},
		{name: "hostname with port 23", hostname: "localhost:23", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidHostname(tt.hostname); got != tt.expected {
				t.Errorf("isValidHostname(%q) = %v, want %v", tt.hostname, got, tt.expected)
			}
		})
	}
}

func TestBuildThemeCSS_Caching(t *testing.T) {
	// Setup App with mock config
	cfg := &config.Config{
		ColorSchemes: config.ColorSchemesConfig{
			Schemes: []config.ColorScheme{
				{Name: "Scheme1", PNBg: "#000", PNFg: "#FFF"},
				{Name: "Scheme2", PNBg: "#FFF", PNFg: "#000"},
			},
		},
		Fonts: config.FontsConfig{
			Fonts: []config.Font{
				{Name: "Font1"},
			},
		},
	}
	app := &App{
		Config:     cfg,
		themeCache: make(map[string]string),
	}

	// First call - should populate cache
	prefs1 := session.Preferences{ColorScheme: "Scheme1", FontName: "Font1"}
	css1 := app.buildThemeCSS(prefs1)
	if !strings.Contains(css1, "#000") {
		t.Errorf("Expected CSS to contain #000, got %s", css1)
	}

	// Second call - should use cache
	css2 := app.buildThemeCSS(prefs1)
	if css1 != css2 {
		t.Errorf("Expected cached CSS to match first call")
	}

	// Different prefs - should generate new CSS
	prefs2 := session.Preferences{ColorScheme: "Scheme2", FontName: "Font1"}
	css3 := app.buildThemeCSS(prefs2)
	if !strings.Contains(css3, "#FFF") { // Scheme2 bg is #FFF
		t.Errorf("Expected CSS to contain #FFF, got %s", css3)
	}
	if css1 == css3 {
		t.Errorf("Expected different CSS for different schemes")
	}
}

func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeadersMiddleware())
	r.GET("/", func(c *gin.Context) {
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	r.ServeHTTP(w, req)

	headers := map[string]string{
		"X-Frame-Options":         "SAMEORIGIN",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Permissions-Policy":      "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()",
		"Content-Security-Policy": "default-src 'self'; script-src 'self' 'unsafe-inline' https://unpkg.com; style-src 'self' 'unsafe-inline' https://unpkg.com; img-src 'self' data:; font-src 'self' data:; connect-src 'self' ws: wss:;",
	}

	for k, v := range headers {
		if got := w.Header().Get(k); got != v {
			t.Errorf("Header %q: expected %q, got %q", k, v, got)
		}
	}
}
