// SPDX-License-Identifier: AGPL-3.0-or-later

package audit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestRecorder(t *testing.T) *Recorder {
	t.Helper()
	return NewRecorder(filepath.Join(t.TempDir(), "audit.log"))
}

func TestLogAndRead(t *testing.T) {
	r := newTestRecorder(t)

	r.Log(Entry{
		Event:    EventLoginSucceeded,
		Actor:    Actor{UserID: "aaaa1111", Username: "alice", Role: "user", Kind: "web"},
		ClientIP: "10.0.0.5",
	})
	r.Log(Entry{
		Event:    EventSessionOpened,
		Actor:    Actor{UserID: "aaaa1111", Username: "alice"},
		Target:   "mainframe.example.test:23",
		ClientIP: "10.0.0.5",
		Detail:   map[string]string{"sessionId": "abc123"},
	})

	entries, err := r.Read(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("read %d entries, want 2", len(entries))
	}
	// Newest first, which is the order somebody reading a trail wants.
	if entries[0].Event != EventSessionOpened {
		t.Errorf("first entry = %s, want the most recent", entries[0].Event)
	}
	if entries[0].Target != "mainframe.example.test:23" {
		t.Errorf("target = %q", entries[0].Target)
	}
	if entries[0].Detail["sessionId"] != "abc123" {
		t.Errorf("detail = %v", entries[0].Detail)
	}
	// An unset outcome means it happened; leaving it empty would make every
	// query have to special-case the blank.
	if entries[0].Outcome != Success {
		t.Errorf("outcome = %q, want %q", entries[0].Outcome, Success)
	}
	if entries[0].Time.IsZero() {
		t.Error("the entry has no time")
	}
	if entries[1].Actor.Username != "alice" {
		t.Errorf("actor = %+v", entries[1].Actor)
	}
}

// The file is read by administrators and can be shipped elsewhere, so it must
// not be readable by other accounts on the host.
func TestFileIsOwnerOnly(t *testing.T) {
	r := newTestRecorder(t)
	r.Log(Entry{Event: EventLogout})

	info, err := os.Stat(r.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

// One JSON object per line, so the file can be read with the tools an
// operator already has rather than only by this program.
func TestFormatIsOneObjectPerLine(t *testing.T) {
	r := newTestRecorder(t)
	for i := 0; i < 3; i++ {
		r.Log(Entry{Event: EventLoginFailed, Outcome: Failure, Target: "alice"})
	}

	data, err := os.ReadFile(r.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3:\n%s", len(lines), data)
	}
	for i, line := range lines {
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d is not JSON: %v", i, err)
		}
	}
}

// Appending, not rewriting: an audit that can lose earlier lines is not one.
func TestAppendsRatherThanReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	NewRecorder(path).Log(Entry{Event: EventLoginSucceeded, Target: "first"})
	// A second Recorder over the same file — the CLI and the server both
	// write here.
	NewRecorder(path).Log(Entry{Event: EventTokenIssued, Target: "second"})

	entries, err := NewRecorder(path).Read(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("read %d, want both writers' entries", len(entries))
	}
}

// Rolling over bounds the file without making the last few minutes vanish
// from a read. Only two generations are kept, so this asserts that a read
// spans both — not that nothing is ever discarded, which is the point of
// having a limit.
func TestRolloverKeepsOneGeneration(t *testing.T) {
	r := newTestRecorder(t)
	r.maxSize = 2000

	const written = 20
	for i := 0; i < written; i++ {
		r.Log(Entry{Event: EventSessionOpened, Target: strings.Repeat("h", 40)})
	}

	if _, err := os.Stat(r.Path() + ".1"); err != nil {
		t.Fatalf("nothing was rolled aside at a %d-byte limit: %v", r.maxSize, err)
	}
	info, err := os.Stat(r.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > r.maxSize {
		t.Errorf("current file is %d bytes, over the %d limit", info.Size(), r.maxSize)
	}

	current := countLines(t, r.Path())
	entries, err := r.Read(1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) <= current {
		t.Errorf("read %d entries but the current file holds %d — the rolled-aside "+
			"generation is not being read", len(entries), current)
	}
	if len(entries) != written {
		t.Errorf("read %d entries, want all %d (one roll should not have dropped any)",
			len(entries), written)
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

// A process killed mid-write leaves a partial final line. That must cost the
// one line, not the file.
func TestATruncatedLineDoesNotHideTheRest(t *testing.T) {
	r := newTestRecorder(t)
	r.Log(Entry{Event: EventLoginSucceeded, Target: "alice"})

	f, err := os.OpenFile(r.Path(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"event":"session.open`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	entries, err := r.Read(10)
	if err != nil {
		t.Fatalf("a half-written line failed the whole read: %v", err)
	}
	if len(entries) != 1 || entries[0].Target != "alice" {
		t.Errorf("entries = %+v, want the one complete line", entries)
	}
}

// Recording must never be the thing that breaks a request, so a call site
// does not check whether auditing is configured — which means the nil case
// has to work.
func TestNilRecorderIsUsable(t *testing.T) {
	var r *Recorder
	r.Log(Entry{Event: EventLoginSucceeded})
	if got, err := r.Read(10); err != nil || got != nil {
		t.Errorf("Read on nil = %v, %v", got, err)
	}
	if err := r.Dump(&bytes.Buffer{}); err != nil {
		t.Errorf("Dump on nil = %v", err)
	}
	if r.Path() != "" {
		t.Error("nil recorder reported a path")
	}
}

// An unwritable file must not take the server down with it.
func TestAnUnwritableLogDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	// A directory where the file should be: opening it for append fails.
	path := filepath.Join(dir, "audit.log")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	NewRecorder(path).Log(Entry{Event: EventLoginSucceeded})
}

func TestConcurrentWriters(t *testing.T) {
	r := newTestRecorder(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Log(Entry{Event: EventSessionOpened, Target: "host.example.test"})
		}()
	}
	wg.Wait()

	entries, err := r.Read(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 20 {
		t.Errorf("read %d entries, want 20 — lines were lost or interleaved", len(entries))
	}
}

func TestReadLimitAndOrdering(t *testing.T) {
	r := newTestRecorder(t)
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		r.Log(Entry{Event: EventLoginSucceeded, Time: base.Add(time.Duration(i) * time.Minute)})
	}

	entries, err := r.Read(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("limit ignored: got %d", len(entries))
	}
	if !entries[0].Time.After(entries[1].Time) {
		t.Error("entries are not newest first")
	}
	if !entries[0].Time.Equal(base.Add(4 * time.Minute)) {
		t.Errorf("first entry is %v, want the newest", entries[0].Time)
	}
}

func TestDumpStreamsBothGenerations(t *testing.T) {
	r := newTestRecorder(t)
	r.maxSize = 2000
	const written = 20
	for i := 0; i < written; i++ {
		r.Log(Entry{Event: EventSessionOpened, Target: strings.Repeat("h", 40)})
	}
	if _, err := os.Stat(r.Path() + ".1"); err != nil {
		t.Fatalf("the test did not produce a rollover: %v", err)
	}

	var buf bytes.Buffer
	if err := r.Dump(&buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(strings.TrimRight(buf.String(), "\n"), "\n") + 1
	if lines != written {
		t.Errorf("streamed %d lines, want all %d across both generations", lines, written)
	}
}
