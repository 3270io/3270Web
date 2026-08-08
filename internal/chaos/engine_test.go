package chaos

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

type scriptedChaosHost struct {
	screens    []*host.Screen
	index      int
	connected  bool
	sentKeys   []string
	writeCalls []string
}

func (h *scriptedChaosHost) Start() error                        { h.connected = true; return nil }
func (h *scriptedChaosHost) Stop() error                         { h.connected = false; return nil }
func (h *scriptedChaosHost) IsConnected() bool                   { return h.connected }
func (h *scriptedChaosHost) UpdateScreen() error                 { return nil }
func (h *scriptedChaosHost) MoveCursor(row, col int) error       { return nil }
func (h *scriptedChaosHost) SubmitScreen() error                 { return nil }
func (h *scriptedChaosHost) SubmitUnformatted(data string) error { return nil }
func (h *scriptedChaosHost) PrintText(format string) (string, error) {
	if s := h.GetScreen(); s != nil {
		return s.Text(), nil
	}
	return "", nil
}
func (h *scriptedChaosHost) Query(string) (string, error) { return "", nil }
func (h *scriptedChaosHost) GetScreen() *host.Screen {
	if len(h.screens) == 0 {
		return nil
	}
	if h.index < 0 {
		h.index = 0
	}
	if h.index >= len(h.screens) {
		h.index = len(h.screens) - 1
	}
	return h.screens[h.index]
}
func (h *scriptedChaosHost) SendKey(key string) error {
	h.sentKeys = append(h.sentKeys, key)
	if h.index < len(h.screens)-1 {
		h.index++
	}
	return nil
}
func (h *scriptedChaosHost) WriteStringAt(row, col int, text string) error {
	h.writeCalls = append(h.writeCalls, text)
	s := h.GetScreen()
	if s == nil {
		return nil
	}
	if row < 0 || row >= s.Height || col < 0 || col >= s.Width {
		return nil
	}
	for i, r := range []rune(text) {
		if col+i >= s.Width {
			break
		}
		s.Buffer[row][col+i] = r
	}
	return nil
}

func buildScriptedChaosScreen(label string, withInput bool) *host.Screen {
	s := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	for i := range s.Buffer {
		s.Buffer[i] = make([]rune, 80)
	}
	for i, r := range []rune(label) {
		if i >= 40 {
			break
		}
		s.Buffer[0][i] = r
	}
	labelEnd := len([]rune(label)) + 1
	if labelEnd > 39 {
		labelEnd = 39
	}
	if labelEnd < 0 {
		labelEnd = 0
	}
	// Protected title field.
	s.Fields = append(s.Fields, host.NewField(s, host.AttrProtected, 0, 0, labelEnd, 0, host.AttrColGreen, host.AttrEhDefault))
	if withInput {
		// One writable field so first-screen KnownData can be applied.
		s.Fields = append(s.Fields, host.NewField(s, 0x00, 10, 4, 21, 4, host.AttrColDefault, host.AttrEhUnderscore))
	}
	return s
}

// buildMockScreen returns a simple formatted screen with one unprotected
// field and one protected label field.
func buildMockScreen() *host.Screen {
	s := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	for i := range s.Buffer {
		s.Buffer[i] = make([]rune, 80)
	}
	// Protected label field at row 0, col 0-9.
	s.Fields = append(s.Fields, host.NewField(s, host.AttrProtected, 0, 0, 9, 0, 0, 0))
	// Unprotected input field at row 2, col 10-19.
	s.Fields = append(s.Fields, host.NewField(s, 0x00, 10, 2, 19, 2, 0, 0))
	return s
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxSteps <= 0 {
		t.Error("DefaultConfig.MaxSteps must be positive")
	}
	if cfg.TimeBudget <= 0 {
		t.Error("DefaultConfig.TimeBudget must be positive")
	}
	if len(cfg.AIDKeyWeights) == 0 {
		t.Error("DefaultConfig.AIDKeyWeights must not be empty")
	}
	// PF(3) commonly logs the user off and Clear wipes the screen on many
	// 3270 applications; AutoBlockExitKeys only catches this when the
	// current screen's help legend literally names the key, which many
	// screens don't have. Both must require explicit opt-in rather than
	// being pressed by default during random exploration.
	for _, dangerous := range []string{"PF(3)", "Clear"} {
		if _, present := cfg.AIDKeyWeights[dangerous]; present {
			t.Errorf("DefaultConfig.AIDKeyWeights must not include %q by default", dangerous)
		}
	}
	if cfg.MaxFieldLength <= 0 {
		t.Error("DefaultConfig.MaxFieldLength must be positive")
	}
	if !cfg.ForceOverrideExistingInputs {
		t.Error("DefaultConfig.ForceOverrideExistingInputs must default to true")
	}
	if cfg.LearnedInputReuseBias != 1.0 {
		t.Errorf("DefaultConfig.LearnedInputReuseBias = %v, want 1.0", cfg.LearnedInputReuseBias)
	}
	if cfg.LearnedKeyReuseBias != 1.0 {
		t.Errorf("DefaultConfig.LearnedKeyReuseBias = %v, want 1.0", cfg.LearnedKeyReuseBias)
	}
	if !cfg.ExcludeNoProgressEvents {
		t.Error("DefaultConfig.ExcludeNoProgressEvents must default to true")
	}
}

func TestLearnedReuseBiasScalingHelpers(t *testing.T) {
	e := New(nil, DefaultConfig())
	if got := e.scaleLearnedInputReuseChance(80); got != 80 {
		t.Fatalf("scaleLearnedInputReuseChance(80) default = %d, want 80", got)
	}
	if got := e.scaleLearnedKeyReuseBoost(120); got != 120 {
		t.Fatalf("scaleLearnedKeyReuseBoost(120) default = %d, want 120", got)
	}

	e.cfg.LearnedInputReuseBias = 0.25
	e.cfg.LearnedKeyReuseBias = 0.25
	if got := e.scaleLearnedInputReuseChance(80); got != 20 {
		t.Fatalf("scaleLearnedInputReuseChance(80) bias=.25 = %d, want 20", got)
	}
	if got := e.scaleLearnedKeyReuseBoost(120); got != 30 {
		t.Fatalf("scaleLearnedKeyReuseBoost(120) bias=.25 = %d, want 30", got)
	}

	e.cfg.LearnedInputReuseBias = 0
	e.cfg.LearnedKeyReuseBias = 0
	if got := e.scaleLearnedInputReuseChance(80); got != 0 {
		t.Fatalf("scaleLearnedInputReuseChance(80) bias=0 = %d, want 0", got)
	}
	if got := e.scaleLearnedKeyReuseBoost(120); got != 0 {
		t.Fatalf("scaleLearnedKeyReuseBoost(120) bias=0 = %d, want 0", got)
	}

	e.cfg.LearnedInputReuseBias = 0.5
	e.cfg.LearnedKeyReuseBias = 1.0
	if got := e.scaleLearnedInputReuseChance(80); got != 40 {
		t.Fatalf("scaleLearnedInputReuseChance(80) input=.5 = %d, want 40", got)
	}
	if got := e.scaleLearnedKeyReuseBoost(120); got != 120 {
		t.Fatalf("scaleLearnedKeyReuseBoost(120) key=1.0 = %d, want 120", got)
	}
}

func TestHashScreen(t *testing.T) {
	s := buildMockScreen()
	h1 := hashScreen(s)
	if len(h1) == 0 {
		t.Fatal("hashScreen returned empty string")
	}

	// Same screen must produce the same hash.
	h2 := hashScreen(s)
	if h1 != h2 {
		t.Error("hashScreen is not deterministic for the same screen")
	}

	// Changing only the cursor position must NOT change the hash: on 3270
	// terminals the cursor moves between input fields (e.g. via Tab) without
	// changing the logical screen, so cursor is not part of screen identity.
	s.CursorX = 5
	h3 := hashScreen(s)
	if h1 != h3 {
		t.Error("hashScreen changed when only cursor moved; cursor is not part of screen identity")
	}

	// Adding a field must change the hash (field count is part of screen identity).
	s.Fields = append(s.Fields, host.NewField(s, 0x00, 20, 5, 29, 5, 0, 0))
	h4 := hashScreen(s)
	if h1 == h4 {
		t.Error("hashScreen did not change when a field was added")
	}

	// Changing a field's position must change the hash even when text content
	// and field count are unchanged.  Two screens with the same text but
	// different field layouts (e.g. a numeric field vs. an alphanumeric field,
	// or fields at different row/column offsets) are distinct screens.
	s2 := buildMockScreen()
	hBefore := hashScreen(s2)
	// Replace the unprotected field at row 2, col 10-19 with one at row 3,
	// col 10-19.  Text content and field count stay the same.
	s2.Fields[1] = host.NewField(s2, 0x00, 10, 3, 19, 3, 0, 0)
	hAfter := hashScreen(s2)
	if hBefore == hAfter {
		t.Error("hashScreen did not change when a field's row position changed")
	}

	// Changing only a field's FieldCode (e.g. marking it numeric) must also
	// change the hash.
	s3 := buildMockScreen()
	hBase := hashScreen(s3)
	s3.Fields[1] = host.NewField(s3, host.AttrNumeric, 10, 2, 19, 2, 0, 0)
	hNumeric := hashScreen(s3)
	if hBase == hNumeric {
		t.Error("hashScreen did not change when a field's attribute code changed")
	}

	// nil screen must return empty string without panicking.
	if hashScreen(nil) != "" {
		t.Error("hashScreen(nil) should return empty string")
	}
}

func TestHashScreen_IgnoresUnprotectedFieldContents(t *testing.T) {
	s := buildMockScreen()
	if len(s.Fields) < 2 || s.Fields[1] == nil || s.Fields[1].IsProtected() {
		t.Fatal("buildMockScreen should provide an unprotected input field")
	}

	// Input field in buildMockScreen is row 2, col 10..19.
	copy(s.Buffer[2][10:], []rune("ALICE     "))
	h1 := hashScreen(s)
	if h1 == "" {
		t.Fatal("hashScreen returned empty hash")
	}

	copy(s.Buffer[2][10:], []rune("BOB       "))
	h2 := hashScreen(s)
	if h2 == "" {
		t.Fatal("hashScreen returned empty hash after input change")
	}
	if h1 != h2 {
		t.Fatalf("hashScreen changed for unprotected field content edit: %q != %q", h1, h2)
	}
}

func TestHashScreen_ProtectedTextStillAffectsHash(t *testing.T) {
	s := buildMockScreen()
	copy(s.Buffer[0][0:], []rune("STATIC LABEL"))
	h1 := hashScreen(s)
	if h1 == "" {
		t.Fatal("hashScreen returned empty hash")
	}

	copy(s.Buffer[0][0:], []rune("CHANGEDLBL "))
	h2 := hashScreen(s)
	if h2 == "" {
		t.Fatal("hashScreen returned empty hash after protected text change")
	}
	if h1 == h2 {
		t.Fatalf("hashScreen should change when protected text changes; both=%q", h1)
	}
}

