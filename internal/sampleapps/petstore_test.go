package sampleapps

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/racingmars/go3270"
)

// The layout tests below draw every screen the pet store has, at every screen
// size a 3270 terminal can negotiate, and check the things a 3270 datastream
// will not tell you about until somebody is looking at a terminal: fields off
// the bottom of the screen, two fields fighting over the same cell, text
// running into the attribute byte of the field after it, and duplicate names
// on writable fields (which silently loses input).

type petLayoutSize struct {
	name       string
	rows, cols int
}

var petLayoutSizes = []petLayoutSize{
	{"model 2 24x80", 24, 80},
	{"model 3 32x80", 32, 80},
	{"model 4 43x80", 43, 80},
	{"model 5 27x132", 27, 132},
}

func newTestSession(rows, cols int) *petSession {
	s := &petSession{
		store:        newPetStore(),
		page:         petPageMenu,
		pending:      map[string]string{},
		lastAID:      "Enter",
		user:         "ADMIN",
		role:         "ADMIN",
		signedOn:     true,
		altRows:      rows,
		altCols:      cols,
		altAvailable: rows > 24 || cols > 80,
	}
	s.useAlt = s.altAvailable
	s.applyDimensions()
	s.msg = "A message long enough to be worth truncating on a narrow screen, over and over again."
	return s
}

// petAllPages is every page the application can be on, so a new screen added
// without a test still gets drawn by these.
func petAllPages() []petPage {
	pages := make([]petPage, 0, len(petScreens))
	for page := range petScreens {
		pages = append(pages, page)
	}
	return pages
}

func TestPetStoreScreensFitEverySupportedModel(t *testing.T) {
	for _, size := range petLayoutSizes {
		for _, page := range petAllPages() {
			s := newTestSession(size.rows, size.cols)
			s.page = page
			// Give the detail screens something to show, and the list
			// screens somewhere in the middle to be scrolled to.
			s.cust = s.store.customers[3]
			s.item = s.store.stock[5]
			s.order = s.store.orders[0]
			s.invoice = s.store.invoices[0]
			s.custTop, s.stockTop, s.orderTop = 5, 7, 2
			s.invTop, s.payTop, s.auditTop, s.userTop, s.jobTop = 2, 1, 3, 1, 2
			s.jobPending = "EODCLOSE"

			screen, curRow, curCol := s.render()
			id := petScreens[page].id
			name := fmt.Sprintf("%s/%s", size.name, id)

			checkScreenBounds(t, name, screen, s.rows, s.cols)
			checkNoFieldCollisions(t, name, screen, s.cols)
			checkUniqueWritableNames(t, name, screen)

			if curRow < 0 || curRow >= s.rows || curCol < 0 || curCol >= s.cols {
				t.Errorf("%s: cursor at %d,%d is off a %dx%d screen", name, curRow, curCol, s.rows, s.cols)
			}
		}
	}
}

func checkScreenBounds(t *testing.T, name string, screen go3270.Screen, rows, cols int) {
	t.Helper()
	for i, f := range screen {
		if f.Row < 0 || f.Row >= rows {
			t.Errorf("%s: field %d (%q) at row %d is off a %d row screen", name, i, f.Content, f.Row, rows)
		}
		if f.Col < 0 || f.Col >= cols {
			t.Errorf("%s: field %d (%q) at col %d is off a %d column screen", name, i, f.Content, f.Col, cols)
		}
		if end := f.Col + len(f.Content); end > cols {
			t.Errorf("%s: field %d (%q) at col %d runs to col %d, past the %d column screen",
				name, i, f.Content, f.Col, end, cols)
		}
	}
}

