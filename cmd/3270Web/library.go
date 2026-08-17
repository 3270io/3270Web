// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/task"
)

// Libraries — a deployment's tasks and host presets as one document that can
// be carried to another deployment.
//
// Both halves were already server-side and both were already reachable one at
// a time: a task could be read back from the catalogue and posted into
// another, and a preset could be typed into the presets page again. What was
// missing is the thing an organisation actually does with them. Configuration
// is built on a test instance and then has to appear on the production one;
// forty presets and a dozen tasks move by hand, in the right order, without
// anybody being told which of them landed. That is not a transport, it is a
// transcription exercise, and it is where the divergence between two
// deployments starts.
//
// So a library is one file with a format version on it, and one import that
// says, per entry, what it did. Three properties are the point:
//
//   - What GET returns is exactly what POST accepts. The file downloaded from
//     one instance is the body posted to the next, with no conversion step in
//     between and nothing to keep in step when either side gains a field.
//   - Nothing is written until everything validates. An entry that this build
//     would refuse is refused before the first one is stored, so an import
//     never half-lands and leaves somebody comparing two host lists to find
//     out where it stopped.
//   - It can be asked what it would do. A dry run produces the same report
//     the real import produces and writes nothing, which is what makes
//     "import this into production" a decision somebody can take with the
//     answer in front of them.
//
// What a library deliberately does not carry is in exportLibrary: it is a way
// to move configuration between deployments, not a backup of one.

const (
	// libraryFormatVersion is stamped on every export and checked on every
	// import. A file from a newer build is refused by name rather than
	// half-understood, because the failure mode of guessing is a preset that
	// silently loses the field the newer version added.
	libraryFormatVersion = 1

	// maxLibraryProfiles bounds the host presets one import may carry. Tasks
	// are bounded by task.MaxTasks, which the catalogue enforces anyway; the
	// preset store has no such ceiling of its own, and a host list nobody can
	// read to the bottom of is not a host list.
	maxLibraryProfiles = 500
)

// Where a library's host presets are read from and written to. A library
// covers the set the caller administers — which is a different set depending
// on who they are, and worth saying out loud in the report rather than
// leaving somebody to infer from what appeared afterwards.
const (
	// libraryScopeInstance: one operator, one host list, no published set to
	// distinguish it from.
	libraryScopeInstance = "instance"
	// libraryScopePublished: the list every account reads and an
	// administrator maintains.
	libraryScopePublished = "published"
	// libraryScopeOwn: the caller's own presets, because publishing to
	// everyone is an administrator's call.
	libraryScopeOwn = "own"
)

// libraryDocument is the file itself.
//
// Tasks and presets travel together because they arrive together: a task
// drives an application on a particular mainframe, and a task catalogue
// delivered to a deployment that cannot reach that mainframe is a menu of
// operations none of which run. Either half may be absent — a library of
// presets alone is an ordinary thing to hand to a new site — so both are
// omitempty and neither is required.
type libraryDocument struct {
	FormatVersion int       `json:"formatVersion"`
	ExportedAt    time.Time `json:"exportedAt,omitempty"`
	// Instance names where this came from, for the person who finds the file
	// six months later with no memory of which environment produced it. It is
	// descriptive only: nothing on import reads it, and it is taken from the
	// Host header — which is whatever the caller asked for — so it is a label
	// on the file rather than a fact about it.
	Instance string `json:"instance,omitempty"`

	Tasks    []task.Task         `json:"tasks,omitempty"`
	Profiles []ConnectionProfile `json:"profiles,omitempty"`

	// Notes record what the export left out or changed, in the file rather
	// than only in the response that carried it. A document that explains its
	// own gaps is one somebody can act on; a note that lived only in the HTTP
	// reply is gone by the time the file is opened at the other end.
	Notes []string `json:"notes,omitempty"`
}

// libraryEntry is one line of an import report.
type libraryEntry struct {
	// Kind is "task" or "profile".
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Action is what happened, or what would have: "added", "replaced",
	// "skipped" or "rejected".
	Action string `json:"action"`
	// Reason is present on anything that was not added, because "skipped"
	// with no explanation is the report telling somebody to go and look.
	Reason string `json:"reason,omitempty"`
}

const (
	libraryAdded    = "added"
	libraryReplaced = "replaced"
	librarySkipped  = "skipped"
	libraryRejected = "rejected"
)

