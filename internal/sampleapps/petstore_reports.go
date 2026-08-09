package sampleapps

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/racingmars/go3270"
)

// Reports. These are read-only and computed from live data every time they are
// drawn, which makes them the easiest way to see that an order, an invoice or
// a stock adjustment entered on another screen actually landed.

var petCategoryOrder = []string{"LIVE", "FOOD", "ACCS", "MEDS"}

var petCategoryNames = map[string]string{
	"LIVE": "LIVESTOCK AND PETS",
	"FOOD": "FOOD AND FEED",
	"ACCS": "ACCESSORIES AND HARDWARE",
	"MEDS": "HEALTH AND MEDICINES",
}

type petSalesRow struct {
	lines int
	units int
	net   int
	vat   int
}

// salesByCategory walks the invoices back to the orders they were raised from,
// which is where the line detail lives.
func (s *petSession) salesByCategory() (map[string]*petSalesRow, int, int) {
	rows := map[string]*petSalesRow{}
	for _, c := range petCategoryOrder {
		rows[c] = &petSalesRow{}
	}
	totalNet, totalVAT := 0, 0

	for _, inv := range s.store.invoices {
		if inv.Status == "VOID" {
			continue
		}
		order := s.store.findOrder(inv.OrderID)
		if order == nil {
			continue
		}
		for _, line := range order.Lines {
			category := "ACCS"
			if item := s.store.findStock(line.SKU); item != nil {
				category = item.Category
			}
			row, ok := rows[category]
			if !ok {
				row = &petSalesRow{}
				rows[category] = row
			}
			net := line.Qty * line.Price
			vat := s.store.vatOn(net)
			row.lines++
			row.units += line.Qty
			row.net += net
			row.vat += vat
			totalNet += net
			totalVAT += vat
		}
	}
	return rows, totalNet, totalVAT
}

func (s *petSession) buildRptSales() (go3270.Screen, int, int) {
	rows, totalNet, totalVAT := s.salesByCategory()
	top := s.bodyTop()

	columns := []petColumn{
		{attr: 0, head: "CATEGORY", width: 26},
		{attr: 28, head: "LINES", width: 6, right: true},
		{attr: 36, head: "UNITS", width: 7, right: true},
		{attr: 45, head: "NET", width: 12, right: true},
		{attr: 59, head: "VAT", width: 10, right: true},
		{attr: 71, head: "SHARE", width: 6, right: true},
	}

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: fmt.Sprintf(
			"Invoiced sales as at %s. Amounts in %s.", petNowStamp(), s.store.cfg.Currency)},
	}
	screen = append(screen, s.headingFields(top+2, columns)...)

	row := top + 3
	for _, category := range petCategoryOrder {
		entry := rows[category]
		share := "0.0%"
		if totalNet > 0 {
			share = fmt.Sprintf("%.1f%%", float64(entry.net)*100/float64(totalNet))
		}
		screen = append(screen, s.cell(row, columns[0], petCategoryNames[category], petCategoryColor(category))...)
		screen = append(screen, s.cell(row, columns[1], strconv.Itoa(entry.lines), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, columns[2], strconv.Itoa(entry.units), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, columns[3], petMoney(entry.net), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, columns[4], petMoney(entry.vat), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, columns[5], share, go3270.Turquoise)...)
		row++
	}

	screen = append(screen, go3270.Field{Row: row, Col: 0, Color: go3270.Blue,
		Content: strings.Repeat("-", s.cols-2)})
	row++
	screen = append(screen, s.cell(row, columns[0], "TOTAL INVOICED", go3270.White)...)
	screen = append(screen, s.cell(row, columns[3], petMoney(totalNet), go3270.White)...)
	screen = append(screen, s.cell(row, columns[4], petMoney(totalVAT), go3270.White)...)
	screen = append(screen, go3270.Field{Row: row + 1, Col: 0, Intense: true, Color: go3270.White,
		Content: petTrunc(fmt.Sprintf("GROSS INVOICED %s %s", s.store.cfg.Currency, petMoney(totalNet+totalVAT)), s.cols-2)})

	// A taller screen gets the best sellers as well.
	if s.bodyBottom()-(row+3) >= 6 {
		screen = append(screen, s.bestSellers(row+3)...)
	}
	return screen, -1, -1
}

