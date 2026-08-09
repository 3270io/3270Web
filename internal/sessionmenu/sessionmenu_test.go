package sessionmenu

import (
	"net"
	"strings"
	"testing"
	"time"
)

func testEntries() []Entry {
	return []Entry{
		{Name: "CICSPROD", Description: "Production CICS region", Detail: "mvs1.example:992"},
		{Name: "CICSTEST", Description: "Test CICS region", Detail: "mvs2.example:23"},
		{Name: "TSO", Detail: "mvs1.example:23"},
	}
}

func startTestMenu(t *testing.T) *Menu {
	t.Helper()
	m, err := Start(Branding{}, testEntries())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Stop)
	return m
}

// The screen is what an operator reads, so it is asserted on as text rather
// than as a field count: a menu that renders every entry into the same row
// would pass any structural check and be unusable.
func TestTheScreenListsEverySystemWithANumber(t *testing.T) {
	m := startTestMenu(t)
	screen, cursorRow, cursorCol := m.screen("")

	rows := map[int][]string{}
	for _, f := range screen {
		if f.Content != "" {
			rows[f.Row] = append(rows[f.Row], f.Content)
		}
	}

	_, _, firstItem := layout(m.brand)
	for i, want := range []string{"CICSPROD", "CICSTEST", "TSO"} {
		row := firstItem + i
		line := strings.Join(rows[row], " ")
		if !strings.Contains(line, want) {
			t.Errorf("row %d = %q, want it to name %s", row, line, want)
		}
		// The number is what the operator types, so it has to be beside the
		// name rather than implied by the order.
		if !strings.Contains(line, strings.TrimSpace(itoa(i+1))) {
			t.Errorf("row %d = %q, want it to carry the selection number", row, line)
		}
	}

	// A description the profile did not give must not leave the column blank
	// with no way to tell two hosts apart: TSO has only a target.
	if line := strings.Join(rows[firstItem+2], " "); !strings.Contains(line, "mvs1.example:23") {
		t.Errorf("an entry with no description shows %q, want the host", line)
	}

	// The cursor starts in the input field. Anywhere else and the first thing
	// an operator types goes nowhere.
	if cursorRow != rowPrompt || cursorCol != selectionCol+1 {
		t.Errorf("cursor starts at %d,%d, want the selection field", cursorRow, cursorCol)
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return ""
}

func TestSelectionAcceptsANumberOrAName(t *testing.T) {
	m := startTestMenu(t)

	for _, in := range []string{"1", " 1 ", "CICSPROD", "cicsprod"} {
		entry, err := m.resolve(strings.TrimSpace(in))
		if err != nil {
			t.Errorf("resolve(%q): %v", in, err)
			continue
		}
		if entry.Name != "CICSPROD" {
			t.Errorf("resolve(%q) = %q", in, entry.Name)
		}
	}
}

// Every refusal has to say what to do instead. An operator staring at a menu
// that ignored them has no way to find out what it wanted.
func TestARefusedSelectionSaysWhatIsWrong(t *testing.T) {
	m := startTestMenu(t)

	for in, want := range map[string]string{
		"":         "Type the number",
		"0":        "no system 0",
		"9":        "no system 9",
		"-1":       "no system -1",
		"NOSUCH":   "No system called",
		"99999999": "no system",
	} {
		_, err := m.resolve(in)
		if err == nil {
			t.Errorf("resolve(%q) was accepted", in)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
			t.Errorf("resolve(%q) said %q, want it to mention %q", in, err, want)
		}
	}
}

// A menu with more entries than the screen holds must not silently drop the
// rest into a row that does not exist.
func TestTheListIsBoundedToWhatAScreenHolds(t *testing.T) {
	many := make([]Entry, 40)
	for i := range many {
		many[i] = Entry{Name: "HOST", Detail: "h.example:23"}
	}
	m, err := Start(Branding{}, many)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	screen, _, _ := m.screen("")
	for _, f := range screen {
		if f.Row > 23 || f.Row < 0 {
			t.Errorf("a field landed on row %d, off a 24-row screen", f.Row)
		}
	}
	if want := maxEntriesFor(m.brand); len(m.entries) != want {
		t.Errorf("the menu kept %d entries, want it capped at %d", len(m.entries), want)
	}
}

func TestStartRefusesAnEmptyMenu(t *testing.T) {
	if _, err := Start(Branding{}, nil); err == nil {
		t.Error("a menu with nothing on it was started")
	}
}

// The listener is the session's, and has to go when the session does — a port
// left bound outlives everything it was for.
func TestStopClosesTheListenerAndIsSafeTwice(t *testing.T) {
	m := startTestMenu(t)
	addr := m.Addr()

	m.Stop()
	m.Stop() // must not panic on a second close

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		conn.Close()
		t.Error("the menu still accepts connections after Stop")
	}
}

// A terminal really does negotiate with this, so the listener has to speak
// telnet rather than merely accept a socket.
func TestTheListenerAnswersAConnection(t *testing.T) {
	m := startTestMenu(t)

	conn, err := net.DialTimeout("tcp", m.Addr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// go3270 opens the negotiation, so something arrives without us sending
	// anything. Without this the first thing a terminal would meet is silence.
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 32)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("the menu sent nothing to a connected terminal: %v", err)
	}
	// IAC, the first byte of every telnet negotiation.
	if n == 0 || buf[0] != 0xFF {
		t.Errorf("first bytes were %v, want a telnet negotiation", buf[:n])
	}
}

