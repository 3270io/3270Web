// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// ---- defaults and legend handling ------------------------------------

func TestDefaultHintsCannotEndASessionOrMoveTheCursor(t *testing.T) {
	for _, hint := range defaultChaosHints() {
		for _, v := range hint.KnownData {
			if v == "LOGOFF" || v == "LOGON" || isDeniedTypedValue(v) {
				t.Errorf("default known data contains %q", v)
			}
		}
		for label, key := range hint.KeyAssignments {
			k := normalizeChaosKeyName(key)
			if k == "PF(3)" {
				t.Errorf("default label %q maps to PF3", label)
			}
			if !isAIDKeyName(k) {
				t.Errorf("default label %q maps to %q, which is not an attention key", label, k)
			}
			if labelIsDenied(label) {
				t.Errorf("default label %q is on the deny list", label)
			}
		}
	}
}

func legendScreen(body, legend string) *host.Screen {
	s := buildMockScreen()
	copy(s.Buffer[5], []rune(body))
	copy(s.Buffer[22], []rune(legend))
	return s
}

func TestHintKeyBoostsMatchWholeWordsInTheLegendOnly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AIDKeyWeights = map[string]int{"Enter": 70, "PF(8)": 5}
	cfg.Hints = []Hint{{KeyAssignments: map[string]string{"UP": "PF7", "FORWARD": "PF8"}}}
	e := New(nil, cfg)

	// "UP" inside "GROUP UPDATE", and FORWARD in body text, are not labels.
	if boosts := e.hintKeyBoostsForScreen(legendScreen("GROUP UPDATE  FORWARD PLANNING", "")); len(boosts) != 0 {
		t.Fatalf("body text produced boosts: %v", boosts)
	}
	if boosts := e.hintKeyBoostsForScreen(legendScreen("", "PF4=GROUPS  PF9=UPDATE")); len(boosts) != 0 {
		t.Fatalf("substring match produced boosts: %v", boosts)
	}
	boosts := e.hintKeyBoostsForScreen(legendScreen("", "PF7=UP  PF8=FORWARD"))
	if boosts["PF(7)"] <= 0 || boosts["PF(8)"] <= 0 {
		t.Fatalf("user mappings in the legend not boosted: %v", boosts)
	}
}

func TestBuiltInHintsNeverIntroduceAKey(t *testing.T) {
	cfg := DefaultConfig() // weights: Enter, PF1, PF2, PF4, PF12
	e := New(nil, cfg)
	boosts := e.hintKeyBoostsForScreen(legendScreen("", "PF1=HELP  PF7=PREV  PF8=NEXT  PF5=REFRESH"))
	for key := range boosts {
		if !containsString(e.candidateKeys, key) {
			t.Errorf("built-in hint boosted %s, which is not in AIDKeyWeights", key)
		}
	}
	if boosts["PF(1)"] <= 0 {
		t.Errorf("HELP -> PF1 is configured and should be boosted: %v", boosts)
	}

	legend := restrictBoostsToKeys(map[string]int{"PF(8)": 70, "PF(1)": 45, "Up": 50, "Tab": 50}, e.candidateKeys)
	if len(legend) != 1 || legend["PF(1)"] != 45 {
		t.Errorf("legend boosts not restricted to configured attention keys: %v", legend)
	}
}

func TestLegendDenyListIsNeverBoosted(t *testing.T) {
	for _, label := range []string{
		"Delete", "Purge", "Remove", "Confirm", "Commit", "Update", "Save", "Submit",
		"End", "Return", "Exit", "Sign off", "Signoff", "Logoff", "Quit", "Cancel",
		"Confirm order", "Save (ADMIN)", "Return to menu",
	} {
		if !labelIsDenied(label) {
			t.Errorf("%q should be on the deny list", label)
		}
		if b := chaosHelpLabelBoost(label); b > 0 {
			t.Errorf("chaosHelpLabelBoost(%q) = %d, want <= 0", label, b)
		}
	}
	for _, label := range []string{"Pending", "Sender", "Updates pending", "Reorder report", "Frobnicate"} {
		if labelIsExitStyle(label) {
			t.Errorf("%q is not an exit label", label)
		}
	}
	if b := chaosHelpLabelBoost("Frobnicate"); b != 0 {
		t.Errorf("unknown label boost = %d, want 0", b)
	}
	if b := chaosHelpLabelBoost("Help"); b <= 0 {
		t.Errorf("Help boost = %d, want > 0", b)
	}

	s := legendScreen("", "PF3=End  PF4=Return  PF5=Save  PF6=Delete  PF8=Forward  Enter=Submit")
	exit := inferAutoBlockedKeys(s)
	if strings.Join(exit, ",") != "PF(3),PF(4)" {
		t.Errorf("exit keys = %v, want PF(3),PF(4)", exit)
	}
	destructive := inferDestructiveKeys(s)
	if strings.Join(destructive, ",") != "PF(5),PF(6)" {
		t.Errorf("destructive keys = %v, want PF(5),PF(6) (Enter is never blocked)", destructive)
	}
}

func TestAutoBlockedKeyStaysBlockedAndIsNotUntried(t *testing.T) {
	app := newFakeApp()
	cfg := fakeAppConfig(120)
	cfg.AIDKeyWeights = map[string]int{"Enter": 40, "PF(1)": 10, "PF(3)": 40, "PF(12)": 10}
	e, attempts := runFakeApp(t, app, cfg)

	for _, p := range app.pressList() {
		if p == "menu:PF(3)" {
			t.Fatalf("PF3 pressed on the main menu although its legend says Sign off")
		}
	}
	menu := areaByLabelPrefix(t, e.Status().MindMap, "APP100")
	if !containsString(menu.AutoBlockedKeys, "PF(3)") {
		t.Errorf("menu AutoBlockedKeys = %v, want PF(3)", menu.AutoBlockedKeys)
	}
	e.mu.Lock()
	untried := e.untriedKeysLocked(menu.Hash, e.mindMap.Areas[menu.Hash])
	e.mu.Unlock()
	if containsString(untried, "PF(3)") {
		t.Errorf("a blocked key is reported as untried: %v", untried)
	}
	if containsString(UntriedCandidateKeysExcluding(menu, e.CandidateKeys(), []string{"PF12"}), "PF(12)") {
		t.Errorf("UntriedCandidateKeysExcluding kept an excluded key")
	}
	for _, a := range attempts {
		if a.SessionEnded {
			t.Errorf("session ended on attempt %d (%s from %s)", a.Attempt, a.AIDKey, a.FromHash)
		}
	}
}

