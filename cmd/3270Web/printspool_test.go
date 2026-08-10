package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/printer"
)

// The spool helper is what pr3287 actually runs, so a break here is a print
// job that vanishes at the moment a host sends one — with nothing in the
// server's log, because the server is not in the loop.
func TestRunPrintSpoolStoresTheJob(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer

	code := runPrintSpool([]string{"-dir", dir}, strings.NewReader("DAILY REPORT\n"), &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %s)", code, stderr.String())
	}

	jobs, err := (printer.Spool{Dir: dir}).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("spool holds %d jobs, want 1", len(jobs))
	}
	body, err := os.ReadFile(filepath.Join(dir, jobs[0].Name))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != "DAILY REPORT\n" {
		t.Errorf("stored %q, want the bytes on standard input", body)
	}
}

// A non-zero exit is how a job that could not be stored becomes visible:
// pr3287 reports it, rather than the job disappearing quietly.
func TestRunPrintSpoolFailsLoudly(t *testing.T) {
	var stderr bytes.Buffer

	if code := runPrintSpool(nil, strings.NewReader("REPORT"), &stderr); code == 0 {
		t.Error("exit code = 0 with no -dir given")
	}
	if !strings.Contains(stderr.String(), "-dir") {
		t.Errorf("stderr = %q, want it to name the missing flag", stderr.String())
	}

	stderr.Reset()
	missing := filepath.Join(t.TempDir(), "gone")
	if code := runPrintSpool([]string{"-dir", missing}, strings.NewReader("REPORT"), &stderr); code == 0 {
		t.Error("exit code = 0 writing into a directory that does not exist")
	}
}