// checkNoFieldCollisions looks for two fields claiming the same cell, and for
// a field whose text runs into the attribute byte of the next one. Both show
// up on a real terminal as text that has been chewed off.
func checkNoFieldCollisions(t *testing.T, name string, screen go3270.Screen, cols int) {
	t.Helper()
	type claim struct {
		index int
		what  string
	}
	occupied := map[int]claim{}
	mark := func(row, col, index int, what string) {
		key := row*cols + col
		if prior, taken := occupied[key]; taken {
			t.Errorf("%s: field %d (%s) collides with field %d (%s) at row %d col %d",
				name, index, what, prior.index, prior.what, row, col)
			return
		}
		occupied[key] = claim{index: index, what: what}
	}
	for i, f := range screen {
		if f.PositionOnly || f.AttributeOnly {
			continue
		}
		mark(f.Row, f.Col, i, fmt.Sprintf("attribute of %q", f.Content))
		for c := 0; c < len(f.Content); c++ {
			mark(f.Row, f.Col+1+c, i, fmt.Sprintf("text of %q", f.Content))
		}
	}
}

func checkUniqueWritableNames(t *testing.T, name string, screen go3270.Screen) {
	t.Helper()
	seen := map[string]bool{}
	for _, f := range screen {
		if !f.Write || f.Name == "" {
			continue
		}
		if seen[f.Name] {
			t.Errorf("%s: two writable fields are both named %q; the second would lose its input", name, f.Name)
		}
		seen[f.Name] = true
	}
}

// Every screen has to identify itself, because that identifier is what screen
// discovery and an AI assistant navigate by.
func TestPetStoreScreensCarryTheirIdentifier(t *testing.T) {
	seen := map[string]petPage{}
	for _, page := range petAllPages() {
		def := petScreens[page]
		if def.id == "" {
			t.Errorf("page %d has no screen identifier", page)
			continue
		}
		if other, dup := seen[def.id]; dup {
			t.Errorf("screen id %s is used by pages %d and %d", def.id, other, page)
		}
		seen[def.id] = page

		s := newTestSession(24, 80)
		s.page = page
		screen, _, _ := s.render()
		found := false
		for _, f := range screen {
			if strings.Contains(f.Content, def.id) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("screen %s does not print its own identifier", def.id)
		}
	}
}

// Every screen except signon must be reachable from the command line, or an
// assistant driving the session by name cannot get to it.
func TestPetStoreCommandsReachEveryScreen(t *testing.T) {
	reachable := map[petPage]bool{petPageSignon: true}
	commands := []string{}
	for _, entry := range petHelpEntries {
		commands = append(commands, entry.command)
	}
	// The detail screens are reached by naming the record, which is the form
	// the help screen documents in its footer.
	commands = append(commands, "CUST C0001", "STOCK LIV-0001", "INV I00001", "ORDER O00001")

	for _, command := range commands {
		s := newTestSession(24, 80)
		if !s.runCommand(command) {
			t.Errorf("command %q was not recognised", command)
			continue
		}
		reachable[s.page] = true
	}
	for _, page := range petAllPages() {
		if !reachable[page] {
			t.Errorf("screen %s cannot be reached from the command line", petScreens[page].id)
		}
	}
}

// A heading wider than its column is silently chopped, which turns "INVOICE"
// into "INVOIC" and is only ever noticed by somebody reading the terminal.
func TestPetStoreColumnHeadingsFitTheirColumns(t *testing.T) {
	sets := map[string][]petColumn{
		"customers": petCustColumns,
		"stock":     petStockColumns,
		"check":     petCheckColumns,
		"orders":    petOrderColumns,
		"invoices":  petInvoiceColumns,
		"payments":  petPaymentColumns,
		"lines":     petLineColumns,
		"users":     petUserColumns,
		"jobs":      petJobColumns,
	}
	for name, columns := range sets {
		for i, c := range columns {
			if len(c.head) > c.width {
				t.Errorf("%s column %d heading %q is %d wide but the column is %d",
					name, i, c.head, len(c.head), c.width)
			}
			if i+1 < len(columns) && c.attr+c.width+1 > columns[i+1].attr {
				t.Errorf("%s column %d (%q) runs into column %d (%q)",
					name, i, c.head, i+1, columns[i+1].head)
			}
		}
	}
}

