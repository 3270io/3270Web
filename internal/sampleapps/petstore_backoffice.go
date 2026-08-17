// SPDX-License-Identifier: AGPL-3.0-or-later

package sampleapps

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/racingmars/go3270"
)

// The back office: configuration, user and security maintenance, the
// housekeeping jobs, and the audit trail everything else writes to.
//
// These are the screens where the role from signon matters. Anyone may look;
// only ADMIN and MANAGER may change anything. Keeping the screens readable to
// everybody is deliberate — a demonstration system whose back half is behind
// a door nobody has the key to is a demonstration of a locked door.

var petUserColumns = []petColumn{
	{attr: 0, head: "A", width: 1},
	{attr: 3, head: "USER ID", width: 8},
	{attr: 13, head: "NAME", width: 30},
	{attr: 45, head: "ROLE", width: 8},
	{attr: 55, head: "STATUS", width: 6},
	{attr: 63, head: "LAST ON", width: 10},
}

var petJobColumns = []petColumn{
	{attr: 0, head: "A", width: 1},
	{attr: 3, head: "JOB", width: 8},
	{attr: 13, head: "DESCRIPTION", width: 30},
	{attr: 45, head: "SCHEDULE", width: 12},
	{attr: 59, head: "LAST RUN", width: 10},
	{attr: 71, head: "STATUS", width: 7},
	{attr: 81, head: "ELAPSED", width: 9, wide: true},
}

// System configuration.

func (s *petSession) buildConfig() (go3270.Screen, int, int) {
	cfg := s.store.cfg
	top := s.bodyTop()

	fields := []struct {
		label, name, stored string
		width               int
		note                string
	}{
		{"Store name  . . :", "gname", cfg.StoreName, 30, ""},
		{"Store number  . :", "gno", cfg.StoreNo, 4, ""},
		{"Region  . . . . :", "gregion", cfg.Region, 10, ""},
		{"Store manager . :", "gmgr", cfg.Manager, 20, ""},
		{"Currency  . . . :", "gcur", cfg.Currency, 3, "Three letter code"},
		{"VAT rate  . . . :", "gvat", petPercent(cfg.VATRate), 6, "Percent, 0.00 to 40.00"},
		{"Reorder cover . :", "grord", strconv.Itoa(cfg.ReorderPct), 3, "Percent above reorder level"},
		{"Loyalty rate  . :", "gloy", petPercent(cfg.LoyaltyPct), 6, "Percent of net spend"},
		{"Credit terms  . :", "gcred", strconv.Itoa(cfg.CreditDays), 3, "Days"},
		{"Opening time  . :", "gopen", cfg.OpenTime, 5, "HH:MM"},
		{"Closing time  . :", "gclose", cfg.CloseTime, 5, "HH:MM"},
	}

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "Parameters used by pricing, reordering and the counter screens."},
	}
	for i, f := range fields {
		row := top + 2 + i
		if row > s.bodyBottom()-1 {
			break
		}
		screen = append(screen, go3270.Field{Row: row, Col: 0, Content: f.label})
		screen = append(screen, petInputField(row, 19, f.width, f.name, s.value(f.name, f.stored))...)
		if f.note != "" {
			noteCol := 19 + f.width + 3
			screen = append(screen, go3270.Field{Row: row, Col: noteCol, Color: go3270.Yellow,
				Content: petTrunc(f.note, s.cols-noteCol-2)})
		}
	}

	note := "PF5 saves. Your role is " + s.role + ", which may not change configuration."
	if s.isAdmin() {
		note = "PF5 saves. Changes are written to the audit log against " + s.user + "."
	}
	screen = append(screen, go3270.Field{Row: s.bodyBottom(), Col: 0, Color: go3270.Yellow,
		Content: petTrunc(note, s.cols-2)})
	return screen, top + 2, 20
}