func TestUserHintLiftsADestructiveBlock(t *testing.T) {
	e := New(nil, DefaultConfig())
	e.mindMap = newMindMap()
	area := e.mindMap.ensureArea("a")
	area.AutoBlockedKeys = []string{"PF(5)"}
	if !isBlacklistedKeyInSet(e.blockedKeysForAreaLocked("a", nil), "PF(5)") {
		t.Fatal("auto-blocked key not in the blocked set")
	}
	e.screenHints = map[string]ScreenHint{"a": {KnownKeys: []string{"PF5"}}}
	if isBlacklistedKeyInSet(e.blockedKeysForAreaLocked("a", nil), "PF(5)") {
		t.Fatal("a key the user named for the screen is still auto-blocked")
	}
	e.blacklistedKeys = normalizeChaosKeySet([]string{"PF5"})
	if !isBlacklistedKeyInSet(e.blockedKeysForAreaLocked("a", nil), "PF(5)") {
		t.Fatal("a hint must not lift the global blacklist")
	}
}

func TestChooseAIDKeyBlockedResamplesInsteadOfFallingBackToEnter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Seed = 3
	cfg.AIDKeyWeights = map[string]int{"Enter": 1, "PF(1)": 1, "PF(2)": 1000}
	e := New(nil, cfg)
	blocked := normalizeChaosKeySet([]string{"PF2"})
	counts := map[string]int{}
	for i := 0; i < 400; i++ {
		counts[e.chooseAIDKeyBlocked(nil, nil, blocked)]++
	}
	if counts["PF(2)"] != 0 {
		t.Fatalf("blocked key chosen %d times", counts["PF(2)"])
	}
	if counts["PF(1)"] < 120 || counts["Enter"] < 120 {
		t.Fatalf("remaining keys should share the draw evenly, got %v", counts)
	}
	// A boost must not bring a blocked key back.
	if got := e.chooseAIDKeyBlocked(map[string]int{"PF(2)": 5000}, map[string]int{"PF(2)": 5000}, blocked); got == "PF(2)" {
		t.Fatal("boosted blocked key was chosen")
	}
}

// ---- session loss ----------------------------------------------------

func TestSessionLossIsDetectedAndTheKeyBlocked(t *testing.T) {
	app := newFakeApp()
	app.menuLegend = "PF1=Help   PF12=Menu" // nothing warns about PF3
	cfg := fakeAppConfig(150)
	cfg.AutoBlockExitKeys = false
	cfg.AIDKeyWeights = map[string]int{"Enter": 40, "PF(1)": 10, "PF(3)": 40, "PF(12)": 10}
	e, attempts := runFakeApp(t, app, cfg)

	ended := 0
	for _, a := range attempts {
		if a.SessionEnded {
			ended++
			if a.Outcome != OutcomeSessionEnded {
				t.Errorf("attempt %d outcome = %q", a.Attempt, a.Outcome)
			}
		}
	}
	if ended != 1 {
		t.Fatalf("session ended %d times, want exactly 1 (the key is blocked after the first)", ended)
	}
	st := e.Status()
	menu := areaByLabelPrefix(t, st.MindMap, "APP100")
	if !containsString(menu.SessionEndingKeys, "PF(3)") || !containsString(menu.AutoBlockedKeys, "PF(3)") {
		t.Errorf("menu session-ending keys = %v, auto-blocked = %v", menu.SessionEndingKeys, menu.AutoBlockedKeys)
	}
	kp := menu.KeyPresses["PF(3)"]
	if kp == nil || kp.SessionEnds != 1 || kp.Progressions != 0 || len(kp.Destinations) != 0 {
		t.Errorf("PF3 on the menu recorded as %+v, want one session end and no progression", kp)
	}
	// The run carried on after signing back on.
	if areaByLabelPrefix(t, st.MindMap, "APP200") == nil {
		t.Fatal("unreachable")
	}
}

func TestSessionEndingValueIsNeverLearnedAsWorking(t *testing.T) {
	mm := newMindMap()
	mm.recordAttempt(Attempt{
		FromHash: "menu", ToHash: "signon", AIDKey: "Enter", Transitioned: true, SessionEnded: true,
		FieldWrites: []AttemptFieldWrite{{Row: 21, Column: 14, Length: 60, Value: "OFF", Success: true}},
	})
	area := mm.Areas["menu"]
	if len(area.KnownWorkingValues["R21C14L60"]) != 0 {
		t.Errorf("session-ending value filed as working: %v", area.KnownWorkingValues)
	}
	if !containsString(area.SessionEndingValues["R21C14L60"], "OFF") {
		t.Errorf("SessionEndingValues = %v", area.SessionEndingValues)
	}
	if kp := area.KeyPresses["Enter"]; kp.Progressions != 0 || len(kp.Destinations) != 0 {
		t.Errorf("session end counted as a progression: %+v", kp)
	}

	// And the generator will not offer it again away from the first screen.
	e := New(nil, DefaultConfig())
	s := &host.Screen{Width: 80, Height: 24}
	f := host.NewField(s, 0x00, 13, 20, 72, 20, 0, 0)
	ctx := &genContext{
		known:         map[string][]string{"R21C14L60": {"OFF"}},
		sessionEnding: map[string]struct{}{"OFF": {}},
	}
	for i := 0; i < 200; i++ {
		if v := strings.TrimSpace(e.generateFieldValue(f, false, ctx)); v == "OFF" {
			t.Fatal("a value that ended the session was generated again")
		}
	}
}

// ---- field keys ------------------------------------------------------

func TestFieldWritesAreKeyedLikeFieldMetadata(t *testing.T) {
	app := newFakeApp()
	e, _ := runFakeApp(t, app, fakeAppConfig(60))
	mm := e.Status().MindMap
	checked := 0
	for _, area := range mm.Areas {
		for _, m := range []map[string][]string{area.KnownTriedValues, area.KnownWorkingValues, area.RejectedValues} {
			for key := range m {
				checked++
				if _, ok := area.FieldMetadata[key]; !ok {
					t.Errorf("area %s: learned-value key %s has no field metadata (%v)", area.Label, key, area.FieldMetadata)
				}
			}
		}
		for key := range area.FieldDiscovery {
			if _, ok := area.FieldMetadata[key]; !ok {
				t.Errorf("area %s: discovery key %s has no field metadata", area.Label, key)
			}
		}
	}
	if checked == 0 {
		t.Fatal("run learned no values to check")
	}
}

