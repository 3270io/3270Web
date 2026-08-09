package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// Connection profiles.
//
// TLS, the LU name, the terminal model and the code page were all
// server-wide environment variables, so every session in a deployment had to
// share them. You could not have one host on TLS and another in the clear,
// could not pin an LU per host, and could not run a model 2 against one
// application and a model 4 against another — all of which are ordinary
// things to need, and all of which every desktop emulator does per session.
// The connect form took "hostname:port" and nothing else.
//
// Profiles are stored server-side rather than in the browser, unlike the
// existing saved-hosts list: connection settings are the kind of thing an
// administrator sets up once for everyone, and a list that evaporates when
// someone clears their cache is not that.
//
// Most of a profile ends up inside s3270's own target syntax:
//
//	[L:][Y:][lu@]host[:port]
//
// where L: requests TLS and Y: skips certificate verification. Model and
// code page are separate flags.

const profilesFileName = "profiles.json"

// ConnectionProfile is a named, reusable host connection.
type ConnectionProfile struct {
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
	// TLS wraps the connection (s3270's "L:" prefix).
	TLS bool `json:"tls"`
	// SkipVerify disables certificate verification ("Y:"). Separate from TLS
	// because "encrypted but unverified" is a real, and materially weaker,
	// configuration that an operator should have to opt into explicitly.
	SkipVerify bool `json:"skipVerify,omitempty"`
	// LUName pins the session to a specific logical unit.
	LUName string `json:"luName,omitempty"`
	// Model is an s3270 terminal model, e.g. "3279-4-E".
	Model string `json:"model,omitempty"`
	// CodePage is an s3270 code page, e.g. "cp037" or "cp1140".
	CodePage    string `json:"codePage,omitempty"`
	Description string `json:"description,omitempty"`
	// Audience decides who a published profile is for. It has no meaning on a
	// profile somebody saved for themselves, which is already theirs alone.
	//
	// All three empty means everyone, which is what every profile in an
	// existing deployment is: turning this on must not quietly take a host
	// list away from the people using it. Naming any of them narrows it to
	// those who match at least one — the three are an or, not an and, because
	// "the payments team, plus Dave who covers for them" is the shape these
	// lists actually come in.
	Users  []string `json:"users,omitempty"`
	Groups []string `json:"groups,omitempty"`
	Roles  []string `json:"roles,omitempty"`
	// NotOffered marks a preset that exists but is on nobody's list yet.
	//
	// The audience alone cannot say this: naming nobody means everybody, and
	// that rule cannot be changed without taking a host list away from every
	// instance already relying on it. So "here, ready, but not handed out" —
	// which is what the bundled sample apps are when they are seeded — needs
	// a field of its own.
	//
	// Negative so the zero value is "offered": every preset written before
	// this existed, and every one saved by a client that has never heard of
	// it, stays exactly as offered as it was.
	NotOffered bool `json:"notOffered,omitempty"`
	// Shared marks a profile that came from the published set rather than the
	// caller's own. Output only: it is derived from which store held the
	// profile, so a client cannot promote one by asserting it.
	Shared bool `json:"shared,omitempty"`
}

type connectionProfileStore struct {
	mu   sync.Mutex
	path string
}

func newConnectionProfileStore(baseDir string) *connectionProfileStore {
	return &connectionProfileStore{path: filepath.Join(baseDir, profilesFileName)}
}

func (p *connectionProfileStore) load() ([]ConnectionProfile, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.loadLocked()
}

func (p *connectionProfileStore) loadLocked() ([]ConnectionProfile, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []ConnectionProfile{}, nil
		}
		return nil, err
	}
	var profiles []ConnectionProfile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, fmt.Errorf("profiles file is not valid JSON: %w", err)
	}
	if profiles == nil {
		profiles = []ConnectionProfile{}
	}
	sortProfiles(profiles)
	return profiles, nil
}

func (p *connectionProfileStore) saveLocked(profiles []ConnectionProfile) error {
	sortProfiles(profiles)
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o750); err != nil {
		return err
	}
	// Write via a temporary file and rename, so a failure part-way through
	// cannot leave the deployment with a truncated profile list.
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p.path)
}