// libraryReport is what an import — real or dry — answers with.
type libraryReport struct {
	DryRun bool `json:"dryRun"`
	// ProfileScope names which host list the presets went into, using the
	// libraryScope* constants.
	ProfileScope string `json:"profileScope,omitempty"`

	Added    int `json:"added"`
	Replaced int `json:"replaced"`
	Skipped  int `json:"skipped"`
	Rejected int `json:"rejected"`

	Entries []libraryEntry `json:"entries"`
	Notes   []string       `json:"notes,omitempty"`
}

func (r *libraryReport) record(kind, name, action, reason string) {
	r.Entries = append(r.Entries, libraryEntry{Kind: kind, Name: name, Action: action, Reason: reason})
	switch action {
	case libraryAdded:
		r.Added++
	case libraryReplaced:
		r.Replaced++
	case librarySkipped:
		r.Skipped++
	case libraryRejected:
		r.Rejected++
	}
}

/* ---------------------------------------------------------------- */
/* Which set a library covers                                        */
/* ---------------------------------------------------------------- */

// libraryProfileTarget picks the host list this caller's library is about,
// and names it.
//
// The rule is one sentence: a library covers the presets the caller
// administers. For an administrator, and for the single operator of an
// instance with no accounts, that is the list everybody connects through; for
// anybody else it is their own. Refusing the request instead would mean a user
// could not carry their own presets between two laptops, and quietly writing
// somebody's private list into the published one is the mistake this
// separation exists to prevent.
func (app *App) libraryProfileTarget(c *gin.Context) (*connectionProfileStore, string) {
	published := app.publishedProfiles(c)
	if published == nil {
		return app.ownProfiles(c), libraryScopeInstance
	}
	if principalFrom(c).IsAdmin() {
		return published, libraryScopePublished
	}
	return app.ownProfiles(c), libraryScopeOwn
}

// libraryScopeNote says in prose which list was used, so a report does not
// depend on the reader knowing what "own" means.
//
// The importing phrasing is present tense rather than past, because the same
// note is on a dry run — which has written nothing — and on the import that
// follows it.
//
// An instance with one operator gets no note at all. There is one host list
// there and nothing to distinguish it from, so naming it is a line that tells
// the reader nothing and trains them to stop reading the ones that do.
func libraryScopeNote(scope string, importing bool) string {
	clause := "were read from "
	if importing {
		clause = "go into "
	}
	switch scope {
	case libraryScopePublished:
		return "Host presets " + clause + "the published list every account connects through."
	case libraryScopeOwn:
		return "Host presets " + clause +
			"your own list: publishing to everyone needs an administrator account."
	default:
		return ""
	}
}

// appendNote adds a note unless there is nothing to say.
func appendNote(notes []string, note string) []string {
	if note == "" {
		return notes
	}
	return append(notes, note)
}

/* ---------------------------------------------------------------- */
/* Export                                                            */
/* ---------------------------------------------------------------- */

