// SPDX-License-Identifier: AGPL-3.0-or-later

package sampleapps

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/racingmars/go3270"
)

// Signon, the main menu, and the two screens that explain the application to
// whoever is standing in front of it: the command reference and the terminal
// information display.

func (s *petSession) buildSignon() (go3270.Screen, int, int) {
	title := "PAWS AND CLAWS PET STORE"
	subtitle := "RETAIL AND BACK OFFICE SYSTEM"
	titleCol := (s.cols-len(title))/2 - 1
	if titleCol < 0 {
		titleCol = 0
	}
	subCol := (s.cols-len(subtitle))/2 - 1
	if subCol < 0 {
		subCol = 0
	}

	userRow := 10
	if s.rows < 24 {
		userRow = s.rows - 12
	}
	if userRow < 6 {
		userRow = 6
	}

	screen := go3270.Screen{
		{Row: 0, Col: 0, Color: go3270.Blue, Content: "PET010"},
		s.rightField(0, petToday(), go3270.Blue),
		{Row: 2, Col: titleCol, Intense: true, Color: go3270.White, Content: title},
		{Row: 3, Col: subCol, Color: go3270.Turquoise, Content: subtitle},
		{Row: 5, Col: 0, Color: go3270.Blue, Content: strings.Repeat("-", s.cols-2)},

		{Row: 7, Col: 0, Content: "Store . . . . . :"},
		{Row: 7, Col: 19, Color: go3270.Turquoise, Content: s.store.cfg.StoreNo + "  " + s.store.cfg.StoreName},
		{Row: 8, Col: 0, Content: "Region  . . . . :"},
		{Row: 8, Col: 19, Color: go3270.Turquoise, Content: s.store.cfg.Region + "   OPEN " + s.store.cfg.OpenTime + " TO " + s.store.cfg.CloseTime},

		{Row: userRow, Col: 0, Content: "User id . . . . :"},
		{Row: userRow, Col: 19, Name: "user", Write: true, Intense: true,
			Highlighting: go3270.Underscore, Content: petTrunc(s.value("user", ""), 8)},
		{Row: userRow, Col: 28, Autoskip: true},
		{Row: userRow + 1, Col: 0, Content: "Password  . . . :"},
		{Row: userRow + 1, Col: 19, Name: "pass", Write: true, Hidden: true},
		{Row: userRow + 1, Col: 28, Autoskip: true},

		{Row: userRow + 3, Col: 0, Color: go3270.Green,
			Content: petTrunc("Sign on as ADMIN / ADMIN for the back office, or any user id for the counter.", s.cols-2)},
		{Row: userRow + 4, Col: 0, Color: go3270.Green,
			Content: petTrunc("Every screen has a command line. HELP lists the commands once you are in.", s.cols-2)},
	}

	msgColor := go3270.Green
	if s.msgErr {
		msgColor = go3270.Red
	}
	screen = append(screen,
		go3270.Field{Row: s.rows - 4, Col: 0, Intense: true, Color: msgColor,
			Content: petTrunc(s.msg, s.cols-2)},
		go3270.Field{Row: s.rows - 2, Col: 0, Color: go3270.Turquoise,
			Content: "Enter=Sign on   PF3=Disconnect"},
		go3270.Field{Row: s.rows - 1, Col: 0, Color: go3270.Yellow,
			Content: petTrunc(s.statusLine(), s.cols-2)},
	)
	return screen, userRow, 20
}

func (s *petSession) handleSignon(resp go3270.Response) bool {
	switch resp.AID {
	case go3270.AIDPF3:
		return true
	case go3270.AIDClear:
		s.pending = map[string]string{}
		s.info("Cleared.")
		return false
	case go3270.AIDEnter:
	default:
		s.fail("Press Enter to sign on, or PF3 to disconnect.")
		return false
	}

	user := strings.ToUpper(strings.TrimSpace(resp.Values["user"]))
	pass := strings.TrimSpace(resp.Values["pass"])
	if user == "" {
		s.fail("A user id is required.")
		return false
	}
	if pass == "" {
		s.fail("A password is required. Any password is accepted on this sample.")
		return false
	}
	if len(user) > 8 {
		s.fail("User id must be 8 characters or fewer.")
		return false
	}
	if known := s.store.findUser(user); known != nil && known.Status == "LOCKED" {
		s.fail("User %s is locked. Sign on as ADMIN and unlock it on PET720.", user)
		return false
	}

	s.user = user
	s.role = petRoleFor(s.store, user)
	s.signedOn = true
	s.store.logAudit(user, "SIGNON", "SIGNED ON WITH ROLE "+s.role)
	s.pending = map[string]string{}
	s.page = petPageMenu
	s.info("Signed on as %s with role %s. Type a menu option or a command.", user, s.role)
	return false
}