func TestPetStoreCommandArgumentsOpenTheNamedRecord(t *testing.T) {
	s := newTestSession(24, 80)

	if !s.runCommand("CUST C0004") || s.page != petPageCustDetail || s.cust.ID != "C0004" {
		t.Fatalf("CUST C0004 did not open account C0004, landed on %s", petScreens[s.page].id)
	}
	if !s.runCommand("STOCK LIV-0006") || s.page != petPageStockDetail || s.item.SKU != "LIV-0006" {
		t.Fatalf("STOCK LIV-0006 did not open that item, landed on %s", petScreens[s.page].id)
	}
	if !s.runCommand("INV I00002") || s.page != petPageInvoiceDetail || s.invoice.ID != "I00002" {
		t.Fatalf("INV I00002 did not open that invoice, landed on %s", petScreens[s.page].id)
	}
	if !s.runCommand("ORDER O00003") || s.page != petPageOrderDetail || s.order.ID != "O00003" {
		t.Fatalf("ORDER O00003 did not open that order, landed on %s", petScreens[s.page].id)
	}

	// A name that does not exist has to say so rather than open something
	// else, and has to leave the operator somewhere they can recover from.
	s.runCommand("CUST C9999")
	if !s.msgErr || s.page != petPageCustList {
		t.Errorf("an unknown account should report an error and show the list, got %q on %s",
			s.msg, petScreens[s.page].id)
	}
}

// The selling chain is the part with consequences, so it gets driven end to
// end: order, invoice (which moves stock), payment (which clears the debt).
func TestPetStoreSellingChain(t *testing.T) {
	s := newTestSession(24, 80)
	item := s.store.findStock("FOD-0001")
	startQty := item.Qty
	customer := s.store.findCustomer("C0002")
	startBalance := s.store.customerBalance(customer.ID)

	s.startOrderEntry("C0002")
	s.confirmOrder(go3270.Response{Values: map[string]string{
		"ocust": "C0002", "osku0": "FOD-0001", "oqty0": "3",
	}})
	if s.page != petPageOrderDetail || s.order == nil {
		t.Fatalf("confirming an order did not open the order: %q", s.msg)
	}
	order := s.order
	if got, want := order.net(), 3*item.Price; got != want {
		t.Errorf("order net = %d, want %d", got, want)
	}
	if item.Qty != startQty {
		t.Errorf("raising an order moved stock; it should not until the invoice")
	}

	s.raiseInvoice(order)
	if s.page != petPageInvoiceDetail || s.invoice == nil {
		t.Fatalf("raising an invoice did not open it: %q", s.msg)
	}
	inv := s.invoice
	if item.Qty != startQty-3 {
		t.Errorf("stock on hand = %d, want %d after invoicing 3", item.Qty, startQty-3)
	}
	if want := order.net() + s.store.vatOn(order.net()); inv.Total != want {
		t.Errorf("invoice total = %d, want %d", inv.Total, want)
	}
	if got := s.store.customerBalance(customer.ID); got != startBalance+inv.Total {
		t.Errorf("customer balance = %d, want %d", got, startBalance+inv.Total)
	}

	// An invoice cannot be overpaid.
	s.postPayment(go3270.Response{Values: map[string]string{
		"payinv": inv.ID, "payamt": petMoney(inv.Total + 100), "paymeth": "CARD",
	}})
	if !s.msgErr {
		t.Errorf("an overpayment was accepted: %q", s.msg)
	}
	if inv.Paid != 0 {
		t.Errorf("a refused payment still moved money: paid = %d", inv.Paid)
	}

	// A part payment leaves it PART, and the rest clears it.
	s.postPayment(go3270.Response{Values: map[string]string{
		"payinv": inv.ID, "payamt": petMoney(inv.Total / 2), "paymeth": "CASH",
	}})
	if s.msgErr || inv.Status != "PART" {
		t.Fatalf("part payment left invoice %s (%q)", inv.Status, s.msg)
	}
	s.postPayment(go3270.Response{Values: map[string]string{
		"payinv": inv.ID, "payamt": petMoney(inv.outstanding()), "paymeth": "ACCT",
	}})
	if s.msgErr || inv.Status != "PAID" || inv.outstanding() != 0 {
		t.Fatalf("final payment left invoice %s with %d outstanding (%q)",
			inv.Status, inv.outstanding(), s.msg)
	}
	if got := s.store.customerBalance(customer.ID); got != startBalance {
		t.Errorf("customer balance = %d after paying in full, want %d", got, startBalance)
	}

	// And an order cannot be invoiced for stock that is not there.
	s.startOrderEntry("C0002")
	s.confirmOrder(go3270.Response{Values: map[string]string{
		"ocust": "C0002", "osku0": "FOD-0001", "oqty0": "9999",
	}})
	s.raiseInvoice(s.order)
	if !s.msgErr || !strings.Contains(s.msg, "on hand") {
		t.Errorf("invoicing more than the shelf holds was allowed: %q", s.msg)
	}
}