func TestOldRunFieldKeysAreRepaired(t *testing.T) {
	mm := newMindMap()
	area := mm.ensureArea("a")
	area.FieldMetadata["R11C20L8"] = MindMapFieldMetadata{Row: 11, Column: 20, Length: 8}
	area.KnownWorkingValues["R11C20L3"] = []string{"BOB"}
	area.KnownWorkingValues["R11C20L8"] = []string{"ALICEXYZ"}
	area.KnownTriedValues["R11C20L5"] = []string{"CAROL"}
	area.KnownTriedValues["R99C1L2"] = []string{"ZZ"} // no such field: left alone
	area.FieldDiscovery["R11C20L3"] = MindMapFieldDiscovery{Writes: 2, Progressions: 1, LastValue: "BOB"}

	normalizeMindMapFieldKeys(mm)

	if got := area.KnownWorkingValues["R11C20L8"]; len(got) != 2 {
		t.Errorf("working values under the real key = %v", got)
	}
	if _, stale := area.KnownWorkingValues["R11C20L3"]; stale {
		t.Error("stale key kept")
	}
	if !containsString(area.KnownTriedValues["R11C20L8"], "CAROL") {
		t.Errorf("tried values = %v", area.KnownTriedValues)
	}
	if !containsString(area.KnownTriedValues["R99C1L2"], "ZZ") {
		t.Error("entry with no matching field was dropped")
	}
	if fd := area.FieldDiscovery["R11C20L8"]; fd.Writes != 2 || fd.Progressions != 1 {
		t.Errorf("discovery not moved: %+v", fd)
	}
}

// ---- dedup -----------------------------------------------------------

func TestFoundingSignatureAndLabelAreFrozen(t *testing.T) {
	mm := newMindMap()
	first := buildScriptedChaosScreen("ORIGINAL TITLE", true)
	mm.observeScreen("a", first, time.Now())
	sig, label, preview := mm.Areas["a"].DedupSignature, mm.Areas["a"].Label, mm.Areas["a"].PreviewText

	other := buildScriptedChaosScreen("SOMETHING ELSE ENTIRELY", false)
	mm.observeScreen("a", other, time.Now())
	a := mm.Areas["a"]
	if a.DedupSignature != sig || a.Label != label || a.PreviewText != preview {
		t.Fatalf("area identity drifted: label %q -> %q", label, a.Label)
	}
	if a.Visits != 2 {
		t.Fatalf("visits = %d", a.Visits)
	}
}

func TestSimilarScreensDoNotChainMerge(t *testing.T) {
	// Each screen is within the threshold of the one before it, but the
	// last is far from the first. With a drifting signature all of them
	// collapsed into one area.
	cfg := DefaultConfig()
	cfg.ScreenDedupSimilarity = 0.99
	e := New(nil, cfg)
	e.mindMap = newMindMap()
	mk := func(extraRows int) *host.Screen {
		s := buildScriptedChaosScreen("LIST", false)
		for r := 0; r < extraRows; r++ {
			copy(s.Buffer[2+r], []rune("ROW--"+strings.Repeat("#", 12)))
		}
		return s
	}
	hashes := map[string]bool{}
	for i := 0; i <= 12; i++ {
		s := mk(i)
		h := e.canonicalizeObservedScreenHashLocked(hashScreen(s), s)
		e.mindMap.observeScreen(h, s, time.Now())
		hashes[h] = true
	}
	if len(hashes) < 3 {
		t.Fatalf("13 progressively different screens merged into %d area(s)", len(hashes))
	}
	// A revisit of a merged screen resolves through the alias map.
	s := mk(1)
	h1 := e.canonicalizeObservedScreenHashLocked(hashScreen(s), s)
	sigKey := signatureCanonicalKey(buildAreaDedupSignature(s))
	if h1 != sigKey {
		if e.sigAliases[sigKey] != h1 {
			t.Fatalf("merged signature not remembered as an alias")
		}
	}
}

func TestCappedLevenshteinAgreesWithTheFullOne(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	alphabet := []rune("AB9 -")
	var p, c []int
	for i := 0; i < 400; i++ {
		a := make([]rune, rng.Intn(60))
		for j := range a {
			a[j] = alphabet[rng.Intn(len(alphabet))]
		}
		b := append([]rune(nil), a...)
		for edits := rng.Intn(8); edits > 0 && len(b) > 0; edits-- {
			switch rng.Intn(3) {
			case 0:
				b[rng.Intn(len(b))] = alphabet[rng.Intn(len(alphabet))]
			case 1:
				at := rng.Intn(len(b))
				b = append(b[:at], b[at+1:]...)
			default:
				at := rng.Intn(len(b) + 1)
				b = append(b[:at], append([]rune{alphabet[rng.Intn(len(alphabet))]}, b[at:]...)...)
			}
		}
		want := levenshteinDistance(a, b)
		for _, limit := range []int{0, 1, 3, 6, 100} {
			got := levenshteinDistanceCapped(a, b, limit, &p, &c)
			if want <= limit && got != want {
				t.Fatalf("capped(%q,%q,%d) = %d, want %d", string(a), string(b), limit, got, want)
			}
			if want > limit && got <= limit {
				t.Fatalf("capped(%q,%q,%d) = %d, but the distance is %d", string(a), string(b), limit, got, want)
			}
		}
	}
}

func TestDedupTieBreakIsDeterministic(t *testing.T) {
	sig := "80x24|AAAA\n|fields:0"
	refs := []dedupRef{
		{hash: "bbbb", sig: sig, width: 80, height: 24},
		{hash: "aaaa", sig: sig, width: 80, height: 24},
	}
	for i := 0; i < 20; i++ {
		// matchDedupArea is given refs sorted by hash by its only caller.
		sorted := []dedupRef{refs[1], refs[0]}
		if got := matchDedupArea(sig, "", 80, 24, 0, sorted, 0.95); got != "aaaa" {
			t.Fatalf("tie broken towards %q", got)
		}
	}
	e := New(nil, DefaultConfig())
	e.mindMap = newMindMap()
	for _, h := range []string{"zz", "mm", "aa"} {
		e.mindMap.ensureArea(h).DedupSignature = sig
	}
	got := e.dedupRefsLocked()
	if got[0].hash != "aa" || got[2].hash != "zz" {
		t.Fatalf("refs not sorted: %v", got)
	}
}

func sizedScreen(height int, rows map[int]string) *host.Screen {
	s := &host.Screen{Width: 80, Height: height, IsFormatted: true, Buffer: make([][]rune, height)}
	for i := range s.Buffer {
		s.Buffer[i] = make([]rune, 80)
	}
	for y, text := range rows {
		copy(s.Buffer[y], []rune(text))
		s.Fields = append(s.Fields, host.NewField(s, host.AttrProtected, 0, y, 79, y, 0, 0))
	}
	return s
}

