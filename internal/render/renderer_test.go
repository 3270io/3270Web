package render

import (
	"os"
	"strings"
	"testing"

	"github.com/h3270/h3270-go/internal/host"
)

func TestHtmlRenderer(t *testing.T) {
	// Adjust path relative to where test is run (internal/render)
	dumpPath := "../../webapp/WEB-INF/dump/advantis.dump"
	f, err := os.Open(dumpPath)
	if err != nil {
		t.Skipf("Skipping, dump not found at %s: %v", dumpPath, err)
	}
	defer f.Close()

	screen, err := host.NewScreenFromDump(f)
	if err != nil {
		t.Fatalf("Failed to parse dump: %v", err)
	}

	renderer := NewHtmlRenderer()
	htmlOutput := renderer.Render(screen, "/submit", "123")

	if !strings.Contains(htmlOutput, `<form id="screen-123"`) {
		t.Error("HTML output missing form tag with correct ID")
	}

	if !strings.Contains(htmlOutput, "SYSTEM:") {
		t.Error("HTML output missing text content 'SYSTEM:'")
	}

	if !strings.Contains(htmlOutput, "<input") {
		t.Error("HTML output missing input tags")
	}

	if !strings.Contains(htmlOutput, `name="TERMINAL" value="123"`) {
		t.Error("HTML output missing TERMINAL hidden field")
	}

	// Check for installKeyHandler script
	if !strings.Contains(htmlOutput, "installKeyHandler('screen-123');") {
		t.Error("HTML output missing installKeyHandler call")
	}
}