// petRoleFor decides what a signon is allowed to do. A user id that matches a
// record in user maintenance gets that record's role; anything else is a
// counter clerk, so that exploring the application never requires knowing a
// password in advance.
func petRoleFor(store *petStore, user string) string {
	if u := store.findUser(user); u != nil {
		return u.Role
	}
	if strings.HasPrefix(user, "ADM") || strings.HasPrefix(user, "MGR") {
		return "MANAGER"
	}
	return "CLERK"
}

type petMenuEntry struct {
	option  int
	label   string
	command string
	page    petPage
}

var petMenuEntries = []petMenuEntry{
	{1, "CUSTOMER ENQUIRY AND MAINTENANCE", "CUST", petPageCustList},
	{2, "STOCK CATALOGUE AND ENQUIRY", "STOCK", petPageStockList},
	{3, "ORDER ENTRY - COUNTER SALES", "POS", petPageOrderEntry},
	{4, "ORDER ENQUIRY", "ORDER", petPageOrderList},
	{5, "INVOICE ENQUIRY", "INV", petPageInvoiceList},
	{6, "PAYMENT ENTRY", "PAY", petPagePayEntry},
	{7, "BACK OFFICE AND ADMINISTRATION", "ADMIN", petPageBackOffice},
	{8, "REPORTS", "RPT", petPageReports},
	{9, "TERMINAL AND MODEL INFORMATION", "INFO", petPageTermInfo},
	{10, "HELP AND COMMAND REFERENCE", "HELP", petPageHelp},
}

func (s *petSession) buildMenu() (go3270.Screen, int, int) {
	top := s.bodyTop()
	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Select an option, or type its command on the command line."},
	}
	for i, entry := range petMenuEntries {
		row := top + 2 + i
		if row > s.bodyBottom() {
			break
		}
		screen = append(screen,
			go3270.Field{Row: row, Col: 1, Intense: true, Color: go3270.White,
				Content: fmt.Sprintf("%2d", entry.option)},
			go3270.Field{Row: row, Col: 5, Content: petPad(entry.label, 44)},
			go3270.Field{Row: row, Col: 51, Color: go3270.Turquoise, Content: entry.command},
		)
	}

	optRow := top + 2 + len(petMenuEntries) + 1
	if optRow > s.bodyBottom() {
		optRow = s.bodyBottom()
	}
	screen = append(screen,
		go3270.Field{Row: optRow, Col: 0, Content: "Option  ===>"},
	)
	screen = append(screen, petNumericField(optRow, 13, 3, "opt", s.value("opt", ""))...)

	// A taller or wider terminal gets the trading position as well. This is
	// the plainest demonstration there is that the application reacts to the
	// model it was given rather than assuming 24x80.
	if s.bodyHeight() >= 18 || s.wide() {
		screen = append(screen, s.menuSummary(optRow)...)
	}
	return screen, -1, -1
}

func (s *petSession) menuSummary(optRow int) go3270.Screen {
	open, invoiced := 0, 0
	for _, o := range s.store.orders {
		switch o.Status {
		case "OPEN", "PICKED":
			open++
		case "INVOICED":
			invoiced++
		}
	}
	outstanding := 0
	for _, inv := range s.store.invoices {
		if inv.Status != "VOID" {
			outstanding += inv.outstanding()
		}
	}
	retail, _, units := s.store.stockValue()

	lines := []string{
		fmt.Sprintf("ACCOUNTS %-5d STOCK LINES %-5d UNITS %d", len(s.store.customers), len(s.store.stock), units),
		fmt.Sprintf("ORDERS OPEN %-3d INVOICED %-5d BELOW REORDER %d", open, invoiced, len(s.store.belowReorder())),
		fmt.Sprintf("STOCK AT RETAIL %s %s", s.store.cfg.Currency, petMoney(retail)),
		fmt.Sprintf("DEBT OUTSTANDING %s %s", s.store.cfg.Currency, petMoney(outstanding)),
	}

	// On a wide screen the panel sits beside the menu; otherwise it goes
	// underneath it.
	if s.wide() {
		screen := go3270.Screen{
			{Row: s.bodyTop(), Col: 70, Intense: true, Color: go3270.White, Content: "TRADING POSITION"},
		}
		for i, line := range lines {
			screen = append(screen, go3270.Field{Row: s.bodyTop() + 2 + i, Col: 70,
				Color: go3270.Turquoise, Content: petTrunc(line, s.cols-72)})
		}
		return screen
	}

	start := optRow + 2
	screen := go3270.Screen{}
	for i, line := range lines {
		row := start + i
		if row > s.bodyBottom() {
			break
		}
		screen = append(screen, go3270.Field{Row: row, Col: 0,
			Color: go3270.Turquoise, Content: petTrunc(line, s.cols-2)})
	}
	return screen
}