func TestChosenIsEmptyUntilSomebodyChooses(t *testing.T) {
	m := startTestMenu(t)
	if got := m.Chosen(); got != "" {
		t.Errorf("Chosen() = %q before any selection", got)
	}
	if m.SignedOff() {
		t.Error("SignedOff() is true before anybody pressed PF3")
	}
}

// The default is 3270.io's, because an unconfigured instance should show
// something rather than a blank space waiting to be filled in.
func TestTheDefaultBrandingIsUsedWhenNothingIsConfigured(t *testing.T) {
	rows := Preview(Branding{}, testEntries())
	joined := strings.Join(rows, "\n")

	if !strings.Contains(joined, "3270.io") {
		t.Errorf("the default screen does not name 3270.io:\n%s", joined)
	}
	if !strings.Contains(joined, "#####") {
		t.Error("the default screen has no artwork")
	}
	if len(rows) != screenRows {
		t.Errorf("the preview is %d rows, want %d", len(rows), screenRows)
	}
	for i, row := range rows {
		if len(row) > 80 {
			t.Errorf("row %d is %d columns wide", i, len(row))
		}
	}
}

func TestBrandingReplacesTheDefault(t *testing.T) {
	brand := Branding{
		Title:  "ACME MAINFRAME SERVICES",
		Banner: []string{"  ///  ACME  ///"},
		Footer: "Support: extension 4400",
	}
	joined := strings.Join(Preview(brand, testEntries()), "\n")

	for _, want := range []string{"ACME MAINFRAME SERVICES", "///  ACME  ///", "Support: extension 4400"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the screen does not carry %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "3270.io") {
		t.Error("the default title survived a deployment's own branding")
	}
}

// A shorter banner leaves room for more hosts, and a taller one for fewer —
// rather than writing them off the bottom of the screen.
func TestTheHostListFitsUnderWhateverBrandingIsSet(t *testing.T) {
	many := make([]Entry, 40)
	for i := range many {
		many[i] = Entry{Name: "HOST", Detail: "h.example:23"}
	}

	short := Branding{Banner: []string{"ACME"}}
	tall := Branding{Banner: []string{"1", "2", "3", "4", "5", "6"}}
	if maxEntriesFor(short) <= maxEntriesFor(tall) {
		t.Errorf("a one-line banner fits %d hosts and a six-line one %d",
			maxEntriesFor(short), maxEntriesFor(tall))
	}

	for _, brand := range []Branding{short, tall, {}} {
		for i, row := range Preview(brand, many) {
			if i >= screenRows {
				t.Fatal("the preview ran off the screen")
			}
			_ = row
		}
		// The command line has to survive whatever the banner does.
		rows := Preview(brand, many)
		if !strings.Contains(rows[rowPrompt], "SELECTION ==>") {
			t.Errorf("the selection prompt was overwritten:\n%s", strings.Join(rows, "\n"))
		}
		if !strings.Contains(rows[rowKeys], "PF3") {
			t.Errorf("the key legend was overwritten:\n%s", strings.Join(rows, "\n"))
		}
	}
}

// A 3270 screen is built into a data stream, so a control character in it is
// not a rendering problem but a malformed stream.
func TestBrandingIsSanitisedToWhatAScreenCanDraw(t *testing.T) {
	brand := Branding{
		Title:  "ACME\x00\x1b[31m MAINFRAME",
		Banner: []string{"good", "bad\ttab", strings.Repeat("x", 200), "émoji ✨"},
		Footer: "line\nbreak",
	}
	cleaned, notes := brand.Sanitise()

	if strings.ContainsAny(cleaned.Title, "\x00\x1b") {
		t.Errorf("Title kept control characters: %q", cleaned.Title)
	}
	for _, line := range cleaned.Banner {
		if len(line) > MaxLineWidth {
			t.Errorf("a banner line is %d wide, over the %d limit", len(line), MaxLineWidth)
		}
		for _, r := range line {
			if r < 0x20 || r >= 0x7F {
				t.Errorf("a banner line kept %q, which a code page cannot draw", r)
			}
		}
	}
	if strings.Contains(cleaned.Footer, "\n") {
		t.Errorf("Footer kept a line break: %q", cleaned.Footer)
	}
	// And it says what it changed, rather than quietly dropping the artwork
	// somebody drew.
	if len(notes) == 0 {
		t.Error("nothing was reported despite input being changed")
	}
}

func TestTooManyBannerLinesAreRefusedRatherThanDrawn(t *testing.T) {
	brand := Branding{Banner: []string{"1", "2", "3", "4", "5", "6", "7", "8"}}
	cleaned, notes := brand.Sanitise()
	if len(cleaned.Banner) != MaxBannerLines {
		t.Errorf("kept %d banner lines, want %d", len(cleaned.Banner), MaxBannerLines)
	}
	if len(notes) == 0 {
		t.Error("the dropped lines were not reported")
	}
}

// A deployment that wants no artwork at all has to be able to say so, and get
// a menu rather than the default logo.
func TestAnEmptyBannerMeansNoArtworkRatherThanTheDefault(t *testing.T) {
	joined := strings.Join(Preview(Branding{Title: "ACME", Banner: []string{}}, testEntries()), "\n")
	if strings.Contains(joined, "#####") {
		t.Errorf("the default artwork came back:\n%s", joined)
	}
	if !strings.Contains(joined, "ACME") {
		t.Error("the title is missing")
	}
}