func TestSameScreenAtTwoTerminalSizesIsOneArea(t *testing.T) {
	menu := func(height int, status string, extra bool) *host.Screen {
		rows := map[int]string{
			0:          "PET100                      MAIN MENU                       2026-11-16",
			1:          "STORE 0042 PAWS AND CLAWS",
			3:          "Select an option, or type its command on the command line.",
			5:          " 1  CUSTOMER ENQUIRY AND MAINTENANCE          CUST",
			6:          " 2  STOCK CATALOGUE AND ENQUIRY               STOCK",
			7:          " 3  ORDER ENTRY - COUNTER SALES               POS",
			9:          "Option  ===>",
			height - 4: "Main menu.",
			height - 3: "Command ===>",
			height - 2: "PF1=Help PF3=Sign off PF9=Screen size PF13=Terminal info",
			height - 1: status,
		}
		if extra {
			rows[11] = "ACCOUNTS 12    STOCK LINES 40    UNITS 912"
		}
		return sizedScreen(height, rows)
	}
	e := New(nil, DefaultConfig())
	e.mindMap = newMindMap()
	small := menu(24, "MODEL 4 (43x80)  BUFFER DEFAULT 24x80", false)
	h1 := e.canonicalizeObservedScreenHashLocked(hashScreen(small), small)
	e.mindMap.observeScreen(h1, small, time.Now())
	large := menu(43, "MODEL 4 (43x80)  BUFFER ALTERNATE 43x80", true)
	h2 := e.canonicalizeObservedScreenHashLocked(hashScreen(large), large)
	if h1 != h2 {
		t.Fatalf("the same menu at 24x80 and 43x80 became two areas")
	}

	// A different screen that shares the frame stays separate.
	other := menu(43, "MODEL 4 (43x80)  BUFFER ALTERNATE 43x80", true)
	copy(other.Buffer[0], []rune("PET200                   CUSTOMER ENQUIRY                   2026-11-16"))
	if h3 := e.canonicalizeObservedScreenHashLocked(hashScreen(other), other); h3 == h1 {
		t.Fatalf("a different screen at another size was merged into the menu")
	}
}

func TestAreaLabelsCarryNoDateOrTime(t *testing.T) {
	for in, want := range map[string]string{
		"PET100      MAIN MENU       2026-11-16": "PET100 MAIN MENU",
		"16/11/2026 ORDER ENTRY 14:32:07":        "ORDER ENTRY",
		"REPORT 26.321 PAGE 1":                   "REPORT PAGE 1",
		"2026-11-16":                             "2026-11-16",
	} {
		if got := stripVolatileLabelTokens(in); got != want {
			t.Errorf("stripVolatileLabelTokens(%q) = %q, want %q", in, got, want)
		}
	}
	app := newFakeApp()
	e, _ := runFakeApp(t, app, fakeAppConfig(10))
	for _, area := range e.Status().MindMap.Areas {
		if strings.Contains(area.Label, "2026") {
			t.Errorf("label %q carries the date", area.Label)
		}
	}
}

// ---- outcomes --------------------------------------------------------

func TestSameAreaOutcomes(t *testing.T) {
	base := map[int]string{0: "LIST", 3: "ROW A", 4: "ROW B", 5: "ROW C", 20: ""}
	clone := func(changes map[int]string) *host.Screen {
		rows := map[int]string{}
		for k, v := range base {
			rows[k] = v
		}
		for k, v := range changes {
			rows[k] = v
		}
		return sizedScreen(24, rows)
	}
	before := clone(nil)
	if o, _ := sameAreaOutcome(before, clone(nil)); o != OutcomeNoChange {
		t.Errorf("identical screens: %s", o)
	}
	o, msg := sameAreaOutcome(before, clone(map[int]string{20: "OPTION 77 IS INVALID"}))
	if o != OutcomeRejected || !strings.Contains(msg, "INVALID") {
		t.Errorf("error message: %s %q", o, msg)
	}
	if o, _ := sameAreaOutcome(before, clone(map[int]string{20: "Top of list."})); o != OutcomeMessage {
		t.Errorf("neutral message: %s", o)
	}
	if o, _ := sameAreaOutcome(before, clone(map[int]string{3: "ROW D", 4: "ROW E", 5: "ROW F", 20: "Showing 4 to 6"})); o != OutcomeContent {
		t.Errorf("paged list: %s", o)
	}
	for _, text := range []string{"Customer NOT FOUND", "unknown command", "not valid here", "Request REJECTED", "ERROR 12"} {
		if !textLooksLikeRejection(text) {
			t.Errorf("%q should read as a rejection", text)
		}
	}
	if textLooksLikeRejection("Signed on. Terror level green") {
		t.Error("ERROR inside another word matched")
	}
}

func TestRejectedValuesAreKeptApartAndNotReused(t *testing.T) {
	app := newFakeApp()
	e, attempts := runFakeApp(t, app, fakeAppConfig(150))
	rejected := 0
	for _, a := range attempts {
		if a.Rejected {
			rejected++
			if a.Message == "" {
				t.Errorf("rejected attempt %d has no message", a.Attempt)
			}
			if a.Transitioned {
				t.Errorf("rejected attempt %d marked as a transition", a.Attempt)
			}
		}
	}
	if rejected == 0 {
		t.Fatal("the run never saw a rejection to test with")
	}
	found := false
	for _, area := range e.Status().MindMap.Areas {
		for key, values := range area.RejectedValues {
			for _, v := range values {
				found = true
				if containsString(area.KnownWorkingValues[key], v) {
					t.Errorf("%s: %q is both rejected and working", area.Label, v)
				}
			}
		}
	}
	if !found {
		t.Fatal("no rejected values recorded")
	}

	// A single-field rejection is not typed into that field again.
	seen := map[string]int{}
	for _, a := range attempts {
		if !a.Rejected || len(a.FieldWrites) != 1 || a.FieldWrites[0].Masked {
			continue
		}
		fw := a.FieldWrites[0]
		k := a.FromHash + "|" + mindMapFieldKey(fw.Row, fw.Column, fw.Length) + "|" + strings.TrimSpace(fw.Value)
		seen[k]++
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("rejected value typed again: %s (%d times)", k, n)
		}
	}
}

func TestErrorScreenBounceWithdrawsTheWorkingValue(t *testing.T) {
	mm := newMindMap()
	writes := []AttemptFieldWrite{{Row: 10, Column: 14, Length: 2, Value: "9", Success: true}}
	mm.recordAttempt(Attempt{FromHash: "a", ToHash: "err", AIDKey: "Enter", Transitioned: true, FieldWrites: writes})
	if !containsString(mm.Areas["a"].KnownWorkingValues["R10C14L2"], "9") {
		t.Fatal("setup: value should first look like it worked")
	}
	mm.markValuesRejected("a", writes)
	if containsString(mm.Areas["a"].KnownWorkingValues["R10C14L2"], "9") {
		t.Error("value still filed as working after the bounce")
	}
	if !containsString(mm.Areas["a"].RejectedValues["R10C14L2"], "9") {
		t.Error("value not filed as rejected")
	}
}

// ---- input generation ------------------------------------------------

func TestNumericValuesAreWrittenExactly(t *testing.T) {
	e := New(nil, DefaultConfig())
	for i := 0; i < 50; i++ {
		if got := e.expandWriteValueForField("1", 6, true); got != "1" {
			t.Fatalf("numeric hint 1 written as %q", got)
		}
	}
	if got := e.expandWriteValueForField("CUST", 8, false); got != "CUST    " {
		t.Fatalf("text value = %q", got)
	}
}