// exportLibrary builds the document.
//
// Three things are left out on purpose.
//
// Tasks an installed extension contributed are not copied in. They are not
// this deployment's to distribute — they arrived with a pack, and the way to
// have them at the other end is to install the same pack there. Copying them
// would fork them: the receiving instance would hold a frozen duplicate that
// no longer changes when the extension is updated, shadowing the real one
// under the same name. How many were left out is recorded, because a
// catalogue that exported fewer tasks than the Tasks menu shows is otherwise
// a mystery.
//
// Named-account audiences are removed. A preset offered to "j.okafor" names
// an account on the deployment it came from and nowhere else, so the entry
// would resolve to nobody at the far end — and, more to the point, a file
// meant to be handed to another site should not carry a list of who works
// here. Groups and roles stay: those are the names two deployments plausibly
// share, and they are what an audience is usually written in.
//
// Nothing that is a secret travels, because a preset holds none — a profile
// carries a host, a port, TLS settings, an LU and a model, and the credentials
// for the host are typed by the person who signs on to it. This is worth
// stating rather than leaving to be rediscovered by whoever next adds a field
// to ConnectionProfile.
func (app *App) exportLibrary(c *gin.Context, wantTasks, wantProfiles bool) (libraryDocument, error) {
	doc := libraryDocument{
		FormatVersion: libraryFormatVersion,
		ExportedAt:    time.Now().UTC().Truncate(time.Second),
		Instance:      instanceLabel(c.Request.Host),
	}

	if wantTasks {
		saved, err := app.taskStoreForRequest(c).List()
		if err != nil {
			return libraryDocument{}, fmt.Errorf("could not read the task catalogue: %w", err)
		}
		doc.Tasks = saved
		if n := len(app.extensionTasks()); n > 0 {
			doc.Notes = append(doc.Notes, fmt.Sprintf(
				"%s left out: they come from an installed extension. Install the same "+
					"extension on the receiving deployment rather than copying them.",
				countOf(n, "task")))
		}
	}

	if wantProfiles {
		store, scope := app.libraryProfileTarget(c)
		profiles, err := store.load()
		if err != nil {
			return libraryDocument{}, fmt.Errorf("could not read the host presets: %w", err)
		}
		doc.Notes = appendNote(doc.Notes, libraryScopeNote(scope, false))

		var namedAudiences, withdrawn int
		out := make([]ConnectionProfile, 0, len(profiles))
		for _, p := range profiles {
			// Output-only, derived from which store held it. Carrying it would
			// assert on the far side that the preset is published there, which
			// is not this file's to say.
			p.Shared = false
			if len(p.Users) > 0 {
				namedAudiences++
				p.Users = nil
				// The audience was a restriction, and dropping the only names in
				// it would turn "these four people" into "everybody" — an
				// audience that names nobody is for everybody, deliberately, and
				// that rule must not be reachable by deletion. Withheld instead:
				// the preset lands on the presets page, on nobody's list, for an
				// administrator to give an audience the receiving deployment can
				// actually resolve.
				if !p.hasAudience() {
					p.NotOffered = true
					withdrawn++
				}
			}
			out = append(out, p)
		}
		doc.Profiles = out

		if namedAudiences > 0 {
			doc.Notes = append(doc.Notes, fmt.Sprintf(
				"Named-account audiences were removed from %s: the accounts they "+
					"name exist only on the deployment this came from.",
				countOf(namedAudiences, "preset")))
		}
		if withdrawn > 0 {
			doc.Notes = append(doc.Notes, fmt.Sprintf(
				"%s named nobody else, so %s not offered to anyone on arrival — "+
					"give them an audience before operators can see them.",
				countOf(withdrawn, "preset"), pluralIs(withdrawn)))
		}
	}

	return doc, nil
}

// instanceLabel is the Host header as a caption on the file: trimmed, and
// bounded so a long one cannot make the document unreadable at the top.
func instanceLabel(host string) string {
	host = strings.TrimSpace(host)
	// The longest a fully-qualified name can be, plus room for a port.
	const max = 253 + 6
	if len(host) > max {
		return host[:max]
	}
	return host
}

// countOf writes "1 preset" or "4 presets".
func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func pluralIs(n int) string {
	if n == 1 {
		return "it is"
	}
	return "they are"
}

/* ---------------------------------------------------------------- */
/* Import                                                            */
/* ---------------------------------------------------------------- */

// libraryImportOptions are the knobs, which travel in the query string rather
// than in the body. That is what keeps the downloaded file usable as the
// request body unchanged: wrapping it in an envelope carrying the options
// would mean the two documents were not the same document after all.
type libraryImportOptions struct {
	// Replace decides what happens to a name that already exists. Skipping is
	// the default because an import that silently overwrote a task somebody
	// had edited here would be the expensive kind of surprise, and the report
	// names every skip so nothing is lost quietly.
	Replace bool
	// DryRun validates and reports, and writes nothing.
	DryRun bool
}

func libraryOptionsFrom(c *gin.Context) (libraryImportOptions, error) {
	var opts libraryImportOptions
	switch strings.ToLower(strings.TrimSpace(c.Query("onConflict"))) {
	case "", "skip":
	case "replace":
		opts.Replace = true
	default:
		return opts, fmt.Errorf("onConflict must be \"skip\" or \"replace\"")
	}
	switch strings.ToLower(strings.TrimSpace(c.Query("dryRun"))) {
	case "", "false", "0":
	case "true", "1":
		opts.DryRun = true
	default:
		return opts, fmt.Errorf("dryRun must be true or false")
	}
	return opts, nil
}

// errLibraryRefused marks an import that was refused whole, so the handler
// knows to answer 400 with the report rather than 200.
type errLibraryRefused struct{ report libraryReport }

