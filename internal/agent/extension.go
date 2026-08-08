package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ExtensionManifestName is the file that marks a directory as an extension.
const ExtensionManifestName = "3270-extension.json"

// DisabledFileName lists extension names to skip, one per line, '#' for
// comments. Disabling by file rather than by deleting the folder means an
// operator can turn a pack off without losing the pack.
const DisabledFileName = ".disabled"

// extensionSchemaVersion is the only manifest version understood. A manifest
// declaring anything else is skipped rather than best-guessed: a pack built
// for a newer schema may rely on a contribution kind this build cannot honour,
// and half-loading it would be worse than not loading it.
const extensionSchemaVersion = 1

// Manifest is an extension's declaration of what it contributes.
//
// Contributions are content only — playbooks, policy fragments and saved
// tasks. There is deliberately no way for an extension to register a command,
// a script or an executable tool. 3270Web holds an authenticated session
// against someone's mainframe; the blast radius of "drop a folder in and it
// runs" is not one an operator can reason about, and every capability an
// extension needs is already a built-in tool it can direct the model to use.
type Manifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	DisplayName   string `json:"displayName,omitempty"`
	Description   string `json:"description,omitempty"`
	Author        string `json:"author,omitempty"`

	Requires struct {
		Product    string `json:"product,omitempty"`
		MinVersion string `json:"minVersion,omitempty"`
	} `json:"requires,omitempty"`

	Contributes struct {
		Skills []struct {
			Dir  string `json:"dir"`
			Name string `json:"name"`
		} `json:"skills,omitempty"`
		Instructions []struct {
			File       string `json:"file"`
			AlwaysLoad bool   `json:"alwaysLoad,omitempty"`
		} `json:"instructions,omitempty"`
		Tasks []struct {
			File string `json:"file"`
		} `json:"tasks,omitempty"`
	} `json:"contributes"`
}

// Extension is a validated, loaded extension.
type Extension struct {
	Manifest Manifest `json:"manifest"`
	Dir      string   `json:"-"`
	Disabled bool     `json:"disabled"`
	// Problem records why an extension was skipped, so a listing can explain
	// a pack's absence instead of silently omitting it.
	Problem string `json:"problem,omitempty"`
}

var extensionNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)

// loadExtensions scans root for extension directories. It never fails the
// caller: a broken pack is reported on the Extension it belongs to and
// skipped, because one malformed manifest must not stop 3270Web from
// starting.
func loadExtensions(root, productVersion string) []*Extension {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil // no extensions directory is the normal case
	}

	disabled := readDisabledList(filepath.Join(root, DisabledFileName))

	var out []*Extension
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// '.' and '_' prefixes are reserved for the loader's own files and
		// for templates that are not meant to load.
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}

		dir := filepath.Join(root, name)
		ext := &Extension{Dir: dir, Disabled: disabled[name]}

		manifest, err := readManifest(dir, productVersion)
		if err != nil {
			ext.Manifest.Name = name
			ext.Problem = err.Error()
			out = append(out, ext)
			continue
		}
		ext.Manifest = *manifest

		// A folder whose name differs from the manifest's is confusing but
		// workable; the manifest wins and the loader says so.
		if manifest.Name != name {
			ext.Problem = fmt.Sprintf("directory is %q but the manifest declares %q; the manifest name is used", name, manifest.Name)
		}
		if disabled[manifest.Name] {
			ext.Disabled = true
		}
		out = append(out, ext)
	}
	return out
}

func readManifest(dir, productVersion string) (*Manifest, error) {
	path := filepath.Join(dir, ExtensionManifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no readable %s", ExtensionManifestName)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %v", ExtensionManifestName, err)
	}
	if m.SchemaVersion != extensionSchemaVersion {
		return nil, fmt.Errorf("schemaVersion is %d, this build understands %d", m.SchemaVersion, extensionSchemaVersion)
	}
	if !extensionNameRE.MatchString(m.Name) {
		return nil, fmt.Errorf("name %q must be 2-64 characters of lowercase letters, digits, '-' or '_'", m.Name)
	}
	if strings.TrimSpace(m.Version) == "" {
		return nil, fmt.Errorf("version is required")
	}
	if min := strings.TrimSpace(m.Requires.MinVersion); min != "" && productVersion != "" {
		if compareVersions(min, productVersion) > 0 {
			return nil, fmt.Errorf("needs 3270Web %s or later, this is %s", min, productVersion)
		}
	}
	return &m, nil
}

// resolveInside joins rel onto dir and refuses anything that escapes it.
//
// The prefix test appends a separator on purpose: a bare strings.HasPrefix
// would accept "/opt/ext-backup" as being inside "/opt/ext".
func resolveInside(dir, rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("empty path")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	full, err := filepath.Abs(filepath.Join(root, rel))
	if err != nil {
		return "", err
	}
	if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", fmt.Errorf("%q resolves outside the extension directory", rel)
	}
	return full, nil
}

// readDisabledList parses the newline-delimited .disabled file.
func readDisabledList(path string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = true
	}
	return out
}

// compareVersions compares dotted numeric versions segment by segment,
// ignoring any leading 'v' and any non-numeric suffix. Returns -1, 0 or 1.
func compareVersions(a, b string) int {
	as := versionSegments(a)
	bs := versionSegments(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		}
	}
	return 0
}

func versionSegments(v string) []int {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, part := range strings.Split(v, ".") {
		n, _ := strconv.Atoi(strings.TrimSpace(part))
		out = append(out, n)
	}
	return out
}
