package sampleapps

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/racingmars/go3270"
)

// The stock catalogue, the item maintenance and adjustment screen, and the
// reorder report the stockroom works from.

var petStockColumns = []petColumn{
	{attr: 0, head: "A", width: 1},
	{attr: 3, head: "SKU", width: 8},
	{attr: 13, head: "DESCRIPTION", width: 26},
	{attr: 41, head: "CAT", width: 4},
	{attr: 47, head: "QTY", width: 5, right: true},
	{attr: 54, head: "RORD", width: 4, right: true},
	{attr: 60, head: "PRICE", width: 9, right: true},
	{attr: 71, head: "VALUE", width: 8, right: true},
	{attr: 81, head: "SUPPLIER", width: 20, wide: true},
	{attr: 103, head: "BIN", width: 5, wide: true},
	{attr: 110, head: "LAST IN", width: 10, wide: true},
}

var petCheckColumns = []petColumn{
	{attr: 0, head: "SKU", width: 8},
	{attr: 10, head: "DESCRIPTION", width: 26},
	{attr: 38, head: "CAT", width: 4},
	{attr: 44, head: "QTY", width: 5, right: true},
	{attr: 51, head: "RORD", width: 5, right: true},
	{attr: 58, head: "SHORT", width: 5, right: true},
	{attr: 65, head: "ORDER", width: 5, right: true},
	{attr: 72, head: "COST", width: 7, right: true},
	{attr: 81, head: "SUPPLIER", width: 20, wide: true},
	{attr: 103, head: "BIN", width: 5, wide: true},
}

func (s *petSession) buildStockList() (go3270.Screen, int, int) {
	top := s.bodyTop()
	s.clampTop(&s.stockTop, len(s.store.stock))

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Position to"},
		{Row: top, Col: 35, Color: go3270.Yellow,
			Content: petTrunc("S=Open  O=Add to an order", s.cols-37)},
	}
	screen = append(screen, petInputField(top, 12, 20, "stockfind", s.value("stockfind", ""))...)
	screen = append(screen, s.headingFields(top+1, petStockColumns)...)

	rows := s.listRows()
	for i := 0; i < rows; i++ {
		index := s.stockTop + i
		if index >= len(s.store.stock) {
			break
		}
		item := s.store.stock[index]
		row := top + 2 + i
		name := fmt.Sprintf("sel%d", i)
		qtyColor := go3270.DefaultColor
		if item.Qty <= item.Reorder {
			qtyColor = go3270.Red
		}
		screen = append(screen, petSelectField(row, name, s.value(name, ""))...)
		screen = append(screen, s.cell(row, petStockColumns[1], item.SKU, petCategoryColor(item.Category))...)
		screen = append(screen, s.cell(row, petStockColumns[2], item.Desc, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petStockColumns[3], item.Category, petCategoryColor(item.Category))...)
		screen = append(screen, s.cell(row, petStockColumns[4], strconv.Itoa(item.Qty), qtyColor)...)
		screen = append(screen, s.cell(row, petStockColumns[5], strconv.Itoa(item.Reorder), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petStockColumns[6], petMoney(item.Price), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petStockColumns[7], petMoney(item.Qty*item.Price), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petStockColumns[8], item.Supplier, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petStockColumns[9], item.Bin, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petStockColumns[10], item.LastIn, go3270.DefaultColor)...)
	}

	s.appendListFooter(&screen, len(s.store.stock), s.stockTop, "stock lines")
	return screen, top, 13
}

func petCategoryColor(category string) go3270.Color {
	switch category {
	case "LIVE":
		return go3270.Green
	case "FOOD":
		return go3270.Turquoise
	case "ACCS":
		return go3270.White
	case "MEDS":
		return go3270.Pink
	}
	return go3270.DefaultColor
}

