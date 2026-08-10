package printer

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The spool is where a print job lands.
//
// pr3287 delivers each finished job by running a command with the job's bytes
// on its standard input — `lpr`, by default, which is exactly right for a
// terminal running on the desk beside a printer and exactly wrong for one
// served to a browser tab. There is no printer on the server, and the paper is
// wanted by whoever is looking at the screen somewhere else entirely.
//
// So the command writes a file instead, one per job, in a directory belonging
// to the session that started the printer. The operator downloads it. A shop
// that does want paper still can: the file is a plain SCS or line-printer
// stream and `lpr` will take it.
//
// The jobs are bounded in three directions — per job, per directory, and by
// count — because a printer session left running against a chatty host is
// otherwise a way to fill a disk from the outside.

const (
	// MaxJobBytes is the largest single job kept in full. Beyond it the job
	// is truncated and *named* as truncated: a report that quietly stops
	// two-thirds of the way down is worse than one that says where it
	// stopped.
	MaxJobBytes int64 = 8 << 20 // 8 MiB
	// MaxSpoolBytes and MaxJobs bound one session's spool. The oldest jobs
	// go first, which is the right way round: the job somebody is waiting
	// for is the one that just arrived.
	MaxSpoolBytes int64 = 64 << 20 // 64 MiB
	MaxJobs             = 200
)

// jobNamePattern is what a spooled job may be called. Every name is generated
// by newJobName; this exists so that a name arriving from a request — which is
// how a download names the file it wants — is checked against the shape rather
// than trusted to be one of ours.
var jobNamePattern = regexp.MustCompile(`^job-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}(\.truncated)?\.prt$`)

// partialSuffix marks a job still being written. A job appears in the listing
// under its real name only once it is complete, so a download cannot catch
// half of one.
const partialSuffix = ".part"

// Job is one spooled print job.
type Job struct {
	Name       string    `json:"name"`
	Bytes      int64     `json:"bytes"`
	ReceivedAt time.Time `json:"received_at"`
	// Truncated marks a job that hit MaxJobBytes. The file holds the first
	// MaxJobBytes of the job and nothing after it.
	Truncated bool `json:"truncated"`
}

// Spool is one session's directory of print jobs.
type Spool struct {
	Dir string
}

// newJobName builds a name that sorts by arrival and cannot collide.
func newJobName(now time.Time, truncated bool) (string, error) {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate a job name: %w", err)
	}
	suffix := ""
	if truncated {
		suffix = ".truncated"
	}
	return fmt.Sprintf("job-%s-%s%s.prt",
		now.UTC().Format("20060102T150405Z"), hex.EncodeToString(raw[:]), suffix), nil
}

// Receive writes one job from r and returns the name it was given.
//
// This runs in the spool helper process, not in the server: pr3287 starts it,
// so it is the one place a print job's bytes exist. It writes under a partial
// name and renames on success, then prunes — all of which has to happen here,
// because nothing else finds out that a job arrived.
func (s Spool) Receive(r io.Reader, now time.Time) (Job, error) {
	if strings.TrimSpace(s.Dir) == "" {
		return Job{}, fmt.Errorf("no spool directory was given")
	}
	info, err := os.Stat(s.Dir)
	if err != nil {
		return Job{}, fmt.Errorf("the spool directory is not usable: %w", err)
	}
	if !info.IsDir() {
		return Job{}, fmt.Errorf("the spool destination %s is not a directory", s.Dir)
	}

	tmp, err := os.CreateTemp(s.Dir, "job-*"+partialSuffix)
	if err != nil {
		return Job{}, fmt.Errorf("open a spool file: %w", err)
	}
	tmpName := tmp.Name()
	// Removed on every path that does not rename it, so a failed job leaves
	// nothing behind for the next listing to trip over.
	committed := false
	defer func() {
		tmp.Close()
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()

	// One byte past the limit is what tells truncation apart from a job that
	// happens to be exactly MaxJobBytes long.
	written, err := io.Copy(tmp, io.LimitReader(r, MaxJobBytes+1))
	if err != nil {
		return Job{}, fmt.Errorf("write the print job: %w", err)
	}
	truncated := written > MaxJobBytes
	if truncated {
		if err := tmp.Truncate(MaxJobBytes); err != nil {
			return Job{}, fmt.Errorf("truncate the print job: %w", err)
		}
		written = MaxJobBytes
	}
	if err := tmp.Sync(); err != nil {
		return Job{}, fmt.Errorf("flush the print job: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return Job{}, fmt.Errorf("close the print job: %w", err)
	}

	name, err := newJobName(now, truncated)
	if err != nil {
		return Job{}, err
	}
	if err := os.Rename(tmpName, filepath.Join(s.Dir, name)); err != nil {
		return Job{}, fmt.Errorf("store the print job: %w", err)
	}
	committed = true

	job := Job{Name: name, Bytes: written, ReceivedAt: now.UTC(), Truncated: truncated}
	// A failed prune is not a failed job: the job is on disk and the operator
	// can have it. It is reported by the caller rather than swallowed.
	if err := s.Prune(); err != nil {
		return job, err
	}
	return job, nil
}

// List returns the jobs in the spool, newest first.
func (s Spool) List() ([]Job, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	jobs := make([]Job, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !jobNamePattern.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		jobs = append(jobs, Job{
			Name:       entry.Name(),
			Bytes:      info.Size(),
			ReceivedAt: info.ModTime().UTC(),
			Truncated:  strings.HasSuffix(entry.Name(), ".truncated.prt"),
		})
	}
	// By name, which carries the arrival time to the second and a random tail
	// after it. Modification time would be the obvious key and is the wrong
	// one: a copied or restored spool directory has whatever times the copy
	// gave it, and the name is the only record of when the host sent the job.
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name > jobs[j].Name })
	return jobs, nil
}

// Path resolves a job name to a file, refusing anything that is not a name
// this package generates.
//
// The name arrives from a request. Checking it against the pattern rather than
// cleaning it is what keeps the download endpoint a download endpoint: no
// separator, no dot segment and no absolute path can match, so there is
// nothing to escape from.
func (s Spool) Path(name string) (string, error) {
	if !jobNamePattern.MatchString(name) {
		return "", fmt.Errorf("no print job is named %q", name)
	}
	return filepath.Join(s.Dir, name), nil
}

// Delete removes one job.
func (s Spool) Delete(name string) error {
	path, err := s.Path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no print job is named %q", name)
		}
		return err
	}
	return nil
}

// Prune drops the oldest jobs until the spool is within its bounds.
func (s Spool) Prune() error {
	jobs, err := s.List()
	if err != nil {
		return err
	}
	var total int64
	for _, job := range jobs {
		total += job.Bytes
	}
	// jobs is newest first, so walking backwards removes the oldest. At each
	// step i+1 jobs remain, and jobs[i] is the oldest of them.
	for i := len(jobs) - 1; i >= 0; i-- {
		if i+1 <= MaxJobs && total <= MaxSpoolBytes {
			break
		}
		if err := s.Delete(jobs[i].Name); err != nil {
			return err
		}
		total -= jobs[i].Bytes
	}
	return nil
}

// Remove deletes the whole spool directory. A printer session's output belongs
// to the terminal session that started it, and goes when it does.
func (s Spool) Remove() error {
	if strings.TrimSpace(s.Dir) == "" {
		return nil
	}
	return os.RemoveAll(s.Dir)
}
