package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/agent"
)

// The HTTP surface for skills, instructions and extensions.
//
// These back the list_skills / load_skill / list_instructions /
// load_instruction / list_extensions tools on both front ends: the browser
// chat panel calls them over these routes, and the MCP server calls the same
// routes under /api/v1. One implementation, so a skill the panel can reach is
// one an MCP client can reach.

// skillCatalogue returns the loaded catalogue, building it once.
//
// It is cached because a catalogue read walks the extensions directory and
// every skill file, and list_skills is called at the head of most
// conversations. An operator who installs an extension restarts 3270Web —
// which is also when a manifest's contributions are validated, so a
// half-copied pack cannot be picked up mid-conversation.
func (app *App) skillCatalogue() *agent.Catalogue {
	app.catalogueOnce.Do(func() {
		base := strings.TrimSpace(app.baseDir)
		if base == "" {
			base = resolveBaseDir()
		}
		app.catalogue = agent.Load(agent.DirsFor(base), appVersion)
		if problems := app.catalogue.Problems(); len(problems) > 0 {
			// Already logged individually by the catalogue; the count is
			// what an operator scanning startup output will notice.
			fmt.Printf("agent catalogue: %d entr%s could not be loaded\n",
				len(problems), pluralY(len(problems)))
		}
	})
	return app.catalogue
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// loadSessionFor returns the per-conversation load tracker for a session.
//
// Dedup is scoped to the terminal session rather than to the process: two
// browser tabs against different hosts are two conversations, and telling the
// second one it already has a skill it has never seen would be wrong.
func (app *App) loadSessionFor(sessionID string) *agent.LoadSession {
	app.loadSessionsMu.Lock()
	defer app.loadSessionsMu.Unlock()
	if app.loadSessions == nil {
		app.loadSessions = map[string]*agent.LoadSession{}
	}
	ls, ok := app.loadSessions[sessionID]
	if !ok {
		ls = agent.NewLoadSession()
		app.loadSessions[sessionID] = ls
	}
	return ls
}

// forgetLoadSession drops a conversation's tracker when its session ends, so
// the map does not grow for the life of the process.
func (app *App) forgetLoadSession(sessionID string) {
	app.loadSessionsMu.Lock()
	defer app.loadSessionsMu.Unlock()
	delete(app.loadSessions, sessionID)
}

// ConversationHeader lets a caller name the conversation a request belongs
// to, for the purpose of "have I already been given this?".
//
// The browser panel does not need it: one terminal session is one
// conversation, and the cookie already says which. A headless client has no
// cookie and its conversation is not tied to a session at all — it may read
// the catalogue before connecting anywhere, and keep the same conversation
// across several sessions. Without a way to say so, every load looks like a
// first load and the dedup silently does nothing on the surface where
// conversations are longest.
const ConversationHeader = "X-3270Web-Conversation"

// currentLoadSession resolves the tracker for a request: an explicit
// conversation header first, then the session cookie.
//
// A request with neither gets a throwaway tracker, so an anonymous caller is
// never refused a skill for want of an identifier — it just does not get
// deduplication, which is the right trade when we cannot tell one caller
// from another.
func (app *App) currentLoadSession(c *gin.Context) *agent.LoadSession {
	if id := strings.TrimSpace(c.GetHeader(ConversationHeader)); id != "" {
		return app.loadSessionFor("conversation:" + id)
	}
	if s := app.getSession(c); s != nil {
		return app.loadSessionFor(s.ID)
	}
	return agent.NewLoadSession()
}

// SkillsListHandler handles GET /skills — metadata only.
func (app *App) SkillsListHandler(c *gin.Context) {
	cat := app.skillCatalogue()

	skills := cat.Skills()
	out := make([]gin.H, 0, len(skills))
	for _, s := range skills {
		entry := gin.H{
			"name":        s.Name,
			"description": s.Description,
			"source":      s.SourceLabel,
		}
		if len(s.Invocation) > 0 {
			entry["invocation"] = s.Invocation
		}
		if prev, ok := cat.Shadowed(s.Name); ok {
			// Say so plainly. A model following a replaced playbook should
			// know it is not the one that ships with the product.
			entry["replaces"] = prev.String()
		}
		out = append(out, entry)
	}

	c.JSON(http.StatusOK, gin.H{"count": len(out), "skills": out})
}

// SkillLoadHandler handles GET /skills/load?name=… — the body plus the
// fragments it cites, which are named but deliberately not inlined.
func (app *App) SkillLoadHandler(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	cat := app.skillCatalogue()
	skill, ok := cat.Skill(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("no skill named %q; call list_skills to see what is available", name),
		})
		return
	}

	if !app.currentLoadSession(c).FirstLoad("skill", skill.Name) {
		c.JSON(http.StatusOK, gin.H{
			"name":         skill.Name,
			"already_load": true,
			"body": fmt.Sprintf(
				"[skill:%s] Already loaded in this conversation — you have it. Do not request it again.",
				skill.Name),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"name":             skill.Name,
		"description":      skill.Description,
		"source":           skill.SourceLabel,
		"body":             skill.Body(),
		"instruction_refs": skill.Instructions,
	})
}