func TestImpossibleProbeClassesAreRetired(t *testing.T) {
	cfg := DefaultConfig()
	e := New(nil, cfg)
	s := &host.Screen{Width: 80, Height: 24}
	f := host.NewField(s, 0x00, 10, 2, 19, 2, 0, 0)
	key := mindMapFieldKey(3, 11, 10)

	for _, c := range e.probeClassesForField(10) {
		if c == fieldValueClassEmpty || c == fieldValueClassOverflow {
			t.Errorf("class %q offered although it cannot be written under the defaults", c)
		}
	}
	// Once the writable classes are covered there is nothing left to probe.
	tried := map[string][]string{key: {"A", "AAAAA", "AAAAAAAAAA"}}
	if v := e.lengthProbeValueForField(f, tried); v != "" {
		t.Errorf("probe still offered %q with every writable class tried", v)
	}

	cfg.ForceOverrideExistingInputs = false
	raw := New(nil, cfg)
	if !containsClass(raw.probeClassesForField(10), fieldValueClassOverflow) {
		t.Error("overflow should be offered when values are written as generated")
	}
	if containsClass(raw.probeClassesForField(40), fieldValueClassOverflow) {
		t.Error("overflow offered for a field already at MaxFieldLength")
	}

	// A field wider than MaxFieldLength counts as exactly filled at the cap.
	wide := host.NewField(s, 0x00, 2, 5, 78, 5, 0, 0)
	wideKey := mindMapFieldKey(6, 3, 77)
	if v := e.lengthProbeValueForField(wide, map[string][]string{wideKey: {"A", strings.Repeat("A", 20), strings.Repeat("A", 40)}}); v != "" {
		t.Errorf("wide field still has an untried class: %q", v)
	}
}

func containsClass(list []fieldValueClass, c fieldValueClass) bool {
	for _, x := range list {
		if x == c {
			return true
		}
	}
	return false
}

func TestMineScreenFindsWhatTheScreenOffers(t *testing.T) {
	s := sizedScreen(24, map[int]string{
		0:  "PET100                      MAIN MENU",
		3:  "Select an option, or a command such as CUST or STOCK.",
		5:  "  1  CUSTOMER ENQUIRY AND MAINTENANCE          CUST",
		6:  "  2  STOCK CATALOGUE AND ENQUIRY               STOCK",
		7:  " 10  HELP AND COMMAND REFERENCE                HELP",
		8:  "  X  SIGN OFF                                  OFF",
		12: "Confirm . . :      (Y/N)      Try CUST C0001.",
		20: "Command ===>",
		22: "PF1=Help PF3=Sign off",
	})
	// Rebuild rows that hold inputs as label + field.
	s.Fields = nil
	for y := 0; y < 24; y++ {
		switch y {
		case 10, 12, 20:
		default:
			s.Fields = append(s.Fields, host.NewField(s, host.AttrProtected, 0, y, 79, y, 0, 0))
		}
	}
	copy(s.Buffer[10], []rune("Option  ===>"))
	s.Fields = append(s.Fields,
		host.NewField(s, host.AttrProtected, 0, 10, 12, 10, 0, 0),
		host.NewField(s, host.AttrNumeric, 14, 10, 16, 10, 0, 0),
		host.NewField(s, host.AttrProtected, 0, 12, 13, 12, 0, 0),
		host.NewField(s, 0x00, 15, 12, 15, 12, 0, 0),
		host.NewField(s, host.AttrProtected, 17, 12, 79, 12, 0, 0),
		host.NewField(s, host.AttrProtected, 0, 20, 12, 20, 0, 0),
		host.NewField(s, 0x00, 14, 20, 76, 20, 0, 0),
	)
	m := mineScreen(s)

	optKey := mindMapFieldKey(11, 15, 3)
	if m.optionField != optKey {
		t.Fatalf("option field = %q, want %q", m.optionField, optKey)
	}
	if got := strings.Join(m.candidates[optKey], ","); got != "1,2,10" {
		t.Errorf("option candidates = %q, want 1,2,10 (the sign-off option is not offered)", got)
	}
	cmdKey := mindMapFieldKey(21, 15, 63)
	if m.commandField != cmdKey {
		t.Fatalf("command field = %q, want %q", m.commandField, cmdKey)
	}
	cmds := m.candidates[cmdKey]
	for _, want := range []string{"CUST", "STOCK", "HELP", "CUST C0001"} {
		if !containsString(cmds, want) {
			t.Errorf("command candidates %v lack %q", cmds, want)
		}
	}
	if containsString(cmds, "OFF") {
		t.Errorf("a sign-off command was offered: %v", cmds)
	}
	ynKey := mindMapFieldKey(13, 16, 1)
	if got := strings.Join(m.candidates[ynKey], ","); got != "Y,N" {
		t.Errorf("(Y/N) candidates = %q", got)
	}
	if !containsString(m.identifiers, "C0001") {
		t.Errorf("identifiers = %v", m.identifiers)
	}
}

func TestLabelDrivenValues(t *testing.T) {
	if got := labelDrivenValues("ORDER DATE . . :", 10, "2026-11-16", nil); len(got) == 0 || len(got[0]) != 10 || got[0][4] != '-' {
		t.Errorf("date values = %v", got)
	}
	if got := labelDrivenValues("QTY", 3, "", nil); !containsString(got, "1") {
		t.Errorf("qty values = %v", got)
	}
	if got := labelDrivenValues("CUSTOMER ID", 6, "", []string{"C0001", "TOOLONG99"}); len(got) != 1 || got[0] != "C0001" {
		t.Errorf("id values = %v", got)
	}
}

func TestMenuOptionsAreWorkedThroughInOrder(t *testing.T) {
	app := newFakeApp()
	e, _ := runFakeApp(t, app, fakeAppConfig(60))
	var menuEnters []string
	for _, p := range app.pressList() {
		if p == "menu:Enter" {
			menuEnters = append(menuEnters, p)
		}
	}
	mm := e.Status().MindMap
	for _, prefix := range []string{"APP200", "APP300", "APP800"} {
		areaByLabelPrefix(t, mm, prefix)
	}
	menu := areaByLabelPrefix(t, mm, "APP100")
	optKey := mindMapFieldKey(fakeOptRow+1, fakeOptCol+1, 2)
	if got := strings.Join(menu.CandidateTried[optKey], ","); got != "1,2,3" {
		t.Errorf("options tried = %q, want 1,2,3", got)
	}
	// The first three submissions from the menu are its three options.
	if len(menuEnters) < 3 {
		t.Fatalf("only %d Enter presses on the menu", len(menuEnters))
	}
	if working := menu.KnownWorkingValues[optKey]; len(working) != 3 {
		t.Errorf("working option values = %v", working)
	}
}

