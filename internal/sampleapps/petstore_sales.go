package sampleapps

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/racingmars/go3270"
)

// The selling chain: order entry, order enquiry, the invoice raised from an
// order, and the payment posted against that invoice. Each step checks the
// step before it, which is what gives chaos exploration and an AI assistant
// something with consequences to discover.

var petOrderColumns = []petColumn{
	{attr: 0, head: "A", width: 1},
	{attr: 3, head: "ORDER", width: 6},
	{attr: 11, head: "DATE", width: 10},
	{attr: 23, head: "ACCT", width: 5},
	{attr: 30, head: "NAME", width: 18},
	{attr: 50, head: "LINES", width: 5, right: true},
	{attr: 57, head: "VALUE", width: 11, right: true},
	{attr: 70, head: "STATUS", width: 9},
	{attr: 82, head: "TOWN", width: 14, wide: true},
	{attr: 98, head: "TIER", width: 6, wide: true},
}

var petInvoiceColumns = []petColumn{
	{attr: 0, head: "A", width: 1},
	{attr: 3, head: "INVOICE", width: 7},
	{attr: 12, head: "DATE", width: 10},
	{attr: 24, head: "ACCT", width: 5},
	{attr: 31, head: "ORDER", width: 6},
	{attr: 39, head: "NET", width: 10, right: true},
	{attr: 51, head: "VAT", width: 8, right: true},
	{attr: 61, head: "TOTAL", width: 10, right: true},
	{attr: 73, head: "STATUS", width: 6},
	{attr: 82, head: "PAID", width: 11, right: true, wide: true},
	{attr: 96, head: "OUTSTANDING", width: 12, right: true, wide: true},
}

var petPaymentColumns = []petColumn{
	{attr: 0, head: "PAYMENT", width: 7},
	{attr: 9, head: "DATE", width: 10},
	{attr: 21, head: "INVOICE", width: 7},
	{attr: 30, head: "ACCT", width: 5},
	{attr: 37, head: "METHOD", width: 6},
	{attr: 45, head: "AMOUNT", width: 11, right: true},
	{attr: 58, head: "REFERENCE", width: 20},
	{attr: 81, head: "CUSTOMER", width: 24, wide: true},
}

var petLineColumns = []petColumn{
	{attr: 0, head: "LN", width: 2},
	{attr: 4, head: "ITEM CODE", width: 9},
	{attr: 15, head: "DESCRIPTION", width: 26},
	{attr: 43, head: "QTY", width: 4, right: true},
	{attr: 50, head: "PRICE", width: 9, right: true},
	{attr: 61, head: "VALUE", width: 10, right: true},
}

// Order entry.

func (s *petSession) startOrderEntry(arg string) {
	s.goTo(petPageOrderEntry)
	if arg != "" {
		if c := s.store.findCustomer(arg); c != nil {
			s.cartCustID = c.ID
			s.info("Order entry for %s %s. Enter item codes and quantities.", c.ID, c.Name)
			return
		}
		s.fail("Account %s not found. Enter an account number on the screen.", strings.ToUpper(arg))
		return
	}
	s.info("Order entry. Enter an account, then item codes and quantities, then PF5.")
}

// petOrderEntryLines is how many basket lines fit on the screen in front of
// us. A model 4 terminal gets a longer basket than a model 2 for free.
func (s *petSession) petOrderEntryLines() int {
	n := s.bodyHeight() - 8
	if n < 4 {
		return 4
	}
	if n > 12 {
		return 12
	}
	return n
}