func (s *petSession) handleStockList(resp go3270.Response) {
	switch resp.AID {
	case go3270.AIDPF7:
		s.scroll(&s.stockTop, -1, len(s.store.stock))
		return
	case go3270.AIDPF8:
		s.scroll(&s.stockTop, 1, len(s.store.stock))
		return
	case go3270.AIDPF6:
		s.goTo(petPageStockCheck)
		s.info("Items at or below their reorder level.")
		return
	case go3270.AIDEnter:
	default:
		s.info("Key %s has no action on the stock catalogue.", s.lastAID)
		return
	}

	if action, index, ok := s.lineAction(resp, len(s.store.stock), s.stockTop); ok {
		item := s.store.stock[index]
		switch action {
		case "S", "U", "":
			s.item = item
			s.goTo(petPageStockDetail)
			s.info("Item %s. %d on hand.", item.SKU, item.Qty)
		case "O":
			s.addToOrderFromCatalogue(item)
		default:
			s.fail("%q is not a line action. Use S to open or O to add to an order.", action)
		}
		return
	}

	if find := strings.ToUpper(strings.TrimSpace(resp.Values["stockfind"])); find != "" {
		s.pending["stockfind"] = ""
		for i, item := range s.store.stock {
			if strings.HasPrefix(item.SKU, find) || strings.Contains(item.Desc, find) ||
				item.Category == find || strings.Contains(item.Supplier, find) {
				s.stockTop = i
				s.clampTop(&s.stockTop, len(s.store.stock))
				s.info("Positioned at %s %s.", item.SKU, item.Desc)
				return
			}
		}
		s.fail("No stock line matches %q.", find)
		return
	}

	s.info("Type S beside an item and press Enter, or use Position to.")
}

func (s *petSession) buildStockDetail() (go3270.Screen, int, int) {
	if s.item == nil {
		return s.emptyBody("No stock item selected. Use STOCK to pick one."), -1, -1
	}
	item := s.item
	top := s.bodyTop()

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Item code . . . :"},
		{Row: top, Col: 19, Intense: true, Color: go3270.White, Content: item.SKU},
		{Row: top, Col: 32, Content: "Category"},
		{Row: top, Col: 42, Color: petCategoryColor(item.Category), Content: item.Category},
		{Row: top, Col: 50, Content: "On hand"},
		{Row: top, Col: 59, Intense: true, Color: petQtyColor(item), Content: strconv.Itoa(item.Qty)},
	}

	fields := []struct {
		label, name, stored string
		width               int
	}{
		{"Description . . :", "idesc", item.Desc, 30},
		{"Supplier  . . . :", "isupp", item.Supplier, 20},
		{"Bin location  . :", "ibin", item.Bin, 6},
		{"Unit price  . . :", "iprice", petMoney(item.Price), 10},
		{"Unit cost . . . :", "icost", petMoney(item.Cost), 10},
		{"Reorder level . :", "irord", strconv.Itoa(item.Reorder), 6},
	}
	for i, f := range fields {
		row := top + 2 + i
		if row > s.bodyBottom() {
			break
		}
		screen = append(screen, go3270.Field{Row: row, Col: 0, Content: f.label})
		screen = append(screen, petInputField(row, 19, f.width, f.name, s.value(f.name, f.stored))...)
	}

	adjTop := top + 9
	if adjTop+5 > s.bodyBottom() {
		adjTop = s.bodyBottom() - 5
	}
	if adjTop < top+8 {
		adjTop = top + 8
	}
	screen = append(screen,
		go3270.Field{Row: adjTop, Col: 0, Color: go3270.Blue, Content: strings.Repeat("-", s.cols-2)},
		go3270.Field{Row: adjTop + 1, Col: 0, Intense: true, Color: go3270.White, Content: "STOCK ADJUSTMENT"},
		go3270.Field{Row: adjTop + 1, Col: 22, Color: go3270.Yellow,
			Content: petTrunc("R=Receipt in  I=Issue out  A=Adjust count to", s.cols-24)},
		go3270.Field{Row: adjTop + 2, Col: 0, Content: "Movement type . :"},
	)
	screen = append(screen, petInputField(adjTop+2, 19, 1, "imove", s.value("imove", ""))...)
	screen = append(screen, go3270.Field{Row: adjTop + 3, Col: 0, Content: "Quantity  . . . :"})
	screen = append(screen, petNumericField(adjTop+3, 19, 6, "iqty", s.value("iqty", ""))...)
	screen = append(screen, go3270.Field{Row: adjTop + 4, Col: 0, Content: "Reason  . . . . :"})
	screen = append(screen, petInputField(adjTop+4, 19, 24, "ireason", s.value("ireason", ""))...)

	if adjTop+5 <= s.bodyBottom() {
		screen = append(screen, go3270.Field{Row: adjTop + 5, Col: 0, Color: go3270.Yellow,
			Content: petTrunc("PF5 saves the item and applies any movement. Last goods in "+item.LastIn+".", s.cols-2)})
	}
	return screen, top + 2, 20
}

