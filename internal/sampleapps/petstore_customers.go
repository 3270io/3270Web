// SPDX-License-Identifier: AGPL-3.0-or-later

package sampleapps

import (
	"fmt"
	"strings"

	"github.com/racingmars/go3270"
)

// Customer enquiry, maintenance and account creation.

var petCustColumns = []petColumn{
	{attr: 0, head: "A", width: 1},
	{attr: 3, head: "ACCT", width: 5},
	{attr: 10, head: "NAME", width: 18},
	{attr: 30, head: "TOWN", width: 14},
	{attr: 46, head: "TIER", width: 6},
	{attr: 54, head: "STATUS", width: 6},
	{attr: 62, head: "BALANCE", width: 11, right: true},
	{attr: 75, head: "PHONE", width: 14, wide: true},
	{attr: 91, head: "EMAIL", width: 30, wide: true},
}

func (s *petSession) buildCustList() (go3270.Screen, int, int) {
	top := s.bodyTop()
	s.clampTop(&s.custTop, len(s.store.customers))

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Position to"},
		{Row: top, Col: 35, Color: go3270.Yellow,
			Content: petTrunc("S=Open  D=Close account  I=Invoices", s.cols-37)},
	}
	screen = append(screen, petInputField(top, 12, 20, "custfind", s.value("custfind", ""))...)

	screen = append(screen, s.headingFields(top+1, petCustColumns)...)

	rows := s.listRows()
	for i := 0; i < rows; i++ {
		index := s.custTop + i
		if index >= len(s.store.customers) {
			break
		}
		c := s.store.customers[index]
		row := top + 2 + i
		name := fmt.Sprintf("sel%d", i)
		screen = append(screen, petSelectField(row, name, s.value(name, ""))...)
		screen = append(screen, s.cell(row, petCustColumns[1], c.ID, petStatusColor(c.Status))...)
		screen = append(screen, s.cell(row, petCustColumns[2], c.Name, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petCustColumns[3], c.Town, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petCustColumns[4], c.Tier, petTierColor(c.Tier))...)
		screen = append(screen, s.cell(row, petCustColumns[5], c.Status, petStatusColor(c.Status))...)
		screen = append(screen, s.cell(row, petCustColumns[6], petMoney(c.Balance), petBalanceColor(c.Balance))...)
		screen = append(screen, s.cell(row, petCustColumns[7], c.Phone, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petCustColumns[8], c.Email, go3270.DefaultColor)...)
	}

	s.appendListFooter(&screen, len(s.store.customers), s.custTop, "accounts")
	return screen, top, 13
}

// appendListFooter is the "showing x to y of z" line every list carries. It is
// drawn on the last body row so it stays put when the screen size changes.
func (s *petSession) appendListFooter(screen *go3270.Screen, count, top int, noun string) {
	shown := s.listRows()
	last := top + shown
	if last > count {
		last = count
	}
	text := fmt.Sprintf("No %s to display.", noun)
	if count > 0 {
		text = fmt.Sprintf("Showing %d to %d of %d %s. PF7 back, PF8 forward.", top+1, last, count, noun)
	}
	*screen = append(*screen, go3270.Field{Row: s.bodyBottom(), Col: 0, Color: go3270.Turquoise,
		Content: petTrunc(text, s.cols-2)})
}

func petStatusColor(status string) go3270.Color {
	switch status {
	case "HOLD":
		return go3270.Yellow
	case "CLOSED", "VOID", "CANCELLED", "LOCKED", "FAILED":
		return go3270.Red
	}
	return go3270.Green
}

func petTierColor(tier string) go3270.Color {
	switch tier {
	case "GOLD":
		return go3270.Yellow
	case "SILVER":
		return go3270.White
	}
	return go3270.Turquoise
}

func petBalanceColor(balance int) go3270.Color {
	if balance > 0 {
		return go3270.Red
	}
	return go3270.DefaultColor
}