func (s *petSession) buildOrderEntry() (go3270.Screen, int, int) {
	top := s.bodyTop()
	lines := s.petOrderEntryLines()

	custText := s.value("ocust", s.cartCustID)
	custName := "ACCOUNT NOT RECOGNISED"
	custColor := go3270.Yellow
	if c := s.store.findCustomer(custText); c != nil {
		custName = fmt.Sprintf("%s  %s  %s  BALANCE %s", c.Name, c.Town, c.Tier, petMoney(c.Balance))
		custColor = go3270.Turquoise
		if c.Status != "ACTIVE" {
			custName += "  *** " + c.Status + " ***"
			custColor = go3270.Red
		}
	} else if strings.TrimSpace(custText) == "" {
		custName = "Enter an account number, or press PF4 for the customer list."
	}

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Account . . . :"},
	}
	screen = append(screen, petInputField(top, 16, 6, "ocust", custText)...)
	screen = append(screen, go3270.Field{Row: top, Col: 25, Color: custColor,
		Content: petTrunc(custName, s.cols-27)})
	screen = append(screen, s.headingFields(top+2, petLineColumns)...)

	net := 0
	for i := 0; i < lines; i++ {
		row := top + 3 + i
		skuName := fmt.Sprintf("osku%d", i)
		qtyName := fmt.Sprintf("oqty%d", i)
		sku := strings.ToUpper(strings.TrimSpace(s.value(skuName, "")))
		qtyText := strings.TrimSpace(s.value(qtyName, ""))

		screen = append(screen, s.cell(row, petLineColumns[0], fmt.Sprintf("%2d", i+1), go3270.Blue)...)
		screen = append(screen, petInputField(row, petLineColumns[1].attr, petLineColumns[1].width,
			skuName, s.value(skuName, ""))...)
		screen = append(screen, petNumericField(row, petLineColumns[3].attr, petLineColumns[3].width,
			qtyName, s.value(qtyName, ""))...)

		desc, price, value, color := "", "", "", go3270.DefaultColor
		if sku != "" {
			item := s.store.findStock(sku)
			switch {
			case item == nil:
				desc, color = "ITEM CODE NOT FOUND", go3270.Red
			default:
				desc = item.Desc
				price = petMoney(item.Price)
				if qty, ok := petParseInt(qtyText, 1, 9999); ok {
					value = petMoney(qty * item.Price)
					net += qty * item.Price
					if qty > item.Qty {
						desc = item.Desc + " (ONLY " + strconv.Itoa(item.Qty) + " ON HAND)"
						color = go3270.Yellow
					}
				}
			}
		}
		screen = append(screen, s.cell(row, petLineColumns[2], desc, color)...)
		screen = append(screen, s.cell(row, petLineColumns[4], price, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petLineColumns[5], value, go3270.DefaultColor)...)
	}

	vat := s.store.vatOn(net)
	totalRow := top + 3 + lines
	screen = append(screen,
		go3270.Field{Row: totalRow, Col: 0, Color: go3270.Blue, Content: strings.Repeat("-", s.cols-2)},
		go3270.Field{Row: totalRow + 1, Col: 40, Content: "Net"},
		go3270.Field{Row: totalRow + 1, Col: petLineColumns[5].attr,
			Content: petRight(petMoney(net), petLineColumns[5].width)},
		go3270.Field{Row: totalRow + 2, Col: 40,
			Content: fmt.Sprintf("VAT at %s%%", petPercent(s.store.cfg.VATRate))},
		go3270.Field{Row: totalRow + 2, Col: petLineColumns[5].attr,
			Content: petRight(petMoney(vat), petLineColumns[5].width)},
		go3270.Field{Row: totalRow + 3, Col: 40, Intense: true, Color: go3270.White,
			Content: "Total " + s.store.cfg.Currency},
		go3270.Field{Row: totalRow + 3, Col: petLineColumns[5].attr, Intense: true, Color: go3270.White,
			Content: petRight(petMoney(net+vat), petLineColumns[5].width)},
	)
	return screen, top, 17
}

func (s *petSession) handleOrderEntry(resp go3270.Response) {
	switch resp.AID {
	case go3270.AIDPF4:
		s.goTo(petPageCustList)
		s.info("Pick an account with S, then use POS to return to order entry.")
		return
	case go3270.AIDPF5:
		s.confirmOrder(resp)
		return
	case go3270.AIDEnter:
		s.cartCustID = strings.ToUpper(strings.TrimSpace(resp.Values["ocust"]))
		if _, _, err := s.collectOrderLines(resp); err != "" {
			s.fail("%s", err)
			return
		}
		s.info("Basket priced. Press PF5 to confirm the order.")
	default:
		s.info("Key %s has no action on order entry.", s.lastAID)
	}
}

// collectOrderLines turns what is on the screen into order lines, reporting
// the first problem it finds rather than a list, because a 3270 message line
// holds one message.
func (s *petSession) collectOrderLines(resp go3270.Response) ([]petOrderLine, int, string) {
	var lines []petOrderLine
	net := 0
	for i := 0; i < s.petOrderEntryLines(); i++ {
		sku := strings.ToUpper(strings.TrimSpace(resp.Values[fmt.Sprintf("osku%d", i)]))
		qtyText := strings.TrimSpace(resp.Values[fmt.Sprintf("oqty%d", i)])
		if sku == "" && qtyText == "" {
			continue
		}
		if sku == "" {
			return nil, 0, fmt.Sprintf("Line %d has a quantity but no item code.", i+1)
		}
		item := s.store.findStock(sku)
		if item == nil {
			return nil, 0, fmt.Sprintf("Line %d: item code %s does not exist.", i+1, sku)
		}
		qty, ok := petParseInt(qtyText, 1, 9999)
		if !ok {
			return nil, 0, fmt.Sprintf("Line %d: quantity %q must be a number from 1 to 9999.", i+1, qtyText)
		}
		lines = append(lines, petOrderLine{SKU: item.SKU, Desc: item.Desc, Qty: qty, Price: item.Price})
		net += qty * item.Price
	}
	return lines, net, ""
}