func TestRandomFullWidthStringsAreRare(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Seed = 5
	e := New(nil, cfg)
	s := &host.Screen{Width: 80, Height: 24}
	f := host.NewField(s, 0x00, 2, 5, 61, 5, 0, 0) // 60 wide
	long := 0
	const n = 500
	for i := 0; i < n; i++ {
		if len(strings.TrimSpace(e.generateFieldValue(f, false, &genContext{}))) > 20 {
			long++
		}
	}
	if long > n*3/10 {
		t.Fatalf("%d of %d generated values were long random strings", long, n)
	}
}

// ---- backtracking and saturation --------------------------------------

func TestRunBacktracksAndOnlySaturatesWhenNothingIsLeft(t *testing.T) {
	app := newFakeApp()
	cfg := fakeAppConfig(600)
	cfg.SaturationSteps = 10
	e, attempts := runFakeApp(t, app, cfg)
	st := e.Status()
	if st.TerminationReason != TerminationReasonSaturated {
		t.Fatalf("termination reason = %q after %d steps, want saturated", st.TerminationReason, st.StepsRun)
	}
	for _, prefix := range []string{"APP100", "APP200", "APP300", "APP800", "APP910"} {
		areaByLabelPrefix(t, st.MindMap, prefix)
	}
	if st.CoverageStats == nil || st.CoverageStats.UntriedInputsTotal != 0 {
		t.Errorf("saturated with untried inputs left: %+v", st.CoverageStats)
	}
	e.mu.Lock()
	for hash, area := range e.mindMap.Areas {
		if hash == e.firstScreenHash {
			continue // only reachable by signing off
		}
		if e.areaHasUntriedLocked(hash, area) {
			t.Errorf("saturated while %s still has untried keys %v", area.Label, e.untriedKeysLocked(hash, area))
		}
	}
	e.mu.Unlock()
	navigated := 0
	for _, a := range attempts {
		if a.Plan == "navigate" {
			navigated++
		}
	}
	if navigated == 0 {
		t.Error("the run never navigated back towards the frontier")
	}
}