func TestUnprotectedFields(t *testing.T) {
	s := buildMockScreen()
	fields := unprotectedFields(s)
	if len(fields) != 1 {
		t.Fatalf("expected 1 unprotected field, got %d", len(fields))
	}
	if fields[0].IsProtected() {
		t.Error("field must not be protected")
	}
}

func TestFieldLength(t *testing.T) {
	s := buildMockScreen()
	// The unprotected field spans col 10-19 on a single row → length 10.
	f := s.Fields[1]
	if got := fieldLength(f); got != 10 {
		t.Errorf("fieldLength = %d, want 10", got)
	}
}

func TestAidKeyToStepType(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"Enter", "PressEnter"},
		{"Clear", "PressClear"},
		{"Tab", "PressTab"},
		{"BackTab", "PressBackTab"},
		{"Up", "PressUp"},
		{"Down", "PressDown"},
		{"Delete", "PressDelete"},
		{"PF(1)", "PressPF1"},
		{"PF1", "PressPF1"},
		{"F4", "PressPF4"},
		{"PF(12)", "PressPF12"},
		{"PA(1)", "PressPA1"},
		{"unknown", "PressEnter"},
	}
	for _, c := range cases {
		if got := aidKeyToStepType(c.key); got != c.want {
			t.Errorf("aidKeyToStepType(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

func TestHintKeyBoostsForScreen(t *testing.T) {
	s := buildMockScreen()
	copy(s.Buffer[0], []rune("PF3 - RETURN    PF8 PAGE FORWARD"))

	e := &Engine{
		hintKeyMappings: map[string]string{
			"RETURN":       "PF(3)",
			"PAGE FORWARD": "PF(8)",
			"CONFIRM":      "Enter",
		},
	}

	boosts := e.hintKeyBoostsForScreen(s)
	if boosts == nil {
		t.Fatal("hintKeyBoostsForScreen returned nil, want boosts")
	}
	if boosts["PF(3)"] <= 0 {
		t.Fatalf("PF(3) boost = %d, want > 0", boosts["PF(3)"])
	}
	if boosts["PF(8)"] <= 0 {
		t.Fatalf("PF(8) boost = %d, want > 0", boosts["PF(8)"])
	}
	if _, ok := boosts["Enter"]; ok {
		t.Fatalf("Enter should not be boosted when label is absent, boosts=%v", boosts)
	}
}

func TestInferScreenHelpKeyAssignmentsAndBoosts(t *testing.T) {
	s := buildMockScreen()
	copy(s.Buffer[23], []rune("PF3 Logoff   PF8 Next Page   Enter=Select"))

	assignments := inferScreenHelpKeyAssignments(s)
	if assignments == nil {
		t.Fatal("inferScreenHelpKeyAssignments returned nil")
	}
	if got := assignments["LOGOFF"]; got != "PF(3)" {
		t.Fatalf("LOGOFF assignment = %q, want %q", got, "PF(3)")
	}
	if got := assignments["NEXT PAGE"]; got != "PF(8)" {
		t.Fatalf("NEXT PAGE assignment = %q, want %q", got, "PF(8)")
	}
	if got := assignments["SELECT"]; got != "Enter" {
		t.Fatalf("SELECT assignment = %q, want %q", got, "Enter")
	}

	boosts := inferScreenHelpKeyBoosts(s)
	if boosts["PF(8)"] <= boosts["PF(3)"] {
		t.Fatalf("expected PF(8) boost (%d) to exceed PF(3) boost (%d) for Next vs Logoff", boosts["PF(8)"], boosts["PF(3)"])
	}
	if boosts["Enter"] <= 0 {
		t.Fatalf("Enter boost = %d, want > 0", boosts["Enter"])
	}
}

func TestEngineDefaultHints_AppliedWithUserOverride(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Hints = []Hint{
		{KeyAssignments: map[string]string{"Help": "PF2"}},
	}
	e := New(nil, cfg)
	if len(e.defaultHintTx) == 0 || len(e.defaultHintData) == 0 {
		t.Fatal("expected built-in fallback hints to be populated")
	}
	if e.hintKeyMappings == nil || len(e.hintKeyMappings) == 0 {
		t.Fatal("expected merged key mappings to include defaults")
	}
	if got := e.hintKeyMappings["HELP"]; got != "PF(2)" {
		t.Fatalf("user mapping should override default HELP mapping: got %q want %q", got, "PF(2)")
	}
	if got := e.hintKeyMappings["NEXT"]; got != "PF(8)" {
		t.Fatalf("expected built-in NEXT mapping, got %q", got)
	}
}

func TestPickHintValueForFieldPool_PrefersExactLength(t *testing.T) {
	rng := rand.New(rand.NewSource(1)) //nolint:gosec
	counts := map[string]int{}
	for i := 0; i < 500; i++ {
		got := pickHintValueForFieldPool(rng, []string{"A", "ABC"}, 3, false)
		counts[got]++
	}
	// Exact-length values should be more likely, but not exclusive.
	if counts["ABC"] <= counts["A"] {
		t.Fatalf("expected exact-length hint to be preferred, got counts=%v", counts)
	}
	if counts["A"] == 0 {
		t.Fatalf("expected non-exact hints to remain selectable, got counts=%v", counts)
	}
}

func TestPrepareFieldWriteValue_PadsToOverwriteExistingTail(t *testing.T) {
	s := &host.Screen{Width: 80, Height: 24}
	f := host.NewField(s, 0x00, 0, 0, 5, 0, 0, 0) // length 6
	f.SetValue("EXISTS")

	e := New(nil, DefaultConfig())
	e.rng = rand.New(rand.NewSource(21)) //nolint:gosec

	got := e.prepareFieldWriteValue(f, "ABC")
	if len(got) != 6 {
		t.Fatalf("prepareFieldWriteValue len = %d, want 6", len(got))
	}
	if got != "ABC   " {
		t.Fatalf("prepareFieldWriteValue = %q, want %q", got, "ABC   ")
	}
}

func TestPrepareFieldWriteValue_AvoidsReusingSamePrefilledValue(t *testing.T) {
	s := &host.Screen{Width: 80, Height: 24}
	f := host.NewField(s, 0x00, 0, 0, 5, 0, 0, 0) // length 6
	f.SetValue("ABC   ")

	e := New(nil, DefaultConfig())
	e.rng = rand.New(rand.NewSource(42)) //nolint:gosec

	got := e.prepareFieldWriteValue(f, "ABC")
	if len(got) != 6 {
		t.Fatalf("prepareFieldWriteValue len = %d, want 6", len(got))
	}
	if normalizeFieldValueForCompare(got) == normalizeFieldValueForCompare("ABC   ") {
		t.Fatalf("prepareFieldWriteValue reused existing field value: %q", got)
	}
}

func TestClearFieldBeforeWrite_ClearsFieldCells(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Connected = true
	s := &host.Screen{Width: 80, Height: 24}
	f := host.NewField(s, 0x00, 2, 1, 7, 1, 0, 0) // row 1, col 2-7 (len 6)
	if err := h.WriteStringAt(1, 2, "ABCDEF"); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	e := New(h, DefaultConfig())
	if err := e.clearFieldBeforeWrite(f); err != nil {
		t.Fatalf("clearFieldBeforeWrite error: %v", err)
	}
	for x := 2; x <= 7; x++ {
		if ch := h.Screen.CharAt(x, 1); ch != ' ' {
			t.Fatalf("screen char at (%d,%d) = %q, want space", x, 1, ch)
		}
	}
}

func TestChooseAIDKeyBoosted_RespectsKeyBlacklist(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AIDKeyWeights = map[string]int{
		"Enter": 1,
		"PF3":   100,
	}
	cfg.KeyBlacklist = []string{"PF(3)"}

	e := New(nil, cfg)
	e.rng = rand.New(rand.NewSource(11)) //nolint:gosec

	for i := 0; i < 50; i++ {
		if got := e.chooseAIDKeyBoosted(map[string]int{"PF(3)": 1000}); got == "PF(3)" {
			t.Fatalf("blacklisted key PF(3) was selected on iteration %d", i)
		}
	}
}

func TestChooseAIDKeyBoosted_AllFallbackKeysBlockedReturnsEmpty(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AIDKeyWeights = map[string]int{
		"PF3": 100,
	}
	cfg.KeyBlacklist = []string{"Enter", "PF1", "PF2", "PF3", "PF4", "PF7", "PF8", "PF12", "Tab"}

	e := New(nil, cfg)
	if got := e.chooseAIDKeyBoosted(nil); got != "" {
		t.Fatalf("chooseAIDKeyBoosted = %q, want empty key when all candidates are blacklisted", got)
	}
}

func TestSetKeyBlacklist_LiveUpdateNormalizesAliases(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AIDKeyWeights = map[string]int{
		"PF3":   100,
		"Enter": 1,
	}
	cfg.KeyBlacklist = nil

	e := New(nil, cfg)
	e.rng = rand.New(rand.NewSource(7)) //nolint:gosec

	e.SetKeyBlacklist([]string{"PF(3)"})

	for i := 0; i < 25; i++ {
		if got := e.chooseAIDKeyBoosted(map[string]int{"PF3": 1000}); got == "PF(3)" {
			t.Fatalf("blacklisted key PF(3) selected after live update on iteration %d", i)
		}
	}
}

func TestChooseAIDKeyBoosted_RespectsBlacklistForPressPFAlias(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AIDKeyWeights = map[string]int{
		"PressPF3": 100,
		"Enter":    1,
	}
	cfg.KeyBlacklist = []string{"PF3"}

	e := New(nil, cfg)
	e.rng = rand.New(rand.NewSource(13)) //nolint:gosec

	for i := 0; i < 25; i++ {
		if got := e.chooseAIDKeyBoosted(map[string]int{"PressPF3": 1000}); got == "PF(3)" || got == "PressPF3" {
			t.Fatalf("blacklisted PF3 selected via PressPF alias on iteration %d: %q", i, got)
		}
	}
}

func TestNormalizeChaosKeyName_WorkflowPressAliases(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"PressEnter", "Enter"},
		{"Press Enter", "Enter"},
		{"return", "Enter"},
		{"PressTab", "Tab"},
		{"PressBackTab", "BackTab"},
		{"Press Back Tab", "BackTab"},
		{"back_tab", "BackTab"},
		{"PressClear", "Clear"},
		{"PressReset", "Reset"},
		{"PressEraseEOF", "EraseEOF"},
		{"PressErase_EOF", "EraseEOF"},
		{"erase-eof", "EraseEOF"},
		{"PressEraseInput", "EraseInput"},
		{"PressErase_Input", "EraseInput"},
		{"PressDup", "Dup"},
		{"PressFieldMark", "FieldMark"},
		{"PressField_Mark", "FieldMark"},
		{"PressSysReq", "SysReq"},
		{"PressSys_Req", "SysReq"},
		{"PressAttn", "Attn"},
		{"PressNewline", "Newline"},
		{"PressNew_Line", "Newline"},
		{"PressBackspace", "BackSpace"},
		{"PressDelete", "Delete"},
		{"PressInsert", "Insert"},
		{"PressHome", "Home"},
		{"PressUp", "Up"},
		{"PressDown", "Down"},
		{"PressLeft", "Left"},
		{"PressRight", "Right"},
		{"PressPF3", "PF(3)"},
		{"Press PF3", "PF(3)"},
		{"Press PF(3)", "PF(3)"},
		{"PressPF24", "PF(24)"},
		{"PF 3", "PF(3)"},
		{"PF-3", "PF(3)"},
		{"PF_3", "PF(3)"},
		{"F 3", "PF(3)"},
		{"PressPA1", "PA(1)"},
		{"PA 1", "PA(1)"},
		{"presspf3", "PF(3)"},
	}
	for _, tc := range cases {
		if got := normalizeChaosKeyName(tc.in); got != tc.want {
			t.Fatalf("normalizeChaosKeyName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEngineStartStop(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Screen = buildMockScreen()
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 3
	cfg.StepDelay = 0
	cfg.Seed = 42

	e := New(h, cfg)

	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for the engine to finish (MaxSteps = 3 with no delay).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	status := e.Status()
	if status.Active {
		t.Error("engine should have stopped after MaxSteps")
		e.Stop()
	}
	if status.StepsRun == 0 {
		t.Error("engine ran 0 steps")
	}
}

func TestEngineNotConnected(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	// Connected = false by default.

	e := New(h, DefaultConfig())
	if err := e.Start(); err == nil {
		t.Error("Start() should fail when not connected")
	}
}

func TestEngineDoubleStart(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Connected = true
	h.Screen = buildMockScreen()

	cfg := DefaultConfig()
	cfg.MaxSteps = 0 // unlimited – we will stop manually
	cfg.StepDelay = 50 * time.Millisecond
	cfg.Seed = 1

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	defer e.Stop()

	if err := e.Start(); err == nil {
		t.Error("second Start() should return an error")
	}
}

func TestExportWorkflow(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Connected = true
	h.Screen = buildMockScreen()

	cfg := DefaultConfig()
	cfg.MaxSteps = 2
	cfg.StepDelay = 25 * time.Millisecond
	cfg.Seed = 7
	cfg.ExportHost = "export-host"
	cfg.ExportPort = 4023

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	data, err := e.ExportWorkflow("testhost", 3270)
	if err != nil {
		t.Fatalf("ExportWorkflow error: %v", err)
	}
	if len(data) == 0 {
		t.Error("ExportWorkflow returned empty JSON")
	}
	var exported exportedWorkflow
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatalf("ExportWorkflow JSON parse failed: %v", err)
	}
	if exported.Host != "testhost" {
		t.Fatalf("ExportWorkflow Host = %q, want %q", exported.Host, "testhost")
	}
	if exported.Port != 3270 {
		t.Fatalf("ExportWorkflow Port = %d, want 3270", exported.Port)
	}
	if exported.EveryStepDelay == nil {
		t.Fatal("ExportWorkflow EveryStepDelay is nil")
	}
	if exported.EveryStepDelay.Min != 0.025 || exported.EveryStepDelay.Max != 0.025 {
		t.Fatalf("ExportWorkflow EveryStepDelay = %+v, want Min=Max=0.025", exported.EveryStepDelay)
	}
	if exported.RampUpBatchSize == 0 {
		t.Fatal("ExportWorkflow missing RampUpBatchSize")
	}
	if exported.EndOfTaskDelay == nil {
		t.Fatal("ExportWorkflow missing EndOfTaskDelay")
	}
}

func TestBuildExportWorkflowSteps_SuccessOnlyByDefault(t *testing.T) {
	transitions := []Transition{
		{FromHash: "a", ToHash: "b", Steps: []session.WorkflowStep{{Type: "PressEnter"}}},
		{FromHash: "b", ToHash: "c", Steps: []session.WorkflowStep{{Type: "PressPF3"}}},
	}
	attempts := []Attempt{
		{Attempt: 1, FromHash: "a", Transitioned: true},
		{Attempt: 2, FromHash: "b", AIDKey: "PF(1)", Transitioned: false},
		{Attempt: 3, FromHash: "b", Transitioned: true},
	}
	mindMap := &MindMap{
		Areas: map[string]*MindMapArea{
			"b": {Hash: "b", PreviewText: "Menu Screen", PreviewWidth: 80, PreviewHeight: 24},
		},
	}

	steps := buildExportWorkflowSteps(transitions, attempts, mindMap, 1.0)
	if len(steps) != 2 {
		t.Fatalf("steps len=%d, want 2 successful steps only", len(steps))
	}
	if steps[0].Type != "PressEnter" || steps[1].Type != "PressPF3" {
		t.Fatalf("unexpected exported steps: %#v", steps)
	}
}

func TestBuildExportWorkflowSteps_IncludesSafeUnsuccessfulChecksWhenBalanced(t *testing.T) {
	transitions := []Transition{
		{FromHash: "a", ToHash: "b", Steps: []session.WorkflowStep{{Type: "PressEnter"}}},
		{FromHash: "b", ToHash: "c", Steps: []session.WorkflowStep{{Type: "PressPF3"}}},
	}
	attempts := []Attempt{
		{Attempt: 1, FromHash: "a", Transitioned: true},
		{Attempt: 2, FromHash: "b", AIDKey: "PF(1)", Transitioned: false},
		{Attempt: 3, FromHash: "b", Transitioned: true},
	}
	mindMap := &MindMap{
		Areas: map[string]*MindMapArea{
			"b": {Hash: "b", PreviewText: "  3270 Example Application\n", PreviewWidth: 80, PreviewHeight: 24},
		},
	}

	steps := buildExportWorkflowSteps(transitions, attempts, mindMap, 0.5)
	if len(steps) < 3 {
		t.Fatalf("steps len=%d, want successful steps plus at least one CheckValue", len(steps))
	}
	if steps[0].Type != "PressEnter" {
		t.Fatalf("first step = %q, want PressEnter", steps[0].Type)
	}
	foundCheck := false
	foundFinal := false
	for _, step := range steps {
		if step.Type == "CheckValue" {
			foundCheck = true
			if step.Coordinates == nil || step.Coordinates.Row != 1 || step.Coordinates.Column <= 0 || step.Coordinates.Length <= 0 {
				t.Fatalf("invalid CheckValue coordinates: %+v", step.Coordinates)
			}
		}
		if step.Type == "PressPF3" {
			foundFinal = true
		}
	}
	if !foundCheck {
		t.Fatalf("expected a safe CheckValue step for unsuccessful attempt")
	}
	if !foundFinal {
		t.Fatalf("expected final successful transition step to remain present")
	}
}

func TestBuildExportWorkflowSteps_ExcludesCheckStepsForUnreachableScreens(t *testing.T) {
	// Transition only visits screens "a" -> "b". Screen "x" never appears in transitions.
	transitions := []Transition{
		{FromHash: "a", ToHash: "b", Steps: []session.WorkflowStep{{Type: "PressEnter"}}},
	}
	attempts := []Attempt{
		{Attempt: 1, FromHash: "a", Transitioned: true},
		// This failed attempt was on screen "x" which is NOT in any transition.
		{Attempt: 2, FromHash: "x", AIDKey: "PF(1)", Transitioned: false, Error: "host error"},
	}
	mindMap := &MindMap{
		Areas: map[string]*MindMapArea{
			"x": {Hash: "x", PreviewText: "  Error Screen Content\n", PreviewWidth: 80, PreviewHeight: 24},
		},
	}

	steps := buildExportWorkflowSteps(transitions, attempts, mindMap, 0.5)
	for _, step := range steps {
		if step.Type == "CheckValue" {
			t.Fatalf("expected no CheckValue steps for unreachable screen 'x', but got one: %+v", step)
		}
	}
	if len(steps) == 0 {
		t.Fatalf("expected transition steps to be present")
	}
	if steps[0].Type != "PressEnter" {
		t.Fatalf("first step = %q, want PressEnter", steps[0].Type)
	}
}

func TestBuildExportWorkflowSteps_IncludesCheckStepsForReachableScreens(t *testing.T) {
	// Transition visits "a" -> "b". Screen "b" is reachable and has a failed attempt.
	transitions := []Transition{
		{FromHash: "a", ToHash: "b", Steps: []session.WorkflowStep{{Type: "PressEnter"}}},
		{FromHash: "b", ToHash: "c", Steps: []session.WorkflowStep{{Type: "PressPF3"}}},
	}
	attempts := []Attempt{
		{Attempt: 1, FromHash: "a", Transitioned: true},
		// Failed attempt on "b" which IS reachable via transitions.
		{Attempt: 2, FromHash: "b", AIDKey: "PF(1)", Transitioned: false, Error: "host error"},
		{Attempt: 3, FromHash: "b", Transitioned: true},
	}
	mindMap := &MindMap{
		Areas: map[string]*MindMapArea{
			"b": {Hash: "b", PreviewText: "  Menu Screen\n", PreviewWidth: 80, PreviewHeight: 24},
		},
	}

	steps := buildExportWorkflowSteps(transitions, attempts, mindMap, 0.5)
	foundCheck := false
	for _, step := range steps {
		if step.Type == "CheckValue" {
			foundCheck = true
		}
	}
	if !foundCheck {
		t.Fatalf("expected CheckValue step for reachable screen 'b', got none")
	}
}

func TestChooseAIDKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Seed = 99
	e := New(nil, cfg)

	counts := make(map[string]int)
	for i := 0; i < 1000; i++ {
		k := e.chooseAIDKey()
		counts[k]++
	}
	// Enter has the highest weight (70%) – it should be chosen most often.
	enterCount := counts["Enter"]
	if enterCount < 500 {
		t.Errorf("Enter chosen only %d/1000 times, expected majority", enterCount)
	}
}

func TestGenerateValue_Numeric(t *testing.T) {
	s := &host.Screen{Width: 80, Height: 24}
	// Numeric field (AttrNumeric bit set).
	f := host.NewField(s, host.AttrNumeric, 0, 0, 9, 0, 0, 0)
	e := New(nil, DefaultConfig())
	e.rng = rand.New(rand.NewSource(42)) //nolint:gosec

	v := e.generateValue(f)
	if len(v) == 0 {
		t.Fatal("generateValue returned empty string for numeric field")
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			t.Errorf("generateValue for numeric field contains non-digit %q", c)
		}
	}
}

func TestGenerateValue_RespectsMaxFieldLength(t *testing.T) {
	s := &host.Screen{Width: 80, Height: 24}
	// Wide unprotected field: col 0-49 = length 50
	f := host.NewField(s, 0x00, 0, 0, 49, 0, 0, 0)

	cfg := DefaultConfig()
	cfg.MaxFieldLength = 5
	e := New(nil, cfg)
	e.rng = rand.New(rand.NewSource(1)) //nolint:gosec

	v := e.generateValue(f)
	if len(v) > 5 {
		t.Errorf("generateValue produced %d chars, want at most 5", len(v))
	}
}

func TestGenerateValueForField_UsesHints(t *testing.T) {
	s := &host.Screen{Width: 80, Height: 24}
	f := host.NewField(s, 0x00, 0, 0, 9, 0, 0, 0)

	cfg := DefaultConfig()
	cfg.MaxFieldLength = 10
	cfg.Hints = []Hint{
		{Transaction: "CEMT", KnownData: []string{"ABC123"}},
	}
	e := New(nil, cfg)
	e.rng = rand.New(rand.NewSource(2)) //nolint:gosec

	hintHits := 0
	randomHits := 0
	for i := 0; i < 100; i++ {
		v := e.generateValueForField(f, true)
		if v == "" {
			t.Fatal("generateValueForField returned empty value with hints configured")
		}
		if strings.TrimSpace(v) == "CEMT" || strings.TrimSpace(v) == "ABC123" {
			hintHits++
		} else {
			randomHits++
		}
	}
	if hintHits == 0 {
		t.Fatalf("expected hint values to be used sometimes, got hintHits=%d randomHits=%d", hintHits, randomHits)
	}
	if randomHits == 0 {
		t.Fatalf("expected non-hint values to be used sometimes (chaos), got hintHits=%d randomHits=%d", hintHits, randomHits)
	}
}

func TestEngineFirstScreenHintKey_UsesHintOnFirstAttempt(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Screen = buildMockScreen()
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 1
	cfg.StepDelay = 0
	cfg.Seed = 20260223
	cfg.ExcludeNoProgressEvents = false
	cfg.ScreenHints = map[string]ScreenHint{
		FirstScreenHintKey: {
			KnownData: []string{"CEMT"},
		},
	}

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := e.Status()
	if st.LastAttempt == nil {
		t.Fatal("expected LastAttempt to be populated")
	}
	if len(st.LastAttempt.FieldWrites) == 0 {
		t.Fatalf("expected a field write on first attempt, got %#v", st.LastAttempt)
	}
	got := strings.TrimSpace(st.LastAttempt.FieldWrites[0].Value)
	if got != "CEMT" {
		t.Fatalf("first attempt wrote %q, want first-screen hint value %q", got, "CEMT")
	}
}

func TestEngineFirstScreenHintKey_PreservesSamePrefilledHintValue(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	s := buildMockScreen()
	if len(s.Fields) < 2 {
		t.Fatal("buildMockScreen should include an input field")
	}
	s.Fields[1].SetValue("CEMT      ")
	h.Screen = s
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 1
	cfg.StepDelay = 0
	cfg.Seed = 9
	cfg.ExcludeNoProgressEvents = false
	cfg.ScreenHints = map[string]ScreenHint{
		FirstScreenHintKey: {
			KnownData: []string{"CEMT"},
		},
	}

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := e.Status()
	if st.LastAttempt == nil || len(st.LastAttempt.FieldWrites) == 0 {
		t.Fatalf("expected field write attempt, got %#v", st.LastAttempt)
	}
	got := strings.TrimSpace(st.LastAttempt.FieldWrites[0].Value)
	if got != "CEMT" {
		t.Fatalf("first attempt rewrote hinted prefilled value as %q, want %q", got, "CEMT")
	}
}

func TestEngineFirstScreenHintKey_ReusedWhenFirstScreenHashReappears(t *testing.T) {
	screenA1 := buildScriptedChaosScreen("FIRST SCREEN A", true)
	screenB := buildScriptedChaosScreen("SECOND SCREEN B", false)
	screenA2 := buildScriptedChaosScreen("FIRST SCREEN A", true)
	h := &scriptedChaosHost{
		screens:   []*host.Screen{screenA1, screenB, screenA2},
		connected: true,
	}

	cfg := DefaultConfig()
	cfg.MaxSteps = 3
	cfg.StepDelay = 0
	cfg.Seed = 12345
	cfg.ExcludeNoProgressEvents = false
	cfg.ScreenDedupSimilarity = 1
	cfg.AIDKeyWeights = map[string]int{"Enter": 1}
	cfg.KeyBlacklist = []string{"Enter"}
	cfg.ScreenHints = map[string]ScreenHint{
		FirstScreenHintKey: {
			KnownData: []string{"CEMT"},
			KnownKeys: []string{"PF6"},
		},
	}

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := e.Status()
	if st.StepsRun != 3 {
		t.Fatalf("StepsRun=%d, want 3", st.StepsRun)
	}
	if len(st.RecentAttempts) < 3 {
		t.Fatalf("RecentAttempts len=%d, want >= 3", len(st.RecentAttempts))
	}
	a1 := st.RecentAttempts[len(st.RecentAttempts)-3]
	a2 := st.RecentAttempts[len(st.RecentAttempts)-2]
	a3 := st.RecentAttempts[len(st.RecentAttempts)-1]

	if a1.FromHash == "" || a3.FromHash == "" {
		t.Fatalf("expected non-empty hashes for repeated first screen: a1=%q a3=%q", a1.FromHash, a3.FromHash)
	}
	if a1.FromHash != a3.FromHash {
		t.Fatalf("first screen hash mismatch on revisit: a1=%q a3=%q", a1.FromHash, a3.FromHash)
	}
	if a2.FromHash == a1.FromHash {
		t.Fatalf("middle screen hash should differ from first screen hash, both=%q", a1.FromHash)
	}

	if a1.AIDKey != "PF(6)" {
		t.Fatalf("attempt1 AIDKey=%q, want PF(6) from first-screen known key hint", a1.AIDKey)
	}
	if a3.AIDKey != "PF(6)" {
		t.Fatalf("attempt3 AIDKey=%q, want PF(6) when first screen reappears", a3.AIDKey)
	}

	if len(a1.FieldWrites) == 0 || strings.TrimSpace(a1.FieldWrites[0].Value) != "CEMT" {
		t.Fatalf("attempt1 first-screen hint data not used, got %#v", a1.FieldWrites)
	}
	if len(a3.FieldWrites) == 0 || strings.TrimSpace(a3.FieldWrites[0].Value) != "CEMT" {
		t.Fatalf("attempt3 first-screen hint data not reused on repeated first screen, got %#v", a3.FieldWrites)
	}
}

func TestEngineFirstScreenHintKey_BlockedKeysApplyOnlyToFirstScreen(t *testing.T) {
	screenA := buildScriptedChaosScreen("FIRST SCREEN A", true)
	screenB := buildScriptedChaosScreen("SECOND SCREEN B", true)
	// Use exact dedup so the test's per-screen hint keyed by hashScreen
	// matches the runtime canonical hash unchanged.
	screenBHash := hashScreen(screenB)
	h := &scriptedChaosHost{
		screens:   []*host.Screen{screenA, screenB},
		connected: true,
	}

	cfg := DefaultConfig()
	cfg.MaxSteps = 2
	cfg.StepDelay = 0
	cfg.Seed = 424242
	cfg.ExcludeNoProgressEvents = false
	cfg.ScreenDedupSimilarity = 1
	cfg.DedupMode = DedupModeExact
	cfg.AIDKeyWeights = map[string]int{"Enter": 1}
	cfg.KeyBlacklist = []string{"Enter"}
	cfg.ScreenHints = map[string]ScreenHint{
		FirstScreenHintKey: {
			KnownKeys:   []string{"PF6"},
			BlockedKeys: []string{"PF6"},
		},
		screenBHash: {
			KnownKeys: []string{"PF6"},
		},
	}

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := e.Status()
	if len(st.RecentAttempts) < 2 {
		t.Fatalf("RecentAttempts len=%d, want >=2", len(st.RecentAttempts))
	}
	a1 := st.RecentAttempts[len(st.RecentAttempts)-2]
	a2 := st.RecentAttempts[len(st.RecentAttempts)-1]
	if a1.AIDKey == "PF(6)" {
		t.Fatalf("attempt1 AIDKey=%q, want first-screen blocked key to be skipped", a1.AIDKey)
	}
	if a2.AIDKey != "PF(6)" {
		t.Fatalf("attempt2 AIDKey=%q, want PF(6) allowed on non-first screen", a2.AIDKey)
	}
}

func TestCanonicalizeObservedScreenHashLocked_MergesEchoValueVariants(t *testing.T) {
	makeEchoScreen := func(protectedLine string) *host.Screen {
		s := &host.Screen{
			Width:       80,
			Height:      24,
			IsFormatted: true,
			Buffer:      make([][]rune, 24),
		}
		for y := range s.Buffer {
			s.Buffer[y] = make([]rune, 80)
		}
		for i, r := range []rune(protectedLine) {
			if i >= 79 {
				break
			}
			s.Buffer[2][i] = r
		}
		s.Fields = []*host.Field{
			host.NewField(s, host.AttrProtected, 0, 2, minInt(len([]rune(protectedLine)), 79), 2, host.AttrColDefault, host.AttrEhDefault),
			host.NewField(s, 0x00, 10, 4, 21, 4, host.AttrColDefault, host.AttrEhUnderscore),
		}
		return s
	}

	screenA := makeEchoScreen("ACCOUNT 123456 ENTER TO CONTINUE")
	screenB := makeEchoScreen("ACCOUNT 987654 ENTER TO CONTINUE")
	hashA := hashScreen(screenA)
	hashB := hashScreen(screenB)
	if hashA == hashB {
		t.Fatalf("raw hashes unexpectedly equal; test requires distinct hashes")
	}

	e := New(nil, DefaultConfig())
	e.mindMap = newMindMap()
	now := time.Now()
	e.mu.Lock()
	canonicalA := e.canonicalizeObservedScreenHashLocked(hashA, screenA)
	e.mu.Unlock()
	e.mindMap.observeScreen(canonicalA, screenA, now)

	e.mu.Lock()
	got := e.canonicalizeObservedScreenHashLocked(hashB, screenB)
	e.mu.Unlock()
	if got != canonicalA {
		t.Fatalf("canonicalized hash = %q, want existing canonical %q for echoed-value variant", got, canonicalA)
	}
}

func TestCanonicalizeObservedScreenHashLocked_DoesNotMergeDifferentTitlesSameLayout(t *testing.T) {
	makeScreen := func(title string) *host.Screen {
		s := &host.Screen{
			Width:       80,
			Height:      24,
			IsFormatted: true,
			Buffer:      make([][]rune, 24),
		}
		for y := range s.Buffer {
			s.Buffer[y] = make([]rune, 80)
		}
		for i, r := range []rune(title) {
			if i >= 79 {
				break
			}
			s.Buffer[1][i] = r
		}
		copy(s.Buffer[3], []rune("Press Enter to continue"))
		s.Fields = []*host.Field{
			host.NewField(s, host.AttrProtected, 0, 1, minInt(len([]rune(title))-1, 79), 1, host.AttrColDefault, host.AttrEhDefault),
			host.NewField(s, host.AttrProtected, 0, 3, 22, 3, host.AttrColDefault, host.AttrEhDefault),
			host.NewField(s, 0x00, 10, 5, 21, 5, host.AttrColDefault, host.AttrEhUnderscore),
		}
		return s
	}

	screenA := makeScreen("Customer Search")
	screenB := makeScreen("Payment Review")
	hashA := hashScreen(screenA)
	hashB := hashScreen(screenB)
	if hashA == hashB {
		t.Fatalf("raw hashes unexpectedly equal; test requires distinct hashes")
	}

	e := New(nil, DefaultConfig())
	e.mindMap = newMindMap()
	now := time.Now()
	e.mu.Lock()
	canonicalA := e.canonicalizeObservedScreenHashLocked(hashA, screenA)
	e.mu.Unlock()
	e.mindMap.observeScreen(canonicalA, screenA, now)

	e.mu.Lock()
	got := e.canonicalizeObservedScreenHashLocked(hashB, screenB)
	e.mu.Unlock()
	if got == canonicalA {
		t.Fatalf("canonicalized hash = %q matched %q; different-title screens must stay distinct", got, canonicalA)
	}
}

func TestFitHintValueForField_NumericFilter(t *testing.T) {
	got := fitHintValueForField("AB12CD34", 6, true)
	if got != "1234" {
		t.Fatalf("fitHintValueForField numeric = %q, want %q", got, "1234")
	}
	got = fitHintValueForField("TXN12345", 3, false)
	if got != "TXN" {
		t.Fatalf("fitHintValueForField trim = %q, want %q", got, "TXN")
	}
}

func TestEngineMetadata(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Screen = buildMockScreen()
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 3
	cfg.StepDelay = 0
	cfg.Seed = 77

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := e.Status()
	if st.UniqueScreens == 0 {
		t.Error("expected at least one unique screen hash after exploration")
	}
	if st.AIDKeyCounts == nil || len(st.AIDKeyCounts) == 0 {
		t.Error("expected AIDKeyCounts to be populated after exploration")
	}
	total := 0
	for _, v := range st.AIDKeyCounts {
		total += v
	}
	if total != st.StepsRun {
		t.Errorf("sum of AIDKeyCounts (%d) != StepsRun (%d)", total, st.StepsRun)
	}
}

func TestEngineStatusIncludesAttemptDetails(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Screen = buildMockScreen()
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 2
	cfg.StepDelay = 0
	cfg.Seed = 123
	cfg.ExcludeNoProgressEvents = false

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := e.Status()
	if len(st.RecentAttempts) == 0 {
		t.Fatal("expected RecentAttempts to be populated")
	}
	if st.LastAttempt == nil {
		t.Fatal("expected LastAttempt to be populated")
	}
	if st.LastAttempt.Attempt <= 0 {
		t.Errorf("LastAttempt.Attempt = %d, want > 0", st.LastAttempt.Attempt)
	}
	if st.LastAttempt.AIDKey == "" {
		t.Error("LastAttempt.AIDKey should not be empty")
	}
	if st.LastAttempt.FieldsTargeted <= 0 {
		t.Errorf("LastAttempt.FieldsTargeted = %d, want > 0", st.LastAttempt.FieldsTargeted)
	}
	if len(st.LastAttempt.FieldWrites) == 0 {
		t.Error("expected at least one field write record in LastAttempt")
	}
}

func TestSelectTargetFields_AllowsOneSeveralOrAll(t *testing.T) {
	e := New(nil, DefaultConfig())
	e.rng = rand.New(rand.NewSource(9)) //nolint:gosec
	s := &host.Screen{Width: 80, Height: 24}
	fields := []*host.Field{
		host.NewField(s, 0x00, 1, 1, 5, 1, 0, 0),
		host.NewField(s, 0x00, 10, 1, 14, 1, 0, 0),
		host.NewField(s, 0x00, 20, 1, 24, 1, 0, 0),
		host.NewField(s, 0x00, 30, 1, 34, 1, 0, 0),
	}

	seenOne := false
	seenSeveral := false
	seenAll := false
	for i := 0; i < 200; i++ {
		targeted := e.selectTargetFields(fields)
		switch len(targeted) {
		case 1:
			seenOne = true
		case len(fields):
			seenAll = true
		default:
			seenSeveral = true
		}
		if seenOne && seenSeveral && seenAll {
			break
		}
	}
	if !seenOne || !seenSeveral || !seenAll {
		t.Fatalf("selection modes seen: one=%v several=%v all=%v", seenOne, seenSeveral, seenAll)
	}
}

func TestSelectTargetFields_AllSingleCellTargetsOne(t *testing.T) {
	e := New(nil, DefaultConfig())
	e.rng = rand.New(rand.NewSource(21)) //nolint:gosec
	s := &host.Screen{Width: 80, Height: 24}
	fields := []*host.Field{
		host.NewField(s, 0x00, 1, 1, 1, 1, 0, 0),
		host.NewField(s, 0x00, 3, 1, 3, 1, 0, 0),
		host.NewField(s, 0x00, 5, 1, 5, 1, 0, 0),
		host.NewField(s, 0x00, 7, 1, 7, 1, 0, 0),
	}
	for i := 0; i < 50; i++ {
		if got := len(e.selectTargetFields(fields)); got != 1 {
			t.Fatalf("selectTargetFields len = %d, want 1 for single-cell field screens", got)
		}
	}
}

func TestSelectTargetFieldsForScreen_PrefersLearnedSuccessfulFieldCount(t *testing.T) {
	e := New(nil, DefaultConfig())
	e.rng = rand.New(rand.NewSource(5)) //nolint:gosec
	e.mindMap = newMindMap()
	area := e.mindMap.ensureArea("hash-learned-count")
	area.FieldCountProgressions[3] = 12
	area.MultiFieldProgressions = 12

	s := &host.Screen{Width: 80, Height: 24}
	fields := []*host.Field{
		host.NewField(s, 0x00, 1, 1, 5, 1, 0, 0),
		host.NewField(s, 0x00, 10, 1, 14, 1, 0, 0),
		host.NewField(s, 0x00, 20, 1, 24, 1, 0, 0),
		host.NewField(s, 0x00, 30, 1, 34, 1, 0, 0),
	}

	counts := map[int]int{}
	for i := 0; i < 300; i++ {
		counts[len(e.selectTargetFieldsForScreen("hash-learned-count", fields))]++
	}
	if counts[3] <= counts[1] || counts[3] <= counts[len(fields)] {
		t.Fatalf("expected learned target size 3 to be preferred, got counts=%v", counts)
	}
}

func TestEngineSingleCellInputsTargetOneFieldPerAttempt(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	s := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	for i := range s.Buffer {
		s.Buffer[i] = make([]rune, 80)
	}
	s.Fields = append(s.Fields,
		host.NewField(s, 0x00, 1, 1, 1, 1, 0, 0),
		host.NewField(s, 0x00, 10, 1, 10, 1, 0, 0),
		host.NewField(s, 0x00, 20, 1, 20, 1, 0, 0),
	)
	h.Screen = s
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 1
	cfg.StepDelay = 0
	cfg.Seed = 2026
	cfg.ExcludeNoProgressEvents = false
	cfg.ForceOverrideExistingInputs = false

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := e.Status()
	if st.LastAttempt == nil {
		t.Fatal("expected LastAttempt to be populated")
	}
	if st.LastAttempt.FieldsTargeted != 1 {
		t.Fatalf("LastAttempt.FieldsTargeted = %d, want 1", st.LastAttempt.FieldsTargeted)
	}
	if len(st.LastAttempt.FieldWrites) > 1 {
		t.Fatalf("LastAttempt.FieldWrites len = %d, want <= 1", len(st.LastAttempt.FieldWrites))
	}
	writeCount := 0
	for _, cmd := range h.Commands {
		if cmd == "write" {
			writeCount++
		}
	}
	if writeCount != 1 {
		t.Fatalf("write command count = %d, want 1", writeCount)
	}
}

func TestEngineStatusIncludesMindMap(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Screen = buildMockScreen()
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 2
	cfg.StepDelay = 0
	cfg.Seed = 456
	cfg.ExcludeNoProgressEvents = false

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := e.Status()
	if st.MindMap == nil {
		t.Fatal("expected MindMap in status")
	}
	if len(st.MindMap.Areas) == 0 {
		t.Fatal("expected at least one area in mind map")
	}
	foundKeyPress := false
	foundFieldLearning := false
	for _, area := range st.MindMap.Areas {
		if area == nil {
			continue
		}
		if len(area.KeyPresses) > 0 {
			foundKeyPress = true
		}
		if len(area.KnownTriedValues) > 0 || len(area.FieldDiscovery) > 0 {
			foundFieldLearning = true
		}
		for _, values := range area.KnownWorkingValues {
			if len(values) > 0 {
				foundFieldLearning = true
				break
			}
		}
	}
	if !foundKeyPress {
		t.Error("mind map should include key press metadata")
	}
	if !foundFieldLearning {
		t.Error("mind map should include field discovery or learned values")
	}
}

func TestMindMapRecordAttempt_TracksSingleVsMultiFieldProgressions(t *testing.T) {
	m := newMindMap()
	m.recordAttempt(Attempt{
		FromHash:       "screen-a",
		ToHash:         "screen-b",
		AIDKey:         "Enter",
		Transitioned:   true,
		Time:           time.Now(),
		FieldWrites:    []AttemptFieldWrite{{Row: 1, Column: 1, Length: 4, Value: "ABCD", Success: true}},
		FieldsTargeted: 1,
	})
	m.recordAttempt(Attempt{
		FromHash:     "screen-a",
		ToHash:       "screen-c",
		AIDKey:       "PF(8)",
		Transitioned: true,
		Time:         time.Now(),
		FieldWrites: []AttemptFieldWrite{
			{Row: 1, Column: 1, Length: 4, Value: "ABCD", Success: true},
			{Row: 2, Column: 1, Length: 4, Value: "EFGH", Success: true},
		},
		FieldsTargeted: 2,
	})

	area := m.Areas["screen-a"]
	if area == nil {
		t.Fatal("expected area to be created")
	}
	if area.SingleFieldProgressions != 1 || area.MultiFieldProgressions != 1 {
		t.Fatalf("unexpected field progression counters: single=%d multi=%d", area.SingleFieldProgressions, area.MultiFieldProgressions)
	}
	if area.FieldCountProgressions[1] != 1 || area.FieldCountProgressions[2] != 1 {
		t.Fatalf("unexpected per-field-count progressions: %v", area.FieldCountProgressions)
	}
	if area.KeyPresses["Enter"] == nil || area.KeyPresses["Enter"].SingleFieldProgressions != 1 {
		t.Fatalf("expected Enter single-field progression tracking, got %+v", area.KeyPresses["Enter"])
	}
	if area.KeyPresses["PF(8)"] == nil || area.KeyPresses["PF(8)"].MultiFieldProgressions != 1 {
		t.Fatalf("expected PF(8) multi-field progression tracking, got %+v", area.KeyPresses["PF(8)"])
	}
}

func TestMindMapRecordAttempt_UsesFieldsTargetedForStrategyLearning(t *testing.T) {
	m := newMindMap()
	m.recordAttempt(Attempt{
		FromHash:       "screen-a",
		ToHash:         "screen-b",
		AIDKey:         "Enter",
		Transitioned:   true,
		Time:           time.Now(),
		FieldWrites:    []AttemptFieldWrite{{Row: 1, Column: 1, Length: 4, Value: "ABCD", Success: true}},
		FieldsTargeted: 3,
	})

	area := m.Areas["screen-a"]
	if area == nil {
		t.Fatal("expected area to be created")
	}
	if area.FieldCountProgressions[3] != 1 {
		t.Fatalf("expected progression to be tracked for targeted count 3, got %v", area.FieldCountProgressions)
	}
	if area.FieldCountProgressions[1] != 0 {
		t.Fatalf("did not expect progression to be tracked as single-field, got %v", area.FieldCountProgressions)
	}
	if area.KeyPresses["Enter"] == nil || area.KeyPresses["Enter"].MultiFieldProgressions != 1 {
		t.Fatalf("expected Enter multi-field progression tracking, got %+v", area.KeyPresses["Enter"])
	}
}

func TestEngineStatusFiltersNoProgressAttemptsByDefault(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	s := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	for i := range s.Buffer {
		s.Buffer[i] = make([]rune, 80)
	}
	// No unprotected fields: each attempt should result in no progression.
	s.Fields = append(s.Fields, host.NewField(s, host.AttrProtected, 0, 0, 79, 0, 0, 0))
	h.Screen = s
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 2
	cfg.StepDelay = 0
	cfg.Seed = 321
	// ExcludeNoProgressEvents defaults to true.

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := e.Status()
	if st.StepsRun == 0 {
		t.Fatalf("StepsRun = %d, want > 0", st.StepsRun)
	}
	if len(st.RecentAttempts) != 0 {
		t.Fatalf("RecentAttempts len = %d, want 0 when all attempts have no progression", len(st.RecentAttempts))
	}
	if st.LastAttempt != nil {
		t.Fatal("LastAttempt should be nil when no-progress attempts are filtered")
	}
}

func TestSnapshotAndResume(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Screen = buildMockScreen()
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 2
	cfg.StepDelay = 0
	cfg.Seed = 13

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	snap := e.Snapshot("test-run-1")
	if snap.ID != "test-run-1" {
		t.Errorf("snapshot ID = %q, want %q", snap.ID, "test-run-1")
	}
	if snap.StepsRun == 0 {
		t.Error("snapshot StepsRun should be > 0")
	}
	if len(snap.ScreenHashes) == 0 {
		t.Error("snapshot ScreenHashes should be populated")
	}
	if snap.MindMap == nil || len(snap.MindMap.Areas) == 0 {
		t.Error("snapshot MindMap should be populated")
	}

	// Resume from snapshot on a fresh engine with a higher MaxSteps so that
	// at least 2 new steps are run beyond the original count.
	cfg2 := DefaultConfig()
	cfg2.MaxSteps = snap.StepsRun + 2
	cfg2.StepDelay = 0
	cfg2.Seed = 99
	e2 := New(h, cfg2)

	if err := e2.Resume(snap); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e2.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st2 := e2.Status()
	if st2.LoadedRunID != "test-run-1" {
		t.Errorf("resumed engine LoadedRunID = %q, want %q", st2.LoadedRunID, "test-run-1")
	}
	// Total steps should include those from the original run.
	if st2.StepsRun <= snap.StepsRun {
		t.Errorf("resumed engine StepsRun (%d) should exceed original (%d)", st2.StepsRun, snap.StepsRun)
	}
	// Screen hashes should include those from the original run.
	if st2.UniqueScreens < snap.UniqueScreens {
		t.Errorf("resumed engine UniqueScreens (%d) less than original (%d)", st2.UniqueScreens, snap.UniqueScreens)
	}
	if st2.MindMap == nil || len(st2.MindMap.Areas) == 0 {
		t.Error("resumed engine MindMap should be populated")
	}
}

func TestResumeNilSavedRun(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Screen = buildMockScreen()
	h.Connected = true

	e := New(h, DefaultConfig())
	if err := e.Resume(nil); err == nil {
		t.Fatal("Resume(nil) should return an error")
	}
}

// TestResumeThenTransitionDoesNotPanic guards against a regression where
// Resume() (unlike Start()) left transitionTuples/productiveValues nil.
// The HTTP resume path builds a fresh Engine via New() and calls Resume()
// directly, so the first screen transition after a resume wrote to those
// nil maps and panicked inside the run() goroutine, which has no recover
// and crashes the process.
func TestResumeThenTransitionDoesNotPanic(t *testing.T) {
	screenA := buildScriptedChaosScreen("SCREEN A", true)
	screenB := buildScriptedChaosScreen("SCREEN B", true)
	h := &scriptedChaosHost{
		screens:   []*host.Screen{screenA, screenB},
		connected: true,
	}

	cfg := DefaultConfig()
	cfg.MaxSteps = 1
	cfg.StepDelay = 0
	cfg.Seed = 7
	// Exact dedup so "SCREEN A" -> "SCREEN B" is observed as a real
	// transition instead of being canonicalized to the same structural hash.
	cfg.DedupMode = DedupModeExact

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	snap := e.Snapshot("resume-transition-test")

	// Resume on a FRESH engine built via New() only (mirroring the HTTP
	// resume path, which never calls Start() first) and let it run far
	// enough to force at least one more screen transition.
	h.index = 0
	cfg2 := DefaultConfig()
	cfg2.MaxSteps = snap.StepsRun + 3
	cfg2.StepDelay = 0
	cfg2.Seed = 11
	cfg2.DedupMode = DedupModeExact

	e2 := New(h, cfg2)
	if err := e2.Resume(snap); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e2.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := e2.Status()
	if st.Active {
		t.Fatal("resumed engine still active after deadline")
	}
	if st.Transitions == 0 {
		t.Fatal("expected at least one transition to be recorded after resume")
	}
}

func TestStopIsIdempotent(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Screen = buildMockScreen()
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 0
	cfg.StepDelay = 100 * time.Millisecond
	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	e.Stop()
	e.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("engine should stop after Stop()")
}

func TestPersistenceRoundtrip(t *testing.T) {
	dir := t.TempDir()

	run := &SavedRun{
		SavedRunMeta: SavedRunMeta{
			ID:            "20240101-000000-ab",
			StartedAt:     time.Now().Add(-time.Minute),
			StoppedAt:     time.Now(),
			StepsRun:      5,
			Transitions:   2,
			UniqueScreens: 3,
			UniqueInputs:  4,
		},
		ScreenHashes: map[string]bool{"abc": true, "def": true},
		AIDKeyCounts: map[string]int{"Enter": 4, "PF(1)": 1},
		MindMap: &MindMap{
			Areas: map[string]*MindMapArea{
				"abc": {
					Hash:               "abc",
					Label:              "Sample Area",
					Visits:             2,
					KnownWorkingValues: map[string][]string{"R1C1L4": []string{"CEMT"}},
					KeyPresses: map[string]*MindMapKeyPress{
						"Enter": {
							Presses:      2,
							Progressions: 1,
							Destinations: map[string]int{"def": 1},
						},
					},
				},
			},
		},
	}

	if err := SaveRun(dir, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	metas, err := ListRuns(dir)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("ListRuns: want 1 entry, got %d", len(metas))
	}
	if metas[0].ID != run.ID {
		t.Errorf("ListRuns ID = %q, want %q", metas[0].ID, run.ID)
	}

	loaded, err := LoadRun(dir, run.ID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if loaded.StepsRun != run.StepsRun {
		t.Errorf("loaded StepsRun = %d, want %d", loaded.StepsRun, run.StepsRun)
	}
	if len(loaded.ScreenHashes) != len(run.ScreenHashes) {
		t.Errorf("loaded ScreenHashes len = %d, want %d", len(loaded.ScreenHashes), len(run.ScreenHashes))
	}
	if loaded.MindMap == nil || len(loaded.MindMap.Areas) == 0 {
		t.Fatal("loaded MindMap should be populated")
	}
	area := loaded.MindMap.Areas["abc"]
	if area == nil {
		t.Fatal("loaded MindMap missing area abc")
	}
	if presses := area.KeyPresses["Enter"].Presses; presses != 2 {
		t.Fatalf("loaded MindMap Enter presses = %d, want 2", presses)
	}
}

func TestListRuns_NonExistentDir(t *testing.T) {
	metas, err := ListRuns("/tmp/nonexistent-chaos-dir-xyz-999")
	if err != nil {
		t.Errorf("ListRuns non-existent dir should not error, got: %v", err)
	}
	if metas != nil {
		t.Errorf("expected nil slice for non-existent dir, got %v", metas)
	}
}

// TestChooseAIDKeyBoosted_Determinism verifies that chooseAIDKeyBoosted
// produces the same sequence for the same seed, even when boosts are applied.
func TestChooseAIDKeyBoosted_Determinism(t *testing.T) {
	cfg := DefaultConfig()
	boosts := map[string]int{"PF(1)": 100}

	makeSequence := func(seed int64, n int) []string {
		e := New(nil, cfg)
		e.cfg.Seed = seed
		e.rng = rand.New(rand.NewSource(seed)) //nolint:gosec
		seq := make([]string, n)
		for i := range seq {
			seq[i] = e.chooseAIDKeyBoosted(boosts)
		}
		return seq
	}

	s1 := makeSequence(42, 20)
	s2 := makeSequence(42, 20)
	for i := range s1 {
		if s1[i] != s2[i] {
			t.Fatalf("chooseAIDKeyBoosted not deterministic: position %d: %q vs %q", i, s1[i], s2[i])
		}
	}

	// With a large boost on PF(1) it should be chosen more than Enter.
	counts := make(map[string]int)
	e := New(nil, cfg)
	e.rng = rand.New(rand.NewSource(7)) //nolint:gosec
	for i := 0; i < 500; i++ {
		counts[e.chooseAIDKeyBoosted(boosts)]++
	}
	// PF(1) base weight=5, boost=100 → effective 105. Enter has weight=70.
	// PF(1) should be chosen more often than Enter.
	if counts["PF(1)"] <= counts["Enter"] {
		t.Errorf("boosted PF(1) (%d) should exceed Enter (%d) with boost=100", counts["PF(1)"], counts["Enter"])
	}
}

// TestGenerateValueForFieldWith_PrefersKnownValues verifies that known working
// values supplied via knownValues are preferred over random generation.
func TestGenerateValueForFieldWith_PrefersKnownValues(t *testing.T) {
	s := &host.Screen{Width: 80, Height: 24}
	f := host.NewField(s, 0x00, 0, 0, 7, 0, 0, 0) // col 0-7, length 8

	cfg := DefaultConfig()
	cfg.MaxFieldLength = 10
	e := New(nil, cfg)
	e.rng = rand.New(rand.NewSource(3)) //nolint:gosec

	// The engine converts 0-based field coordinates to 1-based when computing
	// the MindMap key (StartY+1, StartX+1), so a field at (row=0,col=0,len=8)
	// is keyed as "R1C1L8".
	knownValues := map[string][]string{
		mindMapFieldKey(1, 1, 8): {"SIGNONX"},
	}

	hitKnown := 0
	const tries = 50
	for i := 0; i < tries; i++ {
		v := e.generateValueForFieldWith(f, false, knownValues, nil, nil)
		if v == "SIGNONX" {
			hitKnown++
		}
	}
	// Expect the known value to be returned ≥ 80% × 50 = 40 times.
	if hitKnown < 35 {
		t.Errorf("known value used %d/%d times, want ≥35 (80%% rate)", hitKnown, tries)
	}
}

func TestGenerateValueForFieldWith_PrefersRicherKnownPools(t *testing.T) {
	s := &host.Screen{Width: 80, Height: 24}
	f := host.NewField(s, 0x00, 0, 0, 7, 0, 0, 0) // col 0-7, length 8

	cfg := DefaultConfig()
	cfg.MaxFieldLength = 10
	one := New(nil, cfg)
	rich := New(nil, cfg)
	one.rng = rand.New(rand.NewSource(41))  //nolint:gosec
	rich.rng = rand.New(rand.NewSource(41)) //nolint:gosec

	key := mindMapFieldKey(1, 1, 8)
	singlePool := map[string][]string{key: {"SIGNON1"}}
	richPool := map[string][]string{key: {"SIGNON1", "SIGNON2", "SIGNON3", "SIGNON4", "SIGNON5"}}
	richSet := map[string]struct{}{
		"SIGNON1": {},
		"SIGNON2": {},
		"SIGNON3": {},
		"SIGNON4": {},
		"SIGNON5": {},
	}

	const tries = 400
	singleHits := 0
	richHits := 0
	for i := 0; i < tries; i++ {
		if v := one.generateValueForFieldWith(f, false, singlePool, nil, nil); v == "SIGNON1" {
			singleHits++
		}
		if v := rich.generateValueForFieldWith(f, false, richPool, nil, nil); v != "" {
			if _, ok := richSet[v]; ok {
				richHits++
			}
		}
	}
	if richHits <= singleHits {
		t.Fatalf("expected richer known pool reuse to exceed single-value pool reuse, single=%d rich=%d", singleHits, richHits)
	}
}

// TestSnapshotAreaValuesLocked verifies that snapshotAreaValuesLocked returns
// a deep copy of the known working values for a given screen hash.
func TestSnapshotAreaValuesLocked(t *testing.T) {
	e := New(nil, DefaultConfig())
	e.mindMap = newMindMap()
	area := e.mindMap.ensureArea("aabbccdd")
	area.KnownWorkingValues["R1C1L4"] = []string{"CEMT", "CICS"}

	snap := e.snapshotAreaValuesLocked("aabbccdd")
	if snap == nil {
		t.Fatal("snapshotAreaValuesLocked returned nil for populated area")
	}
	if len(snap["R1C1L4"]) != 2 {
		t.Fatalf("snapshot values len = %d, want 2", len(snap["R1C1L4"]))
	}
	// Mutating the snapshot must not affect the MindMap.
	snap["R1C1L4"][0] = "MODIFIED"
	if area.KnownWorkingValues["R1C1L4"][0] != "CEMT" {
		t.Error("snapshot mutation leaked into MindMap")
	}
}

// TestSnapshotKeyBoostsLocked verifies that snapshotKeyBoostsLocked scales
// boosts proportionally to observed progressions and applies penalties to keys
// that have been pressed many times without causing any transition.
func TestSnapshotKeyBoostsLocked(t *testing.T) {
	e := New(nil, DefaultConfig())
	e.mindMap = newMindMap()
	area := e.mindMap.ensureArea("hash1")
	area.KeyPresses["Enter"] = &MindMapKeyPress{Presses: 5, Progressions: 3}
	// PF(3): 2 presses, 0 progressions → below penalty threshold, no boost/penalty.
	area.KeyPresses["PF(3)"] = &MindMapKeyPress{Presses: 2, Progressions: 0}
	// Clear: many presses, 0 progressions → should receive a penalty.
	area.KeyPresses["Clear"] = &MindMapKeyPress{Presses: minPressesForPenalty + 3, Progressions: 0}

	boosts := e.snapshotKeyBoostsLocked("hash1", 0)
	if boosts == nil {
		t.Fatal("snapshotKeyBoostsLocked returned nil when progressions exist")
	}
	if boosts["Enter"] != 30 {
		t.Errorf("Enter boost = %d, want 30 (3 progressions × 10)", boosts["Enter"])
	}
	if _, ok := boosts["PF(3)"]; ok {
		t.Error("PF(3) should not appear in boosts when progressions = 0 and presses below penalty threshold")
	}
	// Clear has been pressed >= minPressesForPenalty times with 0 progressions.
	if boosts["Clear"] >= 0 {
		t.Errorf("Clear should have a negative boost (penalty) after %d presses with no progression, got %d",
			minPressesForPenalty+3, boosts["Clear"])
	}
}

func TestSnapshotKeyBoostsLocked_PrefersKeyForCurrentFieldStrategy(t *testing.T) {
	e := New(nil, DefaultConfig())
	e.mindMap = newMindMap()
	area := e.mindMap.ensureArea("hash-strategy")
	area.KeyPresses["PF(8)"] = &MindMapKeyPress{
		Presses:                 10,
		Progressions:            4,
		SingleFieldProgressions: 1,
		MultiFieldProgressions:  4,
	}
	area.KeyPresses["Enter"] = &MindMapKeyPress{
		Presses:                 10,
		Progressions:            4,
		SingleFieldProgressions: 4,
		MultiFieldProgressions:  1,
	}

	multiBoosts := e.snapshotKeyBoostsLocked("hash-strategy", 3)
	singleBoosts := e.snapshotKeyBoostsLocked("hash-strategy", 1)

	if multiBoosts["PF(8)"] <= multiBoosts["Enter"] {
		t.Fatalf("expected PF(8) to be preferred for multi-field attempts, boosts=%v", multiBoosts)
	}
	if singleBoosts["Enter"] <= singleBoosts["PF(8)"] {
		t.Fatalf("expected Enter to be preferred for single-field attempts, boosts=%v", singleBoosts)
	}
}

func TestSnapshotKeyBoostsLocked_PrefersHigherStrategyConversionRate(t *testing.T) {
	e := New(nil, DefaultConfig())
	e.mindMap = newMindMap()
	area := e.mindMap.ensureArea("hash-rate")
	area.KeyPresses["Enter"] = &MindMapKeyPress{
		Presses:                 20,
		Progressions:            2,
		SingleFieldProgressions: 2,
	}
	area.KeyPresses["PF(8)"] = &MindMapKeyPress{
		Presses:                 2,
		Progressions:            2,
		SingleFieldProgressions: 2,
	}

	boosts := e.snapshotKeyBoostsLocked("hash-rate", 1)
	if boosts["PF(8)"] <= boosts["Enter"] {
		t.Fatalf("expected PF(8) to be preferred with higher single-field conversion rate, boosts=%v", boosts)
	}
}

// TestChooseAIDKeyBoosted_Clamp verifies that a large negative boost is clamped
// to a minimum effective weight of 1, keeping the penalised key selectable for
// exploration breadth rather than silently excluding it.
func TestChooseAIDKeyBoosted_Clamp(t *testing.T) {
	cfg := DefaultConfig()
	e := New(nil, cfg)
	e.rng = rand.New(rand.NewSource(42)) //nolint:gosec

	// Apply a large negative boost to Clear (config weight 5 − 1000 → clamped to 1).
	boosts := map[string]int{"Clear": -1000}
	counts := make(map[string]int)
	for i := 0; i < 1000; i++ {
		counts[e.chooseAIDKeyBoosted(boosts)]++
	}
	// Clear must still be selected occasionally (clamped to min weight 1, not zero).
	if counts["Clear"] == 0 {
		t.Error("Clear should remain selectable even with a large negative boost")
	}
	// Clear's effective weight is 1 vs Enter's 70 → Clear should be chosen far less.
	if counts["Clear"] >= counts["Enter"] {
		t.Errorf("penalised Clear (%d) should be chosen much less than Enter (%d)", counts["Clear"], counts["Enter"])
	}
}

// TestGenerateValue_TextCharset verifies that random values generated for
// non-numeric text fields contain only uppercase letters, digits, and spaces.
// 3270 mainframe applications use uppercase for commands and transaction codes;
// generating lowercase characters wastes exploration entropy.
func TestGenerateValue_TextCharset(t *testing.T) {
	s := &host.Screen{Width: 80, Height: 24}
	// Non-numeric field spanning col 0-19 (length 20).
	f := host.NewField(s, 0x00, 0, 0, 19, 0, 0, 0)

	cfg := DefaultConfig()
	e := New(nil, cfg)
	e.rng = rand.New(rand.NewSource(55)) //nolint:gosec

	for i := 0; i < 200; i++ {
		v := e.generateValue(f)
		for _, c := range v {
			if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == ' ') {
				t.Errorf("generateValue produced non-uppercase/digit/space character %q in %q", c, v)
				return
			}
		}
	}
}

// TestGenerateValue_NoLeadingSpace verifies that random text values never start
// with a space character.  3270 command and transaction-code fields reject
// leading whitespace, so generating values with a leading space wastes
// exploration steps.
func TestGenerateValue_NoLeadingSpace(t *testing.T) {
	s := &host.Screen{Width: 80, Height: 24}
	// Non-numeric field spanning col 0-19 (length 20).
	f := host.NewField(s, 0x00, 0, 0, 19, 0, 0, 0)

	cfg := DefaultConfig()
	e := New(nil, cfg)
	e.rng = rand.New(rand.NewSource(77)) //nolint:gosec

	for i := 0; i < 500; i++ {
		v := e.generateValue(f)
		if len(v) == 0 {
			t.Fatal("generateValue returned empty string")
		}
		if v[0] == ' ' {
			t.Errorf("generateValue returned a value with a leading space: %q", v)
			return
		}
	}
}

// TestSnapshotKeyBoostsLocked_ProgressionCap verifies that the progression
// boost is capped at maxProgressionBoostFactor so that a single successful AID
// key cannot monopolise selection after many transitions from the same screen.
func TestSnapshotKeyBoostsLocked_ProgressionCap(t *testing.T) {
	e := New(nil, DefaultConfig())
	e.mindMap = newMindMap()
	area := e.mindMap.ensureArea("hashcap")
	// Progression count far above the cap.
	area.KeyPresses["Enter"] = &MindMapKeyPress{Presses: 100, Progressions: 100}

	boosts := e.snapshotKeyBoostsLocked("hashcap", 0)
	if boosts == nil {
		t.Fatal("snapshotKeyBoostsLocked returned nil when progressions exist")
	}
	want := maxProgressionBoostFactor * 10
	if boosts["Enter"] != want {
		t.Errorf("Enter boost = %d, want %d (capped at maxProgressionBoostFactor × 10)", boosts["Enter"], want)
	}
}

// the oldest entry is evicted to make room for new unique values, ensuring the
// engine keeps learning throughout the run.
func TestAppendUniqueLimited_SlidingWindow(t *testing.T) {
	// Fill up to the cap with distinct values.
	cap := 3
	var values []string
	values = appendUniqueLimited(values, "A", cap)
	values = appendUniqueLimited(values, "B", cap)
	values = appendUniqueLimited(values, "C", cap)
	if len(values) != 3 {
		t.Fatalf("expected 3 values after filling cap, got %d", len(values))
	}

	// Adding a new unique value must evict the oldest ("A") and append the new one.
	values = appendUniqueLimited(values, "D", cap)
	if len(values) != 3 {
		t.Fatalf("expected length to remain %d after eviction, got %d", cap, len(values))
	}
	if values[0] != "B" || values[1] != "C" || values[2] != "D" {
		t.Errorf("sliding window incorrect: got %v, want [B C D]", values)
	}

	// Appending a duplicate must not change the slice.
	values = appendUniqueLimited(values, "C", cap)
	if len(values) != 3 || values[1] != "C" {
		t.Errorf("duplicate should not alter the slice: got %v", values)
	}
}

// TestSimilarityRatio_SurvivesSingleInsertedCharacter guards against a
// positional character compare, which a single inserted/removed character
// anywhere in the string defeats: everything after the insertion point shifts
// out of alignment and reads as a mismatch, even though the two signatures
// are one edit apart and represent the same screen (e.g. a counter field
// gaining a digit). An edit-distance-based ratio must still score this near 1.
func TestSimilarityRatio_SurvivesSingleInsertedCharacter(t *testing.T) {
	a := "JOB12345 STATUS=RUNNING STEP=COMPILE ELAPSED=00:01:23"
	// Same string with a single character ("9") inserted mid-string.
	b := "JOB12345 STATUS=RUNNING STEP=COMPILE9 ELAPSED=00:01:23"

	got := similarityRatio(a, b)
	if got < 0.95 {
		t.Errorf("similarityRatio(%q, %q) = %f, want >= 0.95 (one edit apart)", a, b, got)
	}

	// Sanity: genuinely different strings of the same length should NOT
	// score highly.
	c := "COMPLETELY DIFFERENT SCREEN TEXT WITH NO OVERLAP AT ALL HERE"
	d := "ANOTHER WHOLLY UNRELATED PIECE OF CONTENT SHARING NO SUBSTRING"
	if got := similarityRatio(c, d); got > 0.5 {
		t.Errorf("similarityRatio(%q, %q) = %f, want a low score for unrelated strings", c, d, got)
	}

	if got := similarityRatio(a, a); got != 1 {
		t.Errorf("similarityRatio of identical strings = %f, want 1", got)
	}
}

func TestLevenshteinDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3},
		{"abcdef", "abcXdef", 1}, // single insertion
		{"abcXdef", "abcdef", 1}, // single deletion
	}
	for _, tc := range cases {
		got := levenshteinDistance([]rune(tc.a), []rune(tc.b))
		if got != tc.want {
			t.Errorf("levenshteinDistance(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestTransitionsAreDedupedByTuple guards against unbounded memory growth on
// a long (or many-times-resumed) chaos run: bouncing between the same two
// screens with the same AID key forever used to append a new Transition
// (each carrying its own copy of the step data) every single time that exact
// edge was re-traversed, so e.transitions grew with the total steps ever
// taken instead of with the size of the discovered state graph. It must
// instead record each distinct (from, to, aidKey) tuple once.
func TestTransitionsAreDedupedByTuple(t *testing.T) {
	a := buildScriptedChaosScreen("FIRST SCREEN A", false)
	b := buildScriptedChaosScreen("SECOND SCREEN B", false)
	h := &loopingChaosHost{screens: []*host.Screen{a, b}, connected: true}

	cfg := DefaultConfig()
	// Exact dedup mode so these two screens are treated as genuinely
	// distinct areas instead of collapsing into one under the default
	// structural mode (which masks away plain label text with no fields).
	cfg.DedupMode = DedupModeExact
	cfg.MaxSteps = 300
	cfg.TimeBudget = 10 * time.Second
	cfg.StepDelay = 0
	cfg.Seed = 1
	cfg.SaturationSteps = 0 // don't let saturation cut the run short
	cfg.AIDKeyWeights = map[string]int{"Enter": 1}
	cfg.AutoBlockExitKeys = false

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForChaosStop(t, e, 5*time.Second)

	e.mu.Lock()
	stepsRun := e.stepsRun
	transitionCount := len(e.transitions)
	e.mu.Unlock()

	if stepsRun < 100 {
		t.Fatalf("stepsRun = %d, want a long run (>=100) for this to be a meaningful check", stepsRun)
	}
	// Only two edges are ever possible here: A->B and B->A on "Enter".
	if transitionCount != 2 {
		t.Fatalf("len(transitions) = %d after %d steps, want exactly 2 (deduped) — transitions must not grow with every repeat of an already-seen edge", transitionCount, stepsRun)
	}
}

// TestStepsLogBoundedAcrossLongRun guards the same memory-growth issue for
// e.steps, the flat unconditional step log kept only as a last-resort export
// fallback (see ExportWorkflowWithSuccessBalance): it used to grow with the
// total steps ever taken across a run, unboundedly. A trailing window must
// cap it regardless of how long the run goes.
func TestStepsLogBoundedAcrossLongRun(t *testing.T) {
	a := buildScriptedChaosScreen("FIRST SCREEN A", false)
	b := buildScriptedChaosScreen("SECOND SCREEN B", false)
	h := &loopingChaosHost{screens: []*host.Screen{a, b}, connected: true}

	cfg := DefaultConfig()
	cfg.DedupMode = DedupModeExact
	cfg.MaxSteps = maxRecentRawSteps + 100
	cfg.TimeBudget = 15 * time.Second
	cfg.StepDelay = 0
	cfg.Seed = 1
	cfg.SaturationSteps = 0
	cfg.AIDKeyWeights = map[string]int{"Enter": 1}
	cfg.AutoBlockExitKeys = false

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForChaosStop(t, e, 10*time.Second)

	e.mu.Lock()
	stepsRun := e.stepsRun
	stepsLogLen := len(e.steps)
	e.mu.Unlock()

	if stepsRun <= maxRecentRawSteps {
		t.Fatalf("stepsRun = %d, want more than maxRecentRawSteps (%d) for this to be a meaningful check", stepsRun, maxRecentRawSteps)
	}
	if stepsLogLen > maxRecentRawSteps {
		t.Fatalf("len(e.steps) = %d after %d steps, want <= maxRecentRawSteps (%d)", stepsLogLen, stepsRun, maxRecentRawSteps)
	}
	if stepsLogLen == 0 {
		t.Fatalf("len(e.steps) = 0; the fallback export log must still have content after a long run")
	}
}

// TestTransitionDedupSurvivesResume guards the same dedup fix across a
// Resume: resetSaturationStateLocked (called by both Start and Resume)
// clears transitionTuples, so without rebuilding it from the restored
// e.transitions, an edge discovered before a resume would look "new" again
// the next time it's re-traversed, appending one more duplicate Transition
// entry per resume cycle instead of staying capped at one entry per edge for
// the run's whole life (across as many resumes as an "extend limits" loop
// performs).
func TestTransitionDedupSurvivesResume(t *testing.T) {
	a := buildScriptedChaosScreen("FIRST SCREEN A", false)
	b := buildScriptedChaosScreen("SECOND SCREEN B", false)
	h := &loopingChaosHost{screens: []*host.Screen{a, b}, connected: true}

	cfg := DefaultConfig()
	cfg.DedupMode = DedupModeExact
	cfg.MaxSteps = 10
	cfg.TimeBudget = 10 * time.Second
	cfg.StepDelay = 0
	cfg.Seed = 1
	cfg.SaturationSteps = 0
	cfg.AIDKeyWeights = map[string]int{"Enter": 1}
	cfg.AutoBlockExitKeys = false

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForChaosStop(t, e, 5*time.Second)
	if got := len(e.transitions); got != 2 {
		t.Fatalf("after first run: len(transitions) = %d, want 2", got)
	}
	snap := e.Snapshot("test-run")

	// Resume several times on fresh engines seeded from the prior snapshot,
	// re-traversing the same two already-known edges each time.
	for i := 0; i < 3; i++ {
		h2 := &loopingChaosHost{screens: []*host.Screen{a, b}, connected: true, index: h.index}
		cfg2 := cfg
		cfg2.MaxSteps = snap.StepsRun + 10
		e2 := New(h2, cfg2)
		if err := e2.Resume(snap); err != nil {
			t.Fatalf("Resume iteration %d: %v", i, err)
		}
		waitForChaosStop(t, e2, 5*time.Second)
		if got := len(e2.transitions); got != 2 {
			t.Fatalf("after resume iteration %d: len(transitions) = %d, want 2 (deduped across the resume boundary too)", i, got)
		}
		snap = e2.Snapshot("test-run")
		h = h2
	}
}
