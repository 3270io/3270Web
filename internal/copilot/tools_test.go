package copilot

import (
	"strings"
	"testing"
)

// businessTools are the tools that power the business-understanding skill.
var businessTools = []string{
	"chaos_list_screens",
	"chaos_annotate_screen",
	"business_list_functions",
	"business_save_function",
	"business_generate_workflow",
}

func TestDefaultToolsIncludesBusinessTools(t *testing.T) {
	names := make(map[string]bool)
	for _, tool := range DefaultTools() {
		if tool.Type != "function" {
			t.Fatalf("tool %q has type %q, want function", tool.Function.Name, tool.Type)
		}
		if tool.Function.Name == "" || tool.Function.Description == "" || tool.Function.Parameters == nil {
			t.Fatalf("tool %+v is missing name/description/parameters", tool.Function)
		}
		if names[tool.Function.Name] {
			t.Fatalf("duplicate tool name %q", tool.Function.Name)
		}
		names[tool.Function.Name] = true
	}
	for _, want := range businessTools {
		if !names[want] {
			t.Fatalf("DefaultTools missing %q", want)
		}
	}
}

// The system prompt must mention every tool it expects the model to call, so
// prompt and tool list cannot drift apart silently.
func TestDefaultSystemPromptMentionsAllTools(t *testing.T) {
	for _, tool := range DefaultTools() {
		if !strings.Contains(DefaultSystemPrompt, tool.Function.Name) {
			t.Fatalf("DefaultSystemPrompt does not mention tool %q", tool.Function.Name)
		}
	}
}

// The procedures the assistant follows now live in skill files rather than in
// this prompt, so what the prompt owes the model is the route to them. That
// the individual playbooks still exist is asserted in internal/agent, where
// they live.
func TestDefaultSystemPromptRoutesToSkills(t *testing.T) {
	for _, want := range []string{"list_skills", "load_skill", "load_instruction"} {
		if !strings.Contains(DefaultSystemPrompt, want) {
			t.Errorf("the prompt must tell the model how to reach its procedures; %q is missing", want)
		}
	}
}

// SystemPrompt composes the core prompt with a generated skill index. With no
// provider installed it must still return something usable, because a caller
// that never wired one up should get a working assistant rather than an empty
// prompt.
func TestSystemPromptComposition(t *testing.T) {
	t.Cleanup(func() { SetSkillIndex(nil) })

	SetSkillIndex(nil)
	if got := SystemPrompt(); got != DefaultSystemPrompt {
		t.Error("with no index provider, SystemPrompt should be the core prompt")
	}

	SetSkillIndex(func() string { return "   " })
	if got := SystemPrompt(); got != DefaultSystemPrompt {
		t.Error("an empty index should not add a trailing section")
	}

	SetSkillIndex(func() string { return "## Available skills\n\n- chaos-monkey — explore" })
	got := SystemPrompt()
	if !strings.Contains(got, DefaultSystemPrompt) || !strings.Contains(got, "chaos-monkey") {
		t.Errorf("SystemPrompt should carry both the core prompt and the index, got:\n%s", got)
	}
}

func findTool(t *testing.T, name string) Tool {
	t.Helper()
	for _, tool := range DefaultTools() {
		if tool.Function.Name == name {
			return tool
		}
	}
	t.Fatalf("DefaultTools missing tool %q", name)
	return Tool{}
}

func requiredParams(t *testing.T, tool Tool) []string {
	t.Helper()
	req, _ := tool.Function.Parameters["required"].([]string)
	return req
}

func containsStr(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// TestConnectSessionMoveCursorWaitForUnlockTools guards the three new tools
// added so a chat request doesn't dead-end when nothing is connected
// (connect_session), the model has no way to position the cursor ahead of a
// cursor-relative action (move_cursor), and a multi-second transaction's
// result isn't visible to the very next get_screen because nothing polled
// for the host to actually finish (wait_for_unlock).
func TestConnectSessionMoveCursorWaitForUnlockTools(t *testing.T) {
	connect := findTool(t, "connect_session")
	if !containsStr(requiredParams(t, connect), "hostname") {
		t.Errorf("connect_session must require hostname, got required=%v", requiredParams(t, connect))
	}

	cursor := findTool(t, "move_cursor")
	req := requiredParams(t, cursor)
	if !containsStr(req, "row") || !containsStr(req, "col") {
		t.Errorf("move_cursor must require row and col, got required=%v", req)
	}

	wait := findTool(t, "wait_for_unlock")
	props, _ := wait.Function.Parameters["properties"].(map[string]any)
	if _, ok := props["timeout_ms"]; !ok {
		t.Error("wait_for_unlock must expose a timeout_ms parameter")
	}
}

// TestAskUserSupportsFreeText guards the fix for ask_user only ever
// offering fixed buttons: the system prompt directs the model to collect
// free-form parameters (e.g. an account number) through ask_user, which the
// schema had no way to express. options must no longer be required (a
// pure-free-text ask_user has no options at all) and allow_free_text must
// exist.
func TestAskUserSupportsFreeText(t *testing.T) {
	askUser := findTool(t, "ask_user")
	req := requiredParams(t, askUser)
	if containsStr(req, "options") {
		t.Errorf("ask_user's options must not be required (a free-text-only question has none), got required=%v", req)
	}
	if !containsStr(req, "question") {
		t.Errorf("ask_user must still require question, got required=%v", req)
	}
	props, _ := askUser.Function.Parameters["properties"].(map[string]any)
	if _, ok := props["allow_free_text"]; !ok {
		t.Error("ask_user must expose an allow_free_text parameter")
	}
}