func (s *petSession) confirmOrder(resp go3270.Response) {
	custID := strings.ToUpper(strings.TrimSpace(resp.Values["ocust"]))
	c := s.store.findCustomer(custID)
	if c == nil {
		s.fail("Account %q does not exist. PF4 lists the accounts.", custID)
		return
	}
	if c.Status != "ACTIVE" {
		s.fail("Account %s is %s and cannot be sold to.", c.ID, c.Status)
		return
	}
	lines, net, problem := s.collectOrderLines(resp)
	if problem != "" {
		s.fail("%s", problem)
		return
	}
	if len(lines) == 0 {
		s.fail("The basket is empty. Enter at least one item code and quantity.")
		return
	}

	order := &petOrder{
		ID:     s.store.nextID("order", "O", 5),
		CustID: c.ID,
		Date:   petToday(),
		Status: "OPEN",
		Lines:  lines,
	}
	s.store.orders = append(s.store.orders, order)
	s.store.logAudit(s.user, "ORDER", fmt.Sprintf("%s RAISED FOR %s, %d LINES, NET %s",
		order.ID, c.ID, len(lines), petMoney(net)))

	s.order = order
	s.cartCustID = ""
	s.goTo(petPageOrderDetail)
	s.info("Order %s created for %s, net %s. PF5 raises the invoice.", order.ID, c.ID, petMoney(net))
}

func (s *petSession) addToOrderFromCatalogue(item *petStockItem) {
	s.goTo(petPageOrderEntry)
	s.pending["osku0"] = item.SKU
	s.pending["oqty0"] = "1"
	s.info("%s added to a new basket. Enter an account and press PF5.", item.SKU)
}

// Order enquiry.

func (s *petSession) buildOrderList() (go3270.Screen, int, int) {
	top := s.bodyTop()
	s.clampTop(&s.orderTop, len(s.store.orders))

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Position to"},
		{Row: top, Col: 27, Color: go3270.Yellow,
			Content: petTrunc("S=Open  C=Customer   PF5 starts a new order", s.cols-29)},
	}
	screen = append(screen, petInputField(top, 12, 12, "orderfind", s.value("orderfind", ""))...)
	screen = append(screen, s.headingFields(top+1, petOrderColumns)...)

	rows := s.listRows()
	for i := 0; i < rows; i++ {
		index := s.orderTop + i
		if index >= len(s.store.orders) {
			break
		}
		o := s.store.orders[index]
		row := top + 2 + i
		name := fmt.Sprintf("sel%d", i)
		custName, town, tier := "", "", ""
		if c := s.store.findCustomer(o.CustID); c != nil {
			custName, town, tier = c.Name, c.Town, c.Tier
		}
		screen = append(screen, petSelectField(row, name, s.value(name, ""))...)
		screen = append(screen, s.cell(row, petOrderColumns[1], o.ID, go3270.White)...)
		screen = append(screen, s.cell(row, petOrderColumns[2], o.Date, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petOrderColumns[3], o.CustID, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petOrderColumns[4], custName, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petOrderColumns[5], strconv.Itoa(len(o.Lines)), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petOrderColumns[6], petMoney(o.net()), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petOrderColumns[7], o.Status, petOrderStatusColor(o.Status))...)
		screen = append(screen, s.cell(row, petOrderColumns[8], town, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petOrderColumns[9], tier, go3270.DefaultColor)...)
	}

	s.appendListFooter(&screen, len(s.store.orders), s.orderTop, "orders")
	return screen, top, 13
}

func petOrderStatusColor(status string) go3270.Color {
	switch status {
	case "OPEN":
		return go3270.Yellow
	case "PICKED":
		return go3270.Turquoise
	case "INVOICED":
		return go3270.Green
	case "CANCELLED":
		return go3270.Red
	}
	return go3270.DefaultColor
}

func (s *petSession) handleOrderList(resp go3270.Response) {
	switch resp.AID {
	case go3270.AIDPF7:
		s.scroll(&s.orderTop, -1, len(s.store.orders))
		return
	case go3270.AIDPF8:
		s.scroll(&s.orderTop, 1, len(s.store.orders))
		return
	case go3270.AIDPF5:
		s.startOrderEntry("")
		return
	case go3270.AIDEnter:
	default:
		s.info("Key %s has no action on the order list.", s.lastAID)
		return
	}

	if action, index, ok := s.lineAction(resp, len(s.store.orders), s.orderTop); ok {
		o := s.store.orders[index]
		switch action {
		case "S", "":
			s.order = o
			s.goTo(petPageOrderDetail)
			s.info("Order %s for account %s.", o.ID, o.CustID)
		case "C":
			if c := s.store.findCustomer(o.CustID); c != nil {
				s.cust = c
				s.goTo(petPageCustDetail)
				s.info("Account %s %s.", c.ID, c.Name)
				return
			}
			s.fail("Account %s no longer exists.", o.CustID)
		default:
			s.fail("%q is not a line action. Use S to open or C for the customer.", action)
		}
		return
	}

	if find := strings.ToUpper(strings.TrimSpace(resp.Values["orderfind"])); find != "" {
		s.pending["orderfind"] = ""
		for i, o := range s.store.orders {
			if strings.HasPrefix(o.ID, find) || o.CustID == find || o.Status == find {
				s.orderTop = i
				s.clampTop(&s.orderTop, len(s.store.orders))
				s.info("Positioned at order %s.", o.ID)
				return
			}
		}
		s.fail("No order matches %q.", find)
		return
	}
	s.info("Type S beside an order and press Enter.")
}

