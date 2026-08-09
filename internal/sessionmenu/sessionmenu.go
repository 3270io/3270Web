// Package sessionmenu draws the screen an operator lands on when their
// account may reach more than one mainframe.
//
// It is a real TN3270 application, not a web page that looks like one. The
// terminal negotiates with it, the operator types a selection into a field and
// presses Enter, and the OIA, the cursor, tabbing and the PF keys are the
// terminal's own — because they are. That is the whole reason for a listener
// rather than a picker in the browser: this is the screen a mainframe operator
// already expects to meet, and years of muscle memory apply to it.
//
// One listener per session, on a loopback port chosen by the kernel. The port
// is therefore the identity: the menu a connection is shown is the menu that
// listener was created with, so nothing has to be read out of the connection
// to know whose it is. It also means the menu disappears when the session
// does, which is the correct lifetime for a screen holding one person's host
// list.
package sessionmenu

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/racingmars/go3270"
)

// Branding is what an instance puts at the top of its selection screen.
//
// An operator meets this screen before anything of their own organisation's,
// so a deployment that cannot put its own name on it is one that looks like
// somebody else's product. The default is 3270.io's.
//
// Everything here is drawn on a 3270 display, which is why it is a list of
// lines rather than markup: the screen is a character grid, and what is
// written is what appears.
type Branding struct {
	// Title sits under the banner. Empty falls back to the default.
	Title string
	// Banner is drawn at the top, one screen row per element. Empty falls
	// back to the default artwork; a Banner of one empty string is how a
	// deployment says it wants no artwork at all.
	Banner []string
	// Footer is an optional line above the key legend — an operations contact,
	// a classification marking, whatever the site puts there.
	Footer string
}

// DefaultBranding is 3270.io's own.
//
// Drawn in "#" rather than block-drawing characters because this is rendered
// through a code page: a 3270 display shows what the code page has, and the
// characters that survive every one of them are the ones on a keyboard.
func DefaultBranding() Branding {
	return Branding{
		Title: "3270.io",
		Banner: []string{
			"##### ##### #####  ###          #        ",
			"    #     #     # #   #                  ",
			" #### #####    #  #   #         #    ### ",
			"    # #       #   #   #         #   #   #",
			"##### #####   #    ###    #     #    ### ",
		},
	}
}

// MaxBannerLines and MaxLineWidth bound what an administrator can put on the
// screen. A banner taller than this leaves no room for the host list, and a
// line wider than the display would be truncated anyway — silently, which is
// worse than being told.
// Exported so the administration page can state the dimensions rather than
// letting somebody discover them by having their artwork truncated.
const (
	MaxBannerLines = 6
	MaxLineWidth   = 78
)

// Sanitise trims branding to what a 3270 screen can draw, and reports what it
// had to change so the administrator is told rather than left guessing.
//
// Only printable ASCII survives. The screen is built into a 3270 data stream,
// and a control character in it is not a rendering problem but a malformed
// stream; a character outside the code page is a question mark at best.
func (b Branding) Sanitise() (Branding, []string) {
	var notes []string
	out := Branding{Title: clean(b.Title, 40)}
	if out.Title != strings.TrimSpace(b.Title) {
		notes = append(notes, "the title was shortened or had characters removed")
	}

	if len(b.Banner) > MaxBannerLines {
		b.Banner = b.Banner[:MaxBannerLines]
		notes = append(notes, fmt.Sprintf("only the first %d banner lines are used", MaxBannerLines))
	}
	for _, line := range b.Banner {
		cleaned := clean(line, MaxLineWidth)
		if len(cleaned) != len(strings.TrimRight(line, " ")) && strings.TrimSpace(line) != "" {
			notes = append(notes, "a banner line was shortened or had characters removed")
		}
		out.Banner = append(out.Banner, cleaned)
	}
	out.Footer = clean(b.Footer, MaxLineWidth)
	return out, notes
}

// clean keeps printable ASCII and bounds the length.
func clean(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r < 0x7F {
			b.WriteRune(r)
		}
	}
	out := strings.TrimRight(b.String(), " ")
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// resolve fills in the defaults for anything the deployment left empty.
func (b Branding) resolve() Branding {
	def := DefaultBranding()
	if strings.TrimSpace(b.Title) == "" {
		b.Title = def.Title
	}
	if b.Banner == nil {
		b.Banner = def.Banner
	}
	return b
}

// Entry is one selectable host.
type Entry struct {
	// Name is the profile name, and what the operator is really choosing.
	Name string
	// Description is the profile's own, shown as-is.
	Description string
	// Detail is the host and port, so two similarly named entries can be told
	// apart without opening either.
	Detail string
}

// screenRows is the display this screen is built for. A model 2 is the
// smallest a 3270 comes in, so a menu that fits one fits them all.
const screenRows = 24

// Menu is a running selection screen.
type Menu struct {
	listener net.Listener
	entries  []Entry
	brand    Branding

	mu       sync.Mutex
	chosen   string
	signedIn bool
	stopped  bool
	done     chan struct{}
}

