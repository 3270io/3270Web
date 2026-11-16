// SPDX-License-Identifier: AGPL-3.0-or-later

package copilot

import (
	"strconv"
	"strings"
	"testing"
)

// Every tool has a class, and there is no class for a tool that does not
// exist. A tool added without one would be treated as whatever the panel
// assumes about a name it has never heard of.
func TestEveryToolHasAClass(t *testing.T) {
	classes := ToolClasses()
	declared := map[string]bool{}
	for _, tool := range DefaultTools() {
		name := tool.Function.Name
		declared[name] = true
		switch classes[name] {
		case ToolClassRead, ToolClassWrite, ToolClassDestructive:
		default:
			t.Errorf("tool %q has class %q; add it to toolClasses", name, classes[name])
		}
	}
	for name := range classes {
		if !declared[name] {
			t.Errorf("toolClasses names %q, which is not a tool", name)
		}
	}
}

func TestTheDestructiveToolsAreTheOnesThatEndReplaceOrAutomate(t *testing.T) {
	classes := ToolClasses()
	for _, name := range []string{"connect_session", "chaos_start", "chaos_resume", "run_task", "snapshot_delete", "printer_stop"} {
		if classes[name] != ToolClassDestructive {
			t.Errorf("%s is %q, want destructive", name, classes[name])
		}
	}
	for _, name := range []string{"get_screen", "chaos_get_screen", "chaos_list_screens", "list_skills", "load_skill", "wait_for_unlock", "snapshot_diff"} {
		if classes[name] != ToolClassRead {
			t.Errorf("%s is %q, want read", name, classes[name])
		}
	}
	for _, name := range []string{"write_field", "submit_screen", "move_cursor", "chaos_annotate_screen"} {
		if classes[name] != ToolClassWrite {
			t.Errorf("%s is %q, want write", name, classes[name])
		}
	}
}

func TestAKeyPressIsClassifiedByItsKey(t *testing.T) {
	for _, key := range []string{"PF3", "pf(3)", "F3", "f03", "Clear"} {
		if got := ClassifyToolCall("send_key", map[string]any{"key": key}); got != ToolClassDestructive {
			t.Errorf("send_key %q is %q, want destructive", key, got)
		}
	}
	for _, key := range []string{"Enter", "PF7", "Tab", "Reset", ""} {
		if got := ClassifyToolCall("send_key", map[string]any{"key": key}); got != ToolClassWrite {
			t.Errorf("send_key %q is %q, want write", key, got)
		}
	}
	if got := ClassifyToolCall("submit_screen", map[string]any{"aid": "F3"}); got != ToolClassDestructive {
		t.Errorf("submit_screen with F3 is %q, want destructive", got)
	}
	if got := ClassifyToolCall("submit_screen", nil); got != ToolClassWrite {
		t.Errorf("submit_screen is %q, want write", got)
	}
	if got := ClassifyToolCall("no_such_tool", nil); got != ToolClassDestructive {
		t.Errorf("an unknown tool is %q; something nobody classified must not run unattended", got)
	}
}

func TestToolClassesIsACopy(t *testing.T) {
	ToolClasses()["chaos_start"] = ToolClassRead
	if ToolClasses()["chaos_start"] != ToolClassDestructive {
		t.Error("a caller changed the policy through the returned map")
	}
}

// --- the prompt and the schemas ---------------------------------------------

// The prompt told the model it had 30 rounds while the panel enforced 150.
func TestThePromptStatesTheRealRoundCeiling(t *testing.T) {
	if strconv.Itoa(MaxToolRounds) != maxToolRoundsText {
		t.Fatalf("MaxToolRounds is %d but the prompt quotes %q", MaxToolRounds, maxToolRoundsText)
	}
	if !strings.Contains(DefaultSystemPrompt, "after "+maxToolRoundsText+" tool-call rounds") {
		t.Error("the prompt does not state the round ceiling")
	}
	if strings.Contains(DefaultSystemPrompt, "30 tool-call rounds") {
		t.Error("the prompt still claims a budget of 30 rounds")
	}
}

// Tab, Home and EraseEOF do not transmit. Calling them AID keys taught the
// model that pressing Tab talks to the host.
func TestOnlyAIDKeysAreCalledAIDKeys(t *testing.T) {
	sendKey := findTool(t, "send_key").Function.Description
	if strings.Contains(sendKey, "Send an AID key") {
		t.Error("send_key still describes every key as an AID key")
	}
	for _, want := range []string{"AID keys — Enter, PF1..PF24, PA1..PA3, Clear", "act on the terminal only"} {
		if !strings.Contains(sendKey, want) {
			t.Errorf("send_key's description should distinguish AID keys from local ones; %q is missing", want)
		}
	}
	if !strings.Contains(DefaultSystemPrompt, "AID keys transmit; other keys do not") {
		t.Error("the prompt should say which keys transmit")
	}
}