func (s *petSession) handleConfig(resp go3270.Response) {
	switch resp.AID {
	case go3270.AIDPF5:
	case go3270.AIDEnter:
		s.info("Press PF5 to save the configuration.")
		return
	default:
		s.info("Key %s has no action on configuration.", s.lastAID)
		return
	}

	if !s.isAdmin() {
		s.fail("Changing configuration needs role ADMIN or MANAGER. You are signed on as %s.", s.role)
		return
	}

	name := strings.ToUpper(strings.TrimSpace(resp.Values["gname"]))
	if name == "" {
		s.fail("Store name is required.")
		return
	}
	storeNo := strings.TrimSpace(resp.Values["gno"])
	if _, ok := petParseInt(storeNo, 0, 9999); !ok {
		s.fail("Store number %q must be a number from 0 to 9999.", storeNo)
		return
	}
	currency := strings.ToUpper(strings.TrimSpace(resp.Values["gcur"]))
	if len(currency) != 3 {
		s.fail("Currency %q must be a three letter code such as GBP.", currency)
		return
	}
	vat, ok := petParseMoney(resp.Values["gvat"])
	if !ok || vat < 0 || vat > 4000 {
		s.fail("VAT rate %q must be a percentage from 0.00 to 40.00.", strings.TrimSpace(resp.Values["gvat"]))
		return
	}
	reorder, ok := petParseInt(resp.Values["grord"], 0, 500)
	if !ok {
		s.fail("Reorder cover %q must be a percentage from 0 to 500.", strings.TrimSpace(resp.Values["grord"]))
		return
	}
	loyalty, ok := petParseMoney(resp.Values["gloy"])
	if !ok || loyalty < 0 || loyalty > 5000 {
		s.fail("Loyalty rate %q must be a percentage from 0.00 to 50.00.", strings.TrimSpace(resp.Values["gloy"]))
		return
	}
	credit, ok := petParseInt(resp.Values["gcred"], 0, 365)
	if !ok {
		s.fail("Credit terms %q must be a number of days from 0 to 365.", strings.TrimSpace(resp.Values["gcred"]))
		return
	}
	open := strings.TrimSpace(resp.Values["gopen"])
	closing := strings.TrimSpace(resp.Values["gclose"])
	if !petLooksLikeTime(open) || !petLooksLikeTime(closing) {
		s.fail("Opening and closing times must be HH:MM, for example 08:30.")
		return
	}

	before := s.store.cfg
	s.store.cfg.StoreName = name
	s.store.cfg.StoreNo = storeNo
	s.store.cfg.Region = strings.ToUpper(strings.TrimSpace(resp.Values["gregion"]))
	s.store.cfg.Manager = strings.ToUpper(strings.TrimSpace(resp.Values["gmgr"]))
	s.store.cfg.Currency = currency
	s.store.cfg.VATRate = vat
	s.store.cfg.ReorderPct = reorder
	s.store.cfg.LoyaltyPct = loyalty
	s.store.cfg.CreditDays = credit
	s.store.cfg.OpenTime = open
	s.store.cfg.CloseTime = closing

	s.pending = map[string]string{}
	if before.VATRate != vat {
		s.store.logAudit(s.user, "CONFIG", fmt.Sprintf("VAT RATE CHANGED FROM %s TO %s",
			petPercent(before.VATRate), petPercent(vat)))
	}
	s.store.logAudit(s.user, "CONFIG", "SYSTEM CONFIGURATION AMENDED")
	s.info("Configuration saved. VAT is now %s%% and new invoices will use it.", petPercent(vat))
}

func petLooksLikeTime(text string) bool {
	if len(text) != 5 || text[2] != ':' {
		return false
	}
	if _, ok := petParseInt(text[:2], 0, 23); !ok {
		return false
	}
	if _, ok := petParseInt(text[3:], 0, 59); !ok {
		return false
	}
	return true
}

// User and security maintenance.

func (s *petSession) buildUsers() (go3270.Screen, int, int) {
	top := s.bodyTop()
	s.clampTop(&s.userTop, len(s.store.users))

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: "New user"},
	}
	screen = append(screen, petInputField(top, 9, 8, "newuser", s.value("newuser", ""))...)
	screen = append(screen, go3270.Field{Row: top, Col: 20, Content: "Name"})
	screen = append(screen, petInputField(top, 25, 24, "newname", s.value("newname", ""))...)
	screen = append(screen, go3270.Field{Row: top, Col: 51, Content: "Role"})
	screen = append(screen, petInputField(top, 56, 8, "newrole", s.value("newrole", ""))...)
	screen = append(screen, go3270.Field{Row: top, Col: 67, Color: go3270.Yellow,
		Content: petTrunc("PF5 adds", s.cols-69)})

	screen = append(screen, s.headingFields(top+1, petUserColumns)...)

	rows := s.listRows()
	for i := 0; i < rows; i++ {
		index := s.userTop + i
		if index >= len(s.store.users) {
			break
		}
		u := s.store.users[index]
		row := top + 2 + i
		name := fmt.Sprintf("sel%d", i)
		screen = append(screen, petSelectField(row, name, s.value(name, ""))...)
		screen = append(screen, s.cell(row, petUserColumns[1], u.ID, go3270.White)...)
		screen = append(screen, s.cell(row, petUserColumns[2], u.Name, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petUserColumns[3], u.Role, petRoleColor(u.Role))...)
		screen = append(screen, s.cell(row, petUserColumns[4], u.Status, petStatusColor(u.Status))...)
		screen = append(screen, s.cell(row, petUserColumns[5], u.Last, go3270.DefaultColor)...)
	}

	s.appendListFooter(&screen, len(s.store.users), s.userTop, "users")
	return screen, top, 10
}

