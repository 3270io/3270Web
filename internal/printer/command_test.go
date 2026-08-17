// SPDX-License-Identifier: AGPL-3.0-or-later

package printer

import (
	"strings"
	"testing"
)

func TestSpoolCommandQuoting(t *testing.T) {
	cases := []struct {
		name    string
		exe     string
		dir     string
		windows bool
		want    string
	}{
		{
			"a plain unix install",
			"/app/3270Web", "/data/printer-jobs/abc", false,
			"'/app/3270Web' print-spool -dir '/data/printer-jobs/abc'",
		},
		{
			// The realistic awkward case rather than a hostile one: a space
			// in the path is ordinary and would split the command in two.
			"a unix path with a space",
			"/opt/3270 Web/3270Web", "/data/print jobs/abc", false,
			"'/opt/3270 Web/3270Web' print-spool -dir '/data/print jobs/abc'",
		},
		{
			"a unix path with a quote in it",
			"/home/o'brien/3270Web", "/tmp/jobs", false,
			`'/home/o'\''brien/3270Web' print-spool -dir '/tmp/jobs'`,
		},
		{
			"a windows install",
			`C:\Program Files\3270Web\3270Web.exe`, `C:\ProgramData\3270Web\printer-jobs\abc`, true,
			`"C:\Program Files\3270Web\3270Web.exe" print-spool -dir "C:\ProgramData\3270Web\printer-jobs\abc"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := spoolCommand(tc.exe, tc.dir, tc.windows)
			if err != nil {
				t.Fatalf("spoolCommand: %v", err)
			}
			if got != tc.want {
				t.Errorf("spoolCommand()\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// The paths here are the server's own, so these can only trip on a bug. They
// are refused anyway, because "the input is trusted" is a claim that has to
// keep being true as the code around it changes.
func TestSpoolCommandRefusesUnquotablePaths(t *testing.T) {
	cases := []struct {
		name    string
		exe     string
		dir     string
		windows bool
	}{
		{"a newline in the executable", "/app/3270Web\nrm -rf /", "/tmp/jobs", false},
		{"a newline in the directory", "/app/3270Web", "/tmp/jobs\nrm -rf /", false},
		{"a NUL byte", "/app/3270Web", "/tmp/jobs\x00", false},
		{"a double quote on windows", `C:\3270Web".exe`, `C:\jobs`, true},
		{"an expandable variable on windows", `C:\3270Web.exe`, `C:\%APPDATA%\jobs`, true},
		{"no executable", "", "/tmp/jobs", false},
		{"no directory", "/app/3270Web", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := spoolCommand(tc.exe, tc.dir, tc.windows); err == nil {
				t.Errorf("spoolCommand() = %q, want a refusal", got)
			}
		})
	}
}

// The subcommand named in the command line has to be the one main() dispatches
// on, or every print job fails at the moment a host sends one.
func TestSpoolCommandNamesTheSubcommand(t *testing.T) {
	got, err := spoolCommand("/app/3270Web", "/tmp/jobs", false)
	if err != nil {
		t.Fatalf("spoolCommand: %v", err)
	}
	if !strings.Contains(got, " "+SpoolSubcommand+" ") {
		t.Errorf("spoolCommand() = %q, want it to invoke %q", got, SpoolSubcommand)
	}
}
