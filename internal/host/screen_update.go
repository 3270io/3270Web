// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	statusPattern = regexp.MustCompile(
		`^[ULE] [FU] [PU] (?:C\([^)]*\)|N) [ILCN] [2-5] [0-9]+ [0-9]+ ([0-9]+) ([0-9]+) 0x0 (?:[0-9.]+|-)`,
	)
)

const (
	attrKeyStartField      = "c0" // 3270 Start Field attribute
	attrKeyExtHighlight    = "41" // Extended Highlight attribute
	attrKeyForegroundColor = "42" // Foreground Color attribute
	attrKeyBackgroundColor = "45" // Background Color attribute
)

// isOrderToken reports whether a token from the screen read is one of the
// terminal's order notations rather than a bare character. SF and SFE open a
// field and occupy the position they are written at; GE names a character from
// the graphic-escape set and occupies one too; SA sets the attributes of the
// characters that follow and occupies none.
func isOrderToken(token string) bool {
	return strings.HasPrefix(token, "SF(") ||
		strings.HasPrefix(token, "SFE(") ||
		strings.HasPrefix(token, "SA(") ||
		strings.HasPrefix(token, "GE(")
}

// occupiesPosition reports whether a token stands in a cell of the buffer.
func occupiesPosition(token string) bool {
	return !strings.HasPrefix(token, "SA(")
}

// countPositions returns how many cells of the buffer a run of tokens covers.
func countPositions(tokens []string) int {
	n := 0
	for _, token := range tokens {
		if occupiesPosition(token) {
			n++
		}
	}
	return n
}

func extractTokens(line string) []string {
	// Pre-allocate tokens with a heuristic (e.g. 1 token per 3 chars) to reduce re-allocations.
	tokens := make([]string, 0, len(line)/3)

	start := -1
	for i := 0; i < len(line); i++ {
		c := line[i]
		// Check for whitespace (space or tab, common in s3270 output)
		if c == ' ' || c == '\t' {
			if start != -1 {
				token := line[start:i]
				if isOrderToken(token) || isCharacterToken(token) {
					tokens = append(tokens, token)
				} else {
					// Replace invalid/unknown tokens with null byte (space) to preserve screen alignment
					tokens = append(tokens, "00")
				}
				start = -1
			}
		} else {
			if start == -1 {
				start = i
			}
		}
	}
	if start != -1 {
		token := line[start:]
		if isOrderToken(token) || isCharacterToken(token) {
			tokens = append(tokens, token)
		} else {
			// Replace invalid/unknown tokens with null byte (space) to preserve screen alignment
			tokens = append(tokens, "00")
		}
	}
	return tokens
}

// isCharacterToken reports whether a token from the screen read is one screen
// position's character.
//
// One position is not one byte. The terminal writes the screen in the local
// codeset, and in UTF-8 — which is what the subprocess is given, see locale.go
// — a character outside ASCII is several bytes, written as one unbroken run of
// hex: "c3a9" for é, "e282ac" for €. Every such run has an even number of
// digits, so the rule is a whole number of bytes rather than exactly one.
//
// Accepting only two digits, which is what this used to do, meant every one of
// those characters failed the test and was replaced with a null to keep the
// columns lined up. The alignment was right and the screen was wrong: a UK
// screen lost its pound signs, a German one its umlauts, and a Cyrillic or
// Greek one very nearly everything, all of them silently and none of them
// recoverable further down.
func isCharacterToken(token string) bool {
	return len(token) >= 2 && len(token)%2 == 0 && isHex(token)
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		isDigit := c >= '0' && c <= '9'
		isHexUpper := c >= 'A' && c <= 'F'
		isHexLower := c >= 'a' && c <= 'f'
		if !isDigit && !isHexUpper && !isHexLower {
			return false
		}
	}
	return true
}