func (s *petSession) buildOrderDetail() (go3270.Screen, int, int) {
	if s.order == nil {
		return s.emptyBody("No order selected. Use ORDER to pick one."), -1, -1
	}
	o := s.order
	top := s.bodyTop()

	custName := ""
	if c := s.store.findCustomer(o.CustID); c != nil {
		custName = c.Name + "  " + c.Town
	}
	invoiceID := ""
	for _, inv := range s.store.invoices {
		if inv.OrderID == o.ID {
			invoiceID = inv.ID
			break
		}
	}

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Order . . :"},
		{Row: top, Col: 12, Intense: true, Color: go3270.White, Content: o.ID},
		{Row: top, Col: 20, Content: "Raised"},
		{Row: top, Col: 28, Color: go3270.Turquoise, Content: o.Date},
		{Row: top, Col: 40, Content: "Status"},
		{Row: top, Col: 48, Intense: true, Color: petOrderStatusColor(o.Status), Content: o.Status},
		{Row: top + 1, Col: 0, Content: "Account . :"},
		{Row: top + 1, Col: 12, Color: go3270.Turquoise, Content: petTrunc(o.CustID+"  "+custName, s.cols-14)},
	}
	screen = append(screen, s.headingFields(top+3, petLineColumns)...)

	rows := s.bodyHeight() - 9
	if rows < 1 {
		rows = 1
	}
	for i, line := range o.Lines {
		if i >= rows {
			break
		}
		row := top + 4 + i
		screen = append(screen, s.cell(row, petLineColumns[0], fmt.Sprintf("%2d", i+1), go3270.Blue)...)
		screen = append(screen, s.cell(row, petLineColumns[1], line.SKU, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petLineColumns[2], line.Desc, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petLineColumns[3], strconv.Itoa(line.Qty), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petLineColumns[4], petMoney(line.Price), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petLineColumns[5], petMoney(line.Qty*line.Price), go3270.DefaultColor)...)
	}

	net := o.net()
	vat := s.store.vatOn(net)
	footer := s.bodyBottom() - 1
	note := "PF5 raises an invoice and takes the stock off the shelf. PF6 cancels."
	if invoiceID != "" {
		note = "Invoice " + invoiceID + " has been raised from this order. INV " + invoiceID + " opens it."
	}
	screen = append(screen,
		go3270.Field{Row: footer, Col: 0, Color: go3270.Blue, Content: strings.Repeat("-", s.cols-2)},
		go3270.Field{Row: footer + 1, Col: 0, Content: petTrunc(fmt.Sprintf(
			"%d lines   Net %s   VAT %s   Total %s %s",
			len(o.Lines), petMoney(net), petMoney(vat), s.store.cfg.Currency, petMoney(net+vat)), s.cols-2)},
	)
	if footer-1 > top+4 {
		screen = append(screen, go3270.Field{Row: footer - 1, Col: 0, Color: go3270.Yellow,
			Content: petTrunc(note, s.cols-2)})
	}
	return screen, -1, -1
}

func (s *petSession) handleOrderDetail(resp go3270.Response) {
	if s.order == nil {
		s.goTo(petPageOrderList)
		s.fail("No order selected.")
		return
	}
	switch resp.AID {
	case go3270.AIDPF5:
		s.raiseInvoice(s.order)
	case go3270.AIDPF6:
		s.cancelOrder(s.order)
	case go3270.AIDEnter:
		s.info("Order %s. PF5 raises an invoice, PF6 cancels the order.", s.order.ID)
	default:
		s.info("Key %s has no action on the order detail.", s.lastAID)
	}
}

func (s *petSession) cancelOrder(o *petOrder) {
	switch o.Status {
	case "CANCELLED":
		s.fail("Order %s is already cancelled.", o.ID)
	case "INVOICED":
		s.fail("Order %s has been invoiced and cannot be cancelled. Void the invoice instead.", o.ID)
	default:
		o.Status = "CANCELLED"
		s.store.logAudit(s.user, "ORDER", o.ID+" CANCELLED")
		s.info("Order %s cancelled.", o.ID)
	}
}

