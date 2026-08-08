package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/task"
	"github.com/jnnngs/3270Web/internal/users"
)

// Where per-user and instance-wide files live under the data directory.
//
//	<base>/users/<ownerID>/…   one person's own files
//	<base>/shared/…            what an administrator has published to everyone
//
// Ownership decides the directory, so the filesystem layout matches the
// authorization rules rather than restating them. Nothing has to remember to
// filter a listing: a user reading their own directory cannot see another's
// because it is not there.
const (
	userDataDirName   = "users"
	sharedDataDirName = "shared"
)

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
	// shared is where published copies live. Empty under a single operator,
	// because then everything already is shared and a second location would
	// only be somewhere else to look.
	shared string
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
	return dataScope{
		base:   filepath.Join(app.baseDir, userDataDirName, ownerID),
		shared: filepath.Join(app.baseDir, sharedDataDirName),
	}
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

// tasksPath is this owner's business-task catalogue. Tasks are private with
// no published set: a task is a recorded procedure someone is working on, not
// infrastructure the team shares like a host list.
func (s dataScope) tasksPath() string { return filepath.Join(s.base, "tasks.json") }

// profilesPath is this owner's own connection profiles.
func (s dataScope) profilesPath() string { return filepath.Join(s.base, "profiles.json") }

// sharedProfilesPath is the published host list every account can read.
// Empty under a single operator, where profilesPath already is it.
func (s dataScope) sharedProfilesPath() string {
	if s.shared == "" {
		return ""
	}
	return filepath.Join(s.shared, profilesFileName)
}

// themesDir is this owner's own custom themes.
func (s dataScope) themesDir() string { return filepath.Join(s.base, "themes") }

// sharedThemesDir is the published theme set. Empty under a single operator.
func (s dataScope) sharedThemesDir() string {
	if s.shared == "" {
		return ""
	}
	return filepath.Join(s.shared, "themes")
}

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

	if err := os.MkdirAll(filepath.Join(app.baseDir, sharedDataDirName), 0o750); err != nil {
		return fmt.Errorf("create shared data directory: %w", err)
	}

	// Where each file goes depends on what it was for.
	//
	// Connection profiles and themes were instance-wide and everybody used
	// them, so they become the published set. Moving a team's host list into
	// one person's directory would take it away from everyone else on an
	// instance that had been working fine, which is a worse first impression
	// of authentication than any amount of separation is worth.
	//
	// Chaos runs, their hints and the task catalogue go to the first
	// administrator. They are working documents rather than infrastructure,
	// and a chaos run holds captured screens — publishing those to everyone
	// is the exact thing this separation exists to stop.
	moved := make([]string, 0, 5)
	for _, item := range []struct{ from, to string }{
		{filepath.Join(app.baseDir, "chaos-runs"), scope.chaosRunsDir()},
		{filepath.Join(app.baseDir, "chaos-hints.json"), scope.chaosHintsPath()},
		{filepath.Join(app.baseDir, "tasks.json"), scope.tasksPath()},
		{filepath.Join(app.baseDir, profilesFileName), scope.sharedProfilesPath()},
		{filepath.Join(app.baseDir, "themes"), scope.sharedThemesDir()},
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
		log.Printf("data: this instance has files from before it had accounts. "+
			"Moved %s — connection profiles and themes are now published to "+
			"everyone; the rest belongs to account %s, the one operator there "+
			"was to attribute them to", strings.Join(moved, ", "), ownerID)
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

// ownerStores caches the per-account file stores.
//
// The stores were sync.Once singletons keyed on nothing, which is right for
// one operator and wrong the moment there are several: one person's catalogue
// would be handed to everybody. A map keyed by owner is the same lazy
// construction, just with the key it always implicitly had.
type ownerStores struct {
	mu       sync.Mutex
	profiles map[string]*connectionProfileStore
	tasks    map[string]*task.Store
}

func newOwnerStores() *ownerStores {
	return &ownerStores{
		profiles: make(map[string]*connectionProfileStore),
		tasks:    make(map[string]*task.Store),
	}
}

// profileStoreAt returns the profile store rooted at dir, creating it once.
func (o *ownerStores) profileStoreAt(dir string) *connectionProfileStore {
	o.mu.Lock()
	defer o.mu.Unlock()
	if s, ok := o.profiles[dir]; ok {
		return s
	}
	s := newConnectionProfileStore(dir)
	o.profiles[dir] = s
	return s
}

// taskStoreAt returns the task catalogue rooted at dir, creating it once.
func (o *ownerStores) taskStoreAt(dir string) *task.Store {
	o.mu.Lock()
	defer o.mu.Unlock()
	if s, ok := o.tasks[dir]; ok {
		return s
	}
	s := task.NewStore(dir)
	o.tasks[dir] = s
	return s
}

// ownProfiles is the caller's own profile store — what a save writes to.
func (app *App) ownProfiles(c *gin.Context) *connectionProfileStore {
	return app.stores().profileStoreAt(app.dataScopeForRequest(c).base)
}

// publishedProfiles is the store an administrator publishes into, and every
// account reads from. Nil under a single operator, where there is only one
// store and publishing would mean nothing.
func (app *App) publishedProfiles(c *gin.Context) *connectionProfileStore {
	scope := app.dataScopeForRequest(c)
	if scope.shared == "" {
		return nil
	}
	return app.stores().profileStoreAt(scope.shared)
}

// taskStoreForRequest returns the caller's own task catalogue.
func (app *App) taskStoreForRequest(c *gin.Context) *task.Store {
	return app.stores().taskStoreAt(app.dataScopeForRequest(c).base)
}

func (app *App) stores() *ownerStores {
	app.storesOnce.Do(func() {
		if app.ownerStores == nil {
			app.ownerStores = newOwnerStores()
		}
	})
	return app.ownerStores
}