func petRoleColor(role string) go3270.Color {
	switch role {
	case "ADMIN":
		return go3270.Red
	case "MANAGER":
		return go3270.Yellow
	case "BATCH":
		return go3270.Turquoise
	}
	return go3270.DefaultColor
}

func (s *petSession) handleUsers(resp go3270.Response) {
	switch resp.AID {
	case go3270.AIDPF7:
		s.scroll(&s.userTop, -1, len(s.store.users))
		return
	case go3270.AIDPF8:
		s.scroll(&s.userTop, 1, len(s.store.users))
		return
	case go3270.AIDPF5:
		s.addUser(resp)
		return
	case go3270.AIDEnter:
	default:
		s.info("Key %s has no action on user maintenance.", s.lastAID)
		return
	}

	action, index, ok := s.lineAction(resp, len(s.store.users), s.userTop)
	if !ok {
		s.info("Type L to lock, U to unlock or D to delete beside a user, or PF5 to add one.")
		return
	}
	u := s.store.users[index]
	if !s.isAdmin() {
		s.fail("Changing users needs role ADMIN or MANAGER. You are signed on as %s.", s.role)
		return
	}
	switch action {
	case "L":
		if u.Status == "LOCKED" {
			s.fail("User %s is already locked.", u.ID)
			return
		}
		u.Status = "LOCKED"
		s.store.logAudit(s.user, "SECURITY", "USER "+u.ID+" LOCKED")
		s.info("User %s locked.", u.ID)
	case "U":
		if u.Status == "ACTIVE" {
			s.fail("User %s is already active.", u.ID)
			return
		}
		u.Status = "ACTIVE"
		s.store.logAudit(s.user, "SECURITY", "USER "+u.ID+" UNLOCKED")
		s.info("User %s unlocked.", u.ID)
	case "D":
		if u.ID == s.user {
			s.fail("You cannot delete the user you are signed on as.")
			return
		}
		if u.Role == "ADMIN" && petCountRole(s.store, "ADMIN") <= 1 {
			s.fail("User %s is the only administrator and cannot be deleted.", u.ID)
			return
		}
		s.store.users = append(s.store.users[:index], s.store.users[index+1:]...)
		s.clampTop(&s.userTop, len(s.store.users))
		s.store.logAudit(s.user, "SECURITY", "USER "+u.ID+" DELETED")
		s.info("User %s deleted.", u.ID)
	default:
		s.fail("%q is not a line action. Use L, U or D.", action)
	}
}

func petCountRole(store *petStore, role string) int {
	count := 0
	for _, u := range store.users {
		if u.Role == role {
			count++
		}
	}
	return count
}

func (s *petSession) addUser(resp go3270.Response) {
	if !s.isAdmin() {
		s.fail("Adding a user needs role ADMIN or MANAGER. You are signed on as %s.", s.role)
		return
	}
	id := strings.ToUpper(strings.TrimSpace(resp.Values["newuser"]))
	if id == "" {
		s.fail("A user id is required.")
		return
	}
	if len(id) > 8 {
		s.fail("User id must be 8 characters or fewer.")
		return
	}
	if s.store.findUser(id) != nil {
		s.fail("User %s already exists.", id)
		return
	}
	role := strings.ToUpper(strings.TrimSpace(resp.Values["newrole"]))
	if role == "" {
		role = "CLERK"
	}
	if !petOneOf(role, "ADMIN", "MANAGER", "CLERK", "BATCH") {
		s.fail("Role must be ADMIN, MANAGER, CLERK or BATCH, not %q.", role)
		return
	}
	name := strings.ToUpper(strings.TrimSpace(resp.Values["newname"]))
	if name == "" {
		name = id
	}

	s.store.users = append(s.store.users, &petUser{
		ID: id, Name: name, Role: role, Status: "ACTIVE", Last: "",
	})
	s.store.logAudit(s.user, "SECURITY", "USER "+id+" CREATED WITH ROLE "+role)
	s.pending = map[string]string{}
	s.info("User %s created with role %s.", id, role)
}