func TestPetStoreRoleGuardsTheBackOffice(t *testing.T) {
	s := newTestSession(24, 80)
	s.user, s.role = "PETOP1", "CLERK"

	// A clerk may look at the back office...
	if !s.runCommand("CONFIG") || s.page != petPageConfig {
		t.Fatalf("a clerk could not open configuration")
	}
	// ...but not change it.
	s.handleConfig(go3270.Response{AID: go3270.AIDPF5, Values: map[string]string{
		"gname": "SOMETHING ELSE", "gno": "1", "gcur": "GBP", "gvat": "20.00",
		"grord": "25", "gloy": "2.50", "gcred": "30", "gopen": "08:30", "gclose": "18:00",
	}})
	if !s.msgErr || s.store.cfg.StoreName == "SOMETHING ELSE" {
		t.Errorf("a clerk changed the configuration: %q", s.msg)
	}

	s.role = "ADMIN"
	s.handleConfig(go3270.Response{AID: go3270.AIDPF5, Values: map[string]string{
		"gname": "SOMETHING ELSE", "gno": "1", "gcur": "GBP", "gvat": "17.50",
		"grord": "25", "gloy": "2.50", "gcred": "30", "gopen": "08:30", "gclose": "18:00",
	}})
	if s.msgErr || s.store.cfg.StoreName != "SOMETHING ELSE" || s.store.cfg.VATRate != 1750 {
		t.Errorf("an administrator could not change the configuration: %q", s.msg)
	}

	// A bad VAT rate is refused rather than stored.
	s.handleConfig(go3270.Response{AID: go3270.AIDPF5, Values: map[string]string{
		"gname": "STILL HERE", "gno": "1", "gcur": "GBP", "gvat": "99.00",
		"grord": "25", "gloy": "2.50", "gcred": "30", "gopen": "08:30", "gclose": "18:00",
	}})
	if !s.msgErr || s.store.cfg.VATRate != 1750 {
		t.Errorf("a 99%% VAT rate was accepted: %q", s.msg)
	}
}

func TestPetStoreHousekeepingJobsDoSomething(t *testing.T) {
	s := newTestSession(24, 80)
	s.page = petPageJobs

	// The purge really removes closed accounts with nothing owing.
	closed := 0
	for _, c := range s.store.customers {
		if c.Status == "CLOSED" && s.store.customerBalance(c.ID) == 0 {
			closed++
		}
	}
	if closed == 0 {
		t.Fatal("the seed data has no closed account for the purge to find")
	}
	before := len(s.store.customers)
	s.jobPending = "CUSTPURG"
	s.runPendingJob()
	if s.msgErr {
		t.Fatalf("CUSTPURG failed: %q", s.msg)
	}
	if got := len(s.store.customers); got != before-closed {
		t.Errorf("after the purge %d accounts remain, want %d", got, before-closed)
	}
	if job := s.store.findJob("CUSTPURG"); job.Status != "OK" || job.LastRun != petToday() {
		t.Errorf("CUSTPURG did not record its run: %+v", job)
	}

	// A clerk cannot run one at all.
	s.role = "CLERK"
	s.jobPending = "EODCLOSE"
	s.runPendingJob()
	if !s.msgErr {
		t.Errorf("a clerk ran a batch job: %q", s.msg)
	}
}