// Start puts a selection screen on a loopback port and returns it.
//
// The caller connects a terminal to Addr and, once Chosen reports a name,
// disconnects and connects that terminal to the host it names.
func Start(brand Branding, entries []Entry) (*Menu, error) {
	if len(entries) == 0 {
		return nil, errors.New("sessionmenu: nothing to choose from")
	}
	brand = brand.resolve()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("sessionmenu: listen: %w", err)
	}
	// Bounded by what is left after the branding, so a taller banner shows
	// fewer hosts rather than writing them off the bottom of the screen.
	if max := maxEntriesFor(brand); len(entries) > max {
		entries = entries[:max]
	}
	m := &Menu{
		listener: listener,
		entries:  entries,
		brand:    brand,
		done:     make(chan struct{}),
	}
	go m.serve()
	return m, nil
}

// Addr is where to point a terminal.
func (m *Menu) Addr() string { return m.listener.Addr().String() }

// Chosen reports the profile the operator selected, or "" while they have not.
func (m *Menu) Chosen() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chosen
}

// SignedOff reports whether the operator asked to leave rather than choose.
func (m *Menu) SignedOff() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.signedIn
}

// Stop closes the listener. Safe to call more than once.
func (m *Menu) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	close(m.done)
	m.mu.Unlock()
	_ = m.listener.Close()
}

func (m *Menu) serve() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			select {
			case <-m.done:
			default:
				if !errors.Is(err, net.ErrClosed) {
					log.Printf("sessionmenu: accept: %v", err)
				}
			}
			return
		}
		go m.handle(conn)
	}
}

// handle draws the screen and reads the operator's answer.
func (m *Menu) handle(conn net.Conn) {
	defer conn.Close()
	// The negotiated device name is not needed: this listener serves one
	// session, so the port already says whose menu this is.
	if _, err := go3270.NegotiateTelnet(conn); err != nil {
		return
	}

	values := map[string]string{}
	for {
		screen, selectionRow, selectionCol := m.screen("")
		response, err := go3270.ShowScreen(screen, values, selectionRow, selectionCol, conn)
		if err != nil {
			return
		}

		switch response.AID {
		case go3270.AIDPF3:
			m.mu.Lock()
			m.signedIn = true
			m.mu.Unlock()
			return
		case go3270.AIDPF12, go3270.AIDClear:
			values["selection"] = ""
			continue
		case go3270.AIDEnter:
		default:
			// Any other key is not a selection. Left on the screen rather than
			// treated as one, which is what a session manager does.
			continue
		}

		choice := strings.TrimSpace(response.Values["selection"])
		entry, err := m.resolve(choice)
		if err != nil {
			values["selection"] = ""
			screen, row, col := m.screen(err.Error())
			// Redrawn with the message rather than looping silently: an
			// operator who typed 9 on a menu of four needs to be told, and the
			// message belongs on the screen, not in a log they cannot see.
			if _, err := go3270.ShowScreen(screen, values, row, col, conn); err != nil {
				return
			}
			continue
		}

		m.mu.Lock()
		m.chosen = entry.Name
		m.mu.Unlock()

		// Left on a screen that says what is happening. The web layer notices
		// the selection and moves the terminal to the host, which replaces
		// this; without it the operator would stare at a menu that appeared to
		// have ignored them for as long as the connection takes.
		connecting, row, col := m.connectingScreen(entry)
		_, _ = go3270.ShowScreen(connecting, map[string]string{}, row, col, conn)
		return
	}
}

// resolve turns what was typed into an entry.
//
// Both a number and a name are accepted. The number is what the screen invites
// and what a regular user will type without looking; the name is what somebody
// who knows the system will type, and refusing it would be pedantry.
func (m *Menu) resolve(choice string) (Entry, error) {
	if choice == "" {
		return Entry{}, errors.New("Type the number of a system and press Enter.")
	}
	if n, err := strconv.Atoi(choice); err == nil {
		if n < 1 || n > len(m.entries) {
			return Entry{}, fmt.Errorf("There is no system %d. Choose 1 to %d.", n, len(m.entries))
		}
		return m.entries[n-1], nil
	}
	for _, e := range m.entries {
		if strings.EqualFold(e.Name, choice) {
			return e, nil
		}
	}
	return Entry{}, fmt.Errorf("No system called %q on this menu.", choice)
}

// Fixed rows at the bottom of the display. The list grows down from the
// branding and stops before these, so a taller banner costs entries rather
// than overwriting the command line.
const (
	rowMessage = screenRows - 4
	rowPrompt  = screenRows - 3
	rowFooter  = screenRows - 2
	rowKeys    = screenRows - 1
	// selectionCol is where the input field's attribute byte sits; the value
	// starts one column after it.
	selectionCol = 17
)

// layout says where the list starts for this branding.
func layout(brand Branding) (heading, rule, firstItem int) {
	heading = len(brand.Banner) + 2
	return heading, heading + 1, heading + 2
}

// maxEntriesFor is how many hosts fit under this branding.
func maxEntriesFor(brand Branding) int {
	_, _, firstItem := layout(brand)
	n := rowMessage - 1 - firstItem
	if n < 1 {
		return 1
	}
	return n
}