// InstructionsListHandler handles GET /instructions.
func (app *App) InstructionsListHandler(c *gin.Context) {
	instructions := app.skillCatalogue().Instructions()
	out := make([]gin.H, 0, len(instructions))
	for _, i := range instructions {
		out = append(out, gin.H{
			"name":   i.Name,
			"title":  i.Title,
			"source": i.SourceLabel,
		})
	}
	c.JSON(http.StatusOK, gin.H{"count": len(out), "instructions": out})
}

// InstructionLoadHandler handles GET /instructions/load?name=….
func (app *App) InstructionLoadHandler(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	inst, ok := app.skillCatalogue().Instruction(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("no instruction named %q; call list_instructions to see what is available", name),
		})
		return
	}

	if !app.currentLoadSession(c).FirstLoad("instruction", inst.Name) {
		c.JSON(http.StatusOK, gin.H{
			"name":         inst.Name,
			"already_load": true,
			"body": fmt.Sprintf(
				"[instruction:%s] Already loaded in this conversation — you have it. Do not request it again.",
				inst.Name),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"name":   inst.Name,
		"title":  inst.Title,
		"source": inst.SourceLabel,
		"body":   inst.Body(),
	})
}

// ExtensionsListHandler handles GET /extensions, including packs that failed
// to load and why — the answer to "where did my skill go".
func (app *App) ExtensionsListHandler(c *gin.Context) {
	exts := app.skillCatalogue().Extensions()
	out := make([]gin.H, 0, len(exts))
	for _, e := range exts {
		entry := gin.H{
			"name":     e.Manifest.Name,
			"version":  e.Manifest.Version,
			"enabled":  !e.Disabled,
			"skills":   len(e.Manifest.Contributes.Skills),
			"loadable": e.Problem == "",
		}
		if e.Manifest.DisplayName != "" {
			entry["display_name"] = e.Manifest.DisplayName
		}
		if e.Manifest.Description != "" {
			entry["description"] = e.Manifest.Description
		}
		if e.Problem != "" {
			entry["problem"] = e.Problem
		}
		out = append(out, entry)
	}

	c.JSON(http.StatusOK, gin.H{
		"count":      len(out),
		"extensions": out,
		"problems":   app.skillCatalogue().Problems(),
	})
}

// skillIndexSection renders the available-skills block appended to the system
// prompt. Generated rather than hand-written, so a skill dropped in beside the
// binary is visible to the model without anyone editing a prompt.
func (app *App) skillIndexSection() string {
	cat := app.skillCatalogue()
	skills := cat.Skills()
	if len(skills) == 0 {
		return ""
	}

	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("\n## Available skills\n\n")
	for _, s := range skills {
		fmt.Fprintf(&b, "- **%s** — %s", s.Name, s.Description)
		if s.Source.Kind != agent.SourceBuiltin {
			fmt.Fprintf(&b, " _(%s)_", s.SourceLabel)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nCall load_skill with one of these names when the user asks for the work it covers.\n")
	return b.String()
}

// registerSkillRoutes wires the browser-facing surface.
func (app *App) registerSkillRoutes(r *gin.Engine) {
	r.GET("/skills", app.SkillsListHandler)
	r.GET("/skills/load", app.SkillLoadHandler)
	r.GET("/instructions", app.InstructionsListHandler)
	r.GET("/instructions/load", app.InstructionLoadHandler)
	r.GET("/extensions", app.ExtensionsListHandler)
}

// catalogueFields is embedded in App; kept here so the catalogue's state
// lives beside the code that uses it.
type catalogueFields struct {
	catalogueOnce  sync.Once
	catalogue      *agent.Catalogue
	loadSessionsMu sync.Mutex
	loadSessions   map[string]*agent.LoadSession
}
