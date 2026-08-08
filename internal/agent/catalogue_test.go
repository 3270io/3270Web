package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkill drops a SKILL.md into dir/<name>/.
func writeSkill(t *testing.T, dir, name, frontmatter, body string) {
	t.Helper()
	sub := filepath.Join(dir, name)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\n" + frontmatter + "\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func emptyDirs(t *testing.T) Dirs {
	t.Helper()
	return DirsFor(t.TempDir())
}

// TestBuiltinsLoadCleanly is the guard on the shipped playbooks: every one
// parses, has a description, and cites only fragments that exist. A skill
// that cites a missing fragment sends the model to call load_instruction for
// something that will never resolve.
func TestBuiltinsLoadCleanly(t *testing.T) {
	c := Load(emptyDirs(t), "1.0.0")

	if problems := c.Problems(); len(problems) > 0 {
		t.Fatalf("built-in catalogue reported problems:\n  %s", strings.Join(problems, "\n  "))
	}

	skills := c.Skills()
	if len(skills) == 0 {
		t.Fatal("no built-in skills were embedded")
	}

	for _, s := range skills {
		if s.Source.Kind != SourceBuiltin {
			t.Errorf("skill %q: source = %q, want builtin", s.Name, s.Source)
		}
		if len(s.Description) > maxDescription {
			t.Errorf("skill %q: description exceeds the limit", s.Name)
		}
		if s.Body() == "" {
			t.Errorf("skill %q: empty body", s.Name)
		}
		for _, ref := range s.Instructions {
			if _, ok := c.Instruction(ref); !ok {
				t.Errorf("skill %q cites %q, which is not in the catalogue", s.Name, ref)
			}
		}
	}
}

// TestFrontmatterParsing covers the shapes people actually write.
func TestFrontmatterParsing(t *testing.T) {
	raw := `---
name: my-skill
description: Does a thing.
invocation: [alpha, "beta", 'gamma']
tools: get_screen, send_key
overrides: my-skill
---

Body text citing ` + "`sensitive-fields.instructions.md`" + ` inline.
`
	s, err := ParseSkill(raw, Source{Kind: SourceLocal})
	if err != nil {
		t.Fatalf("ParseSkill: %v", err)
	}
	if s.Name != "my-skill" || s.Description != "Does a thing." {
		t.Errorf("name/description = %q/%q", s.Name, s.Description)
	}
	if got, want := strings.Join(s.Invocation, ","), "alpha,beta,gamma"; got != want {
		t.Errorf("invocation = %q, want %q — quoting and bracket forms should all work", got, want)
	}
	if got, want := strings.Join(s.Tools, ","), "get_screen,send_key"; got != want {
		t.Errorf("tools = %q, want %q — a bare comma list should work too", got, want)
	}
	if s.Overrides != "my-skill" {
		t.Errorf("overrides = %q", s.Overrides)
	}
	// The body reference should be picked up without being declared.
	if got, want := strings.Join(s.Instructions, ","), "sensitive-fields.instructions.md"; got != want {
		t.Errorf("instructions = %q, want %q", got, want)
	}
	if strings.Contains(s.Body(), "name: my-skill") {
		t.Error("frontmatter leaked into the body")
	}
}

func TestParseSkillRejectsUnusableSkills(t *testing.T) {
	cases := map[string]string{
		"no frontmatter at all": "just a body",
		"no name":               "---\ndescription: x\n---\nbody",
		"no description":        "---\nname: thing\n---\nbody",
		"bad name":              "---\nname: Not_A_Name\ndescription: x\n---\nbody",
	}
	for name, raw := range cases {
		if _, err := ParseSkill(raw, Source{Kind: SourceLocal}); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// TestLocalSkillNeedsOverridesToShadowBuiltin is the precedence rule. A site
// must be able to replace the chaos playbook with one that knows its own
// conventions — but only by saying so, because the built-ins carry safety
// rules and shadowing one by accident does not announce itself.
func TestLocalSkillNeedsOverridesToShadowBuiltin(t *testing.T) {
	t.Run("silent clash keeps the built-in", func(t *testing.T) {
		dirs := emptyDirs(t)
		writeSkill(t, dirs.Skills, "chaos-monkey",
			"name: chaos-monkey\ndescription: Mine.", "local body")

		c := Load(dirs, "1.0.0")
		s, ok := c.Skill("chaos-monkey")
		if !ok {
			t.Fatal("chaos-monkey missing")
		}
		if s.Source.Kind != SourceBuiltin {
			t.Errorf("source = %q, want builtin — a silent clash must not win", s.Source)
		}
		if len(c.Problems()) == 0 {
			t.Error("the rejected skill should be reported, not silently dropped")
		}
	})

	t.Run("declared override wins", func(t *testing.T) {
		dirs := emptyDirs(t)
		writeSkill(t, dirs.Skills, "chaos-monkey",
			"name: chaos-monkey\ndescription: Mine.\noverrides: chaos-monkey", "local body")

		c := Load(dirs, "1.0.0")
		s, ok := c.Skill("chaos-monkey")
		if !ok {
			t.Fatal("chaos-monkey missing")
		}
		if s.Source.Kind != SourceLocal {
			t.Errorf("source = %q, want local", s.Source)
		}
		if prev, ok := c.Shadowed("chaos-monkey"); !ok || prev.Kind != SourceBuiltin {
			t.Errorf("shadowing should be recorded, got %v/%v", prev, ok)
		}
	})
}

func TestSkillLookupToleratesModelSpellings(t *testing.T) {
	dirs := emptyDirs(t)
	writeSkill(t, dirs.Skills, "site-thing",
		"name: site-thing\ndescription: x\ninvocation: [shorthand]", "body")
	c := Load(dirs, "1.0.0")

	for _, spelling := range []string{
		"site-thing", "SITE-THING", " site-thing ", "skills/site-thing/SKILL.md",
		"`site-thing`", "shorthand",
	} {
		if _, ok := c.Skill(spelling); !ok {
			t.Errorf("Skill(%q) not found", spelling)
		}
	}
	if _, ok := c.Skill("no-such-skill"); ok {
		t.Error("an unknown name should not resolve")
	}
}

// writeExtension creates an extension directory with the given manifest.
func writeExtension(t *testing.T, root, name string, m Manifest) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ExtensionManifestName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func sampleManifest(name string) Manifest {
	var m Manifest
	m.SchemaVersion = 1
	m.Name = name
	m.Version = "0.1.0"
	return m
}

func TestExtensionContributesSkill(t *testing.T) {
	dirs := emptyDirs(t)

	m := sampleManifest("acme-pack")
	m.Contributes.Skills = append(m.Contributes.Skills, struct {
		Dir  string `json:"dir"`
		Name string `json:"name"`
	}{Dir: "skills/acme-enquiry", Name: "acme-enquiry"})
	dir := writeExtension(t, dirs.Extensions, "acme-pack", m)
	writeSkill(t, filepath.Join(dir, "skills"), "acme-enquiry",
		"name: acme-enquiry\ndescription: Look up an ACME account.", "steps")

	c := Load(dirs, "1.0.0")

	s, ok := c.Skill("acme-enquiry")
	if !ok {
		t.Fatalf("extension skill missing; problems: %v", c.Problems())
	}
	if s.Source.Kind != SourceExtension || s.Source.Extension != "acme-pack" {
		t.Errorf("source = %q, want extension:acme-pack — a listing must be able to say whose advice this is", s.Source)
	}
}

// TestExtensionCannotEscapeItsDirectory is the containment rule. The prefix
// check appends a separator on purpose, so a sibling directory whose name
// merely starts with the extension's is not treated as inside it.
func TestExtensionCannotEscapeItsDirectory(t *testing.T) {
	dirs := emptyDirs(t)

	// A skill the extension must not be able to reach.
	outside := filepath.Join(filepath.Dir(dirs.Extensions), "outside")
	writeSkill(t, outside, "stolen", "name: stolen\ndescription: x", "body")

	m := sampleManifest("escaper")
	m.Contributes.Skills = append(m.Contributes.Skills, struct {
		Dir  string `json:"dir"`
		Name string `json:"name"`
	}{Dir: "../../outside/stolen", Name: "stolen"})
	writeExtension(t, dirs.Extensions, "escaper", m)

	c := Load(dirs, "1.0.0")

	if _, ok := c.Skill("stolen"); ok {
		t.Fatal("an extension loaded a skill from outside its own directory")
	}
	if !strings.Contains(strings.Join(c.Problems(), "\n"), "outside the extension directory") {
		t.Errorf("the traversal should be reported; problems were %v", c.Problems())
	}

	t.Run("sibling prefix is not inside", func(t *testing.T) {
		if _, err := resolveInside("/opt/ext", "../ext-backup/x"); err == nil {
			t.Error("/opt/ext-backup must not count as inside /opt/ext")
		}
	})
}

func TestExtensionRejections(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{"wrong schema", func(m *Manifest) { m.SchemaVersion = 99 }, "schemaVersion"},
		{"bad name", func(m *Manifest) { m.Name = "Not A Name" }, "name"},
		{"no version", func(m *Manifest) { m.Version = "" }, "version"},
		{"needs newer product", func(m *Manifest) { m.Requires.MinVersion = "9.0.0" }, "or later"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dirs := emptyDirs(t)
			m := sampleManifest("pack")
			tc.mutate(&m)
			writeExtension(t, dirs.Extensions, "pack", m)

			c := Load(dirs, "1.0.0")
			exts := c.Extensions()
			if len(exts) != 1 {
				t.Fatalf("expected the extension to be listed so its absence is explainable, got %d", len(exts))
			}
			if !strings.Contains(exts[0].Problem, tc.want) {
				t.Errorf("problem = %q, want something mentioning %q", exts[0].Problem, tc.want)
			}
		})
	}
}

func TestDisabledExtensionContributesNothing(t *testing.T) {
	dirs := emptyDirs(t)

	m := sampleManifest("acme-pack")
	m.Contributes.Skills = append(m.Contributes.Skills, struct {
		Dir  string `json:"dir"`
		Name string `json:"name"`
	}{Dir: "skills/acme-enquiry", Name: "acme-enquiry"})
	dir := writeExtension(t, dirs.Extensions, "acme-pack", m)
	writeSkill(t, filepath.Join(dir, "skills"), "acme-enquiry",
		"name: acme-enquiry\ndescription: x", "steps")

	disabled := filepath.Join(dirs.Extensions, DisabledFileName)
	if err := os.WriteFile(disabled, []byte("# off for now\nacme-pack\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := Load(dirs, "1.0.0")
	if _, ok := c.Skill("acme-enquiry"); ok {
		t.Error("a disabled extension still contributed a skill")
	}
	exts := c.Extensions()
	if len(exts) != 1 || !exts[0].Disabled {
		t.Errorf("the extension should still be listed as disabled, got %+v", exts)
	}
}

// TestLoadSessionDedup covers what actually enforces the context budget.
func TestLoadSessionDedup(t *testing.T) {
	s := NewLoadSession()

	if !s.FirstLoad("skill", "chaos-monkey") {
		t.Error("first load should report true")
	}
	if s.FirstLoad("skill", "chaos-monkey") {
		t.Error("second load of the same skill should report false")
	}
	if s.FirstLoad("skill", "CHAOS-MONKEY") {
		t.Error("case should not defeat the dedup")
	}
	if !s.FirstLoad("instruction", "chaos-monkey") {
		t.Error("a different kind with the same name is a different entry")
	}

	s.Reset()
	if !s.FirstLoad("skill", "chaos-monkey") {
		t.Error("after Reset the conversation starts clean")
	}

	var nilSession *LoadSession
	if !nilSession.FirstLoad("skill", "x") {
		t.Error("a nil session should always allow the load")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.2.0", "1.10.0", -1},
		{"2.0", "1.9.9", 1},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.0-beta", "1.0.0", 0},
		{"1.0", "1.0.0", 0},
	}
	for _, tc := range cases {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