// screen builds the selection display, with an optional message.
func (m *Menu) screen(message string) (go3270.Screen, int, int) {
	heading, rule, firstItem := layout(m.brand)

	screen := go3270.Screen{}
	for i, line := range m.brand.Banner {
		if strings.TrimSpace(line) == "" {
			continue
		}
		screen = append(screen, go3270.Field{Row: i, Col: 3, Intense: true, Content: line})
	}
	screen = append(screen, go3270.Field{
		Row: len(m.brand.Banner), Col: 3, Color: go3270.Turquoise,
		Content: m.brand.Title + "  -  SESSION SELECTION"})

	screen = append(screen,
		go3270.Field{Row: heading, Col: 3, Color: go3270.Blue, Content: "SEL"},
		go3270.Field{Row: heading, Col: 8, Color: go3270.Blue, Content: "SYSTEM"},
		go3270.Field{Row: heading, Col: 25, Color: go3270.Blue, Content: "DESCRIPTION"},
		go3270.Field{Row: rule, Col: 3, Color: go3270.Blue,
			Content: "---  ---------------  " + strings.Repeat("-", 45)},
	)

	for i, entry := range m.entries {
		row := firstItem + i
		screen = append(screen,
			go3270.Field{Row: row, Col: 4, Intense: true, Content: fmt.Sprintf("%2d", i+1)},
			go3270.Field{Row: row, Col: 8, Content: truncate(entry.Name, 15)},
			go3270.Field{Row: row, Col: 25, Color: go3270.Green, Content: truncate(describe(entry), 50)},
		)
	}

	if message != "" {
		screen = append(screen,
			go3270.Field{Row: rowMessage, Col: 3, Intense: true, Color: go3270.Red,
				Content: truncate(message, MaxLineWidth)})
	}

	screen = append(screen,
		go3270.Field{Row: rowPrompt, Col: 3, Content: "SELECTION ==>"},
		// The input field, and the field that closes it. Without the closing
		// field the input would run to the end of the line and swallow the
		// rest of the row.
		go3270.Field{Row: rowPrompt, Col: selectionCol, Name: "selection", Write: true,
			Highlighting: go3270.Underscore},
		go3270.Field{Row: rowPrompt, Col: selectionCol + 7},
	)
	if m.brand.Footer != "" {
		screen = append(screen,
			go3270.Field{Row: rowFooter, Col: 3, Color: go3270.Green, Content: m.brand.Footer})
	}
	screen = append(screen, go3270.Field{Row: rowKeys, Col: 3, Color: go3270.Turquoise,
		Content: "ENTER  Connect      PF3  Sign off      PF12  Clear"})

	return screen, rowPrompt, selectionCol + 1
}

// connectingScreen replaces the menu once a choice is made.
func (m *Menu) connectingScreen(entry Entry) (go3270.Screen, int, int) {
	return go3270.Screen{
		{Row: 0, Col: 3, Intense: true, Content: m.brand.Title},
		{Row: 2, Col: 3, Color: go3270.Turquoise, Content: "SESSION SELECTION"},
		{Row: 8, Col: 3, Intense: true, Content: "Connecting to " + truncate(entry.Name, 40)},
		{Row: 10, Col: 3, Color: go3270.Green, Content: truncate(describe(entry), 60)},
		{Row: 12, Col: 3, Content: "The session opens in a moment."},
	}, 12, 3
}

// describe is what the description column shows: the profile's own words where
// it has them, and the host it points at where it does not.
func describe(e Entry) string {
	description := strings.TrimSpace(e.Description)
	detail := strings.TrimSpace(e.Detail)
	switch {
	case description != "" && detail != "":
		return description + "  (" + detail + ")"
	case description != "":
		return description
	default:
		return detail
	}
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

// Preview renders the screen as plain text, one string per display row.
//
// The same builder the terminal draws from, laid out into a character grid, so
// what an administrator sees while editing the branding is the screen and not
// an impression of it.
func Preview(brand Branding, entries []Entry) []string {
	brand = brand.resolve()
	if max := maxEntriesFor(brand); len(entries) > max {
		entries = entries[:max]
	}
	m := &Menu{entries: entries, brand: brand}
	screen, _, _ := m.screen("")

	grid := make([][]rune, screenRows)
	for i := range grid {
		grid[i] = []rune(strings.Repeat(" ", 80))
	}
	for _, f := range screen {
		if f.Row < 0 || f.Row >= screenRows {
			continue
		}
		// A writable field shows as the underscores an operator would type
		// over, which is what the terminal draws for it.
		content := f.Content
		if f.Write && content == "" {
			content = strings.Repeat("_", 6)
		}
		for i, r := range content {
			col := f.Col + i
			if col < 0 || col >= 80 {
				break
			}
			grid[f.Row][col] = r
		}
	}

	out := make([]string, screenRows)
	for i, row := range grid {
		out[i] = strings.TrimRight(string(row), " ")
	}
	return out
}