func (s *petSession) bestSellers(startRow int) go3270.Screen {
	type seller struct {
		sku, desc string
		units     int
		value     int
	}
	totals := map[string]*seller{}
	for _, inv := range s.store.invoices {
		if inv.Status == "VOID" {
			continue
		}
		order := s.store.findOrder(inv.OrderID)
		if order == nil {
			continue
		}
		for _, line := range order.Lines {
			entry, ok := totals[line.SKU]
			if !ok {
				entry = &seller{sku: line.SKU, desc: line.Desc}
				totals[line.SKU] = entry
			}
			entry.units += line.Qty
			entry.value += line.Qty * line.Price
		}
	}
	ranked := make([]*seller, 0, len(totals))
	for _, entry := range totals {
		ranked = append(ranked, entry)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].value != ranked[j].value {
			return ranked[i].value > ranked[j].value
		}
		return ranked[i].sku < ranked[j].sku
	})

	screen := go3270.Screen{
		{Row: startRow, Col: 0, Intense: true, Color: go3270.White, Content: "BEST SELLING LINES BY VALUE"},
	}
	limit := s.bodyBottom() - (startRow + 1)
	if limit > 5 {
		limit = 5
	}
	for i, entry := range ranked {
		if i >= limit {
			break
		}
		screen = append(screen, go3270.Field{Row: startRow + 1 + i, Col: 0, Content: petTrunc(fmt.Sprintf(
			"%d. %-8s %-30s %5d units %14s", i+1, entry.sku, entry.desc, entry.units, petMoney(entry.value)), s.cols-2)})
	}
	if len(ranked) == 0 {
		screen = append(screen, go3270.Field{Row: startRow + 1, Col: 0, Content: "Nothing invoiced yet."})
	}
	return screen
}

func (s *petSession) buildRptStock() (go3270.Screen, int, int) {
	top := s.bodyTop()

	type valuation struct {
		lines, units, cost, retail int
	}
	byCategory := map[string]*valuation{}
	for _, c := range petCategoryOrder {
		byCategory[c] = &valuation{}
	}
	totals := valuation{}
	for _, item := range s.store.stock {
		entry, ok := byCategory[item.Category]
		if !ok {
			entry = &valuation{}
			byCategory[item.Category] = entry
		}
		entry.lines++
		entry.units += item.Qty
		entry.cost += item.Qty * item.Cost
		entry.retail += item.Qty * item.Price
		totals.lines++
		totals.units += item.Qty
		totals.cost += item.Qty * item.Cost
		totals.retail += item.Qty * item.Price
	}

	columns := []petColumn{
		{attr: 0, head: "CATEGORY", width: 26},
		{attr: 28, head: "LINES", width: 6, right: true},
		{attr: 36, head: "UNITS", width: 7, right: true},
		{attr: 45, head: "AT COST", width: 12, right: true},
		{attr: 59, head: "AT RETAIL", width: 12, right: true},
		{attr: 73, head: "MARGIN", width: 6, right: true},
	}

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: fmt.Sprintf(
			"Stock on hand as at %s. Amounts in %s.", petNowStamp(), s.store.cfg.Currency)},
	}
	screen = append(screen, s.headingFields(top+2, columns)...)

	row := top + 3
	for _, category := range petCategoryOrder {
		entry := byCategory[category]
		screen = append(screen, s.cell(row, columns[0], petCategoryNames[category], petCategoryColor(category))...)
		screen = append(screen, s.cell(row, columns[1], strconv.Itoa(entry.lines), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, columns[2], strconv.Itoa(entry.units), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, columns[3], petMoney(entry.cost), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, columns[4], petMoney(entry.retail), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, columns[5], petMarginPercent(entry.cost, entry.retail), go3270.Turquoise)...)
		row++
	}

	low := s.store.belowReorder()
	screen = append(screen,
		go3270.Field{Row: row, Col: 0, Color: go3270.Blue,
			Content: strings.Repeat("-", s.cols-2)},
	)
	row++
	screen = append(screen, s.cell(row, columns[0], "TOTAL STOCK ON HAND", go3270.White)...)
	screen = append(screen, s.cell(row, columns[1], strconv.Itoa(totals.lines), go3270.White)...)
	screen = append(screen, s.cell(row, columns[2], strconv.Itoa(totals.units), go3270.White)...)
	screen = append(screen, s.cell(row, columns[3], petMoney(totals.cost), go3270.White)...)
	screen = append(screen, s.cell(row, columns[4], petMoney(totals.retail), go3270.White)...)
	screen = append(screen, s.cell(row, columns[5], petMarginPercent(totals.cost, totals.retail), go3270.White)...)

	if row+2 <= s.bodyBottom() {
		screen = append(screen, go3270.Field{Row: row + 2, Col: 0, Color: go3270.Yellow,
			Content: petTrunc(fmt.Sprintf(
				"%d lines are at or below reorder level. CHECK lists them, PET310 receives.",
				len(low)), s.cols-2)})
	}
	return screen, -1, -1
}