// Housekeeping and batch.

func (s *petSession) buildJobs() (go3270.Screen, int, int) {
	top := s.bodyTop()
	s.clampTop(&s.jobTop, len(s.store.jobs))

	intro := "Type R beside a job and press Enter. PF5 then confirms and runs it."
	if s.jobPending != "" {
		intro = "Press PF5 to confirm running " + s.jobPending + ", or PF3 to abandon it."
	}
	screen := go3270.Screen{
		{Row: top, Col: 0, Color: petIntroColor(s.jobPending), Content: petTrunc(intro, s.cols-2)},
	}
	screen = append(screen, s.headingFields(top+1, petJobColumns)...)

	rows := s.listRows()
	for i := 0; i < rows; i++ {
		index := s.jobTop + i
		if index >= len(s.store.jobs) {
			break
		}
		j := s.store.jobs[index]
		row := top + 2 + i
		name := fmt.Sprintf("sel%d", i)
		last := j.LastRun
		if last == "" {
			last = "-"
		}
		screen = append(screen, petSelectField(row, name, s.value(name, ""))...)
		screen = append(screen, s.cell(row, petJobColumns[1], j.Name, petJobNameColor(j, s.jobPending))...)
		screen = append(screen, s.cell(row, petJobColumns[2], j.Desc, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petJobColumns[3], j.Schedule, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petJobColumns[4], last, go3270.DefaultColor)...)
		screen = append(screen, s.cell(row, petJobColumns[5], j.Status, petStatusColor(j.Status))...)
		screen = append(screen, s.cell(row, petJobColumns[6], j.Elapsed, go3270.DefaultColor)...)
	}

	s.appendListFooter(&screen, len(s.store.jobs), s.jobTop, "jobs")
	return screen, top + 2, 1
}

func petIntroColor(pending string) go3270.Color {
	if pending != "" {
		return go3270.Red
	}
	return go3270.Yellow
}

func petJobNameColor(j *petJob, pending string) go3270.Color {
	if j.Name == pending {
		return go3270.Red
	}
	return go3270.White
}

func (s *petSession) handleJobs(resp go3270.Response) {
	switch resp.AID {
	case go3270.AIDPF7:
		s.scroll(&s.jobTop, -1, len(s.store.jobs))
		return
	case go3270.AIDPF8:
		s.scroll(&s.jobTop, 1, len(s.store.jobs))
		return
	case go3270.AIDPF5:
		s.runPendingJob()
		return
	case go3270.AIDEnter:
	default:
		s.info("Key %s has no action on housekeeping.", s.lastAID)
		return
	}

	action, index, ok := s.lineAction(resp, len(s.store.jobs), s.jobTop)
	if !ok {
		if s.jobPending != "" {
			s.info("%s is waiting for PF5 to confirm.", s.jobPending)
			return
		}
		s.info("Type R beside a job to select it for running.")
		return
	}
	j := s.store.jobs[index]
	switch action {
	case "R", "S", "":
		s.jobPending = j.Name
		s.info("%s selected. Press PF5 to run it now.", j.Name)
	default:
		s.fail("%q is not a line action. Use R to select a job for running.", action)
	}
}

func (s *petSession) runPendingJob() {
	if s.jobPending == "" {
		s.fail("No job is selected. Type R beside one first.")
		return
	}
	job := s.store.findJob(s.jobPending)
	if job == nil {
		s.jobPending = ""
		s.fail("That job no longer exists.")
		return
	}
	if !s.isAdmin() {
		s.fail("Running %s needs role ADMIN or MANAGER. You are signed on as %s.", job.Name, s.role)
		return
	}

	result := s.executeJob(job)
	job.LastRun = petToday()
	job.Status = "OK"
	job.Elapsed = "00:00:0" + strconv.Itoa(1+len(job.Name)%8)
	s.jobPending = ""
	s.store.logAudit(s.user, "JOB", job.Name+" ENDED RC=00 - "+strings.ToUpper(result))
	s.info("%s completed. %s", job.Name, result)
}