func petQtyColor(item *petStockItem) go3270.Color {
	if item.Qty <= item.Reorder {
		return go3270.Red
	}
	return go3270.Green
}

func (s *petSession) handleStockDetail(resp go3270.Response) {
	if s.item == nil {
		s.goTo(petPageStockList)
		s.fail("No stock item selected.")
		return
	}
	switch resp.AID {
	case go3270.AIDPF5:
		s.saveStockItem(resp)
	case go3270.AIDPF10:
		s.stepStock(-1)
	case go3270.AIDPF11:
		s.stepStock(1)
	case go3270.AIDEnter:
		s.info("Item %s. Press PF5 to apply.", s.item.SKU)
	default:
		s.info("Key %s has no action on stock maintenance.", s.lastAID)
	}
}

func (s *petSession) stepStock(direction int) {
	for i, item := range s.store.stock {
		if item != s.item {
			continue
		}
		next := i + direction
		if next < 0 || next >= len(s.store.stock) {
			s.fail("There is no %s item.", map[int]string{-1: "previous", 1: "next"}[direction])
			return
		}
		s.item = s.store.stock[next]
		s.pending = map[string]string{}
		s.info("Item %s %s.", s.item.SKU, s.item.Desc)
		return
	}
}

func (s *petSession) saveStockItem(resp go3270.Response) {
	item := s.item

	desc := strings.ToUpper(strings.TrimSpace(resp.Values["idesc"]))
	if desc == "" {
		s.fail("Description is required.")
		return
	}
	price, ok := petParseMoney(resp.Values["iprice"])
	if !ok || price <= 0 {
		s.fail("Unit price %q is not a valid amount. Use a value such as 12.99.", strings.TrimSpace(resp.Values["iprice"]))
		return
	}
	cost, ok := petParseMoney(resp.Values["icost"])
	if !ok || cost < 0 {
		s.fail("Unit cost %q is not a valid amount.", strings.TrimSpace(resp.Values["icost"]))
		return
	}
	if cost > price {
		s.fail("Unit cost %s is above the unit price %s. Check the figures.", petMoney(cost), petMoney(price))
		return
	}
	reorder, ok := petParseInt(resp.Values["irord"], 0, 99999)
	if !ok {
		s.fail("Reorder level %q must be a number between 0 and 99999.", strings.TrimSpace(resp.Values["irord"]))
		return
	}

	move := strings.ToUpper(strings.TrimSpace(resp.Values["imove"]))
	qtyText := strings.TrimSpace(resp.Values["iqty"])
	movement := ""
	if move != "" || qtyText != "" {
		if !petOneOf(move, "R", "I", "A") {
			s.fail("Movement type must be R, I or A, not %q.", move)
			return
		}
		qty, ok := petParseInt(qtyText, 0, 99999)
		if !ok {
			s.fail("Movement quantity %q must be a number between 0 and 99999.", qtyText)
			return
		}
		reason := strings.ToUpper(strings.TrimSpace(resp.Values["ireason"]))
		if reason == "" {
			s.fail("A reason is required for a stock movement.")
			return
		}
		switch move {
		case "R":
			item.Qty += qty
			item.LastIn = petToday()
			movement = fmt.Sprintf("received %d", qty)
		case "I":
			if qty > item.Qty {
				s.fail("Cannot issue %d: only %d of %s on hand.", qty, item.Qty, item.SKU)
				return
			}
			item.Qty -= qty
			movement = fmt.Sprintf("issued %d", qty)
		case "A":
			item.Qty = qty
			movement = fmt.Sprintf("count adjusted to %d", qty)
		}
		s.store.logAudit(s.user, "STOCK", fmt.Sprintf("%s %s - %s", item.SKU, strings.ToUpper(movement), reason))
	}

	item.Desc = desc
	item.Supplier = strings.ToUpper(strings.TrimSpace(resp.Values["isupp"]))
	item.Bin = strings.ToUpper(strings.TrimSpace(resp.Values["ibin"]))
	item.Price = price
	item.Cost = cost
	item.Reorder = reorder

	s.pending = map[string]string{}
	if movement == "" {
		s.info("Item %s saved. %d on hand.", item.SKU, item.Qty)
		return
	}
	s.info("Item %s saved and %s. %d on hand.", item.SKU, movement, item.Qty)
}

