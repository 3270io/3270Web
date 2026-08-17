// SPDX-License-Identifier: AGPL-3.0-or-later

package sampleapps

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/racingmars/go3270"
)

// The pet store sample application.
//
// The other bundled samples each demonstrate one thing: a form with
// validation, a feed reader, a matrix of every field attribute. This one is
// deliberately a whole system — a retail counter and the back office behind
// it — because most of what 3270Web does only becomes visible against an
// application with somewhere to go. Chaos exploration needs a screen graph
// with branches, dead ends and guarded doors; screen discovery needs screens
// that identify themselves; natural-language navigation needs a command line
// and stable object identifiers to aim at; and the model handling needs an
// application that actually notices how big the terminal is.
//
// It is also the default sample, which is why it opens on a signon screen and
// tells you what to type: the first screen somebody sees when they are
// evaluating a terminal emulator should not be a puzzle.
//
// Everything here is in memory and per connection. Sign on as ADMIN to reach
// the parts of the back office that change things.

type petPage int

const (
	petPageSignon petPage = iota
	petPageMenu
	petPageCustList
	petPageCustDetail
	petPageCustNew
	petPageStockList
	petPageStockDetail
	petPageStockCheck
	petPageOrderEntry
	petPageOrderList
	petPageOrderDetail
	petPageInvoiceList
	petPageInvoiceDetail
	petPagePayEntry
	petPagePayList
	petPageBackOffice
	petPageConfig
	petPageUsers
	petPageJobs
	petPageAudit
	petPageReports
	petPageRptSales
	petPageRptStock
	petPageRptBalances
	petPageTermInfo
	petPageHelp
)

// petScreenDef is the fixed part of a screen: what it calls itself, what the
// key legend says, and how to draw the part between the banner and the
// command line.
type petScreenDef struct {
	id    string
	title string
	keys  string
	build func(*petSession) (go3270.Screen, int, int)

	// bare screens draw no banner, command line or key legend of their own.
	// Only the signon screen is one.
	bare bool
}

const (
	petCmdLabel   = "Command ===>"
	petCmdAttrCol = 13
	petCmdTextCol = 14
)

type petSession struct {
	conn net.Conn
	dev  go3270.DevInfo

	// rows and cols are the screen being drawn on right now, which is either
	// the default 24x80 buffer or the terminal's alternate one. PF9 switches
	// between them.
	rows, cols       int
	altRows, altCols int
	altAvailable     bool
	useAlt           bool

	store *petStore

	user     string
	role     string
	signedOn bool

	page petPage

	msg     string
	msgErr  bool
	lastAID string

	// pending holds what the operator last typed on the current screen, so a
	// screen redrawn after a validation failure shows their input back rather
	// than silently reverting to the stored record. Cleared on navigation.
	pending map[string]string

	// List scroll positions, one per list screen.
	custTop, stockTop, orderTop, invTop, payTop int
	auditTop, userTop, jobTop, rptTop           int

	// The record each detail screen is looking at.
	cust    *petCustomer
	item    *petStockItem
	order   *petOrder
	invoice *petInvoice

	// The account order entry is currently selling to.
	cartCustID string

	// Housekeeping confirmation: the job the operator has been asked to
	// confirm before it runs.
	jobPending string
}

