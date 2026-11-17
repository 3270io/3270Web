// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

// Screen mining.
//
// Most 3270 screens say what they accept: a numbered menu lists its options,
// a command reference lists its commands, a prompt ends in "(Y/N)", a hint
// line reads "try CUST C0001". Typing those is worth far more than typing
// forty random characters, so before generating anything the engine reads
// the screen for candidate values and works out which input field each one
// belongs in.

const (
	// maxCandidatesPerField bounds the mined candidates kept for one field.
	maxCandidatesPerField = 16
	// maxCandidateFieldsPerArea bounds how many fields of one area carry
	// mined candidates.
	maxCandidateFieldsPerArea = 6
	// maxIdentifierPool bounds the run-wide pool of identifier-looking tokens
	// (account numbers, item codes) seen on any screen.
	maxIdentifierPool = 48
	// maxCommandPool bounds the run-wide pool of command words seen on any
	// screen.
	maxCommandPool = 48
)

// minedField describes one input field as the miner sees it.
type minedField struct {
	key    string
	row    int // 0-based
	col    int // 0-based
	length int
	label  string // normalized text to the left of the field
	field  *host.Field
}

// screenMining is what one screen offered.
type screenMining struct {
	// candidates maps a field key to the values worth trying in it, in the
	// order they should be tried.
	candidates map[string][]string
	// optionField is the key of the field that takes a menu option, if the
	// screen has a menu and such a field.
	optionField string
	// commandField is the key of the screen's command line, if it has one.
	commandField string
	// commands and identifiers are everything of that kind seen on the
	// screen, whether or not it found a field here.
	commands    []string
	identifiers []string
}