func (s *petSession) handleCustList(resp go3270.Response) {
	switch resp.AID {
	case go3270.AIDPF7:
		s.scroll(&s.custTop, -1, len(s.store.customers))
		return
	case go3270.AIDPF8:
		s.scroll(&s.custTop, 1, len(s.store.customers))
		return
	case go3270.AIDPF5:
		s.goTo(petPageCustNew)
		s.info("New customer account. Fill the fields and press PF5 to create it.")
		return
	case go3270.AIDEnter:
	default:
		s.info("Key %s has no action on the customer list.", s.lastAID)
		return
	}

	if action, index, ok := s.lineAction(resp, len(s.store.customers), s.custTop); ok {
		c := s.store.customers[index]
		switch action {
		case "S", "U", "":
			s.cust = c
			s.goTo(petPageCustDetail)
			s.info("Account %s %s.", c.ID, c.Name)
		case "D":
			s.closeAccount(c)
		case "I":
			s.cust = c
			s.positionInvoicesTo(c.ID)
			s.goTo(petPageInvoiceList)
			s.info("Invoices for account %s.", c.ID)
		default:
			s.fail("%q is not a line action. Use S, U, D or I.", action)
		}
		return
	}

	if find := strings.ToUpper(strings.TrimSpace(resp.Values["custfind"])); find != "" {
		s.pending["custfind"] = ""
		for i, c := range s.store.customers {
			if strings.HasPrefix(c.ID, find) || strings.Contains(c.Name, find) || strings.Contains(c.Town, find) {
				s.custTop = i
				s.clampTop(&s.custTop, len(s.store.customers))
				s.info("Positioned at %s %s.", c.ID, c.Name)
				return
			}
		}
		s.fail("No account matches %q.", find)
		return
	}

	s.info("Type S beside an account and press Enter, or use Position to.")
}

// lineAction finds the first line on a list screen the operator marked. It
// returns the action character and the index into the underlying slice.
func (s *petSession) lineAction(resp go3270.Response, count, top int) (string, int, bool) {
	for i := 0; i < s.listRows(); i++ {
		name := fmt.Sprintf("sel%d", i)
		action := strings.ToUpper(strings.TrimSpace(resp.Values[name]))
		if action == "" {
			continue
		}
		s.pending[name] = ""
		index := top + i
		if index >= count {
			s.fail("There is no entry on that line.")
			return "", 0, false
		}
		return action, index, true
	}
	return "", 0, false
}

func (s *petSession) closeAccount(c *petCustomer) {
	if !s.isAdmin() {
		s.fail("Closing an account needs role ADMIN or MANAGER. You are signed on as %s.", s.role)
		return
	}
	if c.Status == "CLOSED" {
		s.fail("Account %s is already closed.", c.ID)
		return
	}
	if c.Balance > 0 {
		s.fail("Account %s cannot be closed with %s %s outstanding.", c.ID, s.store.cfg.Currency, petMoney(c.Balance))
		return
	}
	c.Status = "CLOSED"
	s.store.logAudit(s.user, "CUSTOMER", "ACCOUNT "+c.ID+" CLOSED")
	s.info("Account %s closed.", c.ID)
}

func (s *petSession) positionInvoicesTo(custID string) {
	for i, inv := range s.store.invoices {
		if inv.CustID == custID {
			s.invTop = i
			s.clampTop(&s.invTop, len(s.store.invoices))
			return
		}
	}
	s.invTop = 0
}