var petScreens = map[petPage]petScreenDef{
	petPageSignon: {
		id: "PET010", title: "SIGN ON", bare: true,
		build: (*petSession).buildSignon,
	},
	petPageMenu: {
		id: "PET100", title: "MAIN MENU",
		keys:  "PF1=Help PF3=Sign off PF9=Screen size PF13=Terminal info",
		build: (*petSession).buildMenu,
	},
	petPageCustList: {
		id: "PET200", title: "CUSTOMER ENQUIRY",
		keys:  "PF1=Help PF3=Menu PF5=Add PF7=Back PF8=Forward PF12=Menu",
		build: (*petSession).buildCustList,
	},
	petPageCustDetail: {
		id: "PET210", title: "CUSTOMER MAINTENANCE",
		keys:  "PF1=Help PF3=Cancel PF5=Save PF6=Invoices PF10=Prev PF11=Next PF12=Menu",
		build: (*petSession).buildCustDetail,
	},
	petPageCustNew: {
		id: "PET220", title: "NEW CUSTOMER ACCOUNT",
		keys:  "PF1=Help PF3=Cancel PF5=Create PF12=Menu",
		build: (*petSession).buildCustNew,
	},
	petPageStockList: {
		id: "PET300", title: "STOCK CATALOGUE",
		keys:  "PF1=Help PF3=Menu PF6=Reorder report PF7=Back PF8=Forward PF12=Menu",
		build: (*petSession).buildStockList,
	},
	petPageStockDetail: {
		id: "PET310", title: "STOCK ITEM AND ADJUSTMENT",
		keys:  "PF1=Help PF3=Cancel PF5=Apply PF10=Prev PF11=Next PF12=Menu",
		build: (*petSession).buildStockDetail,
	},
	petPageStockCheck: {
		id: "PET320", title: "STOCK CHECK AND REORDER",
		keys:  "PF1=Help PF3=Back PF7=Back PF8=Forward PF12=Menu",
		build: (*petSession).buildStockCheck,
	},
	petPageOrderEntry: {
		id: "PET400", title: "ORDER ENTRY",
		keys:  "PF1=Help PF3=Cancel PF4=Customer list PF5=Confirm order PF12=Menu",
		build: (*petSession).buildOrderEntry,
	},
	petPageOrderList: {
		id: "PET410", title: "ORDER ENQUIRY",
		keys:  "PF1=Help PF3=Menu PF5=New order PF7=Back PF8=Forward PF12=Menu",
		build: (*petSession).buildOrderList,
	},
	petPageOrderDetail: {
		id: "PET420", title: "ORDER DETAIL",
		keys:  "PF1=Help PF3=Back PF5=Raise invoice PF6=Cancel order PF12=Menu",
		build: (*petSession).buildOrderDetail,
	},
	petPageInvoiceList: {
		id: "PET500", title: "INVOICE ENQUIRY",
		keys:  "PF1=Help PF3=Menu PF7=Back PF8=Forward PF12=Menu",
		build: (*petSession).buildInvoiceList,
	},
	petPageInvoiceDetail: {
		id: "PET510", title: "INVOICE DETAIL",
		keys:  "PF1=Help PF3=Back PF5=Take payment PF12=Menu",
		build: (*petSession).buildInvoiceDetail,
	},
	petPagePayEntry: {
		id: "PET600", title: "PAYMENT ENTRY",
		keys:  "PF1=Help PF3=Cancel PF5=Post payment PF6=Payment history PF12=Menu",
		build: (*petSession).buildPayEntry,
	},
	petPagePayList: {
		id: "PET610", title: "PAYMENT HISTORY",
		keys:  "PF1=Help PF3=Back PF7=Back PF8=Forward PF12=Menu",
		build: (*petSession).buildPayList,
	},
	petPageBackOffice: {
		id: "PET700", title: "BACK OFFICE MENU",
		keys:  "PF1=Help PF3=Menu PF12=Menu",
		build: (*petSession).buildBackOffice,
	},
	petPageConfig: {
		id: "PET710", title: "SYSTEM CONFIGURATION",
		keys:  "PF1=Help PF3=Back PF5=Save (ADMIN) PF12=Menu",
		build: (*petSession).buildConfig,
	},
	petPageUsers: {
		id: "PET720", title: "USER AND SECURITY MAINTENANCE",
		keys:  "PF1=Help PF3=Back PF7=Back PF8=Forward PF12=Menu",
		build: (*petSession).buildUsers,
	},
	petPageJobs: {
		id: "PET730", title: "HOUSEKEEPING AND BATCH JOBS",
		keys:  "PF1=Help PF3=Back PF5=Confirm run PF7=Back PF8=Forward PF12=Menu",
		build: (*petSession).buildJobs,
	},
	petPageAudit: {
		id: "PET740", title: "AUDIT LOG",
		keys:  "PF1=Help PF3=Back PF7=Back PF8=Forward PF12=Menu",
		build: (*petSession).buildAudit,
	},
	petPageReports: {
		id: "PET800", title: "REPORTS MENU",
		keys:  "PF1=Help PF3=Menu PF12=Menu",
		build: (*petSession).buildReports,
	},
	petPageRptSales: {
		id: "PET810", title: "SALES SUMMARY",
		keys:  "PF1=Help PF3=Back PF12=Menu",
		build: (*petSession).buildRptSales,
	},
	petPageRptStock: {
		id: "PET820", title: "STOCK VALUATION",
		keys:  "PF1=Help PF3=Back PF12=Menu",
		build: (*petSession).buildRptStock,
	},
	petPageRptBalances: {
		id: "PET830", title: "CUSTOMER BALANCES",
		keys:  "PF1=Help PF3=Back PF7=Back PF8=Forward PF12=Menu",
		build: (*petSession).buildRptBalances,
	},
	petPageTermInfo: {
		id: "PET900", title: "TERMINAL AND MODEL INFORMATION",
		keys:  "PF1=Help PF3=Back PF9=Switch screen size PF12=Menu",
		build: (*petSession).buildTermInfo,
	},
	petPageHelp: {
		id: "PET910", title: "HELP AND COMMAND REFERENCE",
		keys:  "PF1=Help PF3=Back PF12=Menu",
		build: (*petSession).buildHelp,
	},
}