type decodeState struct {
	fieldStartX    int
	fieldStartY    int
	fieldStartCode byte
	color          int
	extHighlight   int
	background     int
	width          int

	// cell holds the character attributes an SA order last set. They apply
	// from that point on — across the end of a row and across the start of the
	// next field — until another SA changes them, which is why this lives on
	// the decode as a whole rather than being reset per line.
	cell CellAttr
	// sawCell records whether any SA order set anything at all, so a screen
	// that has none keeps its attribute grid unallocated.
	sawCell bool
}

// NewScreenFromDump parses an s3270 dump file (data lines + status + ok).
func NewScreenFromDump(r io.Reader) (*Screen, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var lines []string
	var status string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			lines = append(lines, line)
			continue
		}
		if statusPattern.MatchString(line) {
			status = line
			continue
		}
		if strings.TrimSpace(line) == "ok" {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	s := &Screen{IsFormatted: true}
	if err := s.Update(status, lines); err != nil {
		return nil, err
	}
	return s, nil
}

// Update refreshes the screen using a status line and buffer data lines.
func (s *Screen) Update(status string, lines []string) error {
	s.Status = status

	if status == "" {
		s.IsFormatted = true
	} else if len(status) >= 3 && status[2] == 'F' {
		s.IsFormatted = true
	} else {
		s.IsFormatted = false
	}

	var tokenRows [][]string
	var enforcedRows, enforcedCols int
	if rows, cols, ok := screenDimensionsFromStatus(status); ok {
		tokenRows = normalizeScreenTokens(lines, rows, cols)
		enforcedRows = rows
		enforcedCols = cols
	} else {
		tokenRows = make([][]string, len(lines))
		for i, line := range lines {
			if strings.HasPrefix(line, "data:") {
				line = strings.TrimSpace(line[len("data:"):])
			}
			tokenRows[i] = extractTokens(line)
		}
	}

	if err := s.updateBuffer(tokenRows, enforcedRows, enforcedCols); err != nil {
		return err
	}

	for _, f := range s.Fields {
		f.Focused = false
	}

	if status != "" {
		if match := statusPattern.FindStringSubmatch(status); len(match) == 3 {
			row, _ := strconv.Atoi(match[1])
			col, _ := strconv.Atoi(match[2])
			s.CursorY = row
			s.CursorX = col
			if f := s.GetInputFieldAt(s.CursorX, s.CursorY); f != nil {
				f.Focused = true
			}
		} else {
			s.CursorX = 0
			s.CursorY = 0
		}
	}

	return nil
}

// getModelDimensions returns the standard dimensions for IBM 3270 terminal models.
// Returns (rows, cols, true) if the model is recognized, (0, 0, false) otherwise.
func getModelDimensions(model string) (int, int, bool) {
	// Handle both short form (2, 3, 4, 5) and long form (3279-2, 3279-2-E, etc.)
	// Extract the model number from formats like "3279-4-E" -> "4"
	modelNum := model
	if strings.Contains(model, "-") {
		parts := strings.Split(model, "-")
		if len(parts) >= 2 {
			modelNum = parts[1]
		}
	}

	switch modelNum {
	case "2":
		return 24, 80, true
	case "3":
		return 32, 80, true
	case "4":
		return 43, 80, true
	case "5":
		return 27, 132, true
	default:
		return 0, 0, false
	}
}

// ModelDimensions exposes the standard 3270 model dimensions for external callers.
func ModelDimensions(model string) (int, int, bool) {
	return getModelDimensions(model)
}