func TestPlanPathReplaysTheInputsBehindAnEdge(t *testing.T) {
	e := New(nil, DefaultConfig())
	e.mindMap = newMindMap()
	e.mindMap.recordAttempt(Attempt{FromHash: "menu", ToHash: "cust", AIDKey: "Enter", Transitioned: true,
		FieldWrites: []AttemptFieldWrite{{Row: 11, Column: 15, Length: 2, Value: "1", Success: true}}})
	e.mindMap.recordAttempt(Attempt{FromHash: "menu", ToHash: "stock", AIDKey: "Enter", Transitioned: true,
		FieldWrites: []AttemptFieldWrite{{Row: 11, Column: 15, Length: 2, Value: "2", Success: true}}})
	e.mindMap.recordAttempt(Attempt{FromHash: "cust", ToHash: "menu", AIDKey: "PF(12)", Transitioned: true})
	e.mindMap.ensureArea("stock")

	kp := e.mindMap.Areas["menu"].KeyPresses["Enter"]
	if kp.Inputs["stock"][0]["R11C15L2"] != "2" || kp.Inputs["cust"][0]["R11C15L2"] != "1" {
		t.Fatalf("edge inputs not recorded per destination: %v", kp.Inputs)
	}
	path := e.planPathLocked("cust", func(hash string, _ *MindMapArea) bool { return hash == "stock" })
	if len(path) != 2 || path[0].key != "PF(12)" || path[1].key != "Enter" || path[1].inputs["R11C15L2"] != "2" {
		t.Fatalf("path = %+v", path)
	}

	// The same edge data bridges a generated business workflow.
	edges, ok := e.mindMap.findKeyPath("cust", "stock")
	if !ok || len(edges) != 2 || edges[1].Inputs["R11C15L2"] != "2" {
		t.Fatalf("findKeyPath = %+v", edges)
	}
	e.mindMap.BusinessFunctions = map[string]*BusinessFunction{"f": {Name: "f", Steps: []BusinessFunctionStep{
		{ScreenHash: "cust", AIDKey: "Enter", ExpectHash: "cust"},
		{ScreenHash: "stock", AIDKey: "Enter"},
	}}}
	data, err := GenerateBusinessWorkflow(e.mindMap, "f", nil, "h", 23, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wf struct{ Steps []session.WorkflowStep }
	if err := json.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	filled := false
	for _, st := range wf.Steps {
		if st.Type == "FillString" && st.Text == "2" && st.Coordinates != nil && st.Coordinates.Row == 11 {
			filled = true
		}
	}
	if !filled {
		t.Errorf("bridge did not type the menu option: %+v", wf.Steps)
	}

	// A blocked key is not a route.
	e.mindMap.Areas["cust"].AutoBlockedKeys = []string{"PF(12)"}
	if p := e.planPathLocked("cust", func(hash string, _ *MindMapArea) bool { return hash == "stock" }); p != nil {
		t.Fatalf("path through a blocked key: %+v", p)
	}
}

// ---- export ----------------------------------------------------------

func TestExportIsAConnectedWalk(t *testing.T) {
	step := func(name string) []session.WorkflowStep { return []session.WorkflowStep{{Type: name}} }
	transitions := []Transition{
		{FromHash: "A", ToHash: "B", Steps: step("A>B")},
		{FromHash: "B", ToHash: "A", Steps: step("B>A")},
		{FromHash: "A", ToHash: "C", Steps: step("A>C")},
		{FromHash: "C", ToHash: "A", Steps: step("C>A")},
		{FromHash: "B", ToHash: "D", Steps: step("B>D")}, // found after backtracking
		{FromHash: "Z", ToHash: "Y", Steps: step("Z>Y")}, // not reachable from A
	}
	walk := orderTransitionsAsWalk("A", transitions)
	at := "A"
	covered := map[string]bool{}
	for _, tr := range walk {
		if tr.FromHash != at {
			t.Fatalf("step %s pressed while standing on %s", tr.Steps[0].Type, at)
		}
		covered[tr.Steps[0].Type] = true
		at = tr.ToHash
	}
	for _, want := range []string{"A>B", "B>A", "A>C", "C>A", "B>D"} {
		if !covered[want] {
			t.Errorf("walk leaves out %s", want)
		}
	}
	if covered["Z>Y"] {
		t.Error("walk includes an edge it cannot reach")
	}

	// And end to end: what the engine exports plays back on a fresh copy of
	// the application without ever pressing a key on the wrong screen.
	app := newFakeApp()
	e, _ := runFakeApp(t, app, fakeAppConfig(120))
	data, err := e.ExportWorkflow("h", 23)
	if err != nil {
		t.Fatal(err)
	}
	var wf struct{ Steps []session.WorkflowStep }
	if err := json.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	recorded := orderTransitionsAsWalk(e.firstScreenHash, e.transitions)
	labels := map[string]string{}
	for h, a := range e.mindMap.Areas {
		labels[h] = a.Label
	}
	e.mu.Unlock()
	if len(recorded) < 6 {
		t.Fatalf("run recorded only %d transitions", len(recorded))
	}
	replay := newFakeApp()
	pageLabel := map[string]string{"signon": "APP010", "menu": "APP100", "cust": "APP200", "stock": "APP300", "rpt": "APP800", "help": "APP910"}
	for i, tr := range recorded {
		if want := labels[tr.FromHash]; !strings.HasPrefix(want, pageLabel[replay.currentPage()]) {
			t.Fatalf("replay step %d: recorded on %q but playback is on %s", i, want, replay.currentPage())
		}
		for _, st := range tr.Steps {
			if st.Type == "FillString" {
				_ = replay.WriteStringAt(st.Coordinates.Row-1, st.Coordinates.Column-1, st.Text)
				continue
			}
			key := strings.TrimPrefix(st.Type, "Press")
			if strings.HasPrefix(key, "PF") {
				key = "PF(" + strings.TrimPrefix(key, "PF") + ")"
			}
			_ = replay.SendKey(key)
		}
		if want := labels[tr.ToHash]; !strings.HasPrefix(want, pageLabel[replay.currentPage()]) {
			t.Fatalf("replay step %d: expected to land on %q, playback is on %s", i, want, replay.currentPage())
		}
	}
	if len(wf.Steps) == 0 {
		t.Fatal("empty export")
	}
}

// ---- persistence, masking, termination -------------------------------

func TestHiddenFieldValuesAreMaskedEverywhereSerialised(t *testing.T) {
	app := newFakeApp()
	logPath := filepath.Join(t.TempDir(), "transitions.jsonl")
	cfg := fakeAppConfig(80)
	cfg.TransitionLogPath = logPath
	e, attempts := runFakeApp(t, app, cfg)
	if app.lastPassword == "" {
		t.Fatal("the run never signed on")
	}
	passKey := mindMapFieldKey(fakePassRow+1, fakePassCol+1, 8)

	checkWrites := func(where string, list []Attempt, mustSee bool) {
		seen := false
		for _, a := range list {
			for _, fw := range a.FieldWrites {
				if fw.Row == fakePassRow+1 && fw.Column == fakePassCol+1 {
					seen = true
					if !fw.Masked || fw.Value != maskedFieldValue {
						t.Errorf("%s: password write not masked: %+v", where, fw)
					}
				}
			}
		}
		if !seen && mustSee {
			t.Errorf("%s: no password write to check", where)
		}
	}
	checkWrites("attempts", attempts, true)

	run := e.Snapshot("run-1")
	checkWrites("saved attempts", run.Attempts, false)
	for _, tr := range run.TransitionList {
		for _, st := range tr.Steps {
			if st.Type == "FillString" && st.Coordinates.Row == fakePassRow+1 && st.Text != maskedFieldValue {
				t.Errorf("saved transition carries the password: %q", st.Text)
			}
		}
	}
	for _, st := range run.Steps {
		if st.Type == "FillString" && st.Coordinates != nil && st.Coordinates.Row == fakePassRow+1 && st.Text != maskedFieldValue {
			t.Errorf("saved step log carries the password: %q", st.Text)
		}
	}
	for _, area := range run.MindMap.Areas {
		if len(area.KnownTriedValues[passKey])+len(area.KnownWorkingValues[passKey]) > 0 {
			t.Errorf("mind map lists password values: %v %v", area.KnownTriedValues[passKey], area.KnownWorkingValues[passKey])
		}
		if fd, ok := area.FieldDiscovery[passKey]; ok {
			if fd.LastValue != maskedFieldValue || (fd.LastWorkedValue != "" && fd.LastWorkedValue != maskedFieldValue) {
				t.Errorf("field discovery carries the password: %+v", fd)
			}
		}
		for _, kp := range area.KeyPresses {
			for _, sets := range kp.Inputs {
				for _, set := range sets {
					if v, ok := set[passKey]; ok && v != maskedFieldValue {
						t.Errorf("edge inputs carry the password: %q", v)
					}
				}
			}
		}
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("transition log: %v", err)
	}
	var logAttempts []Attempt
	for _, line := range strings.Split(strings.TrimSpace(string(logged)), "\n") {
		var a Attempt
		if err := json.Unmarshal([]byte(line), &a); err != nil {
			t.Fatalf("transition log line: %v", err)
		}
		logAttempts = append(logAttempts, a)
	}
	if len(logAttempts) != len(attempts) {
		t.Errorf("transition log has %d lines for %d attempts", len(logAttempts), len(attempts))
	}
	checkWrites("transition log", logAttempts, true)

	// The live export still has what playback needs to sign on.
	data, err := e.ExportWorkflow("h", 23)
	if err != nil {
		t.Fatal(err)
	}
	var wf struct{ Steps []session.WorkflowStep }
	_ = json.Unmarshal(data, &wf)
	real := false
	for _, st := range wf.Steps {
		if st.Type == "FillString" && st.Coordinates.Row == fakePassRow+1 {
			if strings.TrimSpace(st.Text) == "" || st.Text == maskedFieldValue {
				t.Errorf("live export masks the password fill step")
			}
			real = true
		}
	}
	if !real {
		t.Error("live export has no password fill step")
	}
	// The real value is reused within the run to sign on again.
	e.mu.Lock()
	kept := len(e.hiddenValues[e.firstScreenHash][passKey])
	e.mu.Unlock()
	if kept == 0 {
		t.Error("the working password was not kept in memory for reuse")
	}
}

func TestSnapshotCarriesReasonFirstScreenSeedAndConfig(t *testing.T) {
	app := newFakeApp()
	cfg := fakeAppConfig(30)
	cfg.Seed = 0
	cfg.ExportHost = "mainframe.example"
	cfg.ExportPort = 992
	cfg.Hints = []Hint{{Transaction: "TOPSECRET"}}
	e, _ := runFakeApp(t, app, cfg)
	run := e.Snapshot("run-2")
	st := e.Status()
	if run.TerminationReason != TerminationReasonMaxSteps {
		t.Errorf("TerminationReason = %q", run.TerminationReason)
	}
	if run.FirstScreenHash == "" || run.FirstScreenHash != st.FirstScreenHash {
		t.Errorf("FirstScreenHash = %q, status has %q", run.FirstScreenHash, st.FirstScreenHash)
	}
	if run.TargetHost != "mainframe.example:992" {
		t.Errorf("TargetHost = %q", run.TargetHost)
	}
	if run.Seed == 0 || run.Seed != st.Seed {
		t.Errorf("Seed = %d, status has %d", run.Seed, st.Seed)
	}
	if run.Config == nil || run.Config.MaxSteps != 30 || run.Config.Seed != run.Seed {
		t.Fatalf("Config = %+v", run.Config)
	}
	if len(run.Config.Hints) != 0 || len(run.Config.ScreenHints) != 0 {
		t.Errorf("hints were persisted with the run")
	}
	if run.UniqueInputs != st.UniqueInputs || run.UniqueInputs == 0 {
		t.Errorf("UniqueInputs = %d, status has %d", run.UniqueInputs, st.UniqueInputs)
	}
	data, err := EncodeRun(run)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "TOPSECRET") && !strings.Contains(string(data), `"value": "TOPSECRE`) {
		// The hint may legitimately appear as a typed, visible value.
		t.Errorf("hint text persisted outside of typed values")
	}
}

