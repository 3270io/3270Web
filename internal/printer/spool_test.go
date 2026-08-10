package printer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpoolReceiveAndList(t *testing.T) {
	spool := Spool{Dir: t.TempDir()}

	first, err := spool.Receive(strings.NewReader("PAGE ONE\n"), time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	second, err := spool.Receive(strings.NewReader("PAGE TWO\n"), time.Date(2026, 8, 10, 9, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}

	jobs, err := spool.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("List returned %d jobs, want 2", len(jobs))
	}
	if jobs[0].Name != second.Name {
		t.Errorf("first listed job = %q, want the newest (%q)", jobs[0].Name, second.Name)
	}
	if jobs[1].Name != first.Name {
		t.Errorf("second listed job = %q, want the oldest (%q)", jobs[1].Name, first.Name)
	}
	if jobs[0].Bytes != int64(len("PAGE TWO\n")) {
		t.Errorf("job size = %d, want %d", jobs[0].Bytes, len("PAGE TWO\n"))
	}
	if jobs[0].Truncated {
		t.Error("a job that fit should not be marked truncated")
	}

	path, err := spool.Path(second.Name)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != "PAGE TWO\n" {
		t.Errorf("job content = %q, want the bytes it was given", body)
	}
}

// A job still being written must not appear in the listing: a download that
// caught one would serve half a report and look like a whole one.
func TestSpoolListIgnoresPartialFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "job-1234"+partialSuffix), []byte("half"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	jobs, err := (Spool{Dir: dir}).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("List returned %d jobs, want none — the only file is still being written", len(jobs))
	}
}

// A job over the cap is kept, capped, and *named* as capped. Silently
// truncating one would leave a report that looks complete until somebody
// reaches the end of it.
func TestSpoolReceiveTruncatesAndSaysSo(t *testing.T) {
	spool := Spool{Dir: t.TempDir()}
	oversized := bytes.Repeat([]byte("X"), int(MaxJobBytes)+4096)

	job, err := spool.Receive(bytes.NewReader(oversized), time.Now())
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if !job.Truncated {
		t.Error("an oversized job should be marked truncated")
	}
	if job.Bytes != MaxJobBytes {
		t.Errorf("stored %d bytes, want the %d-byte cap", job.Bytes, MaxJobBytes)
	}
	if !strings.HasSuffix(job.Name, ".truncated.prt") {
		t.Errorf("job name = %q, want it to say it was truncated", job.Name)
	}

	jobs, err := spool.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 1 || !jobs[0].Truncated {
		t.Errorf("List = %+v, want one truncated job", jobs)
	}
}

// The name in a download request is checked against the shape this package
// generates. Nothing with a separator or a dot segment can match, which is
// what keeps the endpoint from reading anything but a job.
func TestSpoolPathRefusesAnythingButAJobName(t *testing.T) {
	spool := Spool{Dir: t.TempDir()}
	for _, name := range []string{
		"",
		"../../etc/passwd",
		"job-20260810T090000Z-deadbeef.prt/../../etc/passwd",
		"/etc/passwd",
		"users.json",
		"job-20260810T090000Z-deadbeef.txt",
		"job-notatime-deadbeef.prt",
		"job-20260810T090000Z-NOTHEX01.prt",
	} {
		if _, err := spool.Path(name); err == nil {
			t.Errorf("Path(%q) was accepted, want a refusal", name)
		}
	}
}

func TestSpoolDelete(t *testing.T) {
	spool := Spool{Dir: t.TempDir()}
	job, err := spool.Receive(strings.NewReader("REPORT"), time.Now())
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if err := spool.Delete(job.Name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	jobs, err := spool.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("List returned %d jobs after a delete, want none", len(jobs))
	}
	if err := spool.Delete(job.Name); err == nil {
		t.Error("deleting a job twice should report that it is not there")
	}
}

// The spool is bounded by count as well as by bytes, and the oldest go first:
// the job somebody is waiting for is the one that just arrived.
func TestSpoolPruneKeepsTheNewest(t *testing.T) {
	spool := Spool{Dir: t.TempDir()}
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)

	var newest string
	for i := 0; i < MaxJobs+5; i++ {
		job, err := spool.Receive(strings.NewReader("J"), start.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("Receive %d: %v", i, err)
		}
		newest = job.Name
	}

	jobs, err := spool.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != MaxJobs {
		t.Fatalf("spool holds %d jobs, want the %d-job cap", len(jobs), MaxJobs)
	}
	if jobs[0].Name != newest {
		t.Errorf("newest job = %q, want %q — pruning must drop the oldest", jobs[0].Name, newest)
	}
}

func TestSpoolRemove(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "session")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	spool := Spool{Dir: dir}
	if _, err := spool.Receive(strings.NewReader("REPORT"), time.Now()); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if err := spool.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the spool directory is still there (%v)", err)
	}
	// A listing of a directory that is gone is empty, not an error: a session
	// whose printer was never started must not fail a status request.
	jobs, err := spool.List()
	if err != nil {
		t.Errorf("List on a missing spool = %v, want no error", err)
	}
	if len(jobs) != 0 {
		t.Errorf("List on a missing spool returned %d jobs", len(jobs))
	}
}

func TestSpoolReceiveRefusesAMissingDirectory(t *testing.T) {
	spool := Spool{Dir: filepath.Join(t.TempDir(), "nope")}
	if _, err := spool.Receive(strings.NewReader("REPORT"), time.Now()); err == nil {
		t.Fatal("Receive accepted a directory that does not exist")
	}
}