func screenDimensionsFromStatus(status string) (int, int, bool) {
	if status == "" {
		return 0, 0, false
	}
	parts := strings.Fields(status)
	if len(parts) <= statusIdxCols {
		return 0, 0, false
	}

	// Extract reported dimensions from s3270 status
	rows, err := strconv.Atoi(parts[statusIdxRows])
	if err != nil || rows <= 0 {
		return 0, 0, false
	}
	cols, err := strconv.Atoi(parts[statusIdxCols])
	if err != nil || cols <= 0 {
		return 0, 0, false
	}

	// The terminal reports the size of the display it is *currently* showing,
	// which is not always the model's own size. A display configured larger
	// than its model — the oversize setting, which this application offers —
	// switches to that larger size the moment an application writes to the
	// alternate screen, and from then on the terminal reports the larger
	// figures with the model number unchanged.
	//
	// Cutting those figures back to the model's would be cropping the screen
	// the host actually drew: an operator running a 30x100 display would lose
	// six rows and twenty columns of it, silently, with the setting that asked
	// for them still switched on. So the terminal's own answer stands, bounded
	// only against a figure no 3270 display can have — the buffer is addressed
	// with fourteen bits, so a screen larger than that is a misread status line
	// rather than a large display, and the model's size is the better guess.
	if rows*cols > maxBufferPositions {
		if len(parts) > statusIdxModel {
			if modelRows, modelCols, ok := getModelDimensions(parts[statusIdxModel]); ok {
				return modelRows, modelCols, true
			}
		}
		return 0, 0, false
	}

	return rows, cols, true
}

// maxBufferPositions is the largest display a 3270 buffer address can reach:
// fourteen bits of address, and every position of the screen has one.
const maxBufferPositions = 16384

func normalizeScreenTokens(lines []string, rows, cols int) [][]string {
	// Fallback to processing lines as-is if we can't normalize
	fallback := func() [][]string {
		out := make([][]string, len(lines))
		for i, line := range lines {
			if strings.HasPrefix(line, "data:") {
				line = strings.TrimSpace(line[len("data:"):])
			}
			out[i] = extractTokens(line)
		}
		return out
	}

	if rows <= 0 || cols <= 0 || len(lines) != 1 {
		return fallback()
	}

	line := strings.TrimSpace(lines[0])
	if strings.HasPrefix(line, "data:") {
		line = strings.TrimSpace(line[len("data:"):])
	}
	tokens := extractTokens(line)

	// Rows are counted in buffer positions rather than in tokens, because an
	// SA order is a token that stands in no cell. Counting it as one puts the
	// row break a column early, and every row after the first coloured run
	// wraps in the wrong place.
	positions := countPositions(tokens)
	if positions < cols || positions%cols != 0 {
		return fallback()
	}
	totalRows := positions / cols
	if totalRows < rows {
		return fallback()
	}

	all := splitTokenRows(tokens, totalRows, cols)
	if len(all) < rows {
		return fallback()
	}
	if totalRows == rows {
		return all
	}
	if totalRows%rows != 0 {
		return fallback()
	}
	if !repeatsScreen(all, rows) {
		return fallback()
	}
	return all[:rows]
}

// splitTokenRows cuts a flat token run into rows of cols buffer positions.
// Tokens that occupy no position travel with the row whose characters they
// describe, which is the row that follows them.
func splitTokenRows(tokens []string, rows, cols int) [][]string {
	if rows <= 0 || cols <= 0 {
		return nil
	}
	out := make([][]string, 0, rows)
	row := make([]string, 0, cols+8)
	filled := 0
	for _, token := range tokens {
		row = append(row, token)
		if !occupiesPosition(token) {
			continue
		}
		filled++
		if filled < cols {
			continue
		}
		out = append(out, row)
		if len(out) == rows {
			return out
		}
		row = make([]string, 0, cols+8)
		filled = 0
	}
	return out
}

func normalizeScreenLinesForTest(lines []string, rows, cols int) []string {
	tokenRows := normalizeScreenTokens(lines, rows, cols)
	out := make([]string, len(tokenRows))
	for i, tokens := range tokenRows {
		out[i] = "data: " + strings.Join(tokens, " ")
	}
	return out
}

func repeatsScreenForTest(tokens []string, rows, cols, totalRows int) bool {
	return repeatsScreen(splitTokenRows(tokens, totalRows, cols), rows)
}

