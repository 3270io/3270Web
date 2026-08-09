package sampleapps

import (
	"fmt"
	"strings"
	"time"
)

// The pet store's data lives here: the record types, the seed, and the small
// number of operations the screens perform on it.
//
// Every connection gets its own copy (see newPetStore). A sample app is a
// demonstration surface — chaos mode drives it by typing generated values into
// every field it can find and pressing every key it knows — so one visitor
// deleting a customer, posting a payment or running an end-of-day job must not
// change what the next visitor sees.

// Amounts are held in pence and formatted on the way out. Nothing in a store
// ledger should be a float.

type petCustomer struct {
	ID       string
	Name     string
	Town     string
	Postcode string
	Phone    string
	Email    string
	Tier     string // BRONZE, SILVER, GOLD
	Status   string // ACTIVE, HOLD, CLOSED
	Balance  int
	Since    string
	Notes    string
}

type petStockItem struct {
	SKU      string
	Desc     string
	Category string // LIVE, FOOD, ACCS, MEDS
	Qty      int
	Reorder  int
	Price    int
	Cost     int
	Supplier string
	Bin      string
	LastIn   string
}

type petOrderLine struct {
	SKU   string
	Desc  string
	Qty   int
	Price int
}

type petOrder struct {
	ID     string
	CustID string
	Date   string
	Status string // OPEN, PICKED, INVOICED, CANCELLED
	Lines  []petOrderLine
}

type petInvoice struct {
	ID      string
	OrderID string
	CustID  string
	Date    string
	Net     int
	VAT     int
	Total   int
	Paid    int
	Status  string // OPEN, PART, PAID, VOID
}

type petPayment struct {
	ID        string
	InvoiceID string
	CustID    string
	Date      string
	Amount    int
	Method    string // CASH, CARD, ACCT, CHEQUE
	Ref       string
}

type petUser struct {
	ID     string
	Name   string
	Role   string // ADMIN, MANAGER, CLERK, BATCH
	Status string // ACTIVE, LOCKED
	Last   string
}

type petAuditEntry struct {
	Stamp  string
	User   string
	Action string
	Detail string
}

type petJob struct {
	Name     string
	Desc     string
	Schedule string
	LastRun  string
	Status   string // OK, FAILED, NEVER, RUNNING
	Elapsed  string
}

type petConfig struct {
	StoreName  string
	StoreNo    string
	Currency   string
	VATRate    int // basis points: 2000 = 20.00%
	ReorderPct int
	OpenTime   string
	CloseTime  string
	Manager    string
	Region     string
	LoyaltyPct int
	CreditDays int
}

type petStore struct {
	cfg       petConfig
	customers []*petCustomer
	stock     []*petStockItem
	orders    []*petOrder
	invoices  []*petInvoice
	payments  []*petPayment
	users     []*petUser
	audit     []petAuditEntry
	jobs      []*petJob
	seq       map[string]int
}

// petMoney renders pence the way a ledger column wants it: grouped thousands,
// always two decimal places, minus sign in front.
func petMoney(pence int) string {
	neg := pence < 0
	if neg {
		pence = -pence
	}
	out := petGroupDigits(pence/100) + fmt.Sprintf(".%02d", pence%100)
	if neg {
		return "-" + out
	}
	return out
}

func petGroupDigits(n int) string {
	digits := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(digits[i])
	}
	return b.String()
}

func petPercent(basisPoints int) string {
	return fmt.Sprintf("%d.%02d", basisPoints/100, basisPoints%100)
}

func petDaysAgo(days int) string {
	return time.Now().AddDate(0, 0, -days).Format("2006-01-02")
}

func petToday() string {
	return time.Now().Format("2006-01-02")
}

func petNowStamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func (s *petStore) nextID(key, prefix string, width int) string {
	s.seq[key]++
	return fmt.Sprintf("%s%0*d", prefix, width, s.seq[key])
}