// send_key and submit_screen wait for the host themselves. Telling the model
// to follow each with wait_for_unlock doubled the rounds for nothing.
func TestWaitForUnlockIsNotRecommendedAfterEveryAction(t *testing.T) {
	for _, stale := range []string{
		"call wait_for_unlock before your next get_screen",
		"Call this after send_key/submit_screen for actions that take noticeable time",
	} {
		if strings.Contains(DefaultSystemPrompt, stale) || strings.Contains(findTool(t, "wait_for_unlock").Function.Description, stale) {
			t.Errorf("still recommends wait_for_unlock after an action that already waits: %q", stale)
		}
	}
	if !strings.Contains(findTool(t, "wait_for_unlock").Function.Description, "already wait") {
		t.Error("wait_for_unlock should say the driving tools already wait")
	}
}

func TestThePromptCoversWhatAModelCannotSee(t *testing.T) {
	for topic, want := range map[string]string{
		"checking the outcome of an AID key": "Check the outcome after every AID key",
		"recovering a locked keyboard":       "send the Reset key",
		"field length":                       "Fields have a fixed length",
		"autoskip":                           "autoskip",
		"clearing a field":                   "write an empty string",
		"0-indexed coordinates":              "0-indexed",
		"the 1-indexed status bar":           "count from 1",
		"never handling passwords":           "Never ask the user to tell you a password",
		"hidden fields are masked":           "its value is masked",
		"screenHash":                         "screenHash",
		"knownScreen":                        "knownScreen",
		"chaos_get_screen":                   "chaos_get_screen",
		"discovery types into a live host":   "types into a live host unattended",
		"blocked keys before discovery":      "blocked_keys",
	} {
		if !strings.Contains(DefaultSystemPrompt, want) {
			t.Errorf("the prompt says nothing about %s (%q is missing)", topic, want)
		}
	}
}

func toolProperties(t *testing.T, name string) map[string]any {
	t.Helper()
	props, _ := findTool(t, name).Function.Parameters["properties"].(map[string]any)
	if props == nil {
		t.Fatalf("%s has no properties", name)
	}
	return props
}

func TestWriteFieldDescribesWhatItDoes(t *testing.T) {
	desc := findTool(t, "write_field").Function.Description
	if strings.Contains(desc, "buffered locally") {
		t.Error("write_field still claims the text is buffered locally; it goes into the terminal at once")
	}
	for _, want := range []string{"erase", "unprotected", "clears the field", "1-indexed", "0-indexed"} {
		if !strings.Contains(desc, want) {
			t.Errorf("write_field's description is missing %q", want)
		}
	}
	if _, ok := toolProperties(t, "write_field")["erase"]; !ok {
		t.Error("write_field must expose erase")
	}
}

// The schema advertised parameters the browser's dispatcher never forwarded,
// and left out one the server reads.
func TestSchemasMatchWhatIsHonoured(t *testing.T) {
	start := toolProperties(t, "chaos_start")
	for _, dropped := range []string{"auto_prefer_navigation_keys", "transition_log_path"} {
		if _, ok := start[dropped]; ok {
			t.Errorf("chaos_start still advertises %q", dropped)
		}
	}
	if _, ok := toolProperties(t, "chaos_save_screen_hint")["key_assignments"]; !ok {
		t.Error("chaos_save_screen_hint must expose key_assignments, which the server reads")
	}

	get := findTool(t, "chaos_get_screen")
	if !containsStr(requiredParams(t, get), "hash") {
		t.Errorf("chaos_get_screen must require hash, got %v", requiredParams(t, get))
	}
}

// get_screen's description promised labels before there were any.
func TestGetScreenDescribesWhatItReturns(t *testing.T) {
	desc := findTool(t, "get_screen").Function.Description
	for _, want := range []string{"label", "keyboard", "screenHash", "knownScreen", "0-indexed"} {
		if !strings.Contains(desc, want) {
			t.Errorf("get_screen's description is missing %q", want)
		}
	}
}

// The confirmation is a person's, so it is not something a model can supply.
func TestTheModelCannotConfirmItsOwnKeyPress(t *testing.T) {
	for _, name := range []string{"send_key", "submit_screen"} {
		props, _ := findTool(t, name).Function.Parameters["properties"].(map[string]any)
		if _, ok := props["confirmed"]; ok {
			t.Errorf("%s exposes \"confirmed\" to the model", name)
		}
	}
}