func (s *petSession) handleMenu(resp go3270.Response) {
	if resp.AID != go3270.AIDEnter {
		s.info("Key %s has no action on the main menu.", s.lastAID)
		return
	}
	opt := strings.TrimSpace(resp.Values["opt"])
	if opt == "" {
		s.fail("Enter an option number, or a command such as CUST or STOCK.")
		return
	}
	n, err := strconv.Atoi(opt)
	if err != nil {
		s.fail("%q is not an option number. Valid options are 1 to %d.", opt, len(petMenuEntries))
		return
	}
	s.pending["opt"] = ""
	s.selectMenuOption(n)
}

func (s *petSession) selectMenuOption(n int) bool {
	for _, entry := range petMenuEntries {
		if entry.option != n {
			continue
		}
		if entry.page == petPageOrderEntry {
			s.startOrderEntry("")
			return true
		}
		s.goTo(entry.page)
		s.info("%s. Command %s reaches this screen from anywhere.", entry.label, entry.command)
		return true
	}
	s.fail("Option %d does not exist. Valid options are 1 to %d.", n, len(petMenuEntries))
	return true
}

// The back office menu is a second level of the same idea, and is the one
// place where the role from signon changes what happens.

type petBackOfficeEntry struct {
	option  int
	label   string
	command string
	page    petPage
	note    string
}

var petBackOfficeEntries = []petBackOfficeEntry{
	{1, "SYSTEM CONFIGURATION", "CONFIG", petPageConfig, "VIEW ALL, CHANGE NEEDS ADMIN"},
	{2, "USER AND SECURITY MAINTENANCE", "USERS", petPageUsers, "VIEW ALL, CHANGE NEEDS ADMIN"},
	{3, "HOUSEKEEPING AND BATCH JOBS", "JOBS", petPageJobs, "RUN NEEDS ADMIN"},
	{4, "AUDIT LOG", "AUDIT", petPageAudit, "VIEW ALL"},
	{5, "STOCK CHECK AND REORDER", "CHECK", petPageStockCheck, "VIEW ALL"},
	{6, "REPORTS", "RPT", petPageReports, "VIEW ALL"},
}

func (s *petSession) buildBackOffice() (go3270.Screen, int, int) {
	top := s.bodyTop()
	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Back office functions. Select an option or use its command."},
	}
	for i, entry := range petBackOfficeEntries {
		row := top + 2 + i
		if row > s.bodyBottom() {
			break
		}
		screen = append(screen,
			go3270.Field{Row: row, Col: 1, Intense: true, Color: go3270.White,
				Content: fmt.Sprintf("%2d", entry.option)},
			go3270.Field{Row: row, Col: 5, Content: petPad(entry.label, 34)},
			go3270.Field{Row: row, Col: 41, Color: go3270.Turquoise, Content: petPad(entry.command, 8)},
			go3270.Field{Row: row, Col: 51, Color: go3270.Yellow, Content: petTrunc(entry.note, s.cols-53)},
		)
	}

	optRow := top + 2 + len(petBackOfficeEntries) + 1
	if optRow > s.bodyBottom()-2 {
		optRow = s.bodyBottom() - 2
	}
	access := fmt.Sprintf("Signed on as %s with role %s.", s.user, s.role)
	if s.isAdmin() {
		access += " This role may change configuration, users and jobs."
	} else {
		access += " This role may view, but not change. Sign on as ADMIN to change."
	}
	screen = append(screen,
		go3270.Field{Row: optRow, Col: 0, Content: "Option  ===>"},
	)
	screen = append(screen, petNumericField(optRow, 13, 3, "boopt", s.value("boopt", ""))...)
	screen = append(screen,
		go3270.Field{Row: optRow + 2, Col: 0, Color: go3270.Yellow, Content: petTrunc(access, s.cols-2)},
	)
	return screen, -1, -1
}

