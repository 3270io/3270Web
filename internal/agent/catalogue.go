// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Instruction is a shared policy fragment several skills cite.
type Instruction struct {
	// Name is the basename, e.g. "untrusted-host-data.instructions.md".
	Name string `json:"name"`
	// Title is the fragment's first heading, for listings.
	Title string `json:"title"`
	// AlwaysLoad marks a fragment an extension wants in every prompt rather
	// than fetched on demand.
	AlwaysLoad  bool   `json:"alwaysLoad,omitempty"`
	Source      Source `json:"-"`
	SourceLabel string `json:"source"`

	body string
}

// Body returns the fragment's markdown.
func (i *Instruction) Body() string { return i.body }

// Catalogue is the merged view of every skill and instruction available.
//
// Layers load in order — built-in, then loose files beside the binary, then
// extensions — and a built-in wins a name collision unless the challenger
// names it in `overrides`. That asymmetry is the point: a site should be able
// to replace the chaos playbook with one that knows its own PF-key
// conventions, but it should have to say so, because the built-ins carry
// safety rules and shadowing one by accident is not a mistake that announces
// itself.
type Catalogue struct {
	skills       map[string]*Skill
	instructions map[string]*Instruction
	extensions   []*Extension
	tasks        []Document
	// shadowed records names where a lower layer was deliberately replaced.
	shadowed map[string]Source
	// problems records entries that could not be loaded, so a listing can
	// explain an absence rather than leave a gap.
	problems []string
}

// Document is a file an extension contributes for the product to interpret.
//
// The bytes travel rather than a parsed value because this package has no
// business knowing what a Guided Business Task is. The gate that decides
// whether one is well-formed lives with the type, and an extension's document
// goes through exactly the same gate as one the operator recorded — which is
// the property worth having, and the one a second parser here would lose.
type Document struct {
	Source Source
	// File is the path it was read from, for an error message that can be
	// acted on.
	File string
	Data []byte
}

// Dirs names where a catalogue looks for user content.
type Dirs struct {
	// Skills is the loose-skill directory, normally <baseDir>/skills.
	Skills string
	// Instructions is the loose-fragment directory.
	Instructions string
	// Extensions is the extension root, normally <baseDir>/extensions.
	Extensions string
}

// DirsFor returns the conventional layout beside a binary.
func DirsFor(baseDir string) Dirs {
	return Dirs{
		Skills:       filepath.Join(baseDir, "skills"),
		Instructions: filepath.Join(baseDir, "instructions"),
		Extensions:   filepath.Join(baseDir, "extensions"),
	}
}

// Load builds the catalogue. It never returns an error: a malformed skill is
// a reason to omit that skill and say why, not a reason to refuse to start a
// terminal emulator.
func Load(dirs Dirs, productVersion string) *Catalogue {
	c := &Catalogue{
		skills:       map[string]*Skill{},
		instructions: map[string]*Instruction{},
		shadowed:     map[string]Source{},
	}

	c.loadBuiltins()
	c.loadLocalDir(dirs.Skills, dirs.Instructions)
	c.loadExtensionDir(dirs.Extensions, productVersion)

	return c
}

// loadBuiltins reads the embedded skills and instructions.
func (c *Catalogue) loadBuiltins() {
	src := Source{Kind: SourceBuiltin}

	entries, err := fs.ReadDir(builtinFS, "builtin/skills")
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			raw, err := fs.ReadFile(builtinFS, path3(("builtin/skills"), e.Name(), "SKILL.md"))
			if err != nil {
				c.problemf("built-in skill %s: %v", e.Name(), err)
				continue
			}
			c.addSkill(string(raw), src, e.Name())
		}
	}

	files, err := fs.ReadDir(builtinFS, "builtin/instructions")
	if err == nil {
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			raw, err := fs.ReadFile(builtinFS, path3("builtin/instructions", f.Name(), ""))
			if err != nil {
				c.problemf("built-in instruction %s: %v", f.Name(), err)
				continue
			}
			c.addInstruction(f.Name(), string(raw), src, false)
		}
	}
}

// loadLocalDir reads loose skills and instructions beside the binary.
func (c *Catalogue) loadLocalDir(skillsDir, instructionsDir string) {
	src := Source{Kind: SourceLocal}

	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "_") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(skillsDir, e.Name(), "SKILL.md"))
			if err != nil {
				continue // a directory with no SKILL.md is not a skill
			}
			c.addSkill(string(raw), src, e.Name())
		}
	}

	if entries, err := os.ReadDir(instructionsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".instructions.md") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(instructionsDir, e.Name()))
			if err != nil {
				c.problemf("instruction %s: %v", e.Name(), err)
				continue
			}
			c.addInstruction(e.Name(), string(raw), src, false)
		}
	}
}