func petMarginPercent(cost, retail int) string {
	if retail <= 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(retail-cost)*100/float64(retail))
}

// balanceRows is the customer balances report: everyone who owes something,
// heaviest debt first.
func (s *petSession) balanceRows() []*petCustomer {
	var owing []*petCustomer
	for _, c := range s.store.customers {
		if s.store.customerBalance(c.ID) > 0 {
			owing = append(owing, c)
		}
	}
	sort.SliceStable(owing, func(i, j int) bool {
		return s.store.customerBalance(owing[i].ID) > s.store.customerBalance(owing[j].ID)
	})
	return owing
}

func (s *petSession) buildRptBalances() (go3270.Screen, int, int) {
	owing := s.balanceRows()
	top := s.bodyTop()
	s.clampTop(&s.rptTop, len(owing))

	total := 0
	for _, c := range owing {
		total += s.store.customerBalance(c.ID)
	}

	columns := []petColumn{
		{attr: 0, head: "ACCT", width: 5},
		{attr: 7, head: "NAME", width: 22},
		{attr: 31, head: "TOWN", width: 14},
		{attr: 47, head: "TIER", width: 6},
		{attr: 55, head: "INVOICES", width: 8, right: true},
		{attr: 65, head: "OUTSTANDING", width: 13, right: true},
		{attr: 81, head: "STATUS", width: 6, wide: true},
		{attr: 89, head: "TERMS", width: 12, wide: true},
	}

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: petTrunc(fmt.Sprintf(
			"%d accounts owe %s %s. Credit terms %d days.",
			len(owing), s.store.cfg.Currency, petMoney(total), s.store.cfg.CreditDays), s.cols-2)},
	}
	screen = append(screen, s.headingFields(top+1, columns)...)

	rows := s.listRows()
	for i := 0; i < rows; i++ {
		index := s.rptTop + i
		if index >= len(owing) {
			break
		}
		c := owing[index]
		row := top + 2 + i
		unpaid := 0
		for _, inv := range s.store.invoicesFor(c.ID) {
			if inv.Status != "VOID" && inv.outstanding() > 0 {
				unpaid++
			}
		}
		screen = append(screen, s.cell(row, columns[0], c.ID, go3270.White)...)
		screen = append(screen, s.cell(row, columns[1], c.Name, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, columns[2], c.Town, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, columns[3], c.Tier, petTierColor(c.Tier))...)
		screen = append(screen, s.cell(row, columns[4], strconv.Itoa(unpaid), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, columns[5], petMoney(s.store.customerBalance(c.ID)), go3270.Red)...)
		screen = append(screen, s.cell(row, columns[6], c.Status, petStatusColor(c.Status))...)
		screen = append(screen, s.cell(row, columns[7],
			fmt.Sprintf("%d DAYS", s.store.cfg.CreditDays), go3270.DefaultColor)...)
	}

	s.appendListFooter(&screen, len(owing), s.rptTop, "accounts in debt")
	return screen, -1, -1
}