// upsert adds or replaces a profile by name (case-insensitively, since
// "TSO" and "tso" being two different profiles would only ever be a mistake).
func (p *connectionProfileStore) upsert(profile ConnectionProfile) ([]ConnectionProfile, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	profiles, err := p.loadLocked()
	if err != nil {
		return nil, err
	}
	replaced := false
	for i := range profiles {
		if strings.EqualFold(profiles[i].Name, profile.Name) {
			profiles[i] = profile
			replaced = true
			break
		}
	}
	if !replaced {
		profiles = append(profiles, profile)
	}
	if err := p.saveLocked(profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}

// mutate applies one change to the whole published set, under one lock and
// one atomic write.
//
// upsert is the right shape for saving a profile somebody edited. It is the
// wrong shape for a change that spans several — renaming a group across every
// audience that names it, or taking one out of them — because each call is its
// own load, its own write and its own chance to fail, so the third of five
// failing leaves two profiles renamed and three not. There is no way back from
// that: the caller cannot tell which half it got.
//
// The callback is handed the loaded list and returns the list to write, and
// whether anything actually changed. Returning false, or an error, writes
// nothing at all — which is what makes "work out whether this is allowed with
// the whole set in front of you, then decide" a thing a caller can do without
// leaving a partial change behind when the answer is no.
func (p *connectionProfileStore) mutate(
	apply func([]ConnectionProfile) ([]ConnectionProfile, bool, error),
) ([]ConnectionProfile, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	profiles, err := p.loadLocked()
	if err != nil {
		return nil, err
	}
	next, changed, err := apply(profiles)
	if err != nil {
		return nil, err
	}
	if !changed {
		return profiles, nil
	}
	if err := p.saveLocked(next); err != nil {
		return nil, err
	}
	return next, nil
}

func (p *connectionProfileStore) delete(name string) ([]ConnectionProfile, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	profiles, err := p.loadLocked()
	if err != nil {
		return nil, err
	}
	out := profiles[:0]
	for _, existing := range profiles {
		if strings.EqualFold(existing.Name, name) {
			continue
		}
		out = append(out, existing)
	}
	if err := p.saveLocked(out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *connectionProfileStore) find(name string) (ConnectionProfile, bool) {
	profiles, err := p.load()
	if err != nil {
		return ConnectionProfile{}, false
	}
	for _, profile := range profiles {
		if strings.EqualFold(profile.Name, name) {
			return profile, true
		}
	}
	return ConnectionProfile{}, false
}

func sortProfiles(profiles []ConnectionProfile) {
	sort.Slice(profiles, func(i, j int) bool {
		return strings.ToLower(profiles[i].Name) < strings.ToLower(profiles[j].Name)
	})
}

// validateProfile checks a profile is safe to turn into s3270 arguments.
//
// These values become argv elements, not a shell command line, so the usual
// quoting hazards do not apply. What does apply is that a value beginning
// with "-" would be read by s3270 as a flag rather than as data, which is
// how an innocuous-looking profile could turn into an option injection.
// Every free-text field is checked for that, and for the delimiters that
// would otherwise let one field spill into another.
func validateProfile(p *ConnectionProfile) error {
	p.Name = strings.TrimSpace(p.Name)
	p.Host = strings.TrimSpace(p.Host)
	p.LUName = strings.TrimSpace(p.LUName)
	p.Model = strings.TrimSpace(p.Model)
	p.CodePage = strings.TrimSpace(p.CodePage)
	p.Description = strings.TrimSpace(p.Description)

	if p.Name == "" {
		return fmt.Errorf("a profile name is required")
	}
	if len(p.Name) > 64 {
		return fmt.Errorf("profile name is too long (max 64 characters)")
	}
	if p.Host == "" {
		return fmt.Errorf("a host is required")
	}
	// The built-in sample apps are addressed as "sampleapp:<id>", which is an
	// identifier rather than a host:port. The connect form accepts that form,
	// so profiles must too — the two paths disagreeing about what a valid
	// host is would be its own bug.
	sampleID, _, isSampleApp := parseSampleAppHost(p.Host)

	if p.Port == 0 {
		// A sample app does not listen on 3270: the web interface itself does,
		// and they run in the same place. Defaulting one to the ordinary port
		// would make "leave the port blank", which is what anybody does, the
		// one way to write a preset that cannot start.
		if isSampleApp {
			p.Port = defaultSampleAppPort
		} else {
			p.Port = 3270
		}
	}
	if p.Port < 1 || p.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if !isValidHostname(p.Host) {
		return fmt.Errorf("%q is not a valid hostname or IP address", p.Host)
	}
	if isSampleApp {
		// Refused when it is written rather than when somebody connects. A
		// preset is stored once and used by everybody it was assigned to, so a
		// bad port here is a host that appears on the session manager and fails
		// for each of them in turn.
		if _, known := sampleAppConfig(sampleID); !known {
			return fmt.Errorf("%q is not one of the bundled sample apps", p.Host)
		}
		if !isAllowedSampleAppPort(p.Port) {
			return fmt.Errorf("a sample app listens on one of %s, not %d",
				joinInts(allowedSampleAppPorts()), p.Port)
		}
	}
	if !isSampleApp {
		// Otherwise the host must not carry its own port, LU or s3270 prefix;
		// those come from the structured fields, and allowing both would make
		// it ambiguous which one wins.
		if strings.ContainsAny(p.Host, " @") || (strings.Contains(p.Host, ":") && !strings.HasPrefix(p.Host, "[")) {
			return fmt.Errorf("put the port in the Port field, not the host")
		}
	}

	for label, value := range map[string]string{
		"LU name":   p.LUName,
		"model":     p.Model,
		"code page": p.CodePage,
	} {
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "-") {
			return fmt.Errorf("%s must not start with '-'", label)
		}
		if strings.ContainsAny(value, " \t\r\n:@,") {
			return fmt.Errorf("%s contains characters that are not allowed", label)
		}
	}
	if !p.TLS && p.SkipVerify {
		return fmt.Errorf("certificate verification only applies to TLS connections")
	}
	if len(p.Description) > 200 {
		return fmt.Errorf("description is too long (max 200 characters)")
	}
	return nil
}

// s3270Target renders the profile as an s3270 connection target:
// [L:][Y:][lu@]host[:port].
// joinInts writes a list of numbers the way a sentence needs them.
func joinInts(values []int) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, ", ")
}

