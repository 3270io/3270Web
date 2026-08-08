package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

// Where per-user and instance-wide files live under the data directory.
//
//	<base>/users/<ownerID>/…   one person's own files
//
// Ownership decides the directory, so the filesystem layout matches the
// authorization rules rather than restating them. Nothing has to remember to
// filter a listing: a user reading their own directory cannot see another's
// because it is not there.
const userDataDirName = "users"

// dataScope resolves where one actor's files live.
//
// It is built from an owner ID rather than a request, because the code that
// needs it does not always have one: a chaos run keeps writing after the
// request that started it has returned, from a goroutine that holds the
// session and nothing else. Deriving the path from the session's owner works
// in both places and cannot drift from who the session belongs to.
type dataScope struct {
	// base is the per-user directory, or the flat data directory when the
	// deployment has a single operator.
	base string
}

// dataScopeFor returns the scope for a given owner.
//
// With one operator the layout is left exactly as it was: a flat directory,
// no users/ level, no migration, nothing to explain. Per-user directories
// only appear once there can be more than one user.
func (app *App) dataScopeFor(ownerID string) dataScope {
	if !app.separatesUsers() || ownerID == "" || ownerID == authz.LocalUserID {
		return dataScope{base: app.baseDir}
	}
	// Owner IDs are server-generated 32-hex values (users.newID), so they are
	// safe as a path component. Validated anyway rather than trusted: this is
	// the only place an identifier becomes a directory, and the cost of being
	// wrong here is one user's files landing in another's tree.
	if !isSafeOwnerID(ownerID) {
		return dataScope{base: filepath.Join(app.baseDir, userDataDirName, "invalid")}
	}
	return dataScope{base: filepath.Join(app.baseDir, userDataDirName, ownerID)}
}

// dataScopeForRequest returns the scope for whoever is making this request,
// for data that is not attached to a terminal session.
func (app *App) dataScopeForRequest(c *gin.Context) dataScope {
	return app.dataScopeFor(principalFrom(c).UserID)
}

// isSafeOwnerID reports whether an owner ID may be used as a directory name.
func isSafeOwnerID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// chaosRunsDir is where this owner's saved chaos runs live.
//
// Chaos runs hold captured screens and the field values that produced them,
// which is the most sensitive thing 3270Web writes to disk — it is a record
// of a real application's contents. Keeping them per-user is the main reason
// this split exists.
func (s dataScope) chaosRunsDir() string { return filepath.Join(s.base, "chaos-runs") }

// chaosHintsPath is this owner's chaos hint document, including the key
// blacklist that says which keys a run must never press.
func (s dataScope) chaosHintsPath() string { return filepath.Join(s.base, "chaos-hints.json") }

// chaosRunsDirForSession resolves the runs directory from a session's owner.
//
// Reading the owner off the session rather than the request is what lets a
// chaos run keep writing to the right place after its request has returned.
func (app *App) chaosRunsDirForSession(ownerID string) string {
	return app.dataScopeFor(ownerID).chaosRunsDir()
}

// migrateFlatDataToOwner moves a single-operator layout into one user's
// directory, once.
//
// Turning authentication on for an instance that has been running would
// otherwise appear to lose everything: the files are still there, but nothing
// looks in the flat directory any more. Moving them to the first
// administrator keeps them reachable by somebody, which is the only answer
// that does not require guessing who each file belonged to — there was one
// operator, so there is one plausible owner.
//
// Runs once and leaves a marker, so a later start does not scoop up files a
// user has since created in the flat layout.
func (app *App) migrateFlatDataToOwner(ownerID string) error {
	if !app.separatesUsers() || !isSafeOwnerID(ownerID) {
		return nil
	}

	marker := filepath.Join(app.baseDir, userDataDirName, ".migrated")
	if _, err := os.Stat(marker); err == nil {
		return nil
	}

	scope := app.dataScopeFor(ownerID)
	if err := os.MkdirAll(scope.base, 0o750); err != nil {
		return fmt.Errorf("create user data directory: %w", err)
	}

	// Only what is actually read from a per-user path is moved. Moving a file
	// the code still looks for in the flat directory would not partition it —
	// it would lose it.
	//
	// Chaos runs and their hints are the working documents that matter most
	// here: a run holds captured screens from a real application, which is
	// the thing least appropriate to leave in a shared pool. They go to the
	// first administrator, because on an instance that had one operator there
	// is exactly one plausible owner and guessing per-file is not possible.
	moved := make([]string, 0, 2)
	for _, item := range []struct{ from, to string }{
		{filepath.Join(app.baseDir, "chaos-runs"), scope.chaosRunsDir()},
		{filepath.Join(app.baseDir, "chaos-hints.json"), scope.chaosHintsPath()},
	} {
		if _, err := os.Stat(item.from); err != nil {
			continue
		}
		if _, err := os.Stat(item.to); err == nil {
			// Already something there; leave both rather than merging blind.
			continue
		}
		if err := os.Rename(item.from, item.to); err != nil {
			return fmt.Errorf("move %s: %w", filepath.Base(item.from), err)
		}
		moved = append(moved, filepath.Base(item.from))
	}

	if err := os.WriteFile(marker, []byte("moved to "+ownerID+"\n"), 0o600); err != nil {
		return fmt.Errorf("write migration marker: %w", err)
	}
	if len(moved) > 0 {
		log.Printf("data: moved %s into the data directory of account %s — "+
			"they were created before this instance had accounts, so there was "+
			"one operator to attribute them to",
			strings.Join(moved, ", "), ownerID)
	}
	return nil
}

// firstAdminID returns the oldest enabled administrator's ID.
//
// Used to decide who inherits files created before the instance had accounts.
// Oldest rather than any, so the answer is the same on every start and two
// runs cannot disagree about where the files went.
func (app *App) firstAdminID() (string, bool, error) {
	list, err := app.userStore().List()
	if err != nil {
		return "", false, err
	}
	var chosen *users.User
	for i := range list {
		u := list[i]
		if u.Role != authz.RoleAdmin || u.Disabled {
			continue
		}
		if chosen == nil || u.CreatedAt.Before(chosen.CreatedAt) {
			chosen = &list[i]
		}
	}
	if chosen == nil {
		return "", false, nil
	}
	return chosen.ID, true, nil
}
