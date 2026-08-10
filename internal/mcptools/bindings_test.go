package mcptools

import (
	"strconv"
	"strings"
	"testing"
)

// Two of the endpoints behind these tools answer a caller that is writing a
// file: the snapshot listing carries every held screen in full, and a print job
// may be megabytes. A conversation is not a file, and neither answer can go to
// a model as it stands.

func TestClipJobLeavesAShortJobAlone(t *testing.T) {
	body := []byte("PAGE 1\nTOTAL 42\n")
	if got := clipJob("job-1", body); got != string(body) {
		t.Errorf("clipJob rewrote a short job: %q", got)
	}
}

func TestClipJobCutsALongJobAndSaysSo(t *testing.T) {
	body := []byte(strings.Repeat("x", maxJobChars+5000))
	got := clipJob("job-1", body)

	if len(got) <= maxJobChars {
		t.Fatalf("clipJob returned %d chars, want the first %d plus a note", len(got), maxJobChars)
	}
	if !strings.HasPrefix(got, strings.Repeat("x", maxJobChars)) {
		t.Error("clipJob did not return the start of the job")
	}
	// The size and the cut have to be in the text: a model handed a truncated
	// listing with no note reports the total it can see as the total there is.
	for _, want := range []string{"job-1", strconv.Itoa(len(body)), strconv.Itoa(maxJobChars)} {
		if !strings.Contains(got, want) {
			t.Errorf("the note does not mention %q: %q", want, got[maxJobChars:])
		}
	}
}

func TestSummariseSnapshotsDropsTheScreenText(t *testing.T) {
	body := []byte(`{"snapshots":[
		{"name":"before","taken_at":"2026-08-10T09:00:00Z",
		 "snapshot":{"rows":24,"cols":80,"status":"U F U","text":"BALANCE 100"}},
		{"name":"after","taken_at":"2026-08-10T09:01:00Z",
		 "snapshot":{"rows":24,"cols":80,"status":"U F U","text":"BALANCE 250"}}
	]}`)

	got, err := summariseSnapshots(body)
	if err != nil {
		t.Fatalf("summariseSnapshots: %v", err)
	}
	for _, name := range []string{"before", "after"} {
		if !strings.Contains(got, name) {
			t.Errorf("summary does not name %q: %s", name, got)
		}
	}
	if strings.Contains(got, "BALANCE") {
		t.Errorf("summary carries the screen text, which is what it exists to leave out: %s", got)
	}
	if !strings.Contains(got, "24x80") {
		t.Errorf("summary does not give each snapshot's size: %s", got)
	}
}

func TestSummariseSnapshotsSaysWhenThereAreNone(t *testing.T) {
	got, err := summariseSnapshots([]byte(`{"snapshots":[]}`))
	if err != nil {
		t.Fatalf("summariseSnapshots: %v", err)
	}
	// "no snapshots" and an empty reply read the same to a model that has just
	// been told a tool succeeded, so the empty case answers in words.
	if !strings.Contains(got, "snapshot_take") {
		t.Errorf("an empty listing should point at how to take one, got %q", got)
	}
}

// A shape this does not recognise must not become an empty answer: the raw
// listing is worse to read and still usable, and silence is not.
func TestSummariseSnapshotsFallsBackToTheRawBody(t *testing.T) {
	body := []byte(`{"held":[{"name":"before"}]}`)
	got, err := summariseSnapshots(body)
	if err != nil {
		t.Fatalf("summariseSnapshots: %v", err)
	}
	if !strings.Contains(got, "before") {
		t.Errorf("an unrecognised shape lost its content: %q", got)
	}
}