// petParent is where PF3 goes from each screen.
var petParent = map[petPage]petPage{
	petPageMenu:          petPageSignon,
	petPageCustList:      petPageMenu,
	petPageCustDetail:    petPageCustList,
	petPageCustNew:       petPageCustList,
	petPageStockList:     petPageMenu,
	petPageStockDetail:   petPageStockList,
	petPageStockCheck:    petPageStockList,
	petPageOrderEntry:    petPageMenu,
	petPageOrderList:     petPageMenu,
	petPageOrderDetail:   petPageOrderList,
	petPageInvoiceList:   petPageMenu,
	petPageInvoiceDetail: petPageInvoiceList,
	petPagePayEntry:      petPageMenu,
	petPagePayList:       petPagePayEntry,
	petPageBackOffice:    petPageMenu,
	petPageConfig:        petPageBackOffice,
	petPageUsers:         petPageBackOffice,
	petPageJobs:          petPageBackOffice,
	petPageAudit:         petPageBackOffice,
	petPageReports:       petPageMenu,
	petPageRptSales:      petPageReports,
	petPageRptStock:      petPageReports,
	petPageRptBalances:   petPageReports,
	petPageTermInfo:      petPageMenu,
	petPageHelp:          petPageMenu,
}

func handlePetStore(conn net.Conn) {
	defer conn.Close()

	dev, err := go3270.NegotiateTelnet(conn)
	if err != nil {
		return
	}

	s := &petSession{
		conn:    conn,
		dev:     dev,
		rows:    24,
		cols:    80,
		altRows: 24,
		altCols: 80,
		store:   newPetStore(),
		page:    petPageSignon,
		pending: map[string]string{},
		lastAID: "[none]",
	}
	if dev != nil {
		if r, c := dev.AltDimensions(); r >= 24 && c >= 80 {
			s.altRows, s.altCols = r, c
			s.altAvailable = r > 24 || c > 80
		}
	}
	// An application that has been given a bigger terminal should use it.
	// Somebody who starts this sample after setting S3270_MODEL=4 has said
	// what size they want; making them press a key to get it would be an odd
	// way to demonstrate that models are supported at all.
	s.useAlt = s.altAvailable
	s.applyDimensions()
	s.msg = "Sign on to continue. ADMIN / ADMIN gives full back office access."

	for {
		screen, row, col := s.render()
		resp, err := s.show(screen, row, col)
		if err != nil {
			return
		}
		s.lastAID = go3270.AIDtoString(resp.AID)
		if s.dispatch(resp) {
			return
		}
	}
}

func (s *petSession) applyDimensions() {
	if s.useAlt {
		s.rows, s.cols = s.altRows, s.altCols
		return
	}
	s.rows, s.cols = 24, 80
}

func (s *petSession) show(screen go3270.Screen, row, col int) (go3270.Response, error) {
	opts := go3270.ScreenOpts{CursorRow: row, CursorCol: col}
	if s.dev != nil {
		opts.Codepage = s.dev.Codepage()
		if s.useAlt {
			opts.AltScreen = s.dev
		}
	}
	return go3270.ShowScreenOpts(screen, nil, s.conn, opts)
}

func (s *petSession) render() (go3270.Screen, int, int) {
	def, ok := petScreens[s.page]
	if !ok {
		s.page = petPageMenu
		def = petScreens[petPageMenu]
	}
	body, row, col := def.build(s)
	if def.bare {
		return body, row, col
	}
	screen := s.chrome(def, body)
	if row < 0 {
		row, col = s.cmdRow(), petCmdTextCol
	}
	return screen, row, col
}

