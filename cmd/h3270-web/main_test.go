package main

import (
	"path/filepath"
	"testing"
)

func TestParseSampleAppHost(t *testing.T) {
	tests := []struct {
		input   string
		wantID  string
		wantOK  bool
	}{
		{input: "sampleapp:app1", wantID: "app1", wantOK: true},
		{input: " sampleapp:app2 ", wantID: "app2", wantOK: true},
		{input: "sampleapp:", wantID: "", wantOK: false},
		{input: "mock", wantID: "", wantOK: false},
	}

	for _, test := range tests {
		gotID, gotOK := parseSampleAppHost(test.input)
		if gotOK != test.wantOK {
			t.Fatalf("parseSampleAppHost(%q) ok=%v, want %v", test.input, gotOK, test.wantOK)
		}
		if gotID != test.wantID {
			t.Fatalf("parseSampleAppHost(%q) id=%q, want %q", test.input, gotID, test.wantID)
		}
	}
}

func TestSampleAppHostname(t *testing.T) {
	if got := sampleAppHostname("app1"); got != "sampleapp:app1" {
		t.Fatalf("sampleAppHostname returned %q", got)
	}
}

func TestResolveSampleDumpPath(t *testing.T) {
	path := resolveSampleDumpPath("advantis.dump")
	if path == "" {
		t.Fatal("expected sample dump path")
	}
	if filepath.Base(path) != "advantis.dump" {
		t.Fatalf("expected advantis.dump, got %q", filepath.Base(path))
	}
	if got := resolveSampleDumpPath("missing.dump"); got != "" {
		t.Fatalf("expected empty path, got %q", got)
	}
}