// raiseInvoice is the one place stock actually leaves the shelves, so it is
// the one place that can refuse for lack of it.
func (s *petSession) raiseInvoice(o *petOrder) {
	if o.Status == "INVOICED" {
		s.fail("Order %s has already been invoiced.", o.ID)
		return
	}
	if o.Status == "CANCELLED" {
		s.fail("Order %s is cancelled and cannot be invoiced.", o.ID)
		return
	}
	if len(o.Lines) == 0 {
		s.fail("Order %s has no lines.", o.ID)
		return
	}
	for _, line := range o.Lines {
		item := s.store.findStock(line.SKU)
		if item == nil {
			s.fail("Item %s is no longer in the catalogue. Amend the order.", line.SKU)
			return
		}
		if item.Qty < line.Qty {
			s.fail("Only %d of %s on hand, order needs %d. Receive stock on PET310 first.",
				item.Qty, item.SKU, line.Qty)
			return
		}
	}
	for _, line := range o.Lines {
		if item := s.store.findStock(line.SKU); item != nil {
			item.Qty -= line.Qty
		}
	}

	net := o.net()
	vat := s.store.vatOn(net)
	inv := &petInvoice{
		ID:      s.store.nextID("invoice", "I", 5),
		OrderID: o.ID,
		CustID:  o.CustID,
		Date:    petToday(),
		Net:     net,
		VAT:     vat,
		Total:   net + vat,
		Status:  "OPEN",
	}
	s.store.invoices = append(s.store.invoices, inv)
	o.Status = "INVOICED"
	if c := s.store.findCustomer(o.CustID); c != nil {
		c.Balance = s.store.customerBalance(c.ID)
	}
	s.store.logAudit(s.user, "INVOICE", fmt.Sprintf("%s RAISED FROM %s, TOTAL %s", inv.ID, o.ID, petMoney(inv.Total)))

	s.invoice = inv
	s.goTo(petPageInvoiceDetail)
	s.info("Invoice %s raised for %s. PF5 takes a payment.", inv.ID, petMoney(inv.Total))
}

// Invoice enquiry.

func (s *petSession) buildInvoiceList() (go3270.Screen, int, int) {
	top := s.bodyTop()
	s.clampTop(&s.invTop, len(s.store.invoices))

	outstanding := 0
	for _, inv := range s.store.invoices {
		if inv.Status != "VOID" {
			outstanding += inv.outstanding()
		}
	}

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Position to"},
		{Row: top, Col: 27, Color: go3270.Yellow, Content: petTrunc(fmt.Sprintf(
			"S=Open  P=Pay   Total outstanding %s %s", s.store.cfg.Currency, petMoney(outstanding)), s.cols-29)},
	}
	screen = append(screen, petInputField(top, 12, 12, "invfind", s.value("invfind", ""))...)
	screen = append(screen, s.headingFields(top+1, petInvoiceColumns)...)

	rows := s.listRows()
	for i := 0; i < rows; i++ {
		index := s.invTop + i
		if index >= len(s.store.invoices) {
			break
		}
		inv := s.store.invoices[index]
		row := top + 2 + i
		name := fmt.Sprintf("sel%d", i)
		screen = append(screen, petSelectField(row, name, s.value(name, ""))...)
		screen = append(screen, s.cell(row, petInvoiceColumns[1], inv.ID, go3270.White)...)
		screen = append(screen, s.cell(row, petInvoiceColumns[2], inv.Date, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petInvoiceColumns[3], inv.CustID, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petInvoiceColumns[4], inv.OrderID, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petInvoiceColumns[5], petMoney(inv.Net), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petInvoiceColumns[6], petMoney(inv.VAT), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petInvoiceColumns[7], petMoney(inv.Total), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petInvoiceColumns[8], inv.Status, petInvoiceStatusColor(inv.Status))...)
		screen = append(screen, s.cell(row, petInvoiceColumns[9], petMoney(inv.Paid), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petInvoiceColumns[10], petMoney(inv.outstanding()),
			petBalanceColor(inv.outstanding()))...)
	}

	s.appendListFooter(&screen, len(s.store.invoices), s.invTop, "invoices")
	return screen, top, 13
}

func petInvoiceStatusColor(status string) go3270.Color {
	switch status {
	case "PAID":
		return go3270.Green
	case "PART":
		return go3270.Yellow
	case "VOID":
		return go3270.Red
	}
	return go3270.Turquoise
}

