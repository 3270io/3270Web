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
	if !strings.Contains(DefaultSystemPrompt, "Business Understanding Skill") {
		t.Fatal("DefaultSystemPrompt missing the Business Understanding Skill section")
	}
}