// Layout. The banner takes the top three rows and the command line and key
// legend the bottom four, whatever size the screen is; everything in between
// belongs to the screen being drawn.

func (s *petSession) bodyTop() int    { return 3 }
func (s *petSession) bodyBottom() int { return s.rows - 6 }
func (s *petSession) bodyHeight() int { return s.bodyBottom() - s.bodyTop() + 1 }
func (s *petSession) sepRow() int     { return s.rows - 5 }
func (s *petSession) msgRow() int     { return s.rows - 4 }
func (s *petSession) cmdRow() int     { return s.rows - 3 }
func (s *petSession) keyRow() int     { return s.rows - 2 }
func (s *petSession) infoRow() int    { return s.rows - 1 }

func (s *petSession) chrome(def petScreenDef, body go3270.Screen) go3270.Screen {
	screen := make(go3270.Screen, 0, len(body)+16)

	titleCol := (s.cols-len(def.title))/2 - 1
	if titleCol < 10 {
		titleCol = 10
	}
	screen = append(screen,
		go3270.Field{Row: 0, Col: 0, Color: go3270.Blue, Content: def.id},
		go3270.Field{Row: 0, Col: titleCol, Intense: true, Color: go3270.White, Content: def.title},
		s.rightField(0, petToday(), go3270.Blue),
		go3270.Field{Row: 1, Col: 0, Color: go3270.Turquoise,
			Content: petTrunc(fmt.Sprintf("STORE %s %s", s.store.cfg.StoreNo, s.store.cfg.StoreName), s.cols/2)},
		s.rightField(1, fmt.Sprintf("USER %-8s ROLE %-7s", s.user, s.role), go3270.Turquoise),
		go3270.Field{Row: 2, Col: 0, Color: go3270.Blue, Content: strings.Repeat("-", s.cols-2)},
	)

	screen = append(screen, body...)

	msgColor := go3270.Green
	if s.msgErr {
		msgColor = go3270.Red
	}
	screen = append(screen,
		go3270.Field{Row: s.sepRow(), Col: 0, Color: go3270.Blue, Content: strings.Repeat("-", s.cols-2)},
		go3270.Field{Row: s.msgRow(), Col: 0, Intense: true, Color: msgColor,
			Content: petTrunc(s.msg, s.cols-2)},
		go3270.Field{Row: s.cmdRow(), Col: 0, Color: go3270.White, Content: petCmdLabel},
	)
	screen = append(screen, petInputField(s.cmdRow(), petCmdAttrCol,
		s.cols-petCmdAttrCol-3, "cmd", s.pending["cmd"])...)
	screen = append(screen,
		go3270.Field{Row: s.keyRow(), Col: 0, Color: go3270.Turquoise, Content: petTrunc(def.keys, s.cols-2)},
		go3270.Field{Row: s.infoRow(), Col: 0, Color: go3270.Yellow, Content: petTrunc(s.statusLine(), s.cols-2)},
	)
	return screen
}

// statusLine is the strip along the bottom of every screen. It carries the
// negotiated terminal model because that is the one fact about a 3270 session
// that changes what every other screen looks like, and having it permanently
// on display saves anyone evaluating model support from having to go looking
// for it.
func (s *petSession) statusLine() string {
	buffer := "DEFAULT"
	if s.useAlt {
		buffer = "ALTERNATE"
	}
	line := fmt.Sprintf("MODEL %s  BUFFER %s %dx%d  %s",
		petModelName(s.altRows, s.altCols), buffer, s.rows, s.cols, s.terminalType())
	if s.altAvailable {
		return line + "  PF9 switches size"
	}
	return line + "  Type HELP for commands"
}

func (s *petSession) terminalType() string {
	if s.dev == nil {
		return "TERM UNKNOWN"
	}
	t := strings.TrimSpace(s.dev.TerminalType())
	if t == "" {
		return "TERM UNKNOWN"
	}
	return t
}

// petModelName names the 3278/3279 model that matches a screen size. A
// terminal whose alternate size is none of the four standard ones is reported
// as dynamic, which is what IBM-DYNAMIC clients negotiate.
func petModelName(rows, cols int) string {
	switch {
	case rows == 24 && cols == 80:
		return "2 (24x80)"
	case rows == 32 && cols == 80:
		return "3 (32x80)"
	case rows == 43 && cols == 80:
		return "4 (43x80)"
	case rows == 27 && cols == 132:
		return "5 (27x132)"
	}
	return fmt.Sprintf("DYNAMIC (%dx%d)", rows, cols)
}