var (
	menuDigitRowRe  = regexp.MustCompile(`^\s{0,20}(\d{1,2})\s*[.)\-=:]?\s+(\S.*)$`)
	menuLetterRowRe = regexp.MustCompile(`^\s{0,20}([A-Z])(?:\s*[.)\-=:]\s*|\s{2,})(\S.*)$`)
	trailingWordRe  = regexp.MustCompile(`\s{2,}([A-Z][A-Z0-9]{1,7})\s*$`)
	exampleLeadRe   = regexp.MustCompile(`(?i)(?:\be\.g\.|\beg\b|\bfor example\b|\bsuch as\b|\btry\b|\btype\b|\blike\b)[:,]?\s+`)
	parenChoiceRe   = regexp.MustCompile(`\(\s*([A-Za-z0-9]{1,8}(?:\s*(?:/|,|\||\bor\b)\s*[A-Za-z0-9]{1,8}){1,6})\s*\)`)
	bareYesNoRe     = regexp.MustCompile(`\bY\s*/\s*N\b`)
	choiceSplitRe   = regexp.MustCompile(`\s*(?:/|,|\||\bor\b)\s*`)
	codeLegendRe    = regexp.MustCompile(`^([A-Z0-9])=(\S.*)$`)
	identifierRe    = regexp.MustCompile(`^[A-Z]{1,4}-?[0-9]{2,}$`)
	upperValueRe    = regexp.MustCompile(`^[A-Z0-9?=][A-Z0-9/.\-=]*$`)
	columnSplitRe   = regexp.MustCompile(`\s{2,}`)
	headerWordRe    = regexp.MustCompile(`\b(?:COMMAND|COMMANDS|CMD|TRANSACTION|TRAN)\b`)
	commandTokenRe  = regexp.MustCompile(`^([A-Z?=][A-Z0-9]{0,7})(?:\s{2,}|\s*$)`)
	screenDateRe    = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2}|\d{2}/\d{2}/\d{4}|\d{2}/\d{2}/\d{2}|\d{2}\.\d{2}\.\d{4})\b`)
)

// protectedRowTexts returns each row of the screen as text with every
// unprotected cell blanked, so values typed into input fields are never read
// back as if the application had offered them.
func protectedRowTexts(screen *host.Screen) []string {
	width, height := screenDimensions(screen)
	if width <= 0 || height <= 0 {
		return nil
	}
	masked := make([]bool, width*height)
	for _, f := range screen.Fields {
		if f == nil || f.IsProtected() {
			continue
		}
		walkFieldCells(f, width, height, func(x, y int) { masked[y*width+x] = true })
	}
	rows := make([]string, height)
	buf := make([]rune, width)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			ch := screen.CharAt(x, y)
			if ch == 0 || masked[y*width+x] {
				ch = ' '
			}
			buf[x] = ch
		}
		rows[y] = string(buf)
	}
	return rows
}

// walkFieldCells calls fn for every cell of f that lies on the screen.
func walkFieldCells(f *host.Field, width, height int, fn func(x, y int)) {
	curX, curY := f.StartX, f.StartY
	for {
		if curX >= 0 && curX < width && curY >= 0 && curY < height {
			fn(curX, curY)
		}
		if curX == f.EndX && curY == f.EndY {
			return
		}
		curX++
		if curX >= width {
			curX = 0
			curY++
			if curY >= height {
				return
			}
		}
	}
}

// fieldLabelFromRow returns the normalized text immediately to the left of a
// field: the last run of words before it on its row, with leader dots and
// prompt arrows removed.
func fieldLabelFromRow(row string, col int) string {
	runes := []rune(row)
	if col > len(runes) {
		col = len(runes)
	}
	if col <= 0 {
		return ""
	}
	left := strings.TrimRight(string(runes[:col]), " ")
	if left == "" {
		return ""
	}
	// Keep only the last column of text: labels to the far left of a wide
	// gap belong to something else.
	if parts := columnSplitRe.Split(left, -1); len(parts) > 0 {
		last := parts[len(parts)-1]
		// "Option  ===>" splits into "Option" and "===>": keep the word too.
		if strings.Trim(last, " .:=>-") == "" && len(parts) > 1 {
			last = parts[len(parts)-2] + " " + last
		}
		left = last
	}
	return normalizeHintAssignmentLabel(left)
}

func labelHasPromptArrow(label string) bool {
	return strings.Contains(label, "==>")
}

func labelWords(label string) string {
	return strings.TrimSpace(strings.Trim(strings.NewReplacer(".", " ", ":", " ", "=", " ", ">", " ").Replace(label), " "))
}

func labelIsOptionPrompt(label string) bool {
	return containsAnyLabelWord(normalizeHintAssignmentLabel(labelWords(label)),
		[]string{"OPTION", "SELECTION", "SELECT", "CHOICE", "CHOOSE"})
}

func labelIsCommandPrompt(label string) bool {
	words := normalizeHintAssignmentLabel(labelWords(label))
	if containsAnyLabelWord(words, []string{"COMMAND", "CMD"}) {
		return true
	}
	return labelHasPromptArrow(label) && !labelIsOptionPrompt(label)
}

// mineScreen reads a screen for candidate input values.
func mineScreen(screen *host.Screen) screenMining {
	out := screenMining{candidates: make(map[string][]string)}
	if screen == nil {
		return out
	}
	rows := protectedRowTexts(screen)
	if len(rows) == 0 {
		return out
	}

	var fields []minedField
	for _, f := range unprotectedFields(screen) {
		if f == nil {
			continue
		}
		length := fieldLength(f)
		if length <= 0 {
			continue
		}
		label := ""
		if f.StartY >= 0 && f.StartY < len(rows) {
			label = fieldLabelFromRow(rows[f.StartY], f.StartX)
		}
		fields = append(fields, minedField{
			key:    mindMapFieldKey(f.StartY+1, f.StartX+1, length),
			row:    f.StartY,
			col:    f.StartX,
			length: length,
			label:  label,
			field:  f,
		})
	}
	sort.SliceStable(fields, func(i, j int) bool {
		if fields[i].row != fields[j].row {
			return fields[i].row < fields[j].row
		}
		return fields[i].col < fields[j].col
	})

	legendStart := len(rows) - legendRowCount
	if legendStart < 0 {
		legendStart = 0
	}

	// --- what the screen offers ---------------------------------------
	var options []string     // menu option numbers / letters
	var commands []string    // command words
	var examples []string    // values introduced by "e.g." / "try" / "such as"
	var selectCodes []string // one-character action codes ("S=Open")
	type rowChoice struct {
		row    int
		col    int
		values []string
	}
	var choices []rowChoice

	optionSeen := map[string]bool{}
	var optionRows []string
	for y, row := range rows {
		trimmed := strings.TrimRight(row, " ")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		inLegend := y >= legendStart

		if !inLegend {
			opt, rest := "", ""
			if m := menuDigitRowRe.FindStringSubmatch(trimmed); m != nil && strings.ContainsAny(m[2], "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz") {
				opt, rest = m[1], m[2]
			} else if m := menuLetterRowRe.FindStringSubmatch(trimmed); m != nil {
				opt, rest = m[1], m[2]
			}
			if opt != "" && !optionSeen[opt] {
				optionSeen[opt] = true
				optionRows = append(optionRows, opt)
				// An option that signs off or destroys something is listed
				// but never offered as a candidate.
				if !labelIsDenied(rest) {
					options = append(options, opt)
					if m := trailingWordRe.FindStringSubmatch(rest); m != nil && len(strings.Fields(rest)) >= 2 {
						commands = appendCandidate(commands, m[1])
					}
				}
			}

			// One-character action codes, laid out in columns.
			for _, seg := range columnSplitRe.Split(strings.TrimSpace(trimmed), -1) {
				if m := codeLegendRe.FindStringSubmatch(seg); m != nil && !labelIsDenied(m[2]) &&
					!containsAnyLabelWord(normalizeHintAssignmentLabel(m[2]), []string{"CLOSE", "VOID"}) {
					selectCodes = appendCandidate(selectCodes, m[1])
				}
			}
		}

		// Values introduced by "e.g.", "try", "such as", "type".
		for _, loc := range exampleLeadRe.FindAllStringIndex(trimmed, -1) {
			for _, v := range parseExampleValues(trimmed[loc[1]:]) {
				examples = appendCandidate(examples, v)
			}
		}

		// Bracketed alternatives: "(Y/N)", "(A, B or C)".
		for _, loc := range parenChoiceRe.FindAllStringSubmatchIndex(trimmed, -1) {
			values := splitChoiceValues(trimmed[loc[2]:loc[3]])
			if len(values) >= 2 {
				choices = append(choices, rowChoice{row: y, col: len([]rune(trimmed[:loc[0]])), values: values})
			}
		}
		if len(choices) == 0 || choices[len(choices)-1].row != y {
			if loc := bareYesNoRe.FindStringIndex(trimmed); loc != nil {
				choices = append(choices, rowChoice{row: y, col: len([]rune(trimmed[:loc[0]])), values: []string{"Y", "N"}})
			}
		}

		// Identifier-looking tokens (account numbers, item codes).
		for _, tok := range strings.Fields(trimmed) {
			tok = strings.Trim(tok, ".,;:()")
			if identifierRe.MatchString(tok) {
				out.identifiers = appendCandidate(out.identifiers, tok)
			}
		}
	}
	// One row that happens to start with a digit is not a menu.
	if len(optionRows) < 2 {
		options = nil
	}

	// Command reference tables: a heading row carrying the word COMMAND,
	// with the commands themselves starting in the same column underneath.
	for y, row := range rows {
		if y >= legendStart {
			break
		}
		locs := headerWordRe.FindAllStringIndex(row, -1)
		if len(locs) == 0 || strings.ContainsAny(row, ".?") {
			continue
		}
		for _, loc := range locs {
			col := len([]rune(row[:loc[0]]))
			for yy := y + 1; yy < legendStart; yy++ {
				r := []rune(rows[yy])
				if col >= len(r) || strings.TrimSpace(rows[yy]) == "" {
					break
				}
				if col > 0 && r[col-1] != ' ' {
					break
				}
				m := commandTokenRe.FindStringSubmatch(string(r[col:]))
				if m == nil {
					break
				}
				commands = appendCandidate(commands, m[1])
			}
		}
	}

	// Examples that are command-shaped join the command list.
	for _, v := range examples {
		if len([]rune(v)) >= 2 {
			commands = appendCandidate(commands, v)
		} else {
			selectCodes = appendCandidate(selectCodes, v)
		}
	}
	commands = filterDeniedTypedValues(commands)
	out.commands = commands

	// --- which field takes what ---------------------------------------
	var optionField, commandField *minedField
	var selectField *minedField
	for i := range fields {
		f := &fields[i]
		if f.field.IsHidden() {
			continue
		}
		switch {
		case optionField == nil && labelIsOptionPrompt(f.label):
			optionField = f
		case commandField == nil && f.length >= 4 && labelIsCommandPrompt(f.label):
			commandField = f
		case selectField == nil && f.length == 1:
			selectField = f
		}
	}
	if len(options) > 0 && optionField == nil {
		// No field labelled as the option prompt: a short numeric field is
		// the next best guess, then the command line (many applications take
		// the option number there), then a lone input field.
		for i := range fields {
			f := &fields[i]
			if !f.field.IsHidden() && f.field.IsNumeric() && f.length <= 3 {
				optionField = f
				break
			}
		}
		if optionField == nil && commandField != nil {
			optionField = commandField
		}
		if optionField == nil && len(fields) == 1 && !fields[0].field.IsHidden() {
			optionField = &fields[0]
		}
	}
	if commandField == nil && len(commands) > 0 {
		// A screen that lists commands but labels no command line: use the
		// widest text field that is not the option field.
		for i := range fields {
			f := &fields[i]
			if f.field.IsHidden() || f.field.IsNumeric() || f == optionField || f.length < 4 {
				continue
			}
			if commandField == nil || f.length > commandField.length {
				commandField = f
			}
		}
	}

	add := func(f *minedField, values []string) {
		if f == nil || len(values) == 0 {
			return
		}
		if _, exists := out.candidates[f.key]; !exists && len(out.candidates) >= maxCandidateFieldsPerArea {
			return
		}
		list := out.candidates[f.key]
		for _, v := range values {
			if len(list) >= maxCandidatesPerField {
				break
			}
			if len([]rune(v)) > f.length {
				continue
			}
			if f.field.IsNumeric() && !isDigitsOnlyString(v) {
				continue
			}
			list = appendCandidate(list, v)
		}
		if len(list) > 0 {
			out.candidates[f.key] = list
		}
	}

	if optionField != nil {
		out.optionField = optionField.key
		add(optionField, options)
	}
	if commandField != nil {
		out.commandField = commandField.key
		add(commandField, commands)
	}
	if selectField != nil {
		add(selectField, selectCodes)
	}
	for _, c := range choices {
		// A bracketed choice belongs to the nearest field before it on the
		// same row, else the nearest field after it.
		var target *minedField
		for i := range fields {
			f := &fields[i]
			if f.row != c.row || f.field.IsHidden() {
				continue
			}
			if f.col <= c.col {
				target = f
			} else if target == nil {
				target = f
			}
		}
		add(target, c.values)
	}

	// Label-driven values for fields nothing else claimed.
	dateFormat := ""
	for _, row := range rows {
		if m := screenDateRe.FindString(row); m != "" {
			dateFormat = m
			break
		}
	}
	for i := range fields {
		f := &fields[i]
		if f.field.IsHidden() || f == optionField || f == commandField {
			continue
		}
		if _, claimed := out.candidates[f.key]; claimed {
			continue
		}
		add(f, labelDrivenValues(f.label, f.length, dateFormat, out.identifiers))
	}
	return out
}

// labelDrivenValues suggests values from what a field's label says it holds.
func labelDrivenValues(label string, length int, dateSample string, identifiers []string) []string {
	words := normalizeHintAssignmentLabel(labelWords(label))
	if words == "" {
		return nil
	}
	switch {
	case containsAnyLabelWord(words, []string{"DATE", "DOB", "BIRTH", "FROM", "UNTIL"}):
		return dateValuesForLength(length, dateSample)
	case containsAnyLabelWord(words, []string{"QTY", "QUANTITY", "AMOUNT", "AMT", "COUNT", "UNITS", "PRICE", "TOTAL"}):
		return []string{"1", "2", "10"}
	case containsAnyLabelWord(words, []string{
		"ID", "NO", "NUM", "NUMBER", "CODE", "ACCOUNT", "ACCT", "CUSTOMER", "ITEM",
		"SKU", "ORDER", "INVOICE", "REF", "REFERENCE", "KEY", "PART", "PRODUCT",
	}):
		var out []string
		for _, id := range identifiers {
			if len([]rune(id)) <= length {
				out = append(out, id)
			}
			if len(out) >= 4 {
				break
			}
		}
		return out
	}
	return nil
}

// dateValuesForLength returns today's date in the formats that fit a field
// of the given length, leading with the format the screen itself displays.
func dateValuesForLength(length int, sample string) []string {
	now := time.Now()
	var layouts []string
	switch {
	case len(sample) == 10 && sample[4] == '-':
		layouts = append(layouts, "2006-01-02")
	case len(sample) == 10 && sample[2] == '/':
		layouts = append(layouts, "02/01/2006")
	case len(sample) == 10 && sample[2] == '.':
		layouts = append(layouts, "02.01.2006")
	case len(sample) == 8 && sample[2] == '/':
		layouts = append(layouts, "02/01/06")
	}
	layouts = append(layouts, "2006-01-02", "20060102", "02/01/2006", "060102", "02/01/06")
	var out []string
	for _, layout := range layouts {
		v := now.Format(layout)
		if len(v) <= length {
			out = appendCandidate(out, v)
		}
		if len(out) >= 2 {
			break
		}
	}
	return out
}

// parseExampleValues reads the values that follow "e.g." / "try" / "such as"
// in a line of text. Values are runs of upper-case tokens; "or", "and" and
// commas separate alternatives; anything else ends the list.
func parseExampleValues(tail string) []string {
	var out []string
	var current []string
	flush := func() {
		if len(current) > 0 {
			out = append(out, strings.Join(current, " "))
			current = nil
		}
	}
	for _, raw := range strings.Fields(tail) {
		endsValue := strings.HasSuffix(raw, ",") || strings.HasSuffix(raw, ".") || strings.HasSuffix(raw, ";")
		tok := strings.Trim(raw, ",.;:'\"()")
		lower := strings.ToLower(tok)
		if lower == "or" || lower == "and" {
			flush()
			continue
		}
		if tok == "" || !upperValueRe.MatchString(tok) || len(tok) > 12 {
			break
		}
		current = append(current, tok)
		if endsValue || len(current) >= 3 {
			flush()
			if strings.HasSuffix(raw, ".") {
				break
			}
		}
		if len(out) >= 4 {
			break
		}
	}
	flush()
	return out
}

func splitChoiceValues(text string) []string {
	var out []string
	for _, part := range choiceSplitRe.Split(text, -1) {
		v := strings.ToUpper(strings.TrimSpace(part))
		if v == "" {
			continue
		}
		out = appendCandidate(out, v)
	}
	return out
}

func appendCandidate(list []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return list
	}
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

func filterDeniedTypedValues(values []string) []string {
	out := values[:0:0]
	for _, v := range values {
		if isDeniedTypedValue(v) {
			continue
		}
		out = append(out, v)
	}
	return out
}

// mergeCandidateLists appends the values of add that base lacks, keeping
// base's order and the per-field bound.
func mergeCandidateLists(base, add []string) []string {
	for _, v := range add {
		if len(base) >= maxCandidatesPerField {
			break
		}
		base = appendCandidate(base, v)
	}
	return base
}

// appendBoundedPool appends the values a pool lacks, dropping the oldest
// entries once it is over max.
func appendBoundedPool(pool, add []string, max int) []string {
	for _, v := range add {
		pool = appendCandidate(pool, v)
	}
	if max > 0 && len(pool) > max {
		pool = append([]string(nil), pool[len(pool)-max:]...)
	}
	return pool
}
