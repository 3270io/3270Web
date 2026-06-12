package chaos

import (
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
)

func TestParseMindMapFieldKey(t *testing.T) {
	row, col, length, ok := parseMindMapFieldKey("R5C20L8")
	if !ok || row != 5 || col != 20 || length != 8 {
		t.Fatalf("parseMindMapFieldKey = (%d,%d,%d,%v), want (5,20,8,true)", row, col, length, ok)
	}
	for _, bad := range []string{"", "R0C1L1", "R1C1", "5,20,8", "RXC1L1"} {
		if _, _, _, ok := parseMindMapFieldKey(bad); ok {
			t.Fatalf("parseMindMapFieldKey(%q) unexpectedly ok", bad)
		}
	}
	// Round-trip with the writer.
	if key := mindMapFieldKey(3, 14, 10); key != "R3C14L10" {
		t.Fatalf("mindMapFieldKey = %q", key)
	}
}

func TestAnnotateMindMapAreaCreatesAndMerges(t *testing.T) {
	mm := newMindMap()
	err := annotateMindMapArea(mm, "hash1", "Customer inquiry", "needs login first", map[string]BusinessFieldSemantic{
		"R5C20L8": {Name: "account_number", Example: "1234"},
	})
	if err != nil {
		t.Fatalf("annotate: %v", err)
	}
	area := mm.Areas["hash1"]
	if area == nil || area.BusinessPurpose != "Customer inquiry" || area.BusinessNotes != "needs login first" {
		t.Fatalf("annotation not stored: %+v", area)
	}
	if got := area.FieldSemantics["R5C20L8"].Name; got != "account_number" {
		t.Fatalf("field semantic name = %q", got)
	}

	// Merge: empty purpose must not clobber, new field key must be added.
	err = annotateMindMapArea(mm, "hash1", "", "", map[string]BusinessFieldSemantic{
		"R7C20L2": {Name: "action_code"},
		"":        {Name: "ignored"},
		"R9C1L1":  {Name: ""}, // no name -> skipped
	})
	if err != nil {
		t.Fatalf("merge annotate: %v", err)
	}
	if area.BusinessPurpose != "Customer inquiry" {
		t.Fatalf("purpose clobbered: %q", area.BusinessPurpose)
	}
	if len(area.FieldSemantics) != 2 {
		t.Fatalf("FieldSemantics = %v, want 2 entries", area.FieldSemantics)
	}

	if err := annotateMindMapArea(mm, "  ", "x", "", nil); err == nil {
		t.Fatal("expected error for blank hash")
	}
}

func TestUpsertMindMapBusinessFunctionValidation(t *testing.T) {
	mm := newMindMap()

	if err := upsertMindMapBusinessFunction(mm, BusinessFunction{}); err == nil {
		t.Fatal("expected error for missing name")
	}
	if err := upsertMindMapBusinessFunction(mm, BusinessFunction{Name: "x"}); err == nil {
		t.Fatal("expected error for missing steps and entry screen")
	}
	if err := upsertMindMapBusinessFunction(mm, BusinessFunction{
		Name:  "x",
		Steps: []BusinessFunctionStep{{ScreenHash: "h", Inputs: []BusinessFunctionInput{{FieldKey: "bogus"}}}},
	}); err == nil {
		t.Fatal("expected error for invalid field key")
	}
	if err := upsertMindMapBusinessFunction(mm, BusinessFunction{
		Name:  "x",
		Steps: []BusinessFunctionStep{{ScreenHash: "h", Inputs: []BusinessFunctionInput{{FieldKey: "R1C1L4", Parameter: "nope"}}}},
	}); err == nil {
		t.Fatal("expected error for undefined parameter reference")
	}

	fn := BusinessFunction{
		Name:       "  Account   Inquiry ",
		Steps:      []BusinessFunctionStep{{ScreenHash: "h", Inputs: []BusinessFunctionInput{{FieldKey: "R1C1L4", Parameter: "acct"}}}},
		Parameters: []BusinessParameter{{Name: "acct", Required: true}},
	}
	if err := upsertMindMapBusinessFunction(mm, fn); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	stored := mm.BusinessFunctions["account inquiry"]
	if stored == nil {
		t.Fatalf("function not stored under normalized key; have %v", mm.BusinessFunctions)
	}
	if stored.Steps[0].AIDKey != "Enter" {
		t.Fatalf("AIDKey not defaulted: %q", stored.Steps[0].AIDKey)
	}
	if stored.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt not set")
	}
}

