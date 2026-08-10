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
	// Title is the name in the top-left of the title bar. Empty falls back to
	// the default.
	Title string
	// Banner is optional artwork drawn under the title bar, one screen row per
	// element. Empty means no artwork, which is both the default and what a
	// deployment says when it wants none — there is nothing to fall back to.
	Banner []string
	// Footer is an optional line above the key legend — an operations contact,
	// a classification marking, whatever the site puts there.
	Footer string
}

// DefaultBranding is 3270.io's own.
//
// A wordmark rather than artwork. Block-letter art was the first thing tried
// and it was the wrong thing: it ate a third of a 24-row screen, it read as
// decoration rather than as a system identifying itself, and it set an example
// a site would follow — a deployment pasting its own five-line logo in gets a
// menu with room for four hosts. A session manager announces itself in one
// line, and the room saved is the host list.
//
// Banner is still there for a site that wants artwork, bounded so it cannot
// crowd out the list. It is simply empty by default.
func DefaultBranding() Branding {
	return Branding{Title: "3270.io"}
}

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
//
// Only the title: an empty banner means no artwork, which is both the default
// and a deployment's way of saying it wants none.
func (b Branding) resolve() Branding {
	if strings.TrimSpace(b.Title) == "" {
		b.Title = DefaultBranding().Title
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
	// Not truncated: the list pages instead. A ceiling here would have been
	// the easy answer and the wrong one — the hosts it dropped would be
	// invisible to the operator and to whoever assigned them.
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
	page := 0
	message := ""

	for {
		screen, cursorRow, cursorCol := m.screen(page, message)
		message = ""
		response, err := go3270.ShowScreen(screen, values, cursorRow, cursorCol, conn)
		if err != nil {
			return
		}

		switch response.AID {
		case go3270.AIDPF3:
			m.mu.Lock()
			m.signedIn = true
			m.mu.Unlock()
			return

		case go3270.AIDPF8:
			// Forward, stopping at the last page rather than wrapping: an
			// operator paging to the end wants to know they are at the end.
			if page < m.pageCount()-1 {
				page++
			}
			values["selection"] = ""
			continue

		case go3270.AIDPF7:
			if page > 0 {
				page--
			}
			values["selection"] = ""
			continue

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
			// Redrawn with the message rather than looping silently: an
			// operator who typed 9 on a menu of four needs to be told, and the
			// message belongs on the screen, not in a log they cannot see.
			values["selection"] = ""
			message = err.Error()
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

// The screen, laid out as a session manager is: a title bar naming the system,
// a ruled header, the list, then a command line and a key legend fixed to the
// bottom. Everything between the header and the command line is the list, so
// branding costs entries and nothing else moves.
const (
	screenCols = 80
	// Left margin. One column in from the edge, which is where a 3270
	// application puts things — flush left reads as an error.
	marginCol = 1

	rowTitle     = 0
	rowTitleRule = 1
	rowMessage   = screenRows - 5
	rowStatus    = screenRows - 4
	rowPrompt    = screenRows - 3
	rowFooter    = screenRows - 2
	rowKeys      = screenRows - 1

	// selectionCol is where the input field's attribute byte sits; the value
	// starts one column after it.
	selectionCol = 15

	// Column positions for the list. SEL and SYSTEM are fixed; where
	// DESCRIPTION begins depends on how long the names on this menu actually
	// are — see nameWidth.
	colSel  = marginCol + 1
	colName = marginCol + 5

	// The gap between the two text columns. Two spaces, so a name that runs
	// to its full width still reads as a separate column.
	colGap = 2

	// The narrowest the SYSTEM column goes. Short names should not drag the
	// column in behind them and leave the list looking ragged against a
	// description column that starts halfway across the screen.
	minNameWidth = 15

	// What DESCRIPTION keeps whatever the names do. A description squeezed
	// below this stops being a sentence and starts being an ellipsis.
	minDescriptionWidth = 22

	// The widest SYSTEM can grow to, so the two floors above cannot fight.
	maxNameWidth = screenCols - marginCol - colName - colGap - minDescriptionWidth
)

// nameWidth is how many columns the SYSTEM column gets on this menu.
//
// It used to be a fixed 15, which is a fine width for the eight-character
// names a mainframe region has and a poor one for everything else: a profile
// called "Pet Store - Retail & Back Office" arrived as "Pet Store - Re…", and
// a list of those is a list an operator cannot read. The column is now as wide
// as the longest name on the menu, bounded so the description keeps a usable
// column of its own.
//
// Measured across every entry rather than the page being drawn, so the columns
// do not shift under an operator paging through the list.
func nameWidth(entries []Entry) int {
	widest := minNameWidth
	for _, e := range entries {
		if n := len(strings.TrimSpace(e.Name)); n > widest {
			widest = n
		}
	}
	if widest > maxNameWidth {
		widest = maxNameWidth
	}
	return widest
}

// columns says where the two text columns of the list sit, and how wide they
// are, for a given set of entries.
func columns(entries []Entry) (nameCols, descriptionCol, descriptionCols int) {
	nameCols = nameWidth(entries)
	descriptionCol = colName + nameCols + colGap
	return nameCols, descriptionCol, screenCols - marginCol - descriptionCol
}

// layout says where the list starts and how tall it is for this branding.
func layout(brand Branding) (heading, firstItem, perPage int) {
	heading = rowTitleRule + 1 + len(brand.Banner)
	firstItem = heading + 2
	perPage = rowMessage - 1 - firstItem
	if perPage < 1 {
		perPage = 1
	}
	return heading, firstItem, perPage
}

// maxEntriesFor is how many hosts one page holds.
func maxEntriesFor(brand Branding) int {
	_, _, perPage := layout(brand)
	return perPage
}

// pageCount is how many pages this menu has.
func (m *Menu) pageCount() int {
	_, _, perPage := layout(m.brand)
	pages := (len(m.entries) + perPage - 1) / perPage
	if pages < 1 {
		return 1
	}
	return pages
}

// screen builds the display for one page, with an optional message.
//
// page is zero-based and clamped, so a caller cannot page past either end.
func (m *Menu) screen(page int, message string) (go3270.Screen, int, int) {
	heading, firstItem, perPage := layout(m.brand)
	pages := m.pageCount()
	if page < 0 {
		page = 0
	}
	if page >= pages {
		page = pages - 1
	}

	rule := strings.Repeat("-", screenCols-2*marginCol)

	screen := go3270.Screen{
		// The title bar: who this is, and what it is. Right-aligned on the
		// same row rather than centred, because a centred title moves every
		// time a site renames itself.
		{Row: rowTitle, Col: marginCol, Intense: true, Content: truncate(m.brand.Title, 40)},
		{Row: rowTitle, Col: screenCols - marginCol - 15, Color: go3270.Turquoise,
			Content: "SESSION MANAGER"},
		{Row: rowTitleRule, Col: marginCol, Color: go3270.Blue, Content: rule},
	}

	for i, line := range m.brand.Banner {
		if strings.TrimSpace(line) == "" {
			continue
		}
		screen = append(screen, go3270.Field{
			Row: rowTitleRule + 1 + i, Col: marginCol, Intense: true, Content: line})
	}

	nameCols, colDescription, descriptionCols := columns(m.entries)

	screen = append(screen,
		go3270.Field{Row: heading, Col: colSel, Color: go3270.Blue, Content: "SEL"},
		go3270.Field{Row: heading, Col: colName, Color: go3270.Blue, Content: "SYSTEM"},
		go3270.Field{Row: heading, Col: colDescription, Color: go3270.Blue, Content: "DESCRIPTION"},
		go3270.Field{Row: heading + 1, Col: marginCol, Color: go3270.Blue, Content: rule},
	)

	first := page * perPage
	for i := first; i < len(m.entries) && i < first+perPage; i++ {
		entry := m.entries[i]
		row := firstItem + (i - first)
		screen = append(screen,
			// Numbered globally rather than per page, so the number beside a
			// system is the same whichever page it was found on — which is
			// what makes "type 12" something an operator can learn.
			go3270.Field{Row: row, Col: colSel, Intense: true, Content: fmt.Sprintf("%3d", i+1)},
			go3270.Field{Row: row, Col: colName, Content: truncate(entry.Name, nameCols)},
			go3270.Field{Row: row, Col: colDescription, Color: go3270.Green,
				Content: truncate(describe(entry), descriptionCols)},
		)
	}

	if message != "" {
		screen = append(screen, go3270.Field{Row: rowMessage, Col: marginCol,
			Intense: true, Color: go3270.Red, Content: truncate(message, MaxLineWidth)})
	}

	// The count and the page, on their own line above the command line. An
	// operator who cannot see that there is a second page will not look for it.
	status := fmt.Sprintf("%d system%s", len(m.entries), plural(len(m.entries)))
	if pages > 1 {
		status += fmt.Sprintf("   -   Page %d of %d", page+1, pages)
	}
	screen = append(screen,
		go3270.Field{Row: rowStatus, Col: marginCol, Color: go3270.Turquoise, Content: status},
		go3270.Field{Row: rowPrompt, Col: marginCol, Content: "SELECTION ==>"},
		go3270.Field{Row: rowPrompt, Col: selectionCol, Name: "selection", Write: true,
			Highlighting: go3270.Underscore},
		go3270.Field{Row: rowPrompt, Col: selectionCol + 7},
	)
	if m.brand.Footer != "" {
		screen = append(screen, go3270.Field{Row: rowFooter, Col: marginCol,
			Color: go3270.Green, Content: m.brand.Footer})
	}
	screen = append(screen, go3270.Field{Row: rowKeys, Col: marginCol,
		Color: go3270.Turquoise, Content: keyLegend(pages > 1)})

	return screen, rowPrompt, selectionCol + 1
}

// keyLegend names the keys that do something on this screen, and only those:
// offering PF7 on a menu with one page teaches an operator a key that does
// nothing.
func keyLegend(paged bool) string {
	if paged {
		return "ENTER Connect   PF7 Back   PF8 Forward   PF3 Sign off   PF12 Clear"
	}
	return "ENTER Connect   PF3 Sign off   PF12 Clear"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// connectingScreen replaces the menu once a choice is made.
func (m *Menu) connectingScreen(entry Entry) (go3270.Screen, int, int) {
	rule := strings.Repeat("-", screenCols-2*marginCol)
	return go3270.Screen{
		{Row: rowTitle, Col: marginCol, Intense: true, Content: truncate(m.brand.Title, 40)},
		{Row: rowTitle, Col: screenCols - marginCol - 15, Color: go3270.Turquoise,
			Content: "SESSION MANAGER"},
		{Row: rowTitleRule, Col: marginCol, Color: go3270.Blue, Content: rule},
		// Room for the whole name: this line is the confirmation that the
		// right system was chosen, and the list above it may itself have had
		// to shorten the name to keep its columns.
		{Row: 8, Col: marginCol, Intense: true, Content: "Connecting to " + truncate(entry.Name, MaxLineWidth-len("Connecting to "))},
		{Row: 10, Col: marginCol, Color: go3270.Green, Content: truncate(describe(entry), 70)},
		{Row: 12, Col: marginCol, Content: "The session opens in a moment."},
	}, 12, marginCol
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

// truncate bounds a string to what the column can draw, marking what it cut.
//
// The marker is ASCII. A "…" is not in the code page the screen is encoded
// into, so what the terminal actually drew was a question mark — a name that
// had been shortened was indistinguishable from a name somebody had typed a
// "?" into. Everything else here is already ASCII-only for the same reason.
func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + ">"
}

// Preview renders the screen as plain text, one string per display row.
//
// The same builder the terminal draws from, laid out into a character grid, so
// what an administrator sees while editing the branding is the screen and not
// an impression of it.
func Preview(brand Branding, entries []Entry) []string {
	return PreviewPage(brand, entries, 0)
}

// PreviewPage renders one page of the screen as plain text.
func PreviewPage(brand Branding, entries []Entry, page int) []string {
	brand = brand.resolve()
	m := &Menu{entries: entries, brand: brand}
	screen, _, _ := m.screen(page, "")

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