func TestResumeUsesTheSavedFirstScreenHash(t *testing.T) {
	app := newFakeApp()
	e, _ := runFakeApp(t, app, fakeAppConfig(60))
	run := e.Snapshot("run-3")
	first := run.FirstScreenHash
	// The attempt history is capped and filtered; it no longer starts on
	// the first screen.
	run.Attempts = []Attempt{{FromHash: "not-the-first-screen", Transitioned: true}}

	cfg := fakeAppConfig(5)
	cfg.Seed = 0
	resumed := New(app, cfg)
	if err := resumed.Resume(run); err != nil {
		t.Fatal(err)
	}
	for resumed.Active() {
		time.Sleep(2 * time.Millisecond)
	}
	st := resumed.Status()
	if st.FirstScreenHash != first {
		t.Fatalf("resumed first screen = %q, want %q", st.FirstScreenHash, first)
	}
	if st.Seed != run.Seed {
		t.Errorf("resumed seed = %d, want the saved %d", st.Seed, run.Seed)
	}
	if !resumed.firstScreenIsGate {
		t.Error("resumed run forgot the first screen is a sign-on screen")
	}

	// A run saved before the field existed falls back to the old guess.
	run.FirstScreenHash = ""
	old := New(app, cfg)
	if err := old.Resume(run); err != nil {
		t.Fatal(err)
	}
	old.Stop()
	for old.Active() {
		time.Sleep(2 * time.Millisecond)
	}
	if got := old.Status().FirstScreenHash; got != "not-the-first-screen" {
		t.Errorf("fallback first screen = %q", got)
	}
}

func TestNilScreenEndsTheRunWithAnError(t *testing.T) {
	app := newFakeApp()
	app.noScreen = true
	e, _ := runFakeApp(t, app, fakeAppConfig(5))
	st := e.Status()
	if st.TerminationReason != TerminationReasonError || !strings.Contains(st.Error, "no screen") {
		t.Fatalf("reason = %q, error = %q", st.TerminationReason, st.Error)
	}
}

func TestPanicInTheRunLoopIsRecordedAsTheRunError(t *testing.T) {
	app := newFakeApp()
	app.panicOnKey = true
	e, _ := runFakeApp(t, app, fakeAppConfig(5))
	st := e.Status()
	if st.Active {
		t.Fatal("engine still active")
	}
	if st.TerminationReason != TerminationReasonError || !strings.Contains(st.Error, "fake host exploded") {
		t.Fatalf("reason = %q, error = %q", st.TerminationReason, st.Error)
	}
}

func TestOutputFileIsWrittenAtomicallyAndFailuresSurface(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "out", "workflow.json")
	cfg := fakeAppConfig(20)
	cfg.OutputFile = good
	e, _ := runFakeApp(t, newFakeApp(), cfg)
	if st := e.Status(); st.OutputFileError != "" {
		t.Fatalf("OutputFileError = %q", st.OutputFileError)
	}
	data, err := os.ReadFile(good)
	if err != nil || !json.Valid(data) {
		t.Fatalf("output file: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(good))
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}

	// A path under a regular file cannot be created.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.OutputFile = filepath.Join(blocker, "workflow.json")
	e, _ = runFakeApp(t, newFakeApp(), cfg)
	st := e.Status()
	if st.OutputFileError == "" {
		t.Fatal("a failed output write was not reported")
	}
	if st.TerminationReason != TerminationReasonMaxSteps {
		t.Errorf("reason = %q", st.TerminationReason)
	}
}

func TestMindMapVersionAndUniqueInputCount(t *testing.T) {
	app := newFakeApp()
	e := New(app, fakeAppConfig(40))
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	var last uint64
	for e.Active() {
		v := e.Status().MindMapVersion
		if v < last {
			t.Fatalf("MindMapVersion went backwards: %d -> %d", last, v)
		}
		last = v
		time.Sleep(time.Millisecond)
	}
	st := e.Status()
	if st.MindMapVersion == 0 {
		t.Fatal("MindMapVersion never moved")
	}
	before := st.MindMapVersion
	if err := e.AnnotateArea(st.FirstScreenHash, "sign on", "", nil); err != nil {
		t.Fatal(err)
	}
	if e.Status().MindMapVersion <= before {
		t.Error("annotating an area did not bump the version")
	}
	raw, _ := json.Marshal(st)
	if !strings.Contains(string(raw), `"mindMapVersion"`) {
		t.Error("status JSON lacks mindMapVersion")
	}
	if st.UniqueInputs == 0 {
		t.Error("UniqueInputs = 0 after a run that typed values")
	}

	// The count is exact while the set of hashes behind it is bounded.
	c := New(nil, DefaultConfig())
	for i := 0; i < 1000; i++ {
		c.noteUniqueInputLocked("V"+strings.Repeat("x", i%7)+string(rune('a'+i%26)), false)
	}
	if c.uniqueInputCount != len(c.uniqueInputSeen) || c.uniqueInputCount == 0 {
		t.Errorf("count %d, hashes %d", c.uniqueInputCount, len(c.uniqueInputSeen))
	}
	if len(c.uniqueInputSample) > maxSavedInputSample {
		t.Errorf("sample grew to %d", len(c.uniqueInputSample))
	}
}

func TestTruncatePreviewCutsOnARuneBoundary(t *testing.T) {
	s := strings.Repeat("é", 10) // two bytes each
	for limit := 1; limit < len(s); limit++ {
		if got := truncatePreview(s, limit); !utf8.ValidString(got) || len(got) > limit {
			t.Fatalf("truncatePreview(_, %d) = %q", limit, got)
		}
	}
	if got := truncatePreview("abc", 2); got != "ab" {
		t.Fatalf("ascii truncation = %q", got)
	}
}

func TestFallbackKeyListIsShared(t *testing.T) {
	if got := fallbackChaosKey(nil); got != fallbackChaosKeyCandidates[0] {
		t.Fatalf("fallbackChaosKey = %q", got)
	}
	blocked := normalizeChaosKeySet(fallbackChaosKeyCandidates[:len(fallbackChaosKeyCandidates)-1])
	if got := fallbackChaosKey(blocked); got != fallbackChaosKeyCandidates[len(fallbackChaosKeyCandidates)-1] {
		t.Fatalf("fallbackChaosKey with all but the last blocked = %q", got)
	}
	if got := strings.Join(CandidateKeys(nil), ","); !strings.Contains(got, "PF(12)") {
		t.Fatalf("CandidateKeys(nil) = %s", got)
	}
}