func (p ConnectionProfile) s3270Target() string {
	var sb strings.Builder
	if p.TLS {
		sb.WriteString("L:")
		if p.SkipVerify {
			sb.WriteString("Y:")
		}
	}
	if p.LUName != "" {
		sb.WriteString(p.LUName)
		sb.WriteString("@")
	}
	sb.WriteString(p.Host)
	port := p.Port
	if port <= 0 {
		port = 3270
	}
	sb.WriteString(":")
	sb.WriteString(strconv.Itoa(port))
	return sb.String()
}

// displayTarget is the profile's host and port without the s3270 prefixes,
// for showing in the UI and for the last-target cookie.
func (p ConnectionProfile) displayTarget() string {
	port := p.Port
	if port <= 0 {
		port = 3270
	}
	return fmt.Sprintf("%s:%d", p.Host, port)
}

// overrideArgs returns the s3270 flags this profile sets, which take
// precedence over the server-wide defaults by being appended after them.
func (p ConnectionProfile) overrideArgs() []string {
	var args []string
	if p.Model != "" {
		args = append(args, "-model", p.Model)
	}
	if p.CodePage != "" {
		args = append(args, "-codepage", p.CodePage)
	}
	return args
}

/* ---------------------------------------------------------------- */
/* Handlers                                                          */
/* ---------------------------------------------------------------- */

func (app *App) ProfilesListHandler(c *gin.Context) {
	// An administrator sees the whole published set, because they are the one
	// who assigns it and cannot edit a profile the page will not show them.
	// Everybody else sees what they may actually reach: a host list is more
	// useful for being short, and there is no reason to name mainframes at
	// somebody who will be refused them.
	lister := app.assignedProfiles
	if principalFrom(c).IsAdmin() {
		lister = app.visibleProfiles
	}
	profiles, err := lister(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read profiles: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"profiles": profiles,
		"canShare": principalFrom(c).IsAdmin() && app.publishedProfiles(c) != nil,
		// The group names in use, so the audience editor can offer them
		// instead of relying on somebody remembering the spelling.
		"groups": app.knownGroups(c),
		// The bundled sample apps, so the profile editor can offer one by
		// name. The host they are dialled by is "sampleapp:<id>" on a port
		// that is not 3270, and typing that from memory is the reason a
		// profile pointing at one was the awkward kind to write.
		"sampleApps": sampleAppPresetOptions(),
	})
}

