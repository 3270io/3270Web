// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

// fakeApp is a small 3270 application behind the host.Host interface: a
// sign-on screen with a non-display password, a numbered main menu, three
// functions and a help screen. It is what the behavioural tests explore, so
// they assert on what the engine did to an application rather than on what
// it did to a canned screen.
type fakeApp struct {
	mu        sync.Mutex
	connected bool
	page      string
	msg       string
	screen    *host.Screen
	// legend overrides the main menu's key legend when set.
	menuLegend string
	// presses records "page:key" for every attention key.
	presses []string
	// lastPassword is what was in the password field at the last sign-on.
	lastPassword string
	// dropOn is a "page:key" press on which the host drops the session and
	// the terminal reconnects, landing on the help page rather than sign-on
	// so that only the connect count gives it away.
	dropOn   string
	connects int64
	// panicOnKey makes SendKey panic, for the recover test.
	panicOnKey bool
	// noScreen makes GetScreenSnapshot return nil.
	noScreen bool
}

const (
	fakeUserRow, fakeUserCol = 5, 14
	fakePassRow, fakePassCol = 6, 14
	fakeOptRow, fakeOptCol   = 10, 14
	fakeFindRow, fakeFindCol = 8, 14
	fakeMsgRow               = 20
	fakeLegendRow            = 22
)

func newFakeApp() *fakeApp {
	a := &fakeApp{connected: true, page: "signon"}
	a.render()
	return a
}

func (a *fakeApp) put(y, x int, text string) {
	for i, r := range []rune(text) {
		if x+i < a.screen.Width {
			a.screen.Buffer[y][x+i] = r
		}
	}
}

func (a *fakeApp) label(y int, text string) {
	a.put(y, 0, text)
	a.screen.Fields = append(a.screen.Fields,
		host.NewField(a.screen, host.AttrProtected, 0, y, a.screen.Width-1, y, host.AttrColDefault, host.AttrEhDefault))
}

func (a *fakeApp) input(y, x, length int, code byte) {
	// Split the row's protected field around the input.
	a.screen.Fields = append(a.screen.Fields,
		host.NewField(a.screen, code, x, y, x+length-1, y, host.AttrColDefault, host.AttrEhUnderscore))
}

// promptRow draws "label [input]" as a protected label followed by an
// unprotected field.
func (a *fakeApp) promptRow(y int, text string, x, length int, code byte) {
	a.put(y, 0, text)
	a.screen.Fields = append(a.screen.Fields,
		host.NewField(a.screen, host.AttrProtected, 0, y, x-2, y, host.AttrColDefault, host.AttrEhDefault))
	a.input(y, x, length, code)
}

func (a *fakeApp) render() {
	s := &host.Screen{Width: 80, Height: 24, IsFormatted: true, Buffer: make([][]rune, 24)}
	for i := range s.Buffer {
		s.Buffer[i] = make([]rune, 80)
		for j := range s.Buffer[i] {
			s.Buffer[i][j] = ' '
		}
	}
	a.screen = s
	switch a.page {
	case "signon":
		a.label(0, "APP010 SIGN ON                                              2026-11-16")
		a.promptRow(fakeUserRow, "User id . . :", fakeUserCol, 8, 0x00)
		a.promptRow(fakePassRow, "Password . . :", fakePassCol, 8, host.AttrDisp1|host.AttrDisp2)
		a.label(fakeLegendRow, "Enter=Sign on   PF3=Disconnect")
	case "menu":
		a.label(0, "APP100 MAIN MENU                                            2026-11-16")
		a.label(3, " 1  CUSTOMER ENQUIRY          CUST")
		a.label(4, " 2  STOCK ENQUIRY             STOCK")
		a.label(5, " 3  REPORTS                   RPT")
		a.promptRow(fakeOptRow, "Option ===>", fakeOptCol, 2, host.AttrNumeric)
		legend := a.menuLegend
		if legend == "" {
			legend = "PF1=Help   PF3=Sign off   PF12=Menu"
		}
		a.label(fakeLegendRow, legend)
	case "cust", "stock", "rpt":
		title := map[string]string{"cust": "APP200 CUSTOMER ENQUIRY", "stock": "APP300 STOCK ENQUIRY", "rpt": "APP800 REPORTS"}[a.page]
		a.label(0, title)
		a.label(3, strings.ToUpper(a.page)+" FUNCTION BODY TEXT THAT MAKES THIS SCREEN ITS OWN")
		a.label(4, strings.Repeat(strings.ToUpper(a.page)+" ", 12))
		// Enough distinct body for the three functions not to be taken for
		// one screen with different words on it.
		filler := map[string]int{"cust": 2, "stock": 6, "rpt": 10}[a.page]
		for i := 0; i < filler; i++ {
			a.label(9+i, fmt.Sprintf("%s-%02d: ======= ------- ####### ******* +++++++ %s", a.page, i, strings.Repeat("<>", 4*(i+1))))
		}
		a.promptRow(fakeFindRow, "Find . . . . :", fakeFindCol, 10, 0x00)
		a.label(fakeLegendRow, "PF1=Help   PF3=Back   PF12=Menu")
	case "help":
		a.label(0, "APP910 HELP")
		a.label(3, "HELP TEXT HELP TEXT HELP TEXT HELP TEXT HELP TEXT HELP TEXT")
		a.label(4, "MORE HELP TEXT MORE HELP TEXT MORE HELP TEXT MORE HELP TEXT")
		a.label(fakeLegendRow, "PF3=Back   PF12=Menu")
	}
	a.label(fakeMsgRow, a.msg)
}