func (s *petSession) rightField(row int, text string, color go3270.Color) go3270.Field {
	text = petTrunc(text, s.cols-2)
	col := s.cols - 1 - len(text)
	if col < 0 {
		col = 0
	}
	return go3270.Field{Row: row, Col: col, Color: color, Content: text}
}

// wide reports whether there is room for the extra columns some of the list
// screens carry. A 132-column terminal is the reason those columns exist.
func (s *petSession) wide() bool { return s.cols >= 120 }

func (s *petSession) isAdmin() bool { return s.role == "ADMIN" || s.role == "MANAGER" }

func (s *petSession) info(format string, args ...any) {
	s.msg = fmt.Sprintf(format, args...)
	s.msgErr = false
}

func (s *petSession) fail(format string, args ...any) {
	s.msg = fmt.Sprintf(format, args...)
	s.msgErr = true
}

// goTo moves to another screen and drops whatever was typed on the old one.
func (s *petSession) goTo(page petPage) {
	if s.page != page {
		s.pending = map[string]string{}
	}
	s.page = page
}

// value is what to draw in an input field: what the operator last typed if
// they are still on the same screen, otherwise the stored value.
func (s *petSession) value(name, stored string) string {
	if v, ok := s.pending[name]; ok {
		return v
	}
	return stored
}

func (s *petSession) capture(resp go3270.Response) {
	if s.pending == nil {
		s.pending = map[string]string{}
	}
	for k, v := range resp.Values {
		s.pending[k] = v
	}
}

func (s *petSession) dispatch(resp go3270.Response) bool {
	s.capture(resp)

	// Signing off puts the session back on the signon screen, and nothing may
	// take it off there again until somebody signs on.
	if !s.signedOn && s.page != petPageSignon {
		s.page = petPageSignon
		s.pending = map[string]string{}
	}

	if s.page == petPageSignon {
		return s.handleSignon(resp)
	}

	switch resp.AID {
	case go3270.AIDPF3:
		if s.page == petPageMenu {
			s.signOff("Signed off. Sign on again to continue.")
			return false
		}
		s.pending = map[string]string{}
		s.goTo(petParent[s.page])
		s.info("Returned to %s.", petScreens[s.page].title)
		return false
	case go3270.AIDPF1:
		s.goTo(petPageHelp)
		s.info("Command reference. PF3 returns to the main menu.")
		return false
	case go3270.AIDPF12, go3270.AIDPA1:
		s.goTo(petPageMenu)
		s.info("Main menu.")
		return false
	case go3270.AIDPF9:
		s.toggleScreenSize()
		return false
	case go3270.AIDPF13:
		s.goTo(petPageTermInfo)
		s.info("Terminal and model information.")
		return false
	case go3270.AIDClear:
		s.pending = map[string]string{}
		s.info("Screen input cleared.")
		return false
	case go3270.AIDPA2:
		s.info("")
		return false
	}

	// The command line outranks whatever else is on the screen: on a real
	// system it is how an operator gets out of a screen that has trapped
	// them, and it is how an AI assistant driving this session navigates.
	unknownCommand := ""
	if resp.AID == go3270.AIDEnter {
		if cmd := strings.TrimSpace(s.pending["cmd"]); cmd != "" {
			s.pending["cmd"] = ""
			if handled := s.runCommand(cmd); handled {
				return false
			}
			unknownCommand = cmd
		}
	}

	s.handlePage(resp)

	// An unrecognised command still lets the screen have its Enter: a
	// selection typed beside a line should not be thrown away because there
	// was also something unusable on the command line. But it must not vanish
	// in silence either, so it is reported unless the screen itself had
	// something more urgent to say.
	if unknownCommand != "" && !s.msgErr {
		s.fail("%q is not a command on this screen. Type HELP for the list.", unknownCommand)
	}
	return false
}

func (s *petSession) toggleScreenSize() {
	if !s.altAvailable {
		s.fail("This is a Model 2 terminal: 24x80 is all it has. Reconnect as model 3, 4 or 5.")
		return
	}
	s.useAlt = !s.useAlt
	s.applyDimensions()
	s.pending = map[string]string{}
	buffer := "default 24x80"
	if s.useAlt {
		buffer = fmt.Sprintf("alternate %dx%d", s.altRows, s.altCols)
	}
	s.info("Switched to the %s screen. Model %s.", buffer, petModelName(s.altRows, s.altCols))
}