func (s *petSession) handleInvoiceList(resp go3270.Response) {
	switch resp.AID {
	case go3270.AIDPF7:
		s.scroll(&s.invTop, -1, len(s.store.invoices))
		return
	case go3270.AIDPF8:
		s.scroll(&s.invTop, 1, len(s.store.invoices))
		return
	case go3270.AIDEnter:
	default:
		s.info("Key %s has no action on the invoice list.", s.lastAID)
		return
	}

	if action, index, ok := s.lineAction(resp, len(s.store.invoices), s.invTop); ok {
		inv := s.store.invoices[index]
		switch action {
		case "S", "":
			s.invoice = inv
			s.goTo(petPageInvoiceDetail)
			s.info("Invoice %s, %s outstanding.", inv.ID, petMoney(inv.outstanding()))
		case "P":
			s.invoice = inv
			s.goTo(petPagePayEntry)
			s.pending["payinv"] = inv.ID
			s.info("Payment against invoice %s. %s outstanding.", inv.ID, petMoney(inv.outstanding()))
		default:
			s.fail("%q is not a line action. Use S to open or P to pay.", action)
		}
		return
	}

	if find := strings.ToUpper(strings.TrimSpace(resp.Values["invfind"])); find != "" {
		s.pending["invfind"] = ""
		for i, inv := range s.store.invoices {
			if strings.HasPrefix(inv.ID, find) || inv.CustID == find || inv.Status == find {
				s.invTop = i
				s.clampTop(&s.invTop, len(s.store.invoices))
				s.info("Positioned at invoice %s.", inv.ID)
				return
			}
		}
		s.fail("No invoice matches %q.", find)
		return
	}
	s.info("Type S beside an invoice and press Enter.")
}

func (s *petSession) buildInvoiceDetail() (go3270.Screen, int, int) {
	if s.invoice == nil {
		return s.emptyBody("No invoice selected. Use INV to pick one."), -1, -1
	}
	inv := s.invoice
	top := s.bodyTop()

	custName := ""
	if c := s.store.findCustomer(inv.CustID); c != nil {
		custName = c.Name + "  " + c.Town + "  " + c.Postcode
	}

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Invoice . :"},
		{Row: top, Col: 12, Intense: true, Color: go3270.White, Content: inv.ID},
		{Row: top, Col: 20, Content: "Raised"},
		{Row: top, Col: 28, Color: go3270.Turquoise, Content: inv.Date},
		{Row: top, Col: 40, Content: "Status"},
		{Row: top, Col: 48, Intense: true, Color: petInvoiceStatusColor(inv.Status), Content: inv.Status},
		{Row: top, Col: 58, Content: "Order"},
		{Row: top, Col: 65, Color: go3270.Turquoise, Content: inv.OrderID},
		{Row: top + 1, Col: 0, Content: "Account . :"},
		{Row: top + 1, Col: 12, Color: go3270.Turquoise, Content: petTrunc(inv.CustID+"  "+custName, s.cols-14)},
	}

	if order := s.store.findOrder(inv.OrderID); order != nil {
		screen = append(screen, s.headingFields(top+3, petLineColumns)...)
		rows := s.bodyHeight() - 11
		if rows < 1 {
			rows = 1
		}
		for i, line := range order.Lines {
			if i >= rows {
				break
			}
			row := top + 4 + i
			screen = append(screen, s.cell(row, petLineColumns[0], fmt.Sprintf("%2d", i+1), go3270.Blue)...)
			screen = append(screen, s.cell(row, petLineColumns[1], line.SKU, go3270.DefaultColor)...)
			screen = append(screen, s.cell(row, petLineColumns[2], line.Desc, go3270.DefaultColor)...)
			screen = append(screen, s.cell(row, petLineColumns[3], strconv.Itoa(line.Qty), go3270.DefaultColor)...)
			screen = append(screen, s.cell(row, petLineColumns[4], petMoney(line.Price), go3270.DefaultColor)...)
			screen = append(screen, s.cell(row, petLineColumns[5], petMoney(line.Qty*line.Price), go3270.DefaultColor)...)
		}
	}

	payments := s.store.paymentsFor(inv.ID)
	summary := s.bodyBottom() - 3
	screen = append(screen,
		go3270.Field{Row: summary, Col: 0, Color: go3270.Blue, Content: strings.Repeat("-", s.cols-2)},
		go3270.Field{Row: summary + 1, Col: 0, Content: petTrunc(fmt.Sprintf(
			"Net %s   VAT at %s%% %s   Total %s %s",
			petMoney(inv.Net), petPercent(s.store.cfg.VATRate), petMoney(inv.VAT),
			s.store.cfg.Currency, petMoney(inv.Total)), s.cols-2)},
		go3270.Field{Row: summary + 2, Col: 0, Content: petTrunc(fmt.Sprintf(
			"Paid %s over %d payment(s)   Outstanding %s %s",
			petMoney(inv.Paid), len(payments), s.store.cfg.Currency, petMoney(inv.outstanding())), s.cols-2)},
		go3270.Field{Row: summary + 3, Col: 0, Color: go3270.Yellow,
			Content: petTrunc("PF5 opens payment entry for this invoice. PAYHIST lists receipts.", s.cols-2)},
	)
	return screen, -1, -1
}