// executeJob is what each housekeeping job actually does to the data. They are
// small, but they are real: running the purge really does remove records, and
// the reorder extract really does count what is short.
func (s *petSession) executeJob(job *petJob) string {
	switch job.Name {
	case "EODCLOSE":
		takings := 0
		today := petToday()
		for _, p := range s.store.payments {
			if p.Date == today {
				takings += p.Amount
			}
		}
		picked := 0
		for _, o := range s.store.orders {
			if o.Status == "OPEN" {
				o.Status = "PICKED"
				picked++
			}
		}
		return fmt.Sprintf("Takings today %s %s, %d orders moved to PICKED.",
			s.store.cfg.Currency, petMoney(takings), picked)
	case "STKREORD":
		low := s.store.belowReorder()
		units := 0
		for _, item := range low {
			units += petSuggestedOrder(item)
		}
		return fmt.Sprintf("%d proposals raised for %d units.", len(low), units)
	case "INVPRINT":
		count := 0
		for _, inv := range s.store.invoices {
			if inv.Status == "OPEN" || inv.Status == "PART" {
				count++
			}
		}
		return fmt.Sprintf("%d invoices queued for print and despatch.", count)
	case "CUSTPURG":
		kept := s.store.customers[:0]
		purged := 0
		for _, c := range s.store.customers {
			if c.Status == "CLOSED" && s.store.customerBalance(c.ID) == 0 {
				purged++
				continue
			}
			kept = append(kept, c)
		}
		s.store.customers = kept
		s.clampTop(&s.custTop, len(s.store.customers))
		if s.cust != nil && s.store.findCustomer(s.cust.ID) == nil {
			s.cust = nil
		}
		return fmt.Sprintf("%d closed accounts purged, %d remain.", purged, len(s.store.customers))
	case "DBREORG":
		return fmt.Sprintf("Indexes rebuilt for %d accounts and %d stock lines.",
			len(s.store.customers), len(s.store.stock))
	case "VATRETN":
		net, vat := 0, 0
		for _, inv := range s.store.invoices {
			if inv.Status == "VOID" {
				continue
			}
			net += inv.Net
			vat += inv.VAT
		}
		return fmt.Sprintf("Net %s, VAT %s extracted for the return.", petMoney(net), petMoney(vat))
	case "LOYALTY":
		changed := 0
		for _, c := range s.store.customers {
			spend := 0
			for _, inv := range s.store.invoicesFor(c.ID) {
				if inv.Status != "VOID" {
					spend += inv.Net
				}
			}
			tier := "BRONZE"
			switch {
			case spend >= 100000:
				tier = "GOLD"
			case spend >= 25000:
				tier = "SILVER"
			}
			if c.Tier != tier {
				c.Tier = tier
				changed++
			}
		}
		return fmt.Sprintf("%d accounts moved tier.", changed)
	case "ARCHLOG":
		archived := len(s.store.audit)
		if archived > 20 {
			s.store.audit = s.store.audit[:20]
		}
		s.clampTop(&s.auditTop, len(s.store.audit))
		return fmt.Sprintf("%d entries archived, %d retained.", archived, len(s.store.audit))
	}
	return "Nothing to do."
}

// Audit log.

func (s *petSession) buildAudit() (go3270.Screen, int, int) {
	top := s.bodyTop()
	s.clampTop(&s.auditTop, len(s.store.audit))

	detailWidth := s.cols - 43
	if detailWidth < 10 {
		detailWidth = 10
	}
	columns := []petColumn{
		{attr: 0, head: "WHEN", width: 19},
		{attr: 21, head: "USER", width: 8},
		{attr: 31, head: "ACTION", width: 8},
		{attr: 41, head: "DETAIL", width: detailWidth},
	}

	screen := go3270.Screen{
		{Row: top, Col: 0, Content: petTrunc(fmt.Sprintf(
			"%d entries, newest first. Everything that changes data writes here.",
			len(s.store.audit)), s.cols-2)},
	}
	screen = append(screen, s.headingFields(top+1, columns)...)

	rows := s.listRows()
	for i := 0; i < rows; i++ {
		index := s.auditTop + i
		if index >= len(s.store.audit) {
			break
		}
		entry := s.store.audit[index]
		row := top + 2 + i
		screen = append(screen, s.cell(row, columns[0], entry.Stamp, go3270.Turquoise)...)
		screen = append(screen, s.cell(row, columns[1], entry.User, go3270.White)...)
		screen = append(screen, s.cell(row, columns[2], entry.Action, petAuditColor(entry.Action))...)
		screen = append(screen, s.cell(row, columns[3], entry.Detail, go3270.DefaultColor)...)
	}

	s.appendListFooter(&screen, len(s.store.audit), s.auditTop, "audit entries")
	return screen, -1, -1
}

func petAuditColor(action string) go3270.Color {
	switch action {
	case "SECURITY":
		return go3270.Red
	case "CONFIG", "JOB":
		return go3270.Yellow
	case "PAYMENT", "INVOICE":
		return go3270.Green
	}
	return go3270.Turquoise
}