func (s *petSession) signOff(message string) {
	s.store.logAudit(s.user, "SIGNOFF", "SESSION ENDED FROM "+petScreens[s.page].id)
	s.signedOn = false
	s.user = ""
	s.role = ""
	s.page = petPageSignon
	s.pending = map[string]string{}
	s.info("%s", message)
}

// handlePage routes everything the global handling above did not claim.
func (s *petSession) handlePage(resp go3270.Response) {
	switch s.page {
	case petPageMenu:
		s.handleMenu(resp)
	case petPageCustList:
		s.handleCustList(resp)
	case petPageCustDetail:
		s.handleCustDetail(resp)
	case petPageCustNew:
		s.handleCustNew(resp)
	case petPageStockList:
		s.handleStockList(resp)
	case petPageStockDetail:
		s.handleStockDetail(resp)
	case petPageStockCheck:
		s.handleStockCheck(resp)
	case petPageOrderEntry:
		s.handleOrderEntry(resp)
	case petPageOrderList:
		s.handleOrderList(resp)
	case petPageOrderDetail:
		s.handleOrderDetail(resp)
	case petPageInvoiceList:
		s.handleInvoiceList(resp)
	case petPageInvoiceDetail:
		s.handleInvoiceDetail(resp)
	case petPagePayEntry:
		s.handlePayEntry(resp)
	case petPagePayList:
		s.handleScrollOnly(resp, &s.payTop, len(s.store.payments))
	case petPageBackOffice:
		s.handleBackOffice(resp)
	case petPageConfig:
		s.handleConfig(resp)
	case petPageUsers:
		s.handleUsers(resp)
	case petPageJobs:
		s.handleJobs(resp)
	case petPageAudit:
		s.handleScrollOnly(resp, &s.auditTop, len(s.store.audit))
	case petPageReports:
		s.handleReports(resp)
	case petPageRptBalances:
		s.handleScrollOnly(resp, &s.rptTop, len(s.balanceRows()))
	default:
		if resp.AID != go3270.AIDEnter {
			s.info("Key %s has no action on this screen.", s.lastAID)
		}
	}
}

// handleScrollOnly serves the screens whose only interaction is paging.
func (s *petSession) handleScrollOnly(resp go3270.Response, top *int, count int) {
	switch resp.AID {
	case go3270.AIDPF7:
		s.scroll(top, -1, count)
	case go3270.AIDPF8:
		s.scroll(top, 1, count)
	case go3270.AIDEnter:
		s.info("Refreshed.")
	default:
		s.info("Key %s has no action on this screen.", s.lastAID)
	}
}

func (s *petSession) scroll(top *int, direction, count int) {
	pageSize := s.listRows()
	if pageSize < 1 {
		pageSize = 1
	}
	*top += direction * pageSize
	s.clampTop(top, count)
	switch {
	case count == 0:
		s.info("Nothing to display.")
	case direction < 0 && *top == 0:
		s.info("Top of list.")
	case *top+pageSize >= count:
		s.info("Bottom of list. %d entries.", count)
	default:
		s.info("Showing %d to %d of %d.", *top+1, min(*top+pageSize, count), count)
	}
}

func (s *petSession) clampTop(top *int, count int) {
	pageSize := s.listRows()
	if pageSize < 1 {
		pageSize = 1
	}
	maxTop := count - pageSize
	if maxTop < 0 {
		maxTop = 0
	}
	if *top > maxTop {
		*top = maxTop
	}
	if *top < 0 {
		*top = 0
	}
}

// listRows is how many data rows a list screen has room for: the body less
// the two heading rows and the "showing x to y of z" footer that every list
// draws. It is what makes a taller model show more of a list without any
// screen having to know which model it is on.
func (s *petSession) listRows() int {
	n := s.bodyHeight() - 3
	if n < 1 {
		return 1
	}
	return n
}

