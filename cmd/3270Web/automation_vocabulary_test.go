// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The automation switch and the recording workflow each had several names for
// one thing: "Engineering tools" in the account menu next to a sub-label that
// read "Business", "Show automation" on the view bar, "Engineering mode" in a
// toast, "Switch workspace mode" in the palette; "Record a workflow" in one
// dialog and "Start recording" in the menu; "Workflow JSON" over a recording;
// a "Workflow status" widget that also reports exploration.
//
// Nothing fails when somebody reintroduces one of those names in a new
// message, which is how there came to be four of them. This pins the words a
// user reads. It deliberately looks at strings only: the stored preference
// values ("business", "engineering"), the data attributes and the routes keep
// their names, because accounts and other modules hold them.
func TestAutomationAndRecordingVocabulary(t *testing.T) {
	type source struct {
		name      string
		read      func(*testing.T, string) string
		file      string
		want      []string
		forbidden []string
	}

	sources := []source{
		{
			name: "account menu, view bar, recording dialog and Run status widget",
			read: templateSource,
			file: "screen.html",
			want: []string{
				`Automation tools<span class="appmenu-sub" data-workspace-label>Off</span>`,
				`data-show-automation>Show automation tools</button>`,
				`data-start-goal="record">Start recording</button>`,
				`data-modal-title>Recording (JSON)</h3>`,
				`<strong>Run status</strong>`,
				`aria-label="Track run status"`,
				`aria-label="Maximize run status"`,
				`aria-label="Restore run status"`,
				`data-chaos-run-panel-export>Download workflow JSON</button>`,
			},
			forbidden: []string{
				"Engineering tools", "Engineering mode", "Business mode",
				"Show automation</button>", "Record a workflow", ">Workflow JSON<",
				"Workflow status", "workflow status",
			},
		},
		{
			name: "automation switch",
			read: staticSource,
			file: "workspace-mode.js",
			want: []string{
				`"Automation tools on — the Automation menu is available."`,
				`"Automation tools off — the Automation menu is hidden."`,
				`automationVisible ? "On" : "Off"`,
				`"Automation tools: On — `,
				`"Automation tools: Off — `,
			},
			forbidden: []string{`"Engineering`, `"Business`},
		},
		{
			name:      "command palette",
			read:      staticSource,
			file:      "ui-polish.js",
			want:      []string{`label: "Turn automation tools on or off"`},
			forbidden: []string{"Switch workspace mode", "Business ↔ Engineering"},
		},
		{
			name: "Tasks empty state",
			read: staticSource,
			file: "tasks.js",
			want: []string{
				`record.textContent = "Start recording";`,
				`Automation tools is off`,
			},
			forbidden: []string{"Record a workflow", "record a workflow", "Business mode", "Engineering tools", "Engineering mode"},
		},
		{
			name: "recording dialog and run status widget controls",
			read: staticSource,
			file: "workflow.js",
			want: []string{
				`options.title || 'Recording (JSON)'`,
				`'Restore run status'`,
				`'Minimize run status'`,
				`'Maximize run status'`,
			},
			forbidden: []string{"'Workflow JSON'", "'Loaded Recording'", "workflow status", "Workflow status", "workflow JSON"},
		},
	}

	for _, s := range sources {
		src := s.read(t, s.file)
		for _, want := range s.want {
			if !strings.Contains(src, want) {
				t.Errorf("%s (%s): missing %q", s.name, s.file, want)
			}
		}
		for _, bad := range s.forbidden {
			if strings.Contains(src, bad) {
				t.Errorf("%s (%s): still contains %q, the old name", s.name, s.file, bad)
			}
		}
	}
}

// The docs describe the setting by the name the interface uses. The old names
// are mentioned once, in the page that explains the setting, so a reader who
// remembers "Engineering tools" can find it; everywhere else they are errors.
func TestDocsUseTheCurrentNames(t *testing.T) {
	root := filepath.Join("..", "..")
	files, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no docs found: %v", err)
	}
	files = append(files, filepath.Join(root, "README.md"))

	// Names that must not appear anywhere in the docs.
	forbidden := []string{
		"Record a workflow", "Show automation**", "Workflow Status widget",
		"Workflow Status panel", "workflow status widget", "workspace mode",
		"Business mode", "Engineering mode",
	}
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		text := string(b)
		for _, bad := range forbidden {
			if strings.Contains(text, bad) {
				t.Errorf("%s: still contains %q, the old name", filepath.Base(file), bad)
			}
		}
		// The previous name of the setting may be named only where it is
		// explained.
		if strings.Contains(text, "Engineering tools") && filepath.Base(file) != "keyboard-and-controls.md" {
			t.Errorf("%s: names the previous setting \"Engineering tools\"; "+
				"only keyboard-and-controls.md, which explains it, should", filepath.Base(file))
		}
	}

	explained, err := os.ReadFile(filepath.Join(root, "docs", "keyboard-and-controls.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"### Automation tools", "previously called **Engineering tools**"} {
		if !strings.Contains(string(explained), want) {
			t.Errorf("keyboard-and-controls.md: missing %q", want)
		}
	}
}
