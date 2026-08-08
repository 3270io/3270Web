package task

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestToolNameFlattensProseToAnIdentifier(t *testing.T) {
	cases := map[string]string{
		"Account balance enquiry": "task_account_balance_enquiry",
		"Check  Balance":          "task_check_balance",
		"check-balance":           "task_check_balance",
		"  Padded  ":              "task_padded",
		"CICS: SIGN-ON (TSO)":     "task_cics_sign_on_tso",
		"Order #42":               "task_order_42",
	}
	for in, want := range cases {
		if got := ToolName(in); got != want {
			t.Errorf("ToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestToolNameProducesValidMCPNames guards the character set the protocol
// allows. A name the SDK rejects would drop the tool with a log line nobody
// reads.
func TestToolNameProducesValidMCPNames(t *testing.T) {
	valid := regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	names := []string{
		"Account balance enquiry", "Ärendehantering", "查询余额",
		"a/b\\c", "task with (brackets) & symbols!", strings.Repeat("x", MaxNameLength),
	}
	for _, name := range names {
		got := ToolName(name)
		if got == "" {
			continue // rejected by ToolNames, which is the documented outcome
		}
		if !valid.MatchString(got) {
			t.Errorf("ToolName(%q) = %q, which is not a valid MCP tool name", name, got)
		}
		if len(got) > 128 {
			t.Errorf("ToolName(%q) is %d bytes, over the 128 the protocol allows", name, len(got))
		}
	}
}

// TestToolNamesReportsWhatItDropped is the point of returning two maps: a task
// that cannot be offered has to say so, because "the model says that task does
// not exist" is otherwise unexplainable from the catalogue.
func TestToolNamesReportsWhatItDropped(t *testing.T) {
	tasks := []Task{
		{Name: "Check balance"},
		{Name: "check-balance"}, // same tool name as the above
		{Name: "查询"},            // nothing to build an identifier from
	}
	byTool, skipped := ToolNames(tasks)

	if _, ok := byTool["task_check_balance"]; !ok {
		t.Fatalf("expected task_check_balance to be offered, got %v", keysOf(byTool))
	}
	if len(byTool) != 1 {
		t.Errorf("expected exactly one tool, got %v", keysOf(byTool))
	}
	if reason, ok := skipped["check-balance"]; !ok || !strings.Contains(reason, "task_check_balance") {
		t.Errorf("the collision should be reported and name the tool it collided on, got %q", reason)
	}
	if _, ok := skipped["查询"]; !ok {
		t.Error("a name with no identifier characters should be reported as skipped")
	}

	// Sorted resolution: whichever way the catalogue is ordered, the same task
	// keeps the name.
	reversed, _ := ToolNames([]Task{{Name: "check-balance"}, {Name: "Check balance"}})
	if !reflect.DeepEqual(keysOf(byTool), keysOf(reversed)) {
		t.Error("collision resolution depends on catalogue order")
	}
	if reversed["task_check_balance"].Name != byTool["task_check_balance"].Name {
		t.Errorf("collision resolved differently by order: %q vs %q",
			reversed["task_check_balance"].Name, byTool["task_check_balance"].Name)
	}
}

// TestInputSchemaMirrorsResolveParameters is the check that keeps the schema a
// projection rather than a second, drifting definition of the same rules.
func TestInputSchemaMirrorsResolveParameters(t *testing.T) {
	task := Task{
		Name: "Mixed",
		Parameters: []Parameter{
			{Name: "required_plain", Required: true},
			{Name: "required_with_default", Required: true, Default: "ACCT"},
			{Name: "optional_bounded", MaxLength: 8},
			{Name: "patterned", Pattern: `\d{4}`},
			{Name: "secret", Sensitive: true, Required: true},
		},
		Steps: []Step{{
			Inputs: []Input{
				{Row: 1, Column: 1, Parameter: "required_plain"},
				{Row: 2, Column: 1, Parameter: "required_with_default"},
				{Row: 3, Column: 1, Parameter: "optional_bounded"},
				{Row: 4, Column: 1, Parameter: "patterned"},
				{Row: 5, Column: 1, Parameter: "secret"},
			},
		}},
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("fixture does not validate: %v", err)
	}

	schema := task.InputSchema()
	props, _ := schema["properties"].(map[string]any)
	if len(props) != len(task.Parameters) {
		t.Fatalf("schema has %d properties for %d parameters", len(props), len(task.Parameters))
	}
	if schema["additionalProperties"] != false {
		t.Error("the schema must refuse undeclared parameters, as ResolveParameters does")
	}

	required, _ := schema["required"].([]string)
	wantRequired := []string{"required_plain", "secret"}
	if !reflect.DeepEqual(required, wantRequired) {
		t.Errorf("required = %v, want %v — a parameter with a default is satisfiable by omission", required, wantRequired)
	}
	// And prove that claim against the gate itself.
	if _, err := task.ResolveParameters(map[string]string{
		"required_plain": "x", "secret": "y",
	}); err != nil {
		t.Errorf("omitting a defaulted required parameter should be accepted, got %v", err)
	}

	bounded, _ := props["optional_bounded"].(map[string]any)
	if bounded["maxLength"] != 8 {
		t.Errorf("maxLength = %v, want 8", bounded["maxLength"])
	}
	patterned, _ := props["patterned"].(map[string]any)
	if got := patterned["pattern"]; got != `^(?:\d{4})$` {
		t.Errorf("pattern = %v, want the anchored form ResolveParameters applies", got)
	}
	secret, _ := props["secret"].(map[string]any)
	if secret["writeOnly"] != true {
		t.Error("a sensitive parameter should be marked writeOnly")
	}
	if desc, _ := secret["description"].(string); !strings.Contains(desc, "Do not repeat it back") {
		t.Errorf("a sensitive parameter's description should say so, got %q", desc)
	}
}

// TestInputSchemaAgreesWithTheGate walks concrete values through both sides.
// Anything the schema's own constraints reject must be rejected by
// ResolveParameters, and anything it accepts must be accepted — that
// equivalence is the whole claim the schema makes to a client.
func TestInputSchemaAgreesWithTheGate(t *testing.T) {
	task := sampleTask()
	if err := task.Validate(); err != nil {
		t.Fatalf("fixture does not validate: %v", err)
	}
	props, _ := task.InputSchema()["properties"].(map[string]any)
	account, _ := props["account_number"].(map[string]any)
	pattern := regexp.MustCompile(account["pattern"].(string))
	maxLen, _ := account["maxLength"].(int)

	for _, value := range []string{"12345678", "1234567", "abcdefgh", "123456789", ""} {
		schemaOK := pattern.MatchString(value) && (maxLen == 0 || len(value) <= maxLen) && value != ""
		_, err := task.ResolveParameters(map[string]string{"account_number": value})
		if gateOK := err == nil; gateOK != schemaOK {
			t.Errorf("value %q: schema accepts=%v but ResolveParameters accepts=%v (%v)",
				value, schemaOK, gateOK, err)
		}
	}
}

// TestInputSchemaIsSerialisable — it is handed to the MCP SDK as `any` and
// marshalled, so a value that will not encode fails at call time rather than
// here.
func TestInputSchemaIsSerialisable(t *testing.T) {
	task := sampleTask()
	encoded, err := json.Marshal(task.InputSchema())
	if err != nil {
		t.Fatalf("schema does not marshal: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(encoded, &round); err != nil {
		t.Fatalf("schema does not round-trip: %v", err)
	}
	if round["type"] != "object" {
		t.Errorf(`type = %v, want "object" — the SDK panics on anything else`, round["type"])
	}
}

func TestToolDescriptionNamesTheOutputs(t *testing.T) {
	got := sampleTask().ToolDescription()
	for _, want := range []string{"Account balance enquiry", "cleared balance", "Cleared balance"} {
		if !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
			t.Errorf("description should mention %q, got %q", want, got)
		}
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