func (s *petSession) buildStockCheck() (go3270.Screen, int, int) {
	low := s.store.belowReorder()
	top := s.bodyTop()
	s.clampTop(&s.stockTop, len(low))

	shortUnits, shortCost := 0, 0
	for _, item := range low {
		suggest := petSuggestedOrder(item)
		shortUnits += suggest
		shortCost += suggest * item.Cost
	}

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: petTrunc(fmt.Sprintf(
			"%d of %d lines at or below reorder. Suggested purchase %d units, %s %s.",
			len(low), len(s.store.stock), shortUnits, s.store.cfg.Currency, petMoney(shortCost)), s.cols-2)},
	}
	screen = append(screen, s.headingFields(top+1, petCheckColumns)...)

	rows := s.listRows()
	for i := 0; i < rows; i++ {
		index := s.stockTop + i
		if index >= len(low) {
			break
		}
		item := low[index]
		row := top + 2 + i
		short := item.Reorder - item.Qty
		if short < 0 {
			short = 0
		}
		suggest := petSuggestedOrder(item)
		screen = append(screen, s.cell(row, petCheckColumns[0], item.SKU, petCategoryColor(item.Category))...)
		screen = append(screen, s.cell(row, petCheckColumns[1], item.Desc, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petCheckColumns[2], item.Category, petCategoryColor(item.Category))...)
		screen = append(screen, s.cell(row, petCheckColumns[3], strconv.Itoa(item.Qty), go3270.Red)...)
		screen = append(screen, s.cell(row, petCheckColumns[4], strconv.Itoa(item.Reorder), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petCheckColumns[5], strconv.Itoa(short), go3270.Yellow)...)
		screen = append(screen, s.cell(row, petCheckColumns[6], strconv.Itoa(suggest), go3270.Green)...)
		screen = append(screen, s.cell(row, petCheckColumns[7], petMoney(suggest*item.Cost), go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petCheckColumns[8], item.Supplier, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petCheckColumns[9], item.Bin, go3270.DefaultColor)...)
	}

	s.appendListFooter(&screen, len(low), s.stockTop, "lines below reorder")
	return screen, -1, -1
}

// petSuggestedOrder is the reorder cover the configuration asks for: enough to
// take the line back to its reorder level plus the configured percentage.
func petSuggestedOrder(item *petStockItem) int {
	target := item.Reorder * 2
	if target < 1 {
		target = 1
	}
	suggest := target - item.Qty
	if suggest < 1 {
		suggest = 1
	}
	return suggest
}

func (s *petSession) handleStockCheck(resp go3270.Response) {
	s.handleScrollOnly(resp, &s.stockTop, len(s.store.belowReorder()))
}

func petParseInt(text string, low, high int) (int, bool) {
	t := strings.TrimSpace(text)
	if t == "" {
		return 0, false
	}
	n, err := strconv.Atoi(t)
	if err != nil {
		return 0, false
	}
	if n < low || n > high {
		return 0, false
	}
	return n, true
}

// petParseMoney reads an amount typed by an operator. Both "1299" and
// "12.99" are accepted, along with grouped thousands, because all three turn
// up on a keyboard that has been used to a different system.
func petParseMoney(text string) (int, bool) {
	t := strings.TrimSpace(text)
	if t == "" {
		return 0, false
	}
	negative := strings.HasPrefix(t, "-")
	t = strings.TrimPrefix(t, "-")
	t = strings.ReplaceAll(t, ",", "")
	if t == "" {
		return 0, false
	}
	units, fraction := t, ""
	if dot := strings.IndexByte(t, '.'); dot >= 0 {
		units, fraction = t[:dot], t[dot+1:]
	}
	if units == "" {
		units = "0"
	}
	whole, err := strconv.Atoi(units)
	if err != nil || whole < 0 || whole > 9999999 {
		return 0, false
	}
	pence := 0
	if fraction != "" {
		switch len(fraction) {
		case 1:
			fraction += "0"
		case 2:
		default:
			return 0, false
		}
		pence, err = strconv.Atoi(fraction)
		if err != nil || pence < 0 {
			return 0, false
		}
	}
	total := whole*100 + pence
	if negative {
		total = -total
	}
	return total, true
}