func (s *petSession) handleBackOffice(resp go3270.Response) {
	if resp.AID != go3270.AIDEnter {
		s.info("Key %s has no action on the back office menu.", s.lastAID)
		return
	}
	opt := strings.TrimSpace(resp.Values["boopt"])
	if opt == "" {
		s.fail("Enter an option number, or a command such as CONFIG or JOBS.")
		return
	}
	n, err := strconv.Atoi(opt)
	if err != nil {
		s.fail("%q is not an option number. Valid options are 1 to %d.", opt, len(petBackOfficeEntries))
		return
	}
	for _, entry := range petBackOfficeEntries {
		if entry.option == n {
			s.pending["boopt"] = ""
			s.goTo(entry.page)
			s.info("%s.", entry.label)
			return
		}
	}
	s.fail("Option %d does not exist. Valid options are 1 to %d.", n, len(petBackOfficeEntries))
}

// Reports menu.

type petReportEntry struct {
	option  int
	label   string
	command string
	page    petPage
}

var petReportEntries = []petReportEntry{
	{1, "SALES SUMMARY BY CATEGORY", "SALES", petPageRptSales},
	{2, "STOCK VALUATION", "VALUE", petPageRptStock},
	{3, "CUSTOMER BALANCES", "BALANCE", petPageRptBalances},
	{4, "STOCK CHECK AND REORDER", "CHECK", petPageStockCheck},
	{5, "PAYMENT HISTORY", "PAYHIST", petPagePayList},
}

func (s *petSession) buildReports() (go3270.Screen, int, int) {
	top := s.bodyTop()
	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Reports are produced from live data as at " + petNowStamp() + "."},
	}
	for i, entry := range petReportEntries {
		row := top + 2 + i
		if row > s.bodyBottom() {
			break
		}
		screen = append(screen,
			go3270.Field{Row: row, Col: 1, Intense: true, Color: go3270.White,
				Content: fmt.Sprintf("%2d", entry.option)},
			go3270.Field{Row: row, Col: 5, Content: petPad(entry.label, 38)},
			go3270.Field{Row: row, Col: 45, Color: go3270.Turquoise, Content: entry.command},
		)
	}
	optRow := top + 2 + len(petReportEntries) + 1
	if optRow > s.bodyBottom() {
		optRow = s.bodyBottom()
	}
	screen = append(screen,
		go3270.Field{Row: optRow, Col: 0, Content: "Option  ===>"},
	)
	screen = append(screen, petNumericField(optRow, 13, 3, "rptopt", s.value("rptopt", ""))...)
	return screen, -1, -1
}

func (s *petSession) handleReports(resp go3270.Response) {
	if resp.AID != go3270.AIDEnter {
		s.info("Key %s has no action on the reports menu.", s.lastAID)
		return
	}
	opt := strings.TrimSpace(resp.Values["rptopt"])
	if opt == "" {
		s.fail("Enter an option number, or a command such as SALES or VALUE.")
		return
	}
	n, err := strconv.Atoi(opt)
	if err != nil {
		s.fail("%q is not an option number. Valid options are 1 to %d.", opt, len(petReportEntries))
		return
	}
	for _, entry := range petReportEntries {
		if entry.option == n {
			s.pending["rptopt"] = ""
			s.goTo(entry.page)
			s.info("%s.", entry.label)
			return
		}
	}
	s.fail("Option %d does not exist. Valid options are 1 to %d.", n, len(petReportEntries))
}

// Terminal information. This screen exists to make the negotiated model
// visible and to let somebody switch between the default and alternate screen
// without leaving the application.

