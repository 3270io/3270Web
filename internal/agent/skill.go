// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agent implements the skills, instructions and extensions that
// shape how an AI assistant drives 3270Web.
//
// A skill is a playbook: prose telling the model how to carry out a piece of
// work — explore an unknown application, build a business model of it, run a
// named business function — written as markdown with a small frontmatter
// block. An instruction is a policy fragment several skills share.
//
// The point of putting them on disk rather than in a string constant is
// two-level disclosure. The system prompt carries an *index* of skills: a
// name and one line each. The model calls LoadSkill when it decides it needs
// one, and LoadInstruction for the fragments that skill cites. A prompt that
// inlined every playbook would spend its budget on advice for work the user
// did not ask for, and would grow with each skill added.
//
// The same structure is what makes the assistant extendable. A site that
// knows its own application can drop a skill beside the binary — or ship a
// folder of them as an extension — and it appears in the index next to the
// built-ins, with its origin always visible.
package agent

import (
	"fmt"
	"regexp"
	"strings"
)

// SourceKind says where a catalogue entry came from.
type SourceKind string

const (
	// SourceBuiltin is compiled into the binary.
	SourceBuiltin SourceKind = "builtin"
	// SourceLocal is a loose file under the binary's skills/ or
	// instructions/ directory.
	SourceLocal SourceKind = "local"
	// SourceExtension came from an installed extension.
	SourceExtension SourceKind = "extension"
)

// Source identifies where an entry came from, so a catalogue listing can
// always say whose advice the model is about to follow.
type Source struct {
	Kind SourceKind
	// Extension names the extension when Kind is SourceExtension.
	Extension string
}

func (s Source) String() string {
	if s.Kind == SourceExtension && s.Extension != "" {
		return "extension:" + s.Extension
	}
	if s.Kind == "" {
		return string(SourceBuiltin)
	}
	return string(s.Kind)
}

// Skill is one playbook.
type Skill struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Invocation  []string `json:"invocation,omitempty"`
	// Tools names the tools the skill needs. It is advisory — the tier gate
	// is what actually decides whether a tool can be called — but it lets a
	// listing say up front that a skill will not be able to finish.
	Tools []string `json:"tools,omitempty"`
	// Instructions are the fragments this skill declares in frontmatter.
	// Refs found in the body are merged into this list when the skill loads.
	Instructions []string `json:"instructions,omitempty"`
	// Overrides names a built-in this skill deliberately replaces. Without
	// it a same-named skill loses to the built-in.
	Overrides string `json:"overrides,omitempty"`

	Source Source `json:"-"`
	// SourceLabel is the JSON-visible rendering of Source.
	SourceLabel string `json:"source"`

	// body is the markdown after the frontmatter block.
	body string
}

// Body returns the skill's markdown, frontmatter stripped.
func (s *Skill) Body() string { return s.body }

// nameRE is the shape a skill or instruction name may take. It is checked
// rather than sanitised: a name is only ever matched against the discovered
// catalogue, never used to build a path, so a rejected name is a
// well-formedness complaint and not a security boundary.
var nameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidSkillName reports whether name is well-formed.
func ValidSkillName(name string) bool { return nameRE.MatchString(name) }

// maxDescription bounds the one-line description, since every skill's
// description appears in the system prompt whether or not it is ever used.
const maxDescription = 1024

// instructionRefRE finds instruction fragments cited in a skill body as
// `something.instructions.md`. The directory prefix is optional so a skill
// shipped in an extension can cite a fragment by bare basename.
var instructionRefRE = regexp.MustCompile("`((?:instructions/)?[A-Za-z0-9._-]+\\.instructions\\.md)`")

// ParseSkill reads a SKILL.md. It returns an error only for problems that
// make the skill unusable: a missing or malformed name, or no description.
// Everything else is optional, because a site writing its first skill should
// not have to satisfy the contract the built-ins hold themselves to.
func ParseSkill(raw string, src Source) (*Skill, error) {
	fm, body := splitFrontmatter(raw)

	s := &Skill{
		Name:         fm.value("name"),
		Description:  fm.value("description"),
		Invocation:   fm.list("invocation"),
		Tools:        fm.list("tools"),
		Instructions: fm.list("instructions"),
		Overrides:    fm.value("overrides"),
		Source:       src,
		SourceLabel:  src.String(),
		body:         body,
	}

	if s.Name == "" {
		return nil, fmt.Errorf("skill has no name in its frontmatter")
	}
	if !ValidSkillName(s.Name) {
		return nil, fmt.Errorf("skill name %q must be lowercase words joined by hyphens", s.Name)
	}
	if s.Description == "" {
		return nil, fmt.Errorf("skill %q has no description", s.Name)
	}
	if len(s.Description) > maxDescription {
		return nil, fmt.Errorf("skill %q description is %d characters, limit is %d",
			s.Name, len(s.Description), maxDescription)
	}

	s.Instructions = mergeRefs(s.Instructions, bodyInstructionRefs(body))
	return s, nil
}

// bodyInstructionRefs returns the instruction fragments a body cites in
// backticks, so a skill that mentions a fragment in prose does not also have
// to list it in frontmatter.
func bodyInstructionRefs(body string) []string {
	var out []string
	for _, m := range instructionRefRE.FindAllStringSubmatch(body, -1) {
		out = append(out, strings.TrimPrefix(m[1], "instructions/"))
	}
	return out
}

// mergeRefs concatenates instruction references, normalising each to a bare
// basename with the .instructions.md suffix and dropping duplicates.
func mergeRefs(lists ...[]string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, list := range lists {
		for _, ref := range list {
			ref = NormalizeInstructionName(ref)
			if ref == "" || seen[ref] {
				continue
			}
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}

// NormalizeInstructionName turns any spelling of an instruction reference
// into the basename the catalogue is keyed by.
func NormalizeInstructionName(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.Trim(ref, "`")
	if i := strings.LastIndexAny(ref, `/\`); i >= 0 {
		ref = ref[i+1:]
	}
	if ref == "" {
		return ""
	}
	if !strings.HasSuffix(ref, ".instructions.md") {
		ref += ".instructions.md"
	}
	return ref
}

// NormalizeSkillName tolerates the spellings a model reaches for: a path, a
// trailing /SKILL.md, stray whitespace or quoting. The result is only ever
// looked up in the catalogue, never joined onto a directory.
func NormalizeSkillName(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, "`\"'")
	s = strings.TrimSuffix(s, "/SKILL.md")
	s = strings.TrimSuffix(s, `\SKILL.md`)
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(s))
}