// loadExtensionDir loads every enabled extension's contributions.
func (c *Catalogue) loadExtensionDir(root, productVersion string) {
	c.extensions = loadExtensions(root, productVersion)

	for _, ext := range c.extensions {
		if ext.Disabled || (ext.Problem != "" && ext.Manifest.SchemaVersion == 0) {
			continue
		}
		src := Source{Kind: SourceExtension, Extension: ext.Manifest.Name}

		for _, decl := range ext.Manifest.Contributes.Skills {
			dir, err := resolveInside(ext.Dir, decl.Dir)
			if err != nil {
				c.problemf("extension %s: skill %s: %v", ext.Manifest.Name, decl.Name, err)
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
			if err != nil {
				c.problemf("extension %s: skill %s has no readable SKILL.md", ext.Manifest.Name, decl.Name)
				continue
			}
			skill, err := ParseSkill(string(raw), src)
			if err != nil {
				c.problemf("extension %s: %v", ext.Manifest.Name, err)
				continue
			}
			// The manifest and the file must agree on the name. If they
			// disagree, the listing and the loader would key the skill
			// differently and load_skill would fail on a name the listing
			// showed.
			if decl.Name != "" && decl.Name != skill.Name {
				c.problemf("extension %s: manifest declares skill %q but SKILL.md says %q",
					ext.Manifest.Name, decl.Name, skill.Name)
				continue
			}
			c.insertSkill(skill)
		}

		for _, decl := range ext.Manifest.Contributes.Tasks {
			file, err := resolveInside(ext.Dir, decl.File)
			if err != nil {
				c.problemf("extension %s: task %s: %v", ext.Manifest.Name, decl.File, err)
				continue
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				c.problemf("extension %s: task %s is not readable", ext.Manifest.Name, decl.File)
				continue
			}
			// Handed over as bytes rather than parsed here. What a task
			// document means belongs to internal/task, which owns the
			// validation gate; this package's job is to find the file, prove
			// it is inside the extension, and say where it came from.
			c.tasks = append(c.tasks, Document{Source: src, File: file, Data: raw})
		}

		for _, decl := range ext.Manifest.Contributes.Instructions {
			file, err := resolveInside(ext.Dir, decl.File)
			if err != nil {
				c.problemf("extension %s: instruction %s: %v", ext.Manifest.Name, decl.File, err)
				continue
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				c.problemf("extension %s: instruction %s is not readable", ext.Manifest.Name, decl.File)
				continue
			}
			c.addInstruction(filepath.Base(file), string(raw), src, decl.AlwaysLoad)
		}
	}
}

// addSkill parses and inserts a skill, reporting rather than raising on a
// malformed one. dirName is used only to check the folder and the frontmatter
// agree.
func (c *Catalogue) addSkill(raw string, src Source, dirName string) {
	skill, err := ParseSkill(raw, src)
	if err != nil {
		c.problemf("%s skill in %s: %v", src, dirName, err)
		return
	}
	if dirName != "" && skill.Name != dirName {
		c.problemf("%s skill directory %q does not match its frontmatter name %q", src, dirName, skill.Name)
		return
	}
	c.insertSkill(skill)
}

// insertSkill applies the precedence rule.
func (c *Catalogue) insertSkill(s *Skill) {
	existing, clash := c.skills[s.Name]
	if !clash {
		c.skills[s.Name] = s
		return
	}
	// A challenger must name what it replaces. Anything else keeps the
	// incumbent — which, given load order, is the more trusted layer.
	if s.Overrides == s.Name {
		c.shadowed[s.Name] = existing.Source
		c.skills[s.Name] = s
		return
	}
	c.problemf("skill %q from %s was not loaded: %s already provides it. Add `overrides: %s` to its frontmatter to replace it deliberately.",
		s.Name, s.Source, existing.Source, s.Name)
}

func (c *Catalogue) addInstruction(name, raw string, src Source, alwaysLoad bool) {
	name = NormalizeInstructionName(name)
	if name == "" {
		return
	}
	body := raw
	if fmBody := stripLeadingFrontmatter(raw); fmBody != "" {
		body = fmBody
	}
	inst := &Instruction{
		Name:        name,
		Title:       firstHeading(body, name),
		AlwaysLoad:  alwaysLoad,
		Source:      src,
		SourceLabel: src.String(),
		body:        body,
	}
	if existing, clash := c.instructions[name]; clash {
		// Instructions carry no override key: a fragment is cited by name
		// from skills the challenger did not write, so replacing one changes
		// the meaning of somebody else's playbook.
		c.problemf("instruction %q from %s was not loaded: %s already provides it", name, src, existing.Source)
		return
	}
	c.instructions[name] = inst
}

func (c *Catalogue) problemf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	c.problems = append(c.problems, msg)
	log.Printf("agent catalogue: %s", msg)
}