func (s *petSession) buildTermInfo() (go3270.Screen, int, int) {
	codepage := "UNKNOWN"
	ge := "NO"
	if s.dev != nil {
		if cp := s.dev.Codepage(); cp != nil {
			codepage = cp.ID()
		}
		if s.dev.SupportsGE() {
			ge = "YES"
		}
	}
	buffer := "DEFAULT 24x80"
	if s.useAlt {
		buffer = fmt.Sprintf("ALTERNATE %dx%d", s.altRows, s.altCols)
	}

	rows := [][2]string{
		{"Terminal type reported", s.terminalType()},
		{"Model implied by size", petModelName(s.altRows, s.altCols)},
		{"Default screen", "24 rows by 80 columns"},
		{"Alternate screen", fmt.Sprintf("%d rows by %d columns", s.altRows, s.altCols)},
		{"Buffer in use now", buffer},
		{"Usable body area", fmt.Sprintf("%d rows, %d columns", s.bodyHeight(), s.cols)},
		{"List rows per page", fmt.Sprintf("%d", s.listRows())},
		{"Wide layout active", petYesNo(s.wide())},
		{"Code page", codepage},
		{"Graphic escape support", ge},
		{"Last AID received", s.lastAID},
	}

	top := s.bodyTop()
	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "What this session negotiated with the terminal."},
	}
	for i, r := range rows {
		row := top + 2 + i
		if row > s.bodyBottom()-2 {
			break
		}
		screen = append(screen,
			go3270.Field{Row: row, Col: 0, Content: petPad(r[0], 26)},
			go3270.Field{Row: row, Col: 28, Intense: true, Color: go3270.Turquoise,
				Content: petTrunc(r[1], s.cols-30)},
		)
	}

	note := "PF9 redraws on the other buffer. A Model 2 has no larger alternate."
	if s.altAvailable {
		note = "PF9 redraws on the other buffer. Lists grow and shrink to fit."
	}
	screen = append(screen, go3270.Field{Row: s.bodyBottom(), Col: 0, Color: go3270.Yellow,
		Content: petTrunc(note, s.cols-2)})
	return screen, -1, -1
}

func petYesNo(b bool) string {
	if b {
		return "YES"
	}
	return "NO"
}

// Help. Everything the command line understands, in two columns.

type petHelpEntry struct{ command, meaning string }

var petHelpEntries = []petHelpEntry{
	{"MENU", "Main menu"},
	{"CUST", "Customer list"},
	{"NEWCUST", "Create an account"},
	{"STOCK", "Stock catalogue"},
	{"CHECK", "Items below reorder"},
	{"POS", "Order entry"},
	{"ORDER", "Order enquiry list"},
	{"INV", "Invoice list"},
	{"PAY", "Payment entry"},
	{"PAYHIST", "Payments received"},
	{"ADMIN", "Back office menu"},
	{"CONFIG", "System configuration"},
	{"USERS", "User and security"},
	{"JOBS", "Housekeeping, batch"},
	{"AUDIT", "Audit log"},
	{"RPT", "Reports menu"},
	{"SALES", "Sales summary"},
	{"VALUE", "Stock valuation"},
	{"BALANCE", "Customer balances"},
	{"INFO", "Terminal and model"},
	{"SIZE", "Switch screen size"},
	{"TOP", "Top of a list"},
	{"HELP", "This screen"},
	{"OFF", "Sign off"},
}

func (s *petSession) buildHelp() (go3270.Screen, int, int) {
	top := s.bodyTop()
	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Every screen has a command line. Type a command and press Enter."},
	}

	// The command list is laid out in as many columns as the screen has room
	// for, and the number of rows per column comes from the body height, so
	// the whole list is visible on a model 2 and stays visible on a model 5.
	columnCount := 2
	if s.wide() {
		columnCount = 3
	}
	columnWidth := (s.cols - 2) / columnCount
	descWidth := columnWidth - 17
	if descWidth < 8 {
		descWidth = 8
	}

	firstRow := top + 3
	lastRow := s.bodyBottom() - 1
	perColumn := lastRow - firstRow + 1
	if perColumn < 1 {
		perColumn = 1
	}
	// Never silently drop a command off the end: if the list does not fit,
	// squeeze it into the columns there are rather than lose the tail.
	if needed := (len(petHelpEntries) + columnCount - 1) / columnCount; needed > perColumn {
		perColumn = needed
	}

	for column := 0; column < columnCount; column++ {
		if column*perColumn >= len(petHelpEntries) {
			break
		}
		screen = append(screen, go3270.Field{Row: top + 2, Col: column * columnWidth,
			Intense: true, Color: go3270.White,
			Content: petPad("COMMAND", 15) + petTrunc("GOES TO", descWidth)})
	}
	for i, entry := range petHelpEntries {
		column := i / perColumn
		row := firstRow + (i % perColumn)
		if column >= columnCount || row > lastRow {
			continue
		}
		screen = append(screen, go3270.Field{Row: row, Col: column * columnWidth,
			Color:   go3270.Turquoise,
			Content: petPad(entry.command, 15) + petTrunc(entry.meaning, descWidth)})
	}

	screen = append(screen, go3270.Field{Row: s.bodyBottom(), Col: 0, Color: go3270.Yellow,
		Content: petTrunc("Name a record to go straight to it: CUST C0007, STOCK LIV-0001, INV I00003.", s.cols-2)})
	return screen, -1, -1
}