// visibleProfiles is what this caller can see: the published host list plus
// their own, with their own winning on a name collision.
//
// Merging rather than choosing one is what keeps a host list usable for a
// team. Profiles are shared infrastructure — everyone connects to the same
// mainframes — so making them purely private would mean every account
// re-entering the same hosts, while making them purely shared is what this
// separation is moving away from.
//
// A preset that is not offered is left out of all of it, an administrator
// included. This is the host list, not the administration of it: the presets
// page reads the store directly and is where an unoffered preset is meant to
// be seen. Showing one here would put a host in a picker that refuses to
// connect to it, because connecting by name goes through assignedProfiles.
func (app *App) visibleProfiles(c *gin.Context) ([]ConnectionProfile, error) {
	own, err := app.ownProfiles(c).load()
	if err != nil {
		return nil, err
	}
	own = offeredOnly(own)

	published := app.publishedProfiles(c)
	if published == nil {
		return own, nil
	}
	shared, err := published.load()
	if err != nil {
		return nil, err
	}

	byName := make(map[string]ConnectionProfile, len(shared)+len(own))
	order := make([]string, 0, len(shared)+len(own))
	add := func(p ConnectionProfile, isShared bool) {
		key := strings.ToLower(strings.TrimSpace(p.Name))
		p.Shared = isShared
		if _, seen := byName[key]; !seen {
			order = append(order, key)
		}
		byName[key] = p
	}
	for _, p := range offeredOnly(shared) {
		add(p, true)
	}
	// Second, so a profile of the caller's own with the same name replaces
	// the published one rather than being hidden by it.
	for _, p := range own {
		add(p, false)
	}

	out := make([]ConnectionProfile, 0, len(order))
	for _, key := range order {
		out = append(out, byName[key])
	}
	sortProfiles(out)
	return out, nil
}

// findVisibleProfile resolves a profile by name across both sets.
//
// It reads the assigned list rather than the visible one, which is what makes
// an audience a restriction rather than a display filter: both paths that
// connect by name come through here, so naming a profile you were not given
// finds nothing — the same answer as naming one that does not exist.
func (app *App) findVisibleProfile(c *gin.Context, name string) (ConnectionProfile, bool) {
	profiles, err := app.assignedProfiles(c)
	if err != nil {
		return ConnectionProfile{}, false
	}
	for _, p := range profiles {
		if strings.EqualFold(strings.TrimSpace(p.Name), strings.TrimSpace(name)) {
			return p, true
		}
	}
	return ConnectionProfile{}, false
}

func (app *App) ProfilesSaveHandler(c *gin.Context) {
	var payload struct {
		ConnectionProfile
		// Publish asks for this to go into the shared host list rather than
		// the caller's own. Named separately from the Shared output field so
		// a client round-tripping a listing cannot publish by accident.
		Publish bool `json:"publish"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	profile := payload.ConnectionProfile
	profile.Shared = false
	if err := validateProfile(&profile); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// An audience only decides who a published profile reaches, so it is
	// dropped from a private save rather than stored where nothing reads it —
	// otherwise publishing that profile later would silently apply a list
	// somebody wrote in a different context.
	if payload.Publish {
		normaliseAudience(&profile)
		settleOffered(&profile)
	} else {
		profile.Users, profile.Groups, profile.Roles = nil, nil, nil
		// Nor can a profile of somebody's own be withheld from them: there is
		// nobody else for it to be offered to, and the flag would only hide it
		// from the one list that shows it.
		profile.NotOffered = false
	}

	store, err := app.profileStoreForWrite(c, payload.Publish)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if _, err := store.upsert(profile); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save profile: %v", err)})
		return
	}

	profiles, err := app.visibleProfiles(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read profiles: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "profiles": profiles})
}

// profileStoreForWrite picks which set a change lands in.
//
// Publishing changes what every account sees, so it is an administrator's
// call. Without the flag a save is always the caller's own, which means an
// ordinary user editing a published profile gets their own copy rather than
// changing it for everybody — the same shape as overriding a setting.
func (app *App) profileStoreForWrite(c *gin.Context, publish bool) (*connectionProfileStore, error) {
	if !publish {
		return app.ownProfiles(c), nil
	}
	published := app.publishedProfiles(c)
	if published == nil {
		// One operator: their own set already is the shared one.
		return app.ownProfiles(c), nil
	}
	if !principalFrom(c).IsAdmin() {
		return nil, errors.New("publishing a profile to everyone requires an administrator account")
	}
	return published, nil
}

func (app *App) ProfilesDeleteHandler(c *gin.Context) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a profile name is required"})
		return
	}
	// Try the caller's own set first. Deleting only what belongs to them is
	// the safe default; removing a published profile takes it away from
	// everybody, so it needs the same authority publishing did.
	own := app.ownProfiles(c)
	if _, found := own.find(name); found {
		if _, err := own.delete(name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to delete profile: %v", err)})
			return
		}
	} else if published := app.publishedProfiles(c); published != nil {
		if _, found := published.find(name); found {
			if !principalFrom(c).IsAdmin() {
				c.JSON(http.StatusForbidden, gin.H{
					"error": "that profile is shared with everyone; removing it requires an administrator account",
				})
				return
			}
			if _, err := published.delete(name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to delete profile: %v", err)})
				return
			}
		}
	}

	profiles, err := app.visibleProfiles(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read profiles: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "profiles": profiles})
}
