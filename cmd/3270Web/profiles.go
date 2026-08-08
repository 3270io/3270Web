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
	if p.Port == 0 {
		p.Port = 3270
	}
	if p.Port < 1 || p.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if !isValidHostname(p.Host) {
		return fmt.Errorf("%q is not a valid hostname or IP address", p.Host)
	}
	// The built-in sample apps are addressed as "sampleapp:<id>", which is an
	// identifier rather than a host:port. The connect form accepts that form,
	// so profiles must too — the two paths disagreeing about what a valid
	// host is would be its own bug.
	_, _, isSampleApp := parseSampleAppHost(p.Host)
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
	profiles, err := app.visibleProfiles(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read profiles: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"profiles": profiles,
		"canShare": principalFrom(c).IsAdmin() && app.publishedProfiles(c) != nil,
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
func (app *App) visibleProfiles(c *gin.Context) ([]ConnectionProfile, error) {
	own, err := app.ownProfiles(c).load()
	if err != nil {
		return nil, err
	}

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
	for _, p := range shared {
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
func (app *App) findVisibleProfile(c *gin.Context, name string) (ConnectionProfile, bool) {
	profiles, err := app.visibleProfiles(c)
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