// runCommand executes the command line. It reports whether it recognised the
// command; an unrecognised one falls through to the screen's own handling so
// that a screen with its own input is not blocked by a typo.
func (s *petSession) runCommand(raw string) bool {
	fields := strings.Fields(strings.ToUpper(raw))
	if len(fields) == 0 {
		return false
	}
	verb := fields[0]
	arg := ""
	if len(fields) > 1 {
		arg = strings.Join(fields[1:], " ")
	}

	switch verb {
	case "MENU", "MAIN", "=X", "HOME":
		s.goTo(petPageMenu)
		s.info("Main menu.")
	case "CUST", "CUSTOMER", "CUSTOMERS":
		if arg != "" {
			return s.openCustomer(arg)
		}
		s.goTo(petPageCustList)
		s.info("Customer enquiry. Type S beside an account, or try CUST C0001.")
	case "NEWCUST", "ADDCUST":
		s.goTo(petPageCustNew)
		s.info("New customer account. Fill the fields and press PF5.")
	case "STOCK", "CAT", "CATALOGUE", "CATALOG", "ITEM":
		if arg != "" {
			return s.openStock(arg)
		}
		s.goTo(petPageStockList)
		s.info("Stock catalogue. Type S beside an item, or try STOCK LIV-0001.")
	case "CHECK", "REORDER", "LOWSTOCK":
		s.goTo(petPageStockCheck)
		s.info("Stock check. Items at or below their reorder level.")
	case "ORDER", "ORDERS":
		if arg != "" {
			return s.openOrder(arg)
		}
		s.goTo(petPageOrderList)
		s.info("Order enquiry.")
	case "POS", "SELL", "NEWORDER", "ENTRY":
		s.startOrderEntry(arg)
	case "INV", "INVC", "INVOICE", "INVOICES":
		if arg != "" {
			return s.openInvoice(arg)
		}
		s.goTo(petPageInvoiceList)
		s.info("Invoice enquiry.")
	case "PAY", "PAYMENT", "PAYMENTS":
		s.goTo(petPagePayEntry)
		if arg != "" {
			s.pending["payinv"] = arg
		}
		s.info("Payment entry. Enter an invoice number and an amount, then press PF5.")
	case "PAYHIST", "RECEIPTS":
		s.goTo(petPagePayList)
		s.info("Payment history.")
	case "ADMIN", "BO", "BACKOFFICE", "BACK":
		s.goTo(petPageBackOffice)
		s.info("Back office. Changes need an ADMIN or MANAGER sign-on.")
	case "CONFIG", "PARM", "PARMS", "SETUP":
		s.goTo(petPageConfig)
		s.info("System configuration.")
	case "USERS", "SEC", "SECURITY":
		s.goTo(petPageUsers)
		s.info("User and security maintenance.")
	case "JOBS", "HOUSEKEEPING", "BATCH", "MAINT":
		s.goTo(petPageJobs)
		s.info("Housekeeping. Type R beside a job and press PF5 to run it.")
	case "AUDIT", "LOG":
		s.goTo(petPageAudit)
		s.info("Audit log, newest first.")
	case "RPT", "REPORT", "REPORTS":
		s.goTo(petPageReports)
		s.info("Reports menu.")
	case "SALES":
		s.goTo(petPageRptSales)
		s.info("Sales summary.")
	case "VALUE", "VALUATION":
		s.goTo(petPageRptStock)
		s.info("Stock valuation.")
	case "BALANCE", "BALANCES", "DEBT":
		s.goTo(petPageRptBalances)
		s.info("Customer balances.")
	case "INFO", "TERM", "TERMINAL", "MODEL":
		s.goTo(petPageTermInfo)
		s.info("Terminal and model information.")
	case "SIZE", "SWITCH", "ALT":
		s.toggleScreenSize()
	case "HELP", "?":
		s.goTo(petPageHelp)
		s.info("Command reference.")
	case "OFF", "SIGNOFF", "LOGOFF", "QUIT":
		s.signOff("Signed off.")
	case "TOP":
		s.resetTops()
		s.info("Top of list.")
	default:
		if n, err := strconv.Atoi(verb); err == nil && s.page == petPageMenu {
			return s.selectMenuOption(n)
		}
		return false
	}
	return true
}

func (s *petSession) resetTops() {
	s.custTop, s.stockTop, s.orderTop = 0, 0, 0
	s.invTop, s.payTop, s.auditTop = 0, 0, 0
	s.userTop, s.jobTop, s.rptTop = 0, 0, 0
}

func (s *petSession) openCustomer(id string) bool {
	c := s.store.findCustomer(id)
	if c == nil {
		s.fail("Customer %s not found. CUST on its own lists them.", strings.ToUpper(id))
		s.goTo(petPageCustList)
		return true
	}
	s.cust = c
	s.goTo(petPageCustDetail)
	s.info("Account %s %s. PF5 saves changes, PF6 lists invoices.", c.ID, c.Name)
	return true
}