func (s *petStore) findCustomer(id string) *petCustomer {
	id = strings.ToUpper(strings.TrimSpace(id))
	for _, c := range s.customers {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func (s *petStore) findStock(sku string) *petStockItem {
	sku = strings.ToUpper(strings.TrimSpace(sku))
	for _, item := range s.stock {
		if item.SKU == sku {
			return item
		}
	}
	return nil
}

func (s *petStore) findOrder(id string) *petOrder {
	id = strings.ToUpper(strings.TrimSpace(id))
	for _, o := range s.orders {
		if o.ID == id {
			return o
		}
	}
	return nil
}

func (s *petStore) findInvoice(id string) *petInvoice {
	id = strings.ToUpper(strings.TrimSpace(id))
	for _, inv := range s.invoices {
		if inv.ID == id {
			return inv
		}
	}
	return nil
}

func (s *petStore) findJob(name string) *petJob {
	name = strings.ToUpper(strings.TrimSpace(name))
	for _, j := range s.jobs {
		if j.Name == name {
			return j
		}
	}
	return nil
}

func (s *petStore) findUser(id string) *petUser {
	id = strings.ToUpper(strings.TrimSpace(id))
	for _, u := range s.users {
		if u.ID == id {
			return u
		}
	}
	return nil
}

// logAudit records an action. The trail is bounded because a chaos run will
// happily generate thousands of entries in a minute and this is all held in
// memory for the life of one connection.
func (s *petStore) logAudit(user, action, detail string) {
	entry := petAuditEntry{Stamp: petNowStamp(), User: user, Action: action, Detail: detail}
	s.audit = append([]petAuditEntry{entry}, s.audit...)
	const maxAudit = 400
	if len(s.audit) > maxAudit {
		s.audit = s.audit[:maxAudit]
	}
}

func (o *petOrder) net() int {
	total := 0
	for _, line := range o.Lines {
		total += line.Qty * line.Price
	}
	return total
}

func (s *petStore) vatOn(net int) int {
	return net * s.cfg.VATRate / 10000
}

func (i *petInvoice) outstanding() int {
	return i.Total - i.Paid
}

// stockValue is the retail value of everything on the shelves.
func (s *petStore) stockValue() (retail, cost, units int) {
	for _, item := range s.stock {
		retail += item.Qty * item.Price
		cost += item.Qty * item.Cost
		units += item.Qty
	}
	return retail, cost, units
}

func (s *petStore) belowReorder() []*petStockItem {
	var low []*petStockItem
	for _, item := range s.stock {
		if item.Qty <= item.Reorder {
			low = append(low, item)
		}
	}
	return low
}

func (s *petStore) customerBalance(id string) int {
	total := 0
	for _, inv := range s.invoices {
		if inv.CustID == id && inv.Status != "VOID" {
			total += inv.outstanding()
		}
	}
	return total
}

func (s *petStore) invoicesFor(custID string) []*petInvoice {
	var out []*petInvoice
	for _, inv := range s.invoices {
		if inv.CustID == custID {
			out = append(out, inv)
		}
	}
	return out
}

func (s *petStore) paymentsFor(invoiceID string) []*petPayment {
	var out []*petPayment
	for _, p := range s.payments {
		if p.InvoiceID == invoiceID {
			out = append(out, p)
		}
	}
	return out
}

// newPetStore builds a fresh, fully populated store.
//
// The data is deliberately deterministic apart from the dates, which are
// relative to today so that a screenshot taken from the sample app never looks
// like it came out of a museum.
func newPetStore() *petStore {
	s := &petStore{
		cfg: petConfig{
			StoreName:  "PAWS AND CLAWS PET STORE",
			StoreNo:    "0042",
			Currency:   "GBP",
			VATRate:    2000,
			ReorderPct: 25,
			OpenTime:   "08:30",
			CloseTime:  "18:00",
			Manager:    "J HOLLOWAY",
			Region:     "NORTH",
			LoyaltyPct: 250,
			CreditDays: 30,
		},
		seq: map[string]int{},
	}
	s.seedCustomers()
	s.seedStock()
	s.seedUsers()
	s.seedJobs()
	s.seedTrading()
	s.seedAudit()
	return s
}

func (s *petStore) seedCustomers() {
	rows := []struct {
		name, town, postcode, phone, email, tier, status string
		balance, sinceDays                               int
		notes                                            string
	}{
		{"ARMITAGE, R", "HARROGATE", "HG1 4AP", "01423 550118", "R.ARMITAGE@EXAMPLE.TEST", "GOLD", "ACTIVE", 0, 1420, "BREEDER - TRADE PRICING AGREED"},
		{"BAINBRIDGE, T", "LEEDS", "LS2 7HG", "0113 496 2210", "T.BAIN@EXAMPLE.TEST", "SILVER", "ACTIVE", 0, 980, ""},
		{"CHOWDHURY, A", "BRADFORD", "BD1 2NE", "01274 330541", "A.CHOW@EXAMPLE.TEST", "BRONZE", "ACTIVE", 0, 210, "PREFERS SATURDAY COLLECTION"},
		{"DALGLEISH, M", "YORK", "YO1 9QL", "01904 771203", "M.DALG@EXAMPLE.TEST", "GOLD", "ACTIVE", 0, 2110, "AQUATICS SPECIALIST - KOI"},
		{"EDWARDS, P", "WAKEFIELD", "WF1 3RD", "01924 880417", "P.EDWARDS@EXAMPLE.TEST", "BRONZE", "HOLD", 0, 640, "ACCOUNT ON HOLD - CREDIT REVIEW"},
		{"FAIRHURST, L", "SHEFFIELD", "S1 4BT", "0114 271 9930", "L.FAIR@EXAMPLE.TEST", "SILVER", "ACTIVE", 0, 1180, ""},
		{"GHOSH, S", "HUDDERSFIELD", "HD1 6PW", "01484 220716", "S.GHOSH@EXAMPLE.TEST", "SILVER", "ACTIVE", 0, 770, "VET PRACTICE - MONTHLY ACCOUNT"},
		{"HOLROYD, K", "HALIFAX", "HX1 1UE", "01422 361904", "K.HOLROYD@EXAMPLE.TEST", "BRONZE", "ACTIVE", 0, 95, ""},
		{"IRELAND, D", "DONCASTER", "DN1 3AA", "01302 449108", "D.IRELAND@EXAMPLE.TEST", "BRONZE", "ACTIVE", 0, 330, ""},
		{"JEFFRIES, N", "HULL", "HU1 2PG", "01482 615370", "N.JEFF@EXAMPLE.TEST", "GOLD", "ACTIVE", 0, 1760, "KENNELS - BULK FEED ORDERS"},
		{"KOWALSKI, W", "SKIPTON", "BD23 1DT", "01756 700254", "W.KOW@EXAMPLE.TEST", "SILVER", "ACTIVE", 0, 520, ""},
		{"LAMBERT, C", "RIPON", "HG4 1BZ", "01765 318825", "C.LAMBERT@EXAMPLE.TEST", "BRONZE", "ACTIVE", 0, 150, ""},
		{"MORRISON, E", "OTLEY", "LS21 3HB", "01943 552901", "E.MORR@EXAMPLE.TEST", "SILVER", "ACTIVE", 0, 880, "REPTILE LICENCE ON FILE"},
		{"NAYLOR, G", "KEIGHLEY", "BD21 4AA", "01535 240733", "G.NAYLOR@EXAMPLE.TEST", "BRONZE", "CLOSED", 0, 2400, "CLOSED AT CUSTOMER REQUEST"},
		{"O'DONNELL, F", "SELBY", "YO8 4PT", "01757 903216", "F.ODON@EXAMPLE.TEST", "BRONZE", "ACTIVE", 0, 270, ""},
		{"PATEL, H", "ROTHERHAM", "S60 1AA", "01709 812445", "H.PATEL@EXAMPLE.TEST", "GOLD", "ACTIVE", 0, 1330, "GROOMING SALON - TRADE"},
		{"QUINN, B", "BARNSLEY", "S70 2DR", "01226 774160", "B.QUINN@EXAMPLE.TEST", "BRONZE", "ACTIVE", 0, 410, ""},
		{"RASHID, Y", "DEWSBURY", "WF13 1LU", "01924 337082", "Y.RASHID@EXAMPLE.TEST", "SILVER", "ACTIVE", 0, 690, ""},
		{"SUTCLIFFE, A", "PONTEFRACT", "WF8 1PN", "01977 620519", "A.SUTC@EXAMPLE.TEST", "BRONZE", "ACTIVE", 0, 120, ""},
		{"THWAITE, J", "SCARBOROUGH", "YO11 1JX", "01723 448277", "J.THWAITE@EXAMPLE.TEST", "SILVER", "ACTIVE", 0, 1050, "SEASONAL - AQUARIUM STOCK"},
		{"UNWIN, R", "WHITBY", "YO21 3PU", "01947 550136", "R.UNWIN@EXAMPLE.TEST", "BRONZE", "ACTIVE", 0, 305, ""},
		{"VARLEY, S", "ILKLEY", "LS29 8HA", "01943 601788", "S.VARLEY@EXAMPLE.TEST", "GOLD", "ACTIVE", 0, 1990, "RESCUE CHARITY - VAT EXEMPT REVIEW"},
		{"WHITTAKER, D", "MALTON", "YO17 7LX", "01653 227094", "D.WHIT@EXAMPLE.TEST", "BRONZE", "ACTIVE", 0, 60, "NEW ACCOUNT"},
		{"YEADON, M", "THIRSK", "YO7 1QS", "01845 336810", "M.YEADON@EXAMPLE.TEST", "SILVER", "ACTIVE", 0, 860, ""},
	}
	for i, r := range rows {
		s.customers = append(s.customers, &petCustomer{
			ID:       fmt.Sprintf("C%04d", i+1),
			Name:     r.name,
			Town:     r.town,
			Postcode: r.postcode,
			Phone:    r.phone,
			Email:    r.email,
			Tier:     r.tier,
			Status:   r.status,
			Balance:  r.balance,
			Since:    petDaysAgo(r.sinceDays),
			Notes:    r.notes,
		})
	}
	s.seq["cust"] = len(rows)
}

func (s *petStore) seedStock() {
	rows := []struct {
		sku, desc, category, supplier, bin string
		qty, reorder, price, cost, lastIn  int
	}{
		{"LIV-0001", "LABRADOR PUPPY (KC REG)", "LIVE", "MOORSIDE KENNELS", "A-01", 3, 2, 95000, 62000, 6},
		{"LIV-0002", "PERSIAN KITTEN", "LIVE", "DALESIDE CATTERY", "A-02", 4, 2, 78000, 51000, 9},
		{"LIV-0003", "BUDGERIGAR (ASSORTED)", "LIVE", "AVIARY SUPPLIES LTD", "A-03", 18, 8, 3500, 1900, 3},
		{"LIV-0004", "COCKATIEL", "LIVE", "AVIARY SUPPLIES LTD", "A-03", 6, 4, 12500, 7400, 3},
		{"LIV-0005", "GOLDFISH (COMMON)", "LIVE", "NORTHERN AQUATICS", "B-01", 140, 60, 350, 140, 1},
		{"LIV-0006", "KOI CARP 15CM", "LIVE", "NORTHERN AQUATICS", "B-02", 12, 10, 8500, 5200, 4},
		{"LIV-0007", "SYRIAN HAMSTER", "LIVE", "SMALLPET DIRECT", "A-05", 9, 6, 1800, 850, 2},
		{"LIV-0008", "GUINEA PIG (PAIR)", "LIVE", "SMALLPET DIRECT", "A-06", 5, 4, 5500, 3100, 5},
		{"LIV-0009", "NETHERLAND DWARF RABBIT", "LIVE", "SMALLPET DIRECT", "A-07", 2, 4, 4800, 2700, 12},
		{"LIV-0010", "BEARDED DRAGON JUVENILE", "LIVE", "EXOTIC HERP CO", "C-01", 3, 2, 16500, 9800, 8},
		{"LIV-0011", "CORN SNAKE", "LIVE", "EXOTIC HERP CO", "C-02", 1, 2, 14000, 8200, 15},
		{"LIV-0012", "AFRICAN GREY PARROT", "LIVE", "AVIARY SUPPLIES LTD", "A-04", 1, 1, 140000, 95000, 21},
		{"FOD-0001", "COMPLETE DOG FOOD 12KG", "FOOD", "PENNINE PETFOODS", "D-01", 64, 24, 3450, 2100, 2},
		{"FOD-0002", "SENIOR DOG FOOD 12KG", "FOOD", "PENNINE PETFOODS", "D-02", 31, 20, 3695, 2250, 2},
		{"FOD-0003", "CAT FOOD PREMIUM 4KG", "FOOD", "PENNINE PETFOODS", "D-03", 88, 30, 1899, 1120, 1},
		{"FOD-0004", "KITTEN MILK REPLACER 250G", "FOOD", "PENNINE PETFOODS", "D-04", 14, 12, 899, 470, 7},
		{"FOD-0005", "WILD BIRD SEED 5KG", "FOOD", "GRANGE MILLS", "D-05", 120, 40, 1250, 640, 1},
		{"FOD-0006", "TROPICAL FISH FLAKES 250G", "FOOD", "NORTHERN AQUATICS", "B-04", 47, 20, 799, 380, 4},
		{"FOD-0007", "MEADOW HAY BALE 10KG", "FOOD", "GRANGE MILLS", "D-06", 22, 25, 1150, 590, 3},
		{"FOD-0008", "RABBIT NUGGETS 3KG", "FOOD", "GRANGE MILLS", "D-07", 38, 18, 1075, 540, 3},
		{"ACC-0001", "LEATHER DOG LEAD 1.2M", "ACCS", "HARDWEAR PET KIT", "E-01", 42, 15, 1650, 780, 5},
		{"ACC-0002", "PADDED DOG HARNESS L", "ACCS", "HARDWEAR PET KIT", "E-02", 17, 10, 2450, 1180, 5},
		{"ACC-0003", "CAT LITTER TRAY HOODED", "ACCS", "HARDWEAR PET KIT", "E-03", 26, 12, 2199, 990, 6},
		{"ACC-0004", "BIRD CAGE LARGE FLIGHT", "ACCS", "AVIARY SUPPLIES LTD", "E-04", 4, 3, 12900, 7600, 11},
		{"ACC-0005", "AQUARIUM 60L WITH LID", "ACCS", "NORTHERN AQUATICS", "B-05", 7, 5, 8999, 5300, 9},
		{"ACC-0006", "AQUARIUM FILTER 400LPH", "ACCS", "NORTHERN AQUATICS", "B-06", 19, 10, 2799, 1450, 4},
		{"ACC-0007", "HAMSTER EXERCISE WHEEL", "ACCS", "SMALLPET DIRECT", "E-05", 33, 15, 899, 390, 3},
		{"ACC-0008", "GROOMING KIT 7 PIECE", "ACCS", "HARDWEAR PET KIT", "E-06", 11, 8, 3250, 1690, 8},
		{"ACC-0009", "ORTHOPAEDIC DOG BED L", "ACCS", "HARDWEAR PET KIT", "E-07", 6, 6, 5495, 2950, 10},
		{"ACC-0010", "VIVARIUM 90CM WOODEN", "ACCS", "EXOTIC HERP CO", "C-04", 2, 2, 18500, 11200, 14},
		{"MED-0001", "FLEA SPOT-ON DOG 4 PACK", "MEDS", "VETLINE WHOLESALE", "F-01", 54, 20, 2295, 1140, 2},
		{"MED-0002", "FLEA SPOT-ON CAT 4 PACK", "MEDS", "VETLINE WHOLESALE", "F-02", 49, 20, 2095, 1030, 2},
		{"MED-0003", "WORMING TABLETS 6 PACK", "MEDS", "VETLINE WHOLESALE", "F-03", 37, 15, 1799, 860, 6},
		{"MED-0004", "EAR CLEANER 100ML", "MEDS", "VETLINE WHOLESALE", "F-04", 12, 12, 949, 440, 7},
		{"MED-0005", "REPTILE VITAMIN DUST 50G", "MEDS", "EXOTIC HERP CO", "C-05", 8, 10, 1199, 560, 13},
		{"MED-0006", "AQUARIUM WATER CONDITIONER", "MEDS", "NORTHERN AQUATICS", "B-07", 25, 12, 1099, 480, 4},
	}
	for _, r := range rows {
		s.stock = append(s.stock, &petStockItem{
			SKU:      r.sku,
			Desc:     r.desc,
			Category: r.category,
			Qty:      r.qty,
			Reorder:  r.reorder,
			Price:    r.price,
			Cost:     r.cost,
			Supplier: r.supplier,
			Bin:      r.bin,
			LastIn:   petDaysAgo(r.lastIn),
		})
	}
}

func (s *petStore) seedUsers() {
	s.users = []*petUser{
		{ID: "ADMIN", Name: "SYSTEM ADMINISTRATOR", Role: "ADMIN", Status: "ACTIVE", Last: petDaysAgo(0)},
		{ID: "PETMGR", Name: "J HOLLOWAY (STORE MANAGER)", Role: "MANAGER", Status: "ACTIVE", Last: petDaysAgo(0)},
		{ID: "PETOP1", Name: "S CARTWRIGHT (COUNTER)", Role: "CLERK", Status: "ACTIVE", Last: petDaysAgo(1)},
		{ID: "PETOP2", Name: "A NDLOVU (COUNTER)", Role: "CLERK", Status: "ACTIVE", Last: petDaysAgo(2)},
		{ID: "PETSTK", Name: "M BRIERLEY (STOCKROOM)", Role: "CLERK", Status: "ACTIVE", Last: petDaysAgo(1)},
		{ID: "PETBAT", Name: "OVERNIGHT BATCH SCHEDULER", Role: "BATCH", Status: "ACTIVE", Last: petDaysAgo(0)},
		{ID: "PETTMP", Name: "SEASONAL TEMPORARY STAFF", Role: "CLERK", Status: "LOCKED", Last: petDaysAgo(46)},
	}
}

func (s *petStore) seedJobs() {
	s.jobs = []*petJob{
		{Name: "EODCLOSE", Desc: "END OF DAY CLOSE AND BANKING", Schedule: "DAILY 18:30", LastRun: petDaysAgo(1), Status: "OK", Elapsed: "00:01:42"},
		{Name: "STKREORD", Desc: "STOCK REORDER PROPOSAL EXTRACT", Schedule: "DAILY 19:00", LastRun: petDaysAgo(1), Status: "OK", Elapsed: "00:00:37"},
		{Name: "INVPRINT", Desc: "INVOICE PRINT AND DESPATCH RUN", Schedule: "DAILY 19:15", LastRun: petDaysAgo(1), Status: "OK", Elapsed: "00:00:58"},
		{Name: "CUSTPURG", Desc: "PURGE CLOSED CUSTOMERS", Schedule: "MONTHLY 01", LastRun: petDaysAgo(23), Status: "OK", Elapsed: "00:02:15"},
		{Name: "DBREORG", Desc: "REORGANISE MASTER INDEXES", Schedule: "WEEKLY SUN", LastRun: petDaysAgo(4), Status: "FAILED", Elapsed: "00:00:11"},
		{Name: "VATRETN", Desc: "QUARTERLY VAT RETURN EXTRACT", Schedule: "QUARTERLY", LastRun: petDaysAgo(51), Status: "OK", Elapsed: "00:03:04"},
		{Name: "LOYALTY", Desc: "RECALCULATE LOYALTY TIERS", Schedule: "WEEKLY MON", LastRun: petDaysAgo(6), Status: "OK", Elapsed: "00:00:22"},
		{Name: "ARCHLOG", Desc: "ARCHIVE AUDIT LOG TO TAPE", Schedule: "WEEKLY SAT", LastRun: "", Status: "NEVER", Elapsed: ""},
	}
}

// seedTrading builds orders, then the invoices raised from them, then the
// payments made against those invoices, so the three ledgers agree with each
// other the way they would on a real system.
func (s *petStore) seedTrading() {
	type seedLine struct {
		sku string
		qty int
	}
	rows := []struct {
		cust    string
		days    int
		status  string
		lines   []seedLine
		invoice bool
		paidPct int
		method  string
	}{
		{"C0001", 26, "INVOICED", []seedLine{{"FOD-0001", 12}, {"MED-0001", 6}}, true, 100, "ACCT"},
		{"C0004", 24, "INVOICED", []seedLine{{"LIV-0006", 4}, {"ACC-0006", 2}, {"MED-0006", 3}}, true, 100, "CARD"},
		{"C0010", 21, "INVOICED", []seedLine{{"FOD-0001", 30}, {"FOD-0002", 10}}, true, 60, "ACCT"},
		{"C0016", 18, "INVOICED", []seedLine{{"ACC-0008", 4}, {"MED-0002", 8}}, true, 100, "CARD"},
		{"C0022", 15, "INVOICED", []seedLine{{"FOD-0005", 20}, {"FOD-0008", 12}, {"ACC-0007", 5}}, true, 0, "ACCT"},
		{"C0007", 12, "INVOICED", []seedLine{{"MED-0003", 10}, {"MED-0004", 6}}, true, 100, "ACCT"},
		{"C0013", 9, "INVOICED", []seedLine{{"LIV-0010", 1}, {"ACC-0010", 1}, {"MED-0005", 2}}, true, 50, "CARD"},
		{"C0020", 7, "INVOICED", []seedLine{{"ACC-0005", 2}, {"LIV-0005", 30}}, true, 0, "CASH"},
		{"C0002", 5, "INVOICED", []seedLine{{"LIV-0003", 2}, {"FOD-0005", 3}}, true, 100, "CASH"},
		{"C0006", 4, "PICKED", []seedLine{{"ACC-0001", 2}, {"ACC-0003", 1}}, false, 0, ""},
		{"C0011", 3, "OPEN", []seedLine{{"LIV-0007", 2}, {"ACC-0007", 2}}, false, 0, ""},
		{"C0018", 2, "OPEN", []seedLine{{"FOD-0003", 6}}, false, 0, ""},
		{"C0003", 1, "CANCELLED", []seedLine{{"LIV-0002", 1}}, false, 0, ""},
		{"C0024", 1, "OPEN", []seedLine{{"FOD-0006", 4}, {"MED-0006", 2}}, false, 0, ""},
	}

	for _, r := range rows {
		order := &petOrder{
			ID:     s.nextID("order", "O", 5),
			CustID: r.cust,
			Date:   petDaysAgo(r.days),
			Status: r.status,
		}
		for _, l := range r.lines {
			item := s.findStock(l.sku)
			if item == nil {
				continue
			}
			order.Lines = append(order.Lines, petOrderLine{
				SKU: item.SKU, Desc: item.Desc, Qty: l.qty, Price: item.Price,
			})
		}
		s.orders = append(s.orders, order)

		if !r.invoice {
			continue
		}
		net := order.net()
		vat := s.vatOn(net)
		inv := &petInvoice{
			ID:      s.nextID("invoice", "I", 5),
			OrderID: order.ID,
			CustID:  order.CustID,
			Date:    petDaysAgo(r.days - 1),
			Net:     net,
			VAT:     vat,
			Total:   net + vat,
			Status:  "OPEN",
		}
		if r.paidPct > 0 {
			paid := inv.Total * r.paidPct / 100
			inv.Paid = paid
			s.payments = append(s.payments, &petPayment{
				ID:        s.nextID("payment", "P", 5),
				InvoiceID: inv.ID,
				CustID:    inv.CustID,
				Date:      petDaysAgo(r.days - 2),
				Amount:    paid,
				Method:    r.method,
				Ref:       "SEED/" + inv.ID,
			})
		}
		switch {
		case inv.Paid >= inv.Total:
			inv.Status = "PAID"
		case inv.Paid > 0:
			inv.Status = "PART"
		}
		s.invoices = append(s.invoices, inv)
	}

	for _, c := range s.customers {
		c.Balance = s.customerBalance(c.ID)
	}
}

func (s *petStore) seedAudit() {
	seed := []petAuditEntry{
		{Stamp: petDaysAgo(0) + " 08:31:04", User: "PETBAT", Action: "SIGNON", Detail: "OVERNIGHT BATCH SCHEDULER STARTED"},
		{Stamp: petDaysAgo(0) + " 08:32:11", User: "PETBAT", Action: "JOB", Detail: "DBREORG ENDED RC=12 - SEE HOUSEKEEPING"},
		{Stamp: petDaysAgo(1) + " 18:31:02", User: "PETBAT", Action: "JOB", Detail: "EODCLOSE ENDED RC=00"},
		{Stamp: petDaysAgo(1) + " 17:44:55", User: "PETMGR", Action: "CONFIG", Detail: "REORDER COVER CHANGED 20 TO 25 PERCENT"},
		{Stamp: petDaysAgo(1) + " 16:02:19", User: "PETOP1", Action: "PAYMENT", Detail: "PAYMENT POSTED AGAINST I00009"},
		{Stamp: petDaysAgo(2) + " 11:15:40", User: "ADMIN", Action: "SECURITY", Detail: "USER PETTMP LOCKED AFTER 3 FAILED SIGNONS"},
		{Stamp: petDaysAgo(3) + " 09:07:33", User: "PETSTK", Action: "STOCK", Detail: "GOODS IN RECEIPT 40 UNITS FOD-0005"},
		{Stamp: petDaysAgo(4) + " 19:00:12", User: "PETBAT", Action: "JOB", Detail: "STKREORD ENDED RC=00 - 6 PROPOSALS RAISED"},
	}
	s.audit = append(s.audit, seed...)
}