func TestPetStoreScreenSizeSwitching(t *testing.T) {
	s := newTestSession(32, 80)
	if !s.useAlt || s.rows != 32 {
		t.Fatalf("a model 3 session did not start on the alternate screen: %dx%d", s.rows, s.cols)
	}
	tallRows := s.listRows()

	s.toggleScreenSize()
	if s.useAlt || s.rows != 24 {
		t.Fatalf("PF9 did not fall back to the default screen: %dx%d", s.rows, s.cols)
	}
	if short := s.listRows(); short >= tallRows {
		t.Errorf("a 24 row screen lists %d rows, a 32 row screen %d; the lists are not adapting", short, tallRows)
	}

	// A model 2 terminal has nowhere to switch to and has to say so rather
	// than draw a screen the terminal cannot show.
	m2 := newTestSession(24, 80)
	m2.toggleScreenSize()
	if !m2.msgErr || m2.rows != 24 {
		t.Errorf("a model 2 session switched to a screen it does not have: %q", m2.msg)
	}
	if got := petModelName(27, 132); got != "5 (27x132)" {
		t.Errorf("petModelName(27,132) = %q", got)
	}
	if got := petModelName(30, 100); !strings.HasPrefix(got, "DYNAMIC") {
		t.Errorf("petModelName(30,100) = %q, want a dynamic size", got)
	}
}

func TestPetStoreMoneyParsing(t *testing.T) {
	cases := []struct {
		in    string
		want  int
		valid bool
	}{
		{"12.99", 1299, true},
		{"12", 1200, true},
		{"1,234.50", 123450, true},
		{"0.05", 5, true},
		{"12.5", 1250, true},
		{"", 0, false},
		{"abc", 0, false},
		{"12.345", 0, false},
		{"12.9a", 0, false},
	}
	for _, c := range cases {
		got, ok := petParseMoney(c.in)
		if ok != c.valid || (ok && got != c.want) {
			t.Errorf("petParseMoney(%q) = %d,%v; want %d,%v", c.in, got, ok, c.want, c.valid)
		}
	}
	if got := petMoney(123456789); got != "1,234,567.89" {
		t.Errorf("petMoney(123456789) = %q", got)
	}
	if got := petMoney(-500); got != "-5.00" {
		t.Errorf("petMoney(-500) = %q", got)
	}
}

// A chaos run types generated values into every field and presses every key,
// so nothing here may panic and every screen has to remain drawable
// afterwards. This drives the dispatcher the way chaos mode does.
func TestPetStoreSurvivesGeneratedInput(t *testing.T) {
	s := newTestSession(32, 80)
	s.page = petPageSignon

	aids := []go3270.AID{
		go3270.AIDEnter, go3270.AIDClear, go3270.AIDPA1, go3270.AIDPA2,
		go3270.AIDPF1, go3270.AIDPF2, go3270.AIDPF3, go3270.AIDPF4, go3270.AIDPF5,
		go3270.AIDPF6, go3270.AIDPF7, go3270.AIDPF8, go3270.AIDPF9, go3270.AIDPF10,
		go3270.AIDPF11, go3270.AIDPF12, go3270.AIDPF13, go3270.AIDPF24,
	}
	junk := []string{"", "X", "1", "99999", "-1", "ZZZZZZZZ", "0.00", "C0001", "S", "R", "D",
		"ADMIN", "LIV-0001", "I00001", "O00001", "CASH", "SELECT * FROM", "  ", "12.99"}

	step := 0
	for round := 0; round < 40; round++ {
		for _, aid := range aids {
			screen, _, _ := s.render()
			values := map[string]string{}
			for _, f := range screen {
				if f.Write && f.Name != "" {
					values[f.Name] = junk[step%len(junk)]
					step++
				}
			}
			disconnected := s.dispatch(go3270.Response{AID: aid, Values: values})
			if disconnected {
				s.page = petPageSignon
				s.pending = map[string]string{}
			}
			// The screen must still be drawable and still be legal.
			after, curRow, curCol := s.render()
			name := fmt.Sprintf("after %s on %s", go3270.AIDtoString(aid), petScreens[s.page].id)
			checkScreenBounds(t, name, after, s.rows, s.cols)
			checkNoFieldCollisions(t, name, after, s.cols)
			checkUniqueWritableNames(t, name, after)
			if curRow >= s.rows || curCol >= s.cols {
				t.Fatalf("%s: cursor at %d,%d off a %dx%d screen", name, curRow, curCol, s.rows, s.cols)
			}
		}
	}
}