// repeatsScreen reports whether a buffer taller than the display is the same
// screen written over and over, which is what a terminal returns when the read
// covers more than one partition's worth of rows.
func repeatsScreen(all [][]string, rows int) bool {
	if rows <= 0 || len(all) <= rows {
		return false
	}
	blocks := len(all) / rows
	for block := 1; block < blocks; block++ {
		for i := 0; i < rows; i++ {
			a, b := all[i], all[block*rows+i]
			if len(a) != len(b) {
				return false
			}
			for j := range a {
				if a[j] != b[j] {
					return false
				}
			}
		}
	}
	return true
}

func (s *Screen) updateBuffer(tokenRows [][]string, enforcedRows, enforcedCols int) error {
	decodedHeight := len(tokenRows)
	if decodedHeight == 0 {
		if enforcedRows > 0 {
			s.Height = enforcedRows
		} else {
			s.Height = 0
		}
		if enforcedCols > 0 {
			s.Width = enforcedCols
		} else {
			s.Width = 0
		}
		if s.Height > 0 && s.Width > 0 {
			s.Buffer = make([][]rune, s.Height)
			for y := 0; y < s.Height; y++ {
				s.Buffer[y] = make([]rune, s.Width)
			}
		} else {
			s.Buffer = nil
		}
		s.Fields = nil
		s.CellAttrs = nil
		return nil
	}

	s.Height = decodedHeight
	s.Buffer = make([][]rune, decodedHeight)
	s.Fields = nil
	s.CellAttrs = nil

	state := &decodeState{
		fieldStartX:    0,
		fieldStartY:    0,
		fieldStartCode: 0xe0,
		color:          AttrColDefault,
		extHighlight:   AttrEhDefault,
		background:     AttrColDefault,
		width:          s.Width,
	}

	width := 0
	rowAttrs := make([][]CellAttr, decodedHeight)
	for y, tokens := range tokenRows {
		state.width = width
		row, attrs, err := decodeLineTokens(tokens, y, s.IsFormatted, s, state)
		if err != nil {
			return err
		}
		if len(row) > width {
			width = len(row)
		}
		s.Buffer[y] = row
		rowAttrs[y] = attrs
	}
	if state.sawCell {
		s.CellAttrs = rowAttrs
	}
	// Preserve the terminal dimensions reported by status/model when available.
	switch {
	case enforcedCols > 0:
		s.Width = enforcedCols
	default:
		s.Width = width
	}
	targetHeight := decodedHeight
	if enforcedRows > 0 {
		targetHeight = enforcedRows
	}
	if targetHeight < 0 {
		targetHeight = 0
	}
	if s.Width < 0 {
		s.Width = 0
	}
	if s.Width > 0 {
		for y := 0; y < len(s.Buffer); y++ {
			if len(s.Buffer[y]) < s.Width {
				row := make([]rune, s.Width)
				copy(row, s.Buffer[y])
				s.Buffer[y] = row
			} else if len(s.Buffer[y]) > s.Width {
				s.Buffer[y] = s.Buffer[y][:s.Width]
			}
		}
		for y := 0; y < len(s.CellAttrs); y++ {
			if len(s.CellAttrs[y]) < s.Width {
				row := make([]CellAttr, s.Width)
				copy(row, s.CellAttrs[y])
				s.CellAttrs[y] = row
			} else if len(s.CellAttrs[y]) > s.Width {
				s.CellAttrs[y] = s.CellAttrs[y][:s.Width]
			}
		}
	}
	if targetHeight < len(s.Buffer) {
		s.Buffer = s.Buffer[:targetHeight]
	} else if targetHeight > len(s.Buffer) {
		for y := len(s.Buffer); y < targetHeight; y++ {
			s.Buffer = append(s.Buffer, make([]rune, s.Width))
		}
	}
	if s.CellAttrs != nil {
		if targetHeight < len(s.CellAttrs) {
			s.CellAttrs = s.CellAttrs[:targetHeight]
		} else {
			for y := len(s.CellAttrs); y < targetHeight; y++ {
				s.CellAttrs = append(s.CellAttrs, make([]CellAttr, s.Width))
			}
		}
	}
	s.Height = targetHeight

	if state.fieldStartX >= 0 && s.Width > 0 && s.Height > 0 {
		endX := s.Width - 1
		endY := s.Height - 1
		if endX >= 0 && endY >= 0 {
			appendDecodedField(s, state, endX, endY)
		}
	}

	return nil
}