// Skills returns every skill, sorted by name.
func (c *Catalogue) Skills() []*Skill {
	out := make([]*Skill, 0, len(c.skills))
	for _, s := range c.skills {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Skill resolves a name, tolerating the spellings a model reaches for and
// falling back to an invocation alias.
func (c *Catalogue) Skill(name string) (*Skill, bool) {
	key := NormalizeSkillName(name)
	if s, ok := c.skills[key]; ok {
		return s, true
	}
	for _, s := range c.skills {
		for _, alias := range s.Invocation {
			if NormalizeSkillName(alias) == key {
				return s, true
			}
		}
	}
	return nil, false
}

// Instructions returns every fragment, sorted by name.
func (c *Catalogue) Instructions() []*Instruction {
	out := make([]*Instruction, 0, len(c.instructions))
	for _, i := range c.instructions {
		out = append(out, i)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Instruction resolves a fragment by any spelling of its name.
func (c *Catalogue) Instruction(name string) (*Instruction, bool) {
	i, ok := c.instructions[NormalizeInstructionName(name)]
	return i, ok
}

// AlwaysLoad returns the fragments an extension asked to be present in every
// prompt.
func (c *Catalogue) AlwaysLoad() []*Instruction {
	var out []*Instruction
	for _, i := range c.Instructions() {
		if i.AlwaysLoad {
			out = append(out, i)
		}
	}
	return out
}

// Extensions returns every extension found, including disabled and broken
// ones, so an operator can see why a pack is not contributing.
func (c *Catalogue) Extensions() []*Extension {
	out := append([]*Extension(nil), c.extensions...)
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.Name < out[j].Manifest.Name })
	return out
}

// Tasks returns the task documents extensions contributed, in the order they
// were declared.
func (c *Catalogue) Tasks() []Document {
	return append([]Document(nil), c.tasks...)
}

// Shadowed reports which built-in a named skill replaced, if any.
func (c *Catalogue) Shadowed(name string) (Source, bool) {
	s, ok := c.shadowed[NormalizeSkillName(name)]
	return s, ok
}

// Problems returns every entry the catalogue could not load.
func (c *Catalogue) Problems() []string {
	return append([]string(nil), c.problems...)
}

// LoadSession tracks what a single conversation has already been given.
//
// Without it the context budget is a request rather than a rule: a model that
// loses track re-requests a playbook it already has, and the second copy costs
// exactly as much as the first while adding nothing. Returning a one-line
// reminder instead of the body is what makes the budget hold.
type LoadSession struct {
	mu   sync.Mutex
	seen map[string]bool
}

// NewLoadSession returns an empty tracker.
func NewLoadSession() *LoadSession {
	return &LoadSession{seen: map[string]bool{}}
}

// FirstLoad records a load and reports whether this is the first one. A nil
// session always reports true, so callers that do not track a conversation do
// not have to special-case it.
func (s *LoadSession) FirstLoad(kind, name string) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := kind + ":" + strings.ToLower(name)
	if s.seen[key] {
		return false
	}
	s.seen[key] = true
	return true
}

// Reset forgets everything loaded, for a conversation that has been cleared.
func (s *LoadSession) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = map[string]bool{}
}

// firstHeading returns a fragment's leading '# ' heading, or fallback.
func firstHeading(body, fallback string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
		if line != "" {
			break
		}
	}
	return strings.TrimSuffix(fallback, ".instructions.md")
}

// stripLeadingFrontmatter removes a `---` block if one is present, returning
// "" when there is nothing to strip.
func stripLeadingFrontmatter(raw string) string {
	_, body := splitFrontmatter(raw)
	if body == raw {
		return ""
	}
	return body
}

// path3 joins embedded-FS path segments, which always use forward slashes
// regardless of the host platform.
func path3(a, b, c string) string {
	if c == "" {
		return a + "/" + b
	}
	return a + "/" + b + "/" + c
}