func (s *petSession) openStock(sku string) bool {
	item := s.store.findStock(sku)
	if item == nil {
		s.fail("Stock item %s not found. STOCK on its own lists them.", strings.ToUpper(sku))
		s.goTo(petPageStockList)
		return true
	}
	s.item = item
	s.goTo(petPageStockDetail)
	s.info("Item %s. Enter an adjustment and press PF5.", item.SKU)
	return true
}

func (s *petSession) openOrder(id string) bool {
	o := s.store.findOrder(id)
	if o == nil {
		s.fail("Order %s not found.", strings.ToUpper(id))
		s.goTo(petPageOrderList)
		return true
	}
	s.order = o
	s.goTo(petPageOrderDetail)
	s.info("Order %s for account %s.", o.ID, o.CustID)
	return true
}

func (s *petSession) openInvoice(id string) bool {
	inv := s.store.findInvoice(id)
	if inv == nil {
		s.fail("Invoice %s not found.", strings.ToUpper(id))
		s.goTo(petPageInvoiceList)
		return true
	}
	s.invoice = inv
	s.goTo(petPageInvoiceDetail)
	s.info("Invoice %s, %s outstanding.", inv.ID, petMoney(inv.outstanding()))
	return true
}

// petColumn describes one column of a list screen. Heading and data cells are
// built from the same description so they cannot drift apart, and a column
// marked wide only appears when the terminal is wide enough to have asked for
// it.
type petColumn struct {
	attr  int
	head  string
	width int
	right bool
	wide  bool
}

func (s *petSession) columnFits(c petColumn) bool {
	if c.wide && !s.wide() {
		return false
	}
	return c.attr+c.width+1 <= s.cols
}

func (s *petSession) headingFields(row int, columns []petColumn) []go3270.Field {
	out := make([]go3270.Field, 0, len(columns))
	for _, c := range columns {
		if !s.columnFits(c) {
			continue
		}
		out = append(out, go3270.Field{Row: row, Col: c.attr, Intense: true, Color: go3270.White,
			Content: petPad(c.head, c.width)})
	}
	return out
}

func (s *petSession) cell(row int, c petColumn, text string, color go3270.Color) []go3270.Field {
	if !s.columnFits(c) {
		return nil
	}
	content := petPad(text, c.width)
	if c.right {
		content = petRight(text, c.width)
	}
	return []go3270.Field{{Row: row, Col: c.attr, Color: color, Content: content}}
}

// petInputField builds a writable field of a given width along with the
// protected field that terminates it. A 3270 field runs until the next field
// attribute, so an input field without one after it swallows the rest of the
// line.
//
// The content is truncated to the field width. Redisplayed input is the value
// the operator last sent, and there is nothing stopping a client — or chaos
// mode — from sending more characters than the field was drawn with; writing
// them back out unchecked would push the terminating attribute along the row
// and corrupt everything to the right of it.
func petInputField(row, col, width int, name, content string) []go3270.Field {
	return []go3270.Field{
		{Row: row, Col: col, Name: name, Write: true, Highlighting: go3270.Underscore,
			Content: petTrunc(content, width)},
		{Row: row, Col: col + width + 1, Autoskip: true},
	}
}

// petSelectField is the one-character action column down the left of every
// list screen.
func petSelectField(row int, name, content string) []go3270.Field {
	return []go3270.Field{
		{Row: row, Col: 0, Name: name, Write: true, Intense: true, Content: petTrunc(content, 1)},
		{Row: row, Col: 2, Autoskip: true},
	}
}

// petNumericField is petInputField for a field that should only accept digits.
func petNumericField(row, col, width int, name, content string) []go3270.Field {
	fields := petInputField(row, col, width, name, content)
	fields[0].NumericOnly = true
	return fields
}

func petTrunc(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(text) <= width {
		return text
	}
	return text[:width]
}

func petPad(text string, width int) string {
	text = petTrunc(text, width)
	if len(text) >= width {
		return text
	}
	return text + strings.Repeat(" ", width-len(text))
}

func petRight(text string, width int) string {
	text = petTrunc(text, width)
	if len(text) >= width {
		return text
	}
	return strings.Repeat(" ", width-len(text)) + text
}