// End to end, through a real terminal. This is the only test here that proves
// the datastream the application emits is one a 3270 client will accept, which
// is not something a unit test on the field list can tell you.
func TestPetStoreThroughS3270(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the terminal test in short mode")
	}
	binary, err := exec.LookPath("s3270")
	if err != nil {
		t.Skip("s3270 is not installed on this machine")
	}

	server, err := StartServerOn("petstore", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not start the pet store: %v", err)
	}
	defer server.Stop()

	term := startTerminal(t, binary, "3", server.Addr())
	defer term.close()

	screen := term.screen(t)
	requireOnScreen(t, screen, "PET010", "RETAIL AND BACK OFFICE SYSTEM", "PAWS AND CLAWS PET STORE")
	if rows := len(screen); rows != 32 {
		t.Errorf("a model 3 terminal was given %d rows, want 32", rows)
	}

	// Sign on.
	term.typeAt(t, findText(t, screen, "User id"), 20, "ADMIN")
	term.typeAt(t, findText(t, screen, "Password"), 20, "ADMIN")
	screen = term.press(t, "Enter()")
	requireOnScreen(t, screen, "PET100", "MAIN MENU", "ROLE ADMIN")

	// Navigate by command line, which is what an assistant driving the
	// session does.
	screen = term.command(t, "CUST C0004")
	requireOnScreen(t, screen, "PET210", "CUSTOMER MAINTENANCE", "DALGLEISH", "C0004")

	screen = term.command(t, "STOCK LIV-0006")
	requireOnScreen(t, screen, "PET310", "KOI CARP", "STOCK ADJUSTMENT")

	screen = term.command(t, "CHECK")
	requireOnScreen(t, screen, "PET320", "reorder level")

	screen = term.command(t, "ADMIN")
	requireOnScreen(t, screen, "PET700", "BACK OFFICE")

	screen = term.command(t, "JOBS")
	requireOnScreen(t, screen, "PET730", "EODCLOSE")

	screen = term.command(t, "AUDIT")
	requireOnScreen(t, screen, "PET740", "SIGNON")

	screen = term.command(t, "SALES")
	requireOnScreen(t, screen, "PET810", "TOTAL INVOICED")

	screen = term.command(t, "INFO")
	requireOnScreen(t, screen, "PET900", "3 (32x80)", "Terminal type reported", "IBM-3279-3")

	// The model switch has to work on a real terminal, not just in the
	// field list: the datastream command changes with it.
	screen = term.press(t, "PF(9)")
	if rows := len(screen); rows != 24 {
		t.Errorf("the terminal still has %d rows after switching to the default screen, want 24", rows)
	}
	requireOnScreen(t, screen, "BUFFER DEFAULT 24x80")
	screen = term.press(t, "PF(9)")
	if rows := len(screen); rows != 32 {
		t.Errorf("the terminal has %d rows after switching back to the alternate screen, want 32", rows)
	}
	requireOnScreen(t, screen, "BUFFER ALTERNATE 32x80")

	// And the counter still works after all that.
	screen = term.command(t, "POS C0002")
	requireOnScreen(t, screen, "PET400", "ORDER ENTRY")
	term.typeAt(t, findText(t, screen, "ITEM CODE")+1, 5, "FOD-0003")
	term.typeAt(t, findText(t, screen, "ITEM CODE")+1, petLineColumns[3].attr+1, "2")
	screen = term.press(t, "Enter()")
	requireOnScreen(t, screen, "CAT FOOD PREMIUM")
	screen = term.press(t, "PF(5)")
	requireOnScreen(t, screen, "PET420", "ORDER DETAIL", "OPEN")
	screen = term.press(t, "PF(5)")
	requireOnScreen(t, screen, "PET510", "INVOICE DETAIL")
	screen = term.press(t, "PF(5)")
	requireOnScreen(t, screen, "PET600", "PAYMENT ENTRY")
	term.typeAt(t, findText(t, screen, "Method"), 17, "CARD")
	screen = term.press(t, "PF(5)")
	requireOnScreen(t, screen, "posted", "PAID")
}