func decodeLineTokens(tokens []string, y int, formatted bool, s *Screen, state *decodeState) ([]rune, []CellAttr, error) {
	// Pre-allocate result to avoid allocations during append.
	// Each token maps to exactly one rune (either a character or a space for SF tokens).
	result := make([]rune, 0, len(tokens))
	var attrs []CellAttr
	index := 0

	// stamp records the character attributes in force at the position just
	// added. The slice stays nil until there is something to record, so a row
	// with no character attributes on it costs nothing.
	stamp := func() {
		if state.cell.IsZero() && attrs == nil {
			return
		}
		if attrs == nil {
			attrs = make([]CellAttr, len(result)-1, len(tokens))
		}
		for len(attrs) < len(result)-1 {
			attrs = append(attrs, CellAttr{})
		}
		attrs = append(attrs, state.cell)
	}

	for _, token := range tokens {
		if strings.HasPrefix(token, "SA(") {
			// Set Attribute order does not consume a screen position.
			processSetAttribute(token, state)
			continue
		}
		if strings.HasPrefix(token, "SF(") || strings.HasPrefix(token, "SFE(") {
			if !formatted {
				return nil, nil, fmt.Errorf("format information in unformatted screen")
			}
			result = append(result, ' ')
			stamp()
			processStartField(token, index, y, s, state)
			index++
			continue
		}
		// A graphic-escape names one character of the alternate set — the box
		// drawing and APL glyphs a panel is ruled with. It stands in a cell
		// like any other character; only the notation around it differs, and a
		// terminal that does not unwrap it draws a hole where the line should
		// be.
		if inner, ok := graphicEscapeCode(token); ok {
			token = inner
		}
		r, err := parseScreenRune(token)
		if err != nil {
			return nil, nil, err
		}
		result = append(result, r)
		stamp()
		index++
	}

	if state.fieldStartX == index && state.fieldStartY == y {
		state.fieldStartX = 0
		state.fieldStartY = y + 1
	}

	return result, attrs, nil
}

// graphicEscapeCode unwraps a GE(xx) token to the character code inside it.
func graphicEscapeCode(token string) (string, bool) {
	if !strings.HasPrefix(token, "GE(") || !strings.HasSuffix(token, ")") {
		return "", false
	}
	return token[len("GE(") : len(token)-1], true
}

// processSetAttribute applies one SA order to the running character-attribute
// state.
//
// An SA naming the attribute's default value — no colour, no highlighting — is
// how the data stream turns a run off again, and it arrives as often as the one
// that turned it on. Both spellings of "nothing" collapse to zero here so that
// a position which has been reset is indistinguishable from one that was never
// set, which is what lets the field's own attribute answer for it again.
func processSetAttribute(token string, state *decodeState) {
	inner := strings.TrimSuffix(strings.TrimPrefix(token, "SA("), ")")
	for inner != "" {
		var attr string
		attr, inner, _ = strings.Cut(inner, ",")

		key, val, ok := strings.Cut(attr, "=")
		if !ok {
			continue
		}
		b, err := parseHexByte(strings.TrimSpace(val))
		if err != nil {
			continue
		}
		switch strings.TrimSpace(key) {
		case attrKeyForegroundColor:
			state.cell.Color = b
		case attrKeyExtHighlight:
			state.cell.Highlight = normaliseHighlight(b)
		case attrKeyBackgroundColor:
			state.cell.Background = b
		default:
			continue
		}
		state.sawCell = state.sawCell || !state.cell.IsZero()
	}
}

// normaliseHighlight folds the explicit "no highlighting" value onto the
// absent-attribute default, so a comparison against AttrEhDefault answers for
// both.
func normaliseHighlight(b byte) uint8 {
	if b == AttrEhNormal {
		return AttrEhDefault
	}
	return b
}