func (a *fakeApp) fieldText(y, x, length int) string {
	return strings.TrimSpace(strings.ReplaceAll(string(a.screen.Buffer[y][x:x+length]), "\x00", " "))
}

func (a *fakeApp) Start() error      { a.mu.Lock(); a.connected = true; a.mu.Unlock(); return nil }
func (a *fakeApp) Stop() error       { a.mu.Lock(); a.connected = false; a.mu.Unlock(); return nil }
func (a *fakeApp) IsConnected() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.connected }
func (a *fakeApp) UpdateScreen() error {
	return nil
}
func (a *fakeApp) MoveCursor(int, int) error        { return nil }
func (a *fakeApp) SubmitScreen() error              { return nil }
func (a *fakeApp) SubmitUnformatted(string) error   { return nil }
func (a *fakeApp) Query(string) (string, error)     { return "", nil }
func (a *fakeApp) PrintText(string) (string, error) { return "", nil }
func (a *fakeApp) SubmitOperatorInput(func(*host.Screen) string) error {
	return nil
}
func (a *fakeApp) GetScreenSnapshot() *host.Screen {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.noScreen {
		return nil
	}
	return a.screen.Clone()
}
func (a *fakeApp) WriteStringAt(row, col int, text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if row < 0 || row >= a.screen.Height {
		return nil
	}
	a.put(row, col, text)
	return nil
}

func (a *fakeApp) currentPage() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.page
}

func (a *fakeApp) pressList() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.presses...)
}

func (a *fakeApp) SendKey(key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.panicOnKey {
		panic("fake host exploded")
	}
	a.presses = append(a.presses, a.page+":"+key)
	a.msg = ""
	if a.dropOn != "" && a.dropOn == a.page+":"+key {
		a.connects++
		a.page = "help"
		a.render()
		return nil
	}
	switch a.page {
	case "signon":
		if key == "Enter" {
			user := a.fieldText(fakeUserRow, fakeUserCol, 8)
			pass := a.fieldText(fakePassRow, fakePassCol, 8)
			switch {
			case user == "":
				a.msg = "A USER ID IS REQUIRED"
			case pass == "":
				a.msg = "A PASSWORD IS REQUIRED"
			default:
				a.lastPassword = pass
				a.page = "menu"
			}
		}
	case "menu":
		switch key {
		case "Enter":
			opt := a.fieldText(fakeOptRow, fakeOptCol, 2)
			switch opt {
			case "1":
				a.page = "cust"
			case "2":
				a.page = "stock"
			case "3":
				a.page = "rpt"
			case "":
				a.msg = "AN OPTION IS REQUIRED"
			default:
				a.msg = fmt.Sprintf("OPTION %s IS INVALID", opt)
			}
		case "PF(3)":
			a.page = "signon"
		case "PF(1)":
			a.page = "help"
		}
	case "help":
		if key == "PF(3)" || key == "PF(12)" {
			a.page = "menu"
		}
	default:
		switch key {
		case "PF(3)", "PF(12)":
			a.page = "menu"
		case "PF(1)":
			a.page = "help"
		case "Enter":
			if find := a.fieldText(fakeFindRow, fakeFindCol, 10); find != "" {
				a.msg = find + " NOT FOUND"
			}
		}
	}
	a.render()
	return nil
}

// runFakeApp runs the engine against the fake application until it stops,
// returning every attempt it made.
func runFakeApp(t *testing.T, app *fakeApp, cfg Config) (*Engine, []Attempt) {
	t.Helper()
	e := New(app, cfg)
	var mu sync.Mutex
	var all []Attempt
	e.attemptHook = func(a Attempt) {
		mu.Lock()
		all = append(all, a)
		mu.Unlock()
	}
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for e.Active() {
		if time.Now().After(deadline) {
			e.Stop()
			t.Fatalf("run did not stop")
		}
		time.Sleep(2 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	return e, append([]Attempt(nil), all...)
}

func fakeAppConfig(steps int) Config {
	cfg := DefaultConfig()
	cfg.MaxSteps = steps
	cfg.StepDelay = 0
	cfg.TimeBudget = 0
	cfg.Seed = 7
	cfg.ExcludeNoProgressEvents = false
	return cfg
}

// areaByLabelPrefix finds the mind-map area whose label starts with prefix.
func areaByLabelPrefix(t *testing.T, mm *MindMap, prefix string) *MindMapArea {
	t.Helper()
	if mm != nil {
		for _, area := range mm.Areas {
			if area != nil && strings.HasPrefix(area.Label, prefix) {
				return area
			}
		}
	}
	t.Fatalf("no area labelled %q", prefix)
	return nil
}

// ConnectCount lets the engine see a reconnect, as the real terminal does.
func (a *fakeApp) ConnectCount() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.connects
}