func (e errLibraryRefused) Error() string {
	if e.report.Rejected == 1 {
		return "one entry in this library cannot be stored by this instance, so none of it was"
	}
	return fmt.Sprintf(
		"%d entries in this library cannot be stored by this instance, so none of it was",
		e.report.Rejected)
}

// importLibrary applies a document, in two passes.
//
// The first pass validates every entry and decides what would happen to it.
// The second writes. Splitting them is what makes "nothing is written until
// everything validates" true rather than aspirational: a library holding one
// task this build refuses is refused entirely, with that task named, instead
// of storing the eleven before it and stopping. Two deployments that disagree
// about half a catalogue is the state this whole feature exists to avoid, and
// an import is exactly where it would be created.
//
// A conflict is not a rejection. Under the default policy an existing name is
// skipped and said so; under replace it is overwritten and said so. Only an
// entry this instance could not store however it was asked — a malformed
// task, a preset naming a host that is not a host — refuses the import.
func (app *App) importLibrary(c *gin.Context, doc libraryDocument, opts libraryImportOptions) (libraryReport, error) {
	report := libraryReport{DryRun: opts.DryRun}

	if doc.FormatVersion > libraryFormatVersion {
		return report, fmt.Errorf(
			"this library is version %d and this instance understands version %d — "+
				"it was written by a newer build of 3270Web",
			doc.FormatVersion, libraryFormatVersion)
	}
	if len(doc.Tasks) > task.MaxTasks {
		return report, fmt.Errorf("a library may carry at most %d tasks; this one has %d",
			task.MaxTasks, len(doc.Tasks))
	}
	if len(doc.Profiles) > maxLibraryProfiles {
		return report, fmt.Errorf("a library may carry at most %d host presets; this one has %d",
			maxLibraryProfiles, len(doc.Profiles))
	}
	if len(doc.Tasks) == 0 && len(doc.Profiles) == 0 {
		return report, fmt.Errorf("this library holds no tasks and no host presets")
	}

	tasks := app.taskStoreForRequest(c)
	profileStore, scope := app.libraryProfileTarget(c)
	if len(doc.Profiles) > 0 {
		report.ProfileScope = scope
		report.Notes = appendNote(report.Notes, libraryScopeNote(scope, true))
	}

	// Pass one: decide, and validate.
	pendingTasks := make([]task.Task, 0, len(doc.Tasks))
	pendingProfiles := make([]ConnectionProfile, 0, len(doc.Profiles))
	seenTasks := make(map[string]bool, len(doc.Tasks))
	seenProfiles := make(map[string]bool, len(doc.Profiles))

	for _, t := range doc.Tasks {
		name := strings.TrimSpace(t.Name)
		if err := t.Validate(); err != nil {
			report.record("task", name, libraryRejected, err.Error())
			continue
		}
		// A file naming the same task twice is malformed rather than
		// interesting: whichever of the two won would be an accident of order.
		key := task.NormalizeName(t.Name)
		if seenTasks[key] {
			report.record("task", name, libraryRejected, "the library names this task twice")
			continue
		}
		seenTasks[key] = true

		// An extension's task is not in the catalogue this writes, so importing
		// over it would report success and change nothing anybody can see — the
		// extension's copy still wins the menu.
		if app.isExtensionTask(c, t.Name) {
			report.record("task", name, librarySkipped,
				"an installed extension already contributes a task with this name")
			continue
		}
		if _, exists := tasks.Find(t.Name); exists {
			if !opts.Replace {
				report.record("task", name, librarySkipped, "a task with this name is already here")
				continue
			}
			report.record("task", name, libraryReplaced, "")
		} else {
			report.record("task", name, libraryAdded, "")
		}
		pendingTasks = append(pendingTasks, t)
	}

	for _, p := range doc.Profiles {
		name := strings.TrimSpace(p.Name)
		// Output only, and never trusted from a file: a document cannot promote
		// its own presets into the published set by asserting that they are
		// already there. Where they land is decided above, by who is asking.
		p.Shared = false
		if err := validateProfile(&p); err != nil {
			report.record("profile", name, libraryRejected, err.Error())
			continue
		}
		key := strings.ToLower(strings.TrimSpace(p.Name))
		if seenProfiles[key] {
			report.record("profile", name, libraryRejected, "the library names this preset twice")
			continue
		}
		seenProfiles[key] = true

		if scope == libraryScopeOwn {
			// An audience decides who a *published* preset reaches. On a private
			// one there is nobody else for it to decide about, and storing it
			// would apply a list written elsewhere the day somebody publishes it.
			p.Users, p.Groups, p.Roles = nil, nil, nil
			p.NotOffered = false
		} else {
			// Unknown roles are dropped here exactly as they are on the presets
			// page; a group the receiving deployment has never heard of is kept,
			// because that is a group somebody is about to create, and silently
			// discarding it would leave a preset that looks assigned and is not.
			normaliseAudience(&p)
			settleOffered(&p)
		}

		if _, exists := profileStore.find(p.Name); exists {
			if !opts.Replace {
				report.record("profile", name, librarySkipped, "a preset with this name is already here")
				continue
			}
			report.record("profile", name, libraryReplaced, "")
		} else {
			report.record("profile", name, libraryAdded, "")
		}
		pendingProfiles = append(pendingProfiles, p)
	}

	if report.Rejected > 0 {
		return report, errLibraryRefused{report: report}
	}
	if opts.DryRun {
		return report, nil
	}

	// Pass two: write. Everything here has already been validated against the
	// same gate the browser goes through, so a failure now is the filesystem
	// rather than the document.
	for _, t := range pendingTasks {
		if _, err := tasks.Upsert(t); err != nil {
			return report, fmt.Errorf("storing task %q: %w", t.Name, err)
		}
	}
	for _, p := range pendingProfiles {
		if _, err := profileStore.upsert(p); err != nil {
			return report, fmt.Errorf("storing host preset %q: %w", p.Name, err)
		}
	}
	return report, nil
}