func TestMindMapCloneDeepCopiesBusinessData(t *testing.T) {
	mm := newMindMap()
	if err := annotateMindMapArea(mm, "h1", "Login", "", map[string]BusinessFieldSemantic{
		"R1C1L8": {Name: "user_id"},
	}); err != nil {
		t.Fatalf("annotate: %v", err)
	}
	if err := upsertMindMapBusinessFunction(mm, BusinessFunction{
		Name:       "Login",
		Steps:      []BusinessFunctionStep{{ScreenHash: "h1", AIDKey: "Enter", Inputs: []BusinessFunctionInput{{FieldKey: "R1C1L8", Parameter: "user"}}}},
		Parameters: []BusinessParameter{{Name: "user"}},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	cloned := mm.clone()
	if cloned == nil {
		t.Fatal("clone returned nil")
	}
	cloned.Areas["h1"].FieldSemantics["R1C1L8"] = BusinessFieldSemantic{Name: "mutated"}
	cloned.Areas["h1"].BusinessPurpose = "mutated"
	cloned.BusinessFunctions["login"].Steps[0].Inputs[0].Parameter = "mutated"
	cloned.BusinessFunctions["login"].Parameters[0].Name = "mutated"

	if mm.Areas["h1"].FieldSemantics["R1C1L8"].Name != "user_id" {
		t.Fatal("clone shares FieldSemantics map with original")
	}
	if mm.Areas["h1"].BusinessPurpose != "Login" {
		t.Fatal("clone shares area struct with original")
	}
	if mm.BusinessFunctions["login"].Steps[0].Inputs[0].Parameter != "user" {
		t.Fatal("clone shares function steps with original")
	}
	if mm.BusinessFunctions["login"].Parameters[0].Name != "user" {
		t.Fatal("clone shares function parameters with original")
	}

	// A mind map with only business functions (no areas) must still clone.
	fnOnly := newMindMap()
	if err := upsertMindMapBusinessFunction(fnOnly, BusinessFunction{Name: "x", EntryScreenHash: "h"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if fnOnly.clone() == nil {
		t.Fatal("clone dropped business-functions-only mind map")
	}
}

// buildBusinessTestMindMap builds a 3-screen map: menu --Enter--> form --PF5--> confirm,
// with an extra dead-end screen reachable from menu via PF9.
func buildBusinessTestMindMap(t *testing.T) *MindMap {
	t.Helper()
	mm := newMindMap()
	menu := mm.ensureArea("menu")
	menu.PreviewText = "MAIN MENU\n1. ACCOUNTS"
	menu.KeyPresses = map[string]*MindMapKeyPress{
		"Enter": {Presses: 3, Destinations: map[string]int{"form": 3}},
		"PF(9)": {Presses: 1, Destinations: map[string]int{"deadend": 1}},
	}
	form := mm.ensureArea("form")
	form.PreviewText = "ACCOUNT INQUIRY\nACCOUNT NO: ________"
	form.KeyPresses = map[string]*MindMapKeyPress{
		"PF(5)": {Presses: 2, Destinations: map[string]int{"confirm": 2}},
	}
	confirm := mm.ensureArea("confirm")
	confirm.PreviewText = "  INQUIRY RESULT\nBALANCE: 100.00"
	mm.ensureArea("deadend")
	return mm
}

func TestFindKeyPath(t *testing.T) {
	mm := buildBusinessTestMindMap(t)

	edges, ok := mm.findKeyPath("menu", "confirm")
	if !ok || len(edges) != 2 {
		t.Fatalf("findKeyPath = %v, %v; want 2 edges", edges, ok)
	}
	if edges[0].AIDKey != "Enter" || edges[0].To != "form" || edges[1].AIDKey != "PF(5)" || edges[1].To != "confirm" {
		t.Fatalf("unexpected path: %+v", edges)
	}

	if _, ok := mm.findKeyPath("confirm", "menu"); ok {
		t.Fatal("expected no reverse path")
	}
	if edges, ok := mm.findKeyPath("menu", "menu"); !ok || len(edges) != 0 {
		t.Fatalf("self path = %v, %v; want empty path, true", edges, ok)
	}
}

func TestGenerateBusinessWorkflow(t *testing.T) {
	mm := buildBusinessTestMindMap(t)
	err := upsertMindMapBusinessFunction(mm, BusinessFunction{
		Name:        "Account Inquiry",
		Description: "Look up an account balance",
		Steps: []BusinessFunctionStep{
			{ScreenHash: "menu", Inputs: []BusinessFunctionInput{{FieldKey: "R20C1L2", Value: "1"}}, AIDKey: "Enter", ExpectHash: "form"},
			{ScreenHash: "form", Inputs: []BusinessFunctionInput{{FieldKey: "R2C13L8", Parameter: "account_number"}}, AIDKey: "PF5", ExpectHash: "confirm"},
		},
		Parameters: []BusinessParameter{
			{Name: "account_number", ScreenHash: "form", FieldKey: "R2C13L8", Required: true},
			{Name: "comment", Required: false},
		},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	data, err := GenerateBusinessWorkflow(mm, "ACCOUNT INQUIRY", map[string]string{"account_number": "1234"}, "myhost", 23, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	text := string(data)

	for _, want := range []string{
		`"Name": "Account Inquiry"`,
		`"BusinessFunction": "Account Inquiry"`,
		`"Description": "Look up an account balance"`,
		`"Host": "myhost"`,
		`"Text": "1234"`,
		`"PressPF5"`,
		`"PressEnter"`,
		`"Connect"`,
		`"Disconnect"`,
		`"CheckValue"`,
		`"Text": "ACCOUNT"`, // CheckValue asserts the first text run of the form preview
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated workflow missing %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ChaosDiscovery") {
		t.Fatal("business workflow should not embed ChaosDiscovery")
	}
	// Parameter metadata records where the value went.
	if !strings.Contains(text, `"Value": "1234"`) || !strings.Contains(text, `"Row": 2`) {
		t.Fatalf("parameter metadata missing:\n%s", text)
	}

	// Missing required parameter.
	if _, err := GenerateBusinessWorkflow(mm, "account inquiry", nil, "h", 23, nil); err == nil || !strings.Contains(err.Error(), "account_number") {
		t.Fatalf("expected missing-parameter error, got %v", err)
	}
	// Unknown function lists available names.
	if _, err := GenerateBusinessWorkflow(mm, "nope", nil, "h", 23, nil); err == nil || !strings.Contains(err.Error(), "Account Inquiry") {
		t.Fatalf("expected unknown-function error with catalog names, got %v", err)
	}
	// Control characters rejected.
	if _, err := GenerateBusinessWorkflow(mm, "account inquiry", map[string]string{"account_number": "12\n34"}, "h", 23, nil); err == nil {
		t.Fatal("expected control-character rejection")
	}
}

func TestGenerateBusinessWorkflowBridgesNavigationGaps(t *testing.T) {
	mm := buildBusinessTestMindMap(t)
	err := upsertMindMapBusinessFunction(mm, BusinessFunction{
		Name: "Two stage",
		Steps: []BusinessFunctionStep{
			// Lands on "form", but the next step starts at "confirm": the
			// generator must bridge via the learned PF(5) edge.
			{ScreenHash: "menu", AIDKey: "Enter", ExpectHash: "form"},
			{ScreenHash: "confirm", AIDKey: "PF3"},
		},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	data, err := GenerateBusinessWorkflow(mm, "two stage", nil, "h", 23, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(string(data), "PressPF5") {
		t.Fatalf("expected bridging PressPF5 step:\n%s", string(data))
	}

	// Unreachable gap produces a clear error.
	err = upsertMindMapBusinessFunction(mm, BusinessFunction{
		Name: "Broken",
		Steps: []BusinessFunctionStep{
			{ScreenHash: "deadend", AIDKey: "Enter", ExpectHash: "deadend"},
			{ScreenHash: "menu", AIDKey: "Enter"},
		},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := GenerateBusinessWorkflow(mm, "broken", nil, "h", 23, nil); err == nil || !strings.Contains(err.Error(), "no known key path") {
		t.Fatalf("expected no-key-path error, got %v", err)
	}
}

func TestGenerateBusinessWorkflowFromEntryScreenOnly(t *testing.T) {
	mm := buildBusinessTestMindMap(t)
	err := upsertMindMapBusinessFunction(mm, BusinessFunction{
		Name:            "Quick lookup",
		EntryScreenHash: "form",
		Parameters:      []BusinessParameter{{Name: "acct", FieldKey: "R2C13L8", Required: true}},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	data, err := GenerateBusinessWorkflow(mm, "quick lookup", map[string]string{"acct": "42"}, "h", 23, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"Text": "42"`) || !strings.Contains(text, "PressEnter") {
		t.Fatalf("entry-screen-only workflow incomplete:\n%s", text)
	}
}

func TestSavedRunBusinessDataRoundTrip(t *testing.T) {
	mm := buildBusinessTestMindMap(t)
	if err := annotateMindMapArea(mm, "form", "Account inquiry entry", "", map[string]BusinessFieldSemantic{
		"R2C13L8": {Name: "account_number", Sensitive: false},
	}); err != nil {
		t.Fatalf("annotate: %v", err)
	}
	if err := upsertMindMapBusinessFunction(mm, BusinessFunction{Name: "Inquiry", EntryScreenHash: "form"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	dir := t.TempDir()
	run := &SavedRun{SavedRunMeta: SavedRunMeta{ID: NewRunID()}, MindMap: mm}
	if err := SaveRun(dir, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	loaded, err := LoadRun(dir, run.ID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	area := loaded.MindMap.Areas["form"]
	if area == nil || area.BusinessPurpose != "Account inquiry entry" {
		t.Fatalf("business purpose lost in round trip: %+v", area)
	}
	if area.FieldSemantics["R2C13L8"].Name != "account_number" {
		t.Fatal("field semantics lost in round trip")
	}
	if loaded.MindMap.BusinessFunctions["inquiry"] == nil {
		t.Fatal("business function lost in round trip")
	}
}

func TestEngineResumeCarriesBusinessData(t *testing.T) {
	mm := buildBusinessTestMindMap(t)
	if err := upsertMindMapBusinessFunction(mm, BusinessFunction{Name: "Inquiry", EntryScreenHash: "form"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	saved := &SavedRun{SavedRunMeta: SavedRunMeta{ID: "run1"}, MindMap: mm}

	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("mock host: %v", err)
	}
	mockHost.Connected = true
	e := New(mockHost, DefaultConfig())
	if err := e.Resume(saved); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	defer e.Stop()
	fns := e.BusinessFunctionsSnapshot()
	if len(fns) != 1 || fns[0].Name != "Inquiry" {
		t.Fatalf("BusinessFunctionsSnapshot = %+v, want the resumed catalog", fns)
	}
}

func TestEngineAnnotateAndUpsertWhileIdle(t *testing.T) {
	e := New(nil, DefaultConfig())
	if err := e.AnnotateArea("h1", "Login screen", "", nil); err != nil {
		t.Fatalf("AnnotateArea: %v", err)
	}
	if err := e.UpsertBusinessFunction(BusinessFunction{Name: "Login", EntryScreenHash: "h1"}); err != nil {
		t.Fatalf("UpsertBusinessFunction: %v", err)
	}
	fns := e.BusinessFunctionsSnapshot()
	if len(fns) != 1 || fns[0].Name != "Login" {
		t.Fatalf("snapshot = %+v", fns)
	}
}

func TestImportMindMapMergesBusinessFunctions(t *testing.T) {
	e := New(nil, DefaultConfig())
	imported := newMindMap()
	imported.ensureArea("h1")
	if err := upsertMindMapBusinessFunction(imported, BusinessFunction{Name: "Inquiry", EntryScreenHash: "h1"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if !e.ImportMindMap(imported) {
		t.Fatal("ImportMindMap returned false")
	}
	if fns := e.BusinessFunctionsSnapshot(); len(fns) != 1 || fns[0].Name != "Inquiry" {
		t.Fatalf("imported catalog = %+v", fns)
	}
}

func TestBuildAreaDedupSignatureIgnoresDisplayAttributes(t *testing.T) {
	makeScreen := func(color, highlight int) *host.Screen {
		s := &host.Screen{
			Width:       80,
			Height:      24,
			IsFormatted: true,
			Buffer:      make([][]rune, 24),
		}
		for y := range s.Buffer {
			s.Buffer[y] = make([]rune, 80)
		}
		copy(s.Buffer[1], []rune("ACCOUNT INQUIRY"))
		s.Fields = []*host.Field{
			host.NewField(s, host.AttrProtected, 0, 1, 14, 1, color, highlight),
			host.NewField(s, 0x00, 10, 4, 21, 4, color, highlight),
		}
		return s
	}
	sigDefault := buildAreaDedupSignature(makeScreen(host.AttrColDefault, host.AttrEhDefault))
	sigStyled := buildAreaDedupSignature(makeScreen(host.AttrColDefault+1, host.AttrEhUnderscore))
	if sigDefault != sigStyled {
		t.Fatal("dedup signature must ignore color/highlight display attributes")
	}
}
