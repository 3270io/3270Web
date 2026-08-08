package mcptools

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/copilot"
)

// TestRegistryCoversTheDeclaredCatalogue is the anti-drift guard. init()
// panics on a mismatch, so reaching this test at all means the join held;
// what it adds is a readable statement of the invariant and of the one tool
// deliberately left out.
func TestRegistryCoversTheDeclaredCatalogue(t *testing.T) {
	declared := map[string]bool{}
	for _, d := range copilot.DefaultTools() {
		declared[d.Function.Name] = true
	}

	bound := map[string]bool{}
	for _, tool := range All() {
		bound[tool.Name] = true
		if !declared[tool.Name] {
			t.Errorf("tool %q is bound but never offered to the model", tool.Name)
		}
	}

	for name := range declared {
		if _, skipped := omitted[name]; skipped {
			if bound[name] {
				t.Errorf("tool %q is listed as omitted but is also bound", name)
			}
			continue
		}
		if !bound[name] {
			t.Errorf("tool %q is offered to the model but has no binding", name)
		}
	}

	// Every omission must be justified in one place, so "why can't it do
	// that?" has an answer that is not archaeology.
	for name, reason := range Omitted() {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("tool %q is omitted with no reason given", name)
		}
		if !declared[name] {
			t.Errorf("tool %q is listed as omitted but is not declared at all", name)
		}
	}
}

// TestSchemasComeFromTheDeclaration pins that this package adds no second
// schema. The hand-written ones carry guidance a generated schema would not
// — which field key format to pass through untouched, option-count bounds —
// and a copy here would drift from them silently.
func TestSchemasComeFromTheDeclaration(t *testing.T) {
	declared := map[string]map[string]any{}
	for _, d := range copilot.DefaultTools() {
		declared[d.Function.Name] = d.Function.Parameters
	}

	for _, tool := range All() {
		want := declared[tool.Name]
		gotJSON, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Errorf("tool %q: schema does not marshal: %v", tool.Name, err)
			continue
		}
		wantJSON, err := json.Marshal(want)
		if err != nil {
			t.Errorf("tool %q: declared schema does not marshal: %v", tool.Name, err)
			continue
		}
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("tool %q: schema differs from the declaration\n got: %s\nwant: %s",
				tool.Name, gotJSON, wantJSON)
		}
		if tool.Description == "" {
			t.Errorf("tool %q: empty description", tool.Name)
		}
		if tool.Handle == nil {
			t.Errorf("tool %q: no handler", tool.Name)
		}
	}
}

// TestTiersAreNested checks the containment the setting implies. A user who
// picks a lower tier expecting fewer capabilities must not get a different
// set of them.
func TestTiersAreNested(t *testing.T) {
	names := func(tier Tier) []string {
		var out []string
		for _, tool := range Enabled(tier) {
			out = append(out, tool.Name)
		}
		sort.Strings(out)
		return out
	}

	read, interact, full := names(TierRead), names(TierInteract), names(TierChaos)

	if len(read) == 0 {
		t.Fatal("the read tier is empty; a client with no write permission could do nothing at all")
	}
	contains := func(set []string, name string) bool {
		for _, s := range set {
			if s == name {
				return true
			}
		}
		return false
	}
	for _, name := range read {
		if !contains(interact, name) {
			t.Errorf("%q is in readonly but not in interactive", name)
		}
	}
	for _, name := range interact {
		if !contains(full, name) {
			t.Errorf("%q is in interactive but not in full", name)
		}
	}
	if len(full) <= len(interact) {
		t.Error("the full tier should add the exploration tools")
	}
}

// TestReadTierChangesNothing is the property that makes the default safe to
// hand out: nothing at the read tier can alter the host.
func TestReadTierChangesNothing(t *testing.T) {
	for _, tool := range Enabled(TierRead) {
		if tool.Destructive {
			t.Errorf("tool %q is in the readonly tier but is marked destructive", tool.Name)
		}
		if !tool.ReadOnly() {
			t.Errorf("tool %q is in the readonly tier but does not report as read-only", tool.Name)
		}
	}
}

// TestChaosToolsAreNotAvailableBelowTheirTier states the specific thing the
// tiering exists for. Exploration presses keys nobody chose, unattended.
func TestChaosToolsAreNotAvailableBelowTheirTier(t *testing.T) {
	for _, name := range []string{"chaos_start", "chaos_stop", "chaos_resume"} {
		tool, ok := Lookup(name)
		if !ok {
			t.Fatalf("%q is not bound", name)
		}
		if tool.Tier != TierChaos {
			t.Errorf("%q should be in the chaos tier, got %v", name, tool.Tier)
		}
	}
	for _, tool := range Enabled(TierInteract) {
		if strings.HasPrefix(tool.Name, "chaos_") && tool.Tier == TierChaos {
			t.Errorf("%q leaked into the interactive tier", tool.Name)
		}
	}
}

func TestParseTier(t *testing.T) {
	cases := map[string]Tier{
		"readonly": TierRead, "read-only": TierRead, "READ": TierRead,
		"": TierInteract, "interactive": TierInteract, " Interact ": TierInteract,
		"full": TierChaos, "chaos": TierChaos, "all": TierChaos,
	}
	for in, want := range cases {
		got, ok := ParseTier(in)
		if !ok || got != want {
			t.Errorf("ParseTier(%q) = %v/%v, want %v/true", in, got, ok, want)
		}
	}

	// An unrecognised value must not be treated as "everything", and must
	// say it was not understood so a typo is visible rather than silently
	// changing what a deployment can do.
	got, ok := ParseTier("ful")
	if ok {
		t.Error("ParseTier should report that an unknown value was not recognised")
	}
	if got != TierInteract {
		t.Errorf("an unrecognised tier should fall back to the default, got %v", got)
	}
}

