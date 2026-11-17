// SPDX-License-Identifier: AGPL-3.0-or-later

package copilot

// What each tool can do to the session, in three words.
//
// The chat panel decides which calls to run unattended and which to put in
// front of a person. It used to make that decision from a list of its own,
// which drifted from the tools the moment either changed. The class is a
// property of the tool, so it is declared here beside the definitions and
// handed to the panel with them.

// ToolClass says how much a tool call can change.
type ToolClass string

const (
	// ToolClassRead observes. Nothing on the host, in the session or in what
	// has been recorded about the application is different afterwards.
	ToolClassRead ToolClass = "read"
	// ToolClassWrite changes something: a field, the cursor, the screen the
	// host shows next, a note or a hint. The session is still there afterwards
	// and a person watching can see what happened.
	ToolClassWrite ToolClass = "write"
	// ToolClassDestructive ends or replaces the session, discards something
	// held, or starts automation that then acts with nobody watching each
	// step.
	ToolClassDestructive ToolClass = "destructive"
)

// toolClasses is the class of every tool in DefaultTools. A test fails if a
// tool is added without one, so a new tool cannot arrive unclassified and be
// treated as whatever the panel's default happens to be.
var toolClasses = map[string]ToolClass{
	// Reading the screen and the session.
	"get_screen":             ToolClassRead,
	"wait_for_unlock":        ToolClassRead,
	"get_connection_details": ToolClassRead,
	"get_display_toggles":    ToolClassRead,
	"ask_user":               ToolClassRead,

	// Driving the screen. send_key is a write for most keys and destructive
	// for the dangerous ones; ClassifyToolCall draws that line.
	"send_key":      ToolClassWrite,
	"write_field":   ToolClassWrite,
	"submit_screen": ToolClassWrite,
	"move_cursor":   ToolClassWrite,

	// Replaces whatever the session was connected to.
	"connect_session": ToolClassDestructive,

	// Exploration: reading what it found.
	"chaos_status":          ToolClassRead,
	"chaos_report":          ToolClassRead,
	"chaos_insights":        ToolClassRead,
	"chaos_get_hints":       ToolClassRead,
	"chaos_list_screens":    ToolClassRead,
	"chaos_get_screen":      ToolClassRead,
	"chaos_export_workflow": ToolClassRead,
	// Exploration: starting it is handing the keyboard to something that
	// types unattended; stopping it is not.
	"chaos_start":            ToolClassDestructive,
	"chaos_resume":           ToolClassDestructive,
	"chaos_stop":             ToolClassWrite,
	"chaos_save_screen_hint": ToolClassWrite,
	"chaos_update_hints":     ToolClassWrite,
	"chaos_annotate_screen":  ToolClassWrite,

	// Business understanding.
	"business_app_overview":      ToolClassRead,
	"business_list_functions":    ToolClassRead,
	"business_save_function":     ToolClassWrite,
	"business_generate_workflow": ToolClassWrite,

	// Skills and instructions.
	"list_skills":       ToolClassRead,
	"load_skill":        ToolClassRead,
	"list_instructions": ToolClassRead,
	"load_instruction":  ToolClassRead,
	"list_extensions":   ToolClassRead,

	// Display, snapshots, printer, tasks.
	"set_display_toggle": ToolClassWrite,
	"snapshot_take":      ToolClassWrite,
	"snapshot_list":      ToolClassRead,
	"snapshot_diff":      ToolClassRead,
	"snapshot_delete":    ToolClassDestructive,
	"printer_status":     ToolClassRead,
	"printer_read_job":   ToolClassRead,
	"printer_start":      ToolClassWrite,
	"printer_stop":       ToolClassDestructive,
	"list_tasks":         ToolClassRead,
	// A task is a recorded sequence of writes and keys that runs to its end.
	"run_task": ToolClassDestructive,
}

// ToolClasses returns the class of every tool, by name.
func ToolClasses() map[string]ToolClass {
	out := make(map[string]ToolClass, len(toolClasses))
	for name, class := range toolClasses {
		out[name] = class
	}
	return out
}

// ClassifyToolCall returns the class of one call, which for most tools is the
// tool's class and for a key press depends on the key: PF3 and Clear end
// sessions and discard work, in any spelling the server accepts.
//
// A tool with no class is destructive. Something nobody classified is not
// something to run unattended.
func ClassifyToolCall(toolName string, args map[string]any) ToolClass {
	class, ok := toolClasses[toolName]
	if !ok {
		return ToolClassDestructive
	}
	switch toolName {
	case "send_key":
		if key, _ := args["key"].(string); IsDangerousKey(key) {
			return ToolClassDestructive
		}
	case "submit_screen":
		if aid, _ := args["aid"].(string); IsDangerousKey(aid) {
			return ToolClassDestructive
		}
	}
	return class
}