func (s *petSession) handleInvoiceDetail(resp go3270.Response) {
	if s.invoice == nil {
		s.goTo(petPageInvoiceList)
		s.fail("No invoice selected.")
		return
	}
	switch resp.AID {
	case go3270.AIDPF5:
		inv := s.invoice
		s.goTo(petPagePayEntry)
		s.pending["payinv"] = inv.ID
		s.pending["payamt"] = petMoney(inv.outstanding())
		s.info("Payment against %s. %s outstanding.", inv.ID, petMoney(inv.outstanding()))
	case go3270.AIDEnter:
		s.info("Invoice %s. PF5 takes a payment.", s.invoice.ID)
	default:
		s.info("Key %s has no action on the invoice detail.", s.lastAID)
	}
}

// Payments.

func (s *petSession) buildPayEntry() (go3270.Screen, int, int) {
	top := s.bodyTop()
	invText := strings.ToUpper(strings.TrimSpace(s.value("payinv", "")))

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Invoice . . . :"},
	}
	screen = append(screen, petInputField(top, 16, 6, "payinv", s.value("payinv", ""))...)

	detail := "Enter an invoice number. INV lists them."
	detailColor := go3270.Yellow
	totals := ""
	var inv *petInvoice
	if invText != "" {
		inv = s.store.findInvoice(invText)
		if inv == nil {
			detail = "INVOICE NOT FOUND"
			detailColor = go3270.Red
		} else {
			custName := ""
			if c := s.store.findCustomer(inv.CustID); c != nil {
				custName = c.Name
			}
			detail = fmt.Sprintf("%s  %s  %s", inv.CustID, custName, inv.Status)
			detailColor = go3270.Turquoise
			totals = fmt.Sprintf("Total %s   Paid %s   Outstanding %s %s   Raised %s",
				petMoney(inv.Total), petMoney(inv.Paid),
				s.store.cfg.Currency, petMoney(inv.outstanding()), inv.Date)
		}
	}
	screen = append(screen, go3270.Field{Row: top, Col: 25, Color: detailColor,
		Content: petTrunc(detail, s.cols-27)})
	if totals != "" {
		screen = append(screen, go3270.Field{Row: top + 1, Col: 0, Color: go3270.Turquoise,
			Content: petTrunc(totals, s.cols-2)})
	}

	screen = append(screen, go3270.Field{Row: top + 2, Col: 0, Content: "Amount  . . . :"})
	screen = append(screen, petInputField(top+2, 16, 12, "payamt", s.value("payamt", ""))...)
	screen = append(screen, go3270.Field{Row: top + 2, Col: 31, Color: go3270.Yellow,
		Content: s.store.cfg.Currency + ", for example 24.99"})

	screen = append(screen, go3270.Field{Row: top + 3, Col: 0, Content: "Method  . . . :"})
	screen = append(screen, petInputField(top+3, 16, 8, "paymeth", s.value("paymeth", ""))...)
	screen = append(screen, go3270.Field{Row: top + 3, Col: 31, Color: go3270.Yellow,
		Content: "CASH, CARD, CHEQUE or ACCT"})

	screen = append(screen, go3270.Field{Row: top + 4, Col: 0, Content: "Reference . . :"})
	screen = append(screen, petInputField(top+4, 16, 20, "payref", s.value("payref", ""))...)

	if inv != nil {
		payments := s.store.paymentsFor(inv.ID)
		screen = append(screen,
			go3270.Field{Row: top + 6, Col: 0, Color: go3270.Blue, Content: strings.Repeat("-", s.cols-2)},
			go3270.Field{Row: top + 7, Col: 0, Intense: true, Color: go3270.White,
				Content: fmt.Sprintf("PAYMENTS ALREADY RECEIVED AGAINST %s", inv.ID)},
		)
		rows := s.bodyHeight() - 10
		if rows > len(payments) {
			rows = len(payments)
		}
		for i := 0; i < rows; i++ {
			p := payments[i]
			screen = append(screen, go3270.Field{Row: top + 9 + i, Col: 0, Content: petTrunc(fmt.Sprintf(
				"%s  %s  %-8s %12s  %s", p.ID, p.Date, p.Method, petMoney(p.Amount), p.Ref), s.cols-2)})
		}
		if len(payments) == 0 {
			screen = append(screen, go3270.Field{Row: top + 9, Col: 0, Content: "None."})
		}
	}

	screen = append(screen, go3270.Field{Row: s.bodyBottom(), Col: 0, Color: go3270.Yellow,
		Content: petTrunc("PF5 posts the payment. An overpayment is refused. PF6 shows receipts.", s.cols-2)})
	return screen, top, 17
}