/* ---------------------------------------------------------------- */
/* Handlers                                                          */
/* ---------------------------------------------------------------- */

// LibraryExportHandler answers with the document.
//
// ?include= narrows it to "tasks" or "profiles"; both is the default, because
// a library of one half is a request somebody makes on purpose and not the
// thing they get by forgetting a parameter. ?download=1 asks for it as a file,
// which is what the browser's button sends and what a curl -O wants.
func (app *App) LibraryExportHandler(c *gin.Context) {
	wantTasks, wantProfiles, err := parseLibraryInclude(c.Query("include"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	doc, err := app.exportLibrary(c, wantTasks, wantProfiles)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	app.auditRequest(c, audit.EventLibraryExported, audit.Success, "", map[string]string{
		"tasks":    strconv.Itoa(len(doc.Tasks)),
		"profiles": strconv.Itoa(len(doc.Profiles)),
	})

	if isTruthyParam(c.Query("download")) {
		c.Header("Content-Disposition", `attachment; filename="3270web-library.json"`)
	}
	c.IndentedJSON(http.StatusOK, doc)
}

// LibraryImportHandler applies a document, or says what it would do.
func (app *App) LibraryImportHandler(c *gin.Context) {
	opts, err := libraryOptionsFrom(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var doc libraryDocument
	if err := c.ShouldBindJSON(&doc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "this is not a 3270Web library file"})
		return
	}

	report, err := app.importLibrary(c, doc, opts)
	if err != nil {
		var refused errLibraryRefused
		if errors.As(err, &refused) {
			// The report is the answer, not a footnote to it: it names every
			// entry that cannot be stored and why, which is the whole reason to
			// refuse the file rather than store the good half of it.
			app.auditRequest(c, audit.EventLibraryImported, audit.Failure, "",
				map[string]string{"rejected": strconv.Itoa(refused.report.Rejected)})
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "report": refused.report})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !opts.DryRun {
		app.auditRequest(c, audit.EventLibraryImported, audit.Success, "", map[string]string{
			"added":    strconv.Itoa(report.Added),
			"replaced": strconv.Itoa(report.Replaced),
			"skipped":  strconv.Itoa(report.Skipped),
			"scope":    report.ProfileScope,
		})
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "report": report})
}

// parseLibraryInclude reads the ?include= list.
func parseLibraryInclude(raw string) (tasks, profiles bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true, true, nil
	}
	for _, part := range strings.Split(raw, ",") {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "":
		case "tasks":
			tasks = true
		case "profiles", "presets":
			profiles = true
		default:
			return false, false, fmt.Errorf("include takes \"tasks\", \"profiles\" or both, not %q", part)
		}
	}
	if !tasks && !profiles {
		return false, false, fmt.Errorf("include takes \"tasks\", \"profiles\" or both")
	}
	return tasks, profiles, nil
}