// A very small s3270 driver: write an action, read data lines until the
// status line and the ok/error that follow it.

type terminal struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func startTerminal(t *testing.T, binary, model, addr string) *terminal {
	t.Helper()
	cmd := exec.Command(binary, "-model", model, "-utf8", addr)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start s3270: %v", err)
	}
	term := &terminal{cmd: cmd, in: in, out: bufio.NewReader(out)}
	term.do(t, "Wait(10,3270Mode)")
	return term
}

func (term *terminal) close() {
	fmt.Fprintln(term.in, "Quit()")
	term.in.Close()
	done := make(chan struct{})
	go func() { _ = term.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = term.cmd.Process.Kill()
	}
}

func (term *terminal) do(t *testing.T, action string) []string {
	t.Helper()
	if _, err := fmt.Fprintln(term.in, action); err != nil {
		t.Fatalf("writing %q to s3270: %v", action, err)
	}
	var data []string
	for {
		line, err := term.out.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the reply to %q: %v", action, err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "data: "):
			data = append(data, strings.TrimPrefix(line, "data: "))
		case line == "ok":
			return data
		case line == "error":
			t.Fatalf("s3270 refused %q: %s", action, strings.Join(data, " | "))
		}
	}
}

func (term *terminal) screen(t *testing.T) []string {
	t.Helper()
	return term.do(t, "Ascii()")
}

func (term *terminal) press(t *testing.T, action string) []string {
	t.Helper()
	term.do(t, action)
	term.do(t, "Wait(10,Unlock)")
	return term.screen(t)
}

func (term *terminal) typeAt(t *testing.T, row, col int, text string) {
	t.Helper()
	term.do(t, fmt.Sprintf("MoveCursor(%d,%d)", row, col))
	term.do(t, fmt.Sprintf("String(%q)", text))
}

// command types on the command line, wherever it has ended up on this screen
// size, and presses Enter.
func (term *terminal) command(t *testing.T, cmd string) []string {
	t.Helper()
	row := findText(t, term.screen(t), petCmdLabel)
	term.typeAt(t, row, petCmdTextCol, cmd)
	return term.press(t, "Enter()")
}

func findText(t *testing.T, screen []string, text string) int {
	t.Helper()
	for i, line := range screen {
		if strings.Contains(line, text) {
			return i
		}
	}
	t.Fatalf("%q is not on the screen:\n%s", text, strings.Join(screen, "\n"))
	return -1
}

func requireOnScreen(t *testing.T, screen []string, wants ...string) {
	t.Helper()
	joined := strings.Join(screen, "\n")
	for _, want := range wants {
		if !strings.Contains(joined, want) {
			t.Fatalf("%q is not on the screen:\n%s", want, joined)
		}
	}
}