func (s *petSession) handlePayEntry(resp go3270.Response) {
	switch resp.AID {
	case go3270.AIDPF6:
		s.goTo(petPagePayList)
		s.info("Payment history.")
		return
	case go3270.AIDPF5:
		s.postPayment(resp)
		return
	case go3270.AIDEnter:
		s.info("Invoice details refreshed. Press PF5 to post the payment.")
	default:
		s.info("Key %s has no action on payment entry.", s.lastAID)
	}
}

func (s *petSession) postPayment(resp go3270.Response) {
	invID := strings.ToUpper(strings.TrimSpace(resp.Values["payinv"]))
	inv := s.store.findInvoice(invID)
	if inv == nil {
		s.fail("Invoice %q does not exist.", invID)
		return
	}
	if inv.Status == "VOID" {
		s.fail("Invoice %s is void and cannot take a payment.", inv.ID)
		return
	}
	if inv.outstanding() <= 0 {
		s.fail("Invoice %s is already paid in full.", inv.ID)
		return
	}
	amount, ok := petParseMoney(resp.Values["payamt"])
	if !ok || amount <= 0 {
		s.fail("Amount %q is not a valid amount. Use a value such as 24.99.",
			strings.TrimSpace(resp.Values["payamt"]))
		return
	}
	if amount > inv.outstanding() {
		s.fail("Payment of %s is more than the %s outstanding on %s.",
			petMoney(amount), petMoney(inv.outstanding()), inv.ID)
		return
	}
	method := strings.ToUpper(strings.TrimSpace(resp.Values["paymeth"]))
	if !petOneOf(method, "CASH", "CARD", "CHEQUE", "ACCT") {
		s.fail("Method must be CASH, CARD, CHEQUE or ACCT, not %q.", method)
		return
	}
	ref := strings.ToUpper(strings.TrimSpace(resp.Values["payref"]))
	if ref == "" {
		ref = method + "/" + petToday()
	}

	payment := &petPayment{
		ID:        s.store.nextID("payment", "P", 5),
		InvoiceID: inv.ID,
		CustID:    inv.CustID,
		Date:      petToday(),
		Amount:    amount,
		Method:    method,
		Ref:       ref,
	}
	s.store.payments = append(s.store.payments, payment)
	inv.Paid += amount
	if inv.Paid >= inv.Total {
		inv.Status = "PAID"
	} else {
		inv.Status = "PART"
	}
	if c := s.store.findCustomer(inv.CustID); c != nil {
		c.Balance = s.store.customerBalance(c.ID)
	}
	s.store.logAudit(s.user, "PAYMENT", fmt.Sprintf("%s OF %s AGAINST %s BY %s",
		payment.ID, petMoney(amount), inv.ID, method))

	s.pending = map[string]string{}
	s.invoice = inv
	s.info("Payment %s of %s posted. Invoice %s is now %s with %s outstanding.",
		payment.ID, petMoney(amount), inv.ID, inv.Status, petMoney(inv.outstanding()))
}

func (s *petSession) buildPayList() (go3270.Screen, int, int) {
	top := s.bodyTop()
	s.clampTop(&s.payTop, len(s.store.payments))

	total := 0
	for _, p := range s.store.payments {
		total += p.Amount
	}

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: petTrunc(fmt.Sprintf(
			"%d payments received, %s %s in total.", len(s.store.payments), s.store.cfg.Currency, petMoney(total)), s.cols-2)},
	}
	screen = append(screen, s.headingFields(top+1, petPaymentColumns)...)

	rows := s.listRows()
	for i := 0; i < rows; i++ {
		index := s.payTop + i
		if index >= len(s.store.payments) {
			break
		}
		p := s.store.payments[index]
		row := top + 2 + i
		custName := ""
		if c := s.store.findCustomer(p.CustID); c != nil {
			custName = c.Name
		}
		screen = append(screen, s.cell(row, petPaymentColumns[0], p.ID, go3270.White)...)
		screen = append(screen, s.cell(row, petPaymentColumns[1], p.Date, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petPaymentColumns[2], p.InvoiceID, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petPaymentColumns[3], p.CustID, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petPaymentColumns[4], p.Method, go3270.Turquoise)...)
		screen = append(screen, s.cell(row, petPaymentColumns[5], petMoney(p.Amount), go3270.Green)...)
		screen = append(screen, s.cell(row, petPaymentColumns[6], p.Ref, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petPaymentColumns[7], custName, go3270.DefaultColor)...)
	}

	s.appendListFooter(&screen, len(s.store.payments), s.payTop, "payments")
	return screen, -1, -1
}