func processStartField(token string, index, y int, s *Screen, state *decodeState) {
	if state.fieldStartX != -1 {
		endX := index - 1
		endY := y
		if endX < 0 {
			if state.width > 0 {
				endX = state.width - 1
				endY = y - 1
			} else {
				endX = 0
				endY = y - 1
			}
		}
		if endY >= 0 {
			appendDecodedField(s, state, endX, endY)
		}
	}

	inner := ""
	if strings.HasPrefix(token, "SF(") {
		inner = strings.TrimSuffix(strings.TrimPrefix(token, "SF("), ")")
	} else if strings.HasPrefix(token, "SFE(") {
		inner = strings.TrimSuffix(strings.TrimPrefix(token, "SFE("), ")")
	}
	startCode := byte(0)
	color := AttrColDefault
	extHighlight := AttrEhDefault
	background := AttrColDefault

	for inner != "" {
		var attr string
		attr, inner, _ = strings.Cut(inner, ",")

		key, val, ok := strings.Cut(attr, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		switch key {
		case attrKeyStartField:
			if b, err := parseHexByte(val); err == nil {
				startCode = b
			}
		case attrKeyExtHighlight:
			if b, err := parseHexByte(val); err == nil {
				extHighlight = int(normaliseHighlight(b))
			}
		case attrKeyForegroundColor:
			if b, err := parseHexByte(val); err == nil {
				color = int(b)
			}
		case attrKeyBackgroundColor:
			if b, err := parseHexByte(val); err == nil {
				background = int(b)
			}
		}
	}

	state.fieldStartX = index + 1
	state.fieldStartY = y
	state.fieldStartCode = startCode
	state.color = color
	state.extHighlight = extHighlight
	state.background = background
}

// parseScreenRune turns one screen position's token into the character it
// stands for.
//
// A single byte is that byte's code point, which is what it has always been and
// what covers every ASCII screen. A longer run is the character's bytes in the
// local codeset — UTF-8 — and is decoded as such. A run that is not valid UTF-8
// is read as a bare code point instead, so a build that spells the character
// out that way still lands on the right character rather than on a hole.
func parseScreenRune(token string) (rune, error) {
	if len(token) <= 2 {
		b, err := parseHexByte(token)
		if err != nil {
			return 0, err
		}
		return rune(b), nil
	}

	raw := make([]byte, 0, len(token)/2)
	for i := 0; i+1 < len(token); i += 2 {
		b, err := parseHexByte(token[i : i+2])
		if err != nil {
			return 0, err
		}
		raw = append(raw, b)
	}
	if r, size := utf8.DecodeRune(raw); r != utf8.RuneError && size == len(raw) {
		return r, nil
	}

	v, err := strconv.ParseUint(token, 16, 32)
	if err != nil || v > utf8.MaxRune {
		return 0, strconv.ErrRange
	}
	return rune(v), nil
}

// appendDecodedField closes the field the decoder is holding open and adds it
// to the screen.
//
// The end may land before the start, which is what two adjacent field
// attributes produce. That field is kept rather than dropped: it owns its
// attribute byte, and the field list is what the renderer walks to lay the
// screen out, so leaving one out shifts every column after it. Field.GetValue
// is where the empty case is answered.
func appendDecodedField(s *Screen, state *decodeState, endX, endY int) {
	f := NewField(s, state.fieldStartCode, state.fieldStartX, state.fieldStartY, endX, endY, state.color, state.extHighlight)
	f.Background = state.background
	s.Fields = append(s.Fields, f)
}

func parseHexByte(s string) (byte, error) {
	if len(s) == 2 {
		v0, ok0 := hexVal(s[0])
		v1, ok1 := hexVal(s[1])
		if !ok0 || !ok1 {
			return 0, strconv.ErrSyntax
		}
		return (v0 << 4) | v1, nil
	}

	v, err := strconv.ParseUint(s, 16, 8)
	if err != nil {
		return 0, err
	}
	return byte(v), nil
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}