func (s *petSession) buildCustDetail() (go3270.Screen, int, int) {
	if s.cust == nil {
		return s.emptyBody("No account selected. Use CUST to pick one."), -1, -1
	}
	c := s.cust
	top := s.bodyTop()

	orders := 0
	for _, o := range s.store.orders {
		if o.CustID == c.ID {
			orders++
		}
	}
	invoices := s.store.invoicesFor(c.ID)

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Account . . . . :"},
		{Row: top, Col: 19, Intense: true, Color: go3270.White, Content: c.ID},
		{Row: top, Col: 30, Content: "Opened"},
		{Row: top, Col: 38, Color: go3270.Turquoise, Content: c.Since},
	}

	fields := []struct {
		label, name, stored string
		width               int
	}{
		{"Name  . . . . . :", "cname", c.Name, 30},
		{"Town  . . . . . :", "ctown", c.Town, 20},
		{"Postcode  . . . :", "cpost", c.Postcode, 10},
		{"Telephone . . . :", "cphone", c.Phone, 16},
		{"Email . . . . . :", "cmail", c.Email, 32},
		{"Tier  . . . . . :", "ctier", c.Tier, 8},
		{"Status  . . . . :", "cstat", c.Status, 8},
		{"Notes . . . . . :", "cnote", c.Notes, 38},
	}
	for i, f := range fields {
		row := top + 2 + i
		if row > s.bodyBottom() {
			break
		}
		width := f.width
		if 20+width+1 > s.cols-2 {
			width = s.cols - 23
		}
		screen = append(screen, go3270.Field{Row: row, Col: 0, Content: f.label})
		screen = append(screen, petInputField(row, 19, width, f.name, s.value(f.name, f.stored))...)
		switch f.name {
		case "ctier":
			screen = append(screen, go3270.Field{Row: row, Col: 19 + width + 3, Color: go3270.Yellow,
				Content: petTrunc("BRONZE, SILVER or GOLD", s.cols-(19+width+5))})
		case "cstat":
			screen = append(screen, go3270.Field{Row: row, Col: 19 + width + 3, Color: go3270.Yellow,
				Content: petTrunc("ACTIVE, HOLD or CLOSED", s.cols-(19+width+5))})
		}
	}

	summaryRow := top + 11
	if summaryRow <= s.bodyBottom()-1 {
		screen = append(screen,
			go3270.Field{Row: summaryRow, Col: 0, Color: go3270.Blue, Content: strings.Repeat("-", s.cols-2)},
			go3270.Field{Row: summaryRow + 1, Col: 0, Content: fmt.Sprintf(
				"Orders %-4d Invoices %-4d Outstanding %s %s",
				orders, len(invoices), s.store.cfg.Currency, petMoney(s.store.customerBalance(c.ID)))},
		)
	}
	if summaryRow+2 <= s.bodyBottom() {
		screen = append(screen, go3270.Field{Row: summaryRow + 2, Col: 0, Color: go3270.Yellow,
			Content: petTrunc("PF5 saves. PF6 lists invoices. PF10 and PF11 step through accounts.", s.cols-2)})
	}
	return screen, top + 2, 20
}

func (s *petSession) handleCustDetail(resp go3270.Response) {
	if s.cust == nil {
		s.goTo(petPageCustList)
		s.fail("No account selected.")
		return
	}
	switch resp.AID {
	case go3270.AIDPF5:
		s.saveCustomer(resp)
	case go3270.AIDPF6:
		s.positionInvoicesTo(s.cust.ID)
		s.goTo(petPageInvoiceList)
		s.info("Invoices for account %s.", s.cust.ID)
	case go3270.AIDPF10:
		s.stepCustomer(-1)
	case go3270.AIDPF11:
		s.stepCustomer(1)
	case go3270.AIDEnter:
		s.info("Account %s. Press PF5 to save your changes.", s.cust.ID)
	default:
		s.info("Key %s has no action on customer maintenance.", s.lastAID)
	}
}

func (s *petSession) stepCustomer(direction int) {
	for i, c := range s.store.customers {
		if c != s.cust {
			continue
		}
		next := i + direction
		if next < 0 || next >= len(s.store.customers) {
			s.fail("There is no %s account.", map[int]string{-1: "previous", 1: "next"}[direction])
			return
		}
		s.cust = s.store.customers[next]
		s.pending = map[string]string{}
		s.info("Account %s %s.", s.cust.ID, s.cust.Name)
		return
	}
}

func (s *petSession) saveCustomer(resp go3270.Response) {
	c := s.cust
	name := strings.ToUpper(strings.TrimSpace(resp.Values["cname"]))
	tier := strings.ToUpper(strings.TrimSpace(resp.Values["ctier"]))
	status := strings.ToUpper(strings.TrimSpace(resp.Values["cstat"]))

	if name == "" {
		s.fail("Name is required.")
		return
	}
	if !petOneOf(tier, "BRONZE", "SILVER", "GOLD") {
		s.fail("Tier must be BRONZE, SILVER or GOLD, not %q.", tier)
		return
	}
	if !petOneOf(status, "ACTIVE", "HOLD", "CLOSED") {
		s.fail("Status must be ACTIVE, HOLD or CLOSED, not %q.", status)
		return
	}
	if status == "CLOSED" && c.Status != "CLOSED" && s.store.customerBalance(c.ID) > 0 {
		s.fail("Account %s cannot be closed with %s outstanding.", c.ID, petMoney(s.store.customerBalance(c.ID)))
		return
	}
	email := strings.ToUpper(strings.TrimSpace(resp.Values["cmail"]))
	if email != "" && !strings.Contains(email, "@") {
		s.fail("Email address %q does not look like an address.", email)
		return
	}

	c.Name = name
	c.Town = strings.ToUpper(strings.TrimSpace(resp.Values["ctown"]))
	c.Postcode = strings.ToUpper(strings.TrimSpace(resp.Values["cpost"]))
	c.Phone = strings.TrimSpace(resp.Values["cphone"])
	c.Email = email
	c.Tier = tier
	c.Status = status
	c.Notes = strings.ToUpper(strings.TrimSpace(resp.Values["cnote"]))

	s.pending = map[string]string{}
	s.store.logAudit(s.user, "CUSTOMER", "ACCOUNT "+c.ID+" AMENDED")
	s.info("Account %s saved.", c.ID)
}

func petOneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

func (s *petSession) buildCustNew() (go3270.Screen, int, int) {
	top := s.bodyTop()
	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "The account number is allocated when the record is created."},
		{Row: top + 1, Col: 0, Content: "Next number  . :"},
		{Row: top + 1, Col: 19, Intense: true, Color: go3270.White,
			Content: fmt.Sprintf("C%04d", s.store.seq["cust"]+1)},
	}

	fields := []struct {
		label, name string
		width       int
	}{
		{"Name  . . . . . :", "nname", 30},
		{"Town  . . . . . :", "ntown", 20},
		{"Postcode  . . . :", "npost", 10},
		{"Telephone . . . :", "nphone", 16},
		{"Email . . . . . :", "nmail", 32},
		{"Tier  . . . . . :", "ntier", 8},
		{"Notes . . . . . :", "nnote", 38},
	}
	for i, f := range fields {
		row := top + 3 + i
		if row > s.bodyBottom() {
			break
		}
		width := f.width
		if 20+width+1 > s.cols-2 {
			width = s.cols - 23
		}
		screen = append(screen, go3270.Field{Row: row, Col: 0, Content: f.label})
		screen = append(screen, petInputField(row, 19, width, f.name, s.value(f.name, ""))...)
	}
	hintRow := top + 11
	if hintRow <= s.bodyBottom() {
		screen = append(screen, go3270.Field{Row: hintRow, Col: 0, Color: go3270.Yellow,
			Content: petTrunc("Name is required. Tier defaults to BRONZE. PF5 creates, PF3 abandons.", s.cols-2)})
	}
	return screen, top + 3, 20
}

func (s *petSession) handleCustNew(resp go3270.Response) {
	switch resp.AID {
	case go3270.AIDPF5:
	case go3270.AIDEnter:
		s.info("Press PF5 to create the account.")
		return
	default:
		s.info("Key %s has no action here. PF5 creates, PF3 abandons.", s.lastAID)
		return
	}

	name := strings.ToUpper(strings.TrimSpace(resp.Values["nname"]))
	if name == "" {
		s.fail("Name is required.")
		return
	}
	tier := strings.ToUpper(strings.TrimSpace(resp.Values["ntier"]))
	if tier == "" {
		tier = "BRONZE"
	}
	if !petOneOf(tier, "BRONZE", "SILVER", "GOLD") {
		s.fail("Tier must be BRONZE, SILVER or GOLD, not %q.", tier)
		return
	}
	email := strings.ToUpper(strings.TrimSpace(resp.Values["nmail"]))
	if email != "" && !strings.Contains(email, "@") {
		s.fail("Email address %q does not look like an address.", email)
		return
	}

	c := &petCustomer{
		ID:       s.store.nextID("cust", "C", 4),
		Name:     name,
		Town:     strings.ToUpper(strings.TrimSpace(resp.Values["ntown"])),
		Postcode: strings.ToUpper(strings.TrimSpace(resp.Values["npost"])),
		Phone:    strings.TrimSpace(resp.Values["nphone"]),
		Email:    email,
		Tier:     tier,
		Status:   "ACTIVE",
		Since:    petToday(),
		Notes:    strings.ToUpper(strings.TrimSpace(resp.Values["nnote"])),
	}
	s.store.customers = append(s.store.customers, c)
	s.store.logAudit(s.user, "CUSTOMER", "ACCOUNT "+c.ID+" CREATED")
	s.cust = c
	s.goTo(petPageCustDetail)
	s.info("Account %s created for %s.", c.ID, c.Name)
}

// emptyBody is what a detail screen draws when the record it was going to show
// has gone away - which happens if somebody reaches it by command with nothing
// selected.
func (s *petSession) emptyBody(message string) go3270.Screen {
	return go3270.Screen{
		{Row: s.bodyTop(), Col: 0, Intense: true, Color: go3270.Red,
			Content: petTrunc(message, s.cols-2)},
	}
}
