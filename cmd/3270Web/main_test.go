package main

import "testing"

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