// recorder is an Invoker that captures requests and replays canned replies.
type recorder struct {
	calls []string
	reply func(method, path string) Response
}

func (r *recorder) Do(_ context.Context, method, path string, query url.Values, _ any) (Response, error) {
	full := method + " " + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	r.calls = append(r.calls, full)
	if r.reply != nil {
		return r.reply(method, path), nil
	}
	return Response{Status: 200, Body: []byte(`{"ok":true}`)}, nil
}

func call(t *testing.T, name string, args map[string]any, rec *recorder) string {
	t.Helper()
	tool, ok := Lookup(name)
	if !ok {
		t.Fatalf("tool %q is not bound", name)
	}
	out, err := tool.Handle(Call{Ctx: context.Background(), Inv: rec, SessionID: "SESSION1", Args: args})
	if err != nil {
		t.Fatalf("tool %q: %v", name, err)
	}
	return out
}

func TestBindingsTargetTheExpectedRoutes(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"get_screen", nil, "GET /api/v1/sessions/SESSION1/screen.json"},
		{"move_cursor", map[string]any{"row": 4, "col": 9}, "POST /api/v1/sessions/SESSION1/cursor"},
		{"chaos_insights", nil, "GET /api/v1/sessions/SESSION1/chaos/insights"},
		{"chaos_status", map[string]any{"verbose": true}, "GET /api/v1/sessions/SESSION1/chaos/status?verbose=true"},
		{"list_skills", nil, "GET /api/v1/skills"},
		{"load_skill", map[string]any{"name": "chaos-monkey"}, "GET /api/v1/skills/load?name=chaos-monkey"},
		{"list_extensions", nil, "GET /api/v1/extensions"},
	}

	for _, tc := range cases {
		rec := &recorder{}
		call(t, tc.tool, tc.args, rec)
		if len(rec.calls) == 0 || rec.calls[0] != tc.want {
			t.Errorf("%s issued %v, want first call %q", tc.tool, rec.calls, tc.want)
		}
	}
}

// TestHostErrorsReachTheModel covers the choice to hand a failed request back
// as text. The handlers answer with a message that usually says what to do
// differently; converting it to a transport error would throw that away and
// leave the model retrying the same call.
func TestHostErrorsReachTheModel(t *testing.T) {
	rec := &recorder{reply: func(method, path string) Response {
		return Response{Status: 400, Body: []byte(`{"error":"unrecognized key \"PF25\""}`)}
	}}

	out := call(t, "send_key", map[string]any{"key": "PF25"}, rec)
	if !strings.Contains(out, "PF25") {
		t.Errorf("the rejected key should reach the model verbatim, got %q", out)
	}
	// It must not have gone on to report a screen as though the key landed.
	if strings.Contains(out, "Sent PF25") {
		t.Errorf("a rejected key must not be reported as sent, got %q", out)
	}
}

// TestActionsReportTheResultingScreen: "sent PF3" alone says nothing about
// where it landed, and the model's next decision depends on the screen.
func TestActionsReportTheResultingScreen(t *testing.T) {
	rec := &recorder{reply: func(method, path string) Response {
		if strings.HasSuffix(path, "/screen.json") {
			return Response{Status: 200, Body: []byte(`{"text":"MAIN MENU"}`)}
		}
		return Response{Status: 200, Body: []byte(`{"ok":true}`)}
	}}

	out := call(t, "send_key", map[string]any{"key": "PF3"}, rec)
	if !strings.Contains(out, "Sent PF3") || !strings.Contains(out, "MAIN MENU") {
		t.Errorf("send_key should report the action and the screen it produced, got %q", out)
	}
}

// TestWriteFieldForwardsFieldKeyUntouched: the key is 1-indexed and
// get_screen coordinates are 0-indexed. Converting in more than one place is
// how an off-by-one gets in, so the binding forwards whichever form the
// caller gave and lets the server resolve it.
func TestWriteFieldForwardsFieldKeyUntouched(t *testing.T) {
	var sent map[string]any
	rec := &recorder{}
	tool, _ := Lookup("write_field")
	inv := invokerFunc(func(_ context.Context, method, path string, _ url.Values, body any) (Response, error) {
		sent, _ = body.(map[string]any)
		return Response{Status: 200, Body: []byte(`{"ok":true}`)}, nil
	})
	_, err := tool.Handle(Call{Ctx: context.Background(), Inv: inv, SessionID: "S", Args: map[string]any{
		"field_key": "R5C10L8", "text": "1234",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if sent["field_key"] != "R5C10L8" {
		t.Errorf("field_key should be forwarded as-is, got %v", sent["field_key"])
	}
	if _, hasRow := sent["row"]; hasRow {
		t.Error("row/col must not be derived from field_key here")
	}

	// Without a field key, row/col are used instead.
	_, err = tool.Handle(Call{Ctx: context.Background(), Inv: inv, SessionID: "S", Args: map[string]any{
		"row": 4, "col": 9, "text": "x",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if sent["row"] != 4 || sent["col"] != 9 {
		t.Errorf("row/col should be forwarded, got %v", sent)
	}
	_ = rec
}

type invokerFunc func(context.Context, string, string, url.Values, any) (Response, error)

func (f invokerFunc) Do(ctx context.Context, method, path string, q url.Values, body any) (Response, error) {
	return f(ctx, method, path, q, body)
}
