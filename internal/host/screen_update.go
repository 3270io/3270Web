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
)

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
				if !strings.HasPrefix(token, "SA(") {
					if strings.HasPrefix(token, "SF(") || strings.HasPrefix(token, "SFE(") || isCharacterToken(token) {
						tokens = append(tokens, token)
					} else {
						// Replace invalid/unknown tokens with null byte (space) to preserve screen alignment
						tokens = append(tokens, "00")
					}
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
		if !strings.HasPrefix(token, "SA(") {
			if strings.HasPrefix(token, "SF(") || strings.HasPrefix(token, "SFE(") || (len(token) == 2 && isHex(token)) {
				tokens = append(tokens, token)
			} else {
				// Replace invalid/unknown tokens with null byte (space) to preserve screen alignment
				tokens = append(tokens, "00")
			}
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
	width          int
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

	// Extract model number from status
	if len(parts) > statusIdxModel {
		modelNum := parts[statusIdxModel]
		if expectedRows, expectedCols, ok := getModelDimensions(modelNum); ok {
			// Validate and enforce model-specific dimension limits
			if rows > expectedRows {
				rows = expectedRows
			}
			if cols > expectedCols {
				cols = expectedCols
			}
		}
	}

	return rows, cols, true
}

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

	if len(tokens) < cols || len(tokens)%cols != 0 {
		return fallback()
	}
	totalRows := len(tokens) / cols
	if totalRows < rows {
		return fallback()
	}

	// Helper to slice tokens into rows
	splitTokens := func(t []string, r, c int) [][]string {
		if r <= 0 || c <= 0 {
			return nil
		}
		out := make([][]string, 0, r)
		for i := 0; i < r; i++ {
			start := i * c
			end := start + c
			if end > len(t) {
				break
			}
			out = append(out, t[start:end])
		}
		return out
	}

	if totalRows == rows {
		return splitTokens(tokens, rows, cols)
	}
	if totalRows%rows != 0 {
		return fallback()
	}
	if !repeatsScreen(tokens, rows, cols, totalRows) {
		return fallback()
	}
	return splitTokens(tokens, rows, cols)
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
	return repeatsScreen(tokens, rows, cols, totalRows)
}

func repeatsScreen(tokens []string, rows, cols, totalRows int) bool {
	if rows <= 0 || cols <= 0 || totalRows <= rows {
		return false
	}
	blockSize := rows * cols
	if blockSize <= 0 || blockSize > len(tokens) {
		return false
	}
	blocks := totalRows / rows
	for block := 1; block < blocks; block++ {
		offset := block * blockSize
		if offset+blockSize > len(tokens) {
			return false
		}
		for i := 0; i < blockSize; i++ {
			if tokens[i] != tokens[offset+i] {
				return false
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
		return nil
	}

	s.Height = decodedHeight
	s.Buffer = make([][]rune, decodedHeight)
	s.Fields = nil

	state := &decodeState{
		fieldStartX:    0,
		fieldStartY:    0,
		fieldStartCode: 0xe0,
		color:          AttrColDefault,
		extHighlight:   AttrEhDefault,
		width:          s.Width,
	}

	width := 0
	for y, tokens := range tokenRows {
		state.width = width
		row, err := decodeLineTokens(tokens, y, s.IsFormatted, s, state)
		if err != nil {
			return err
		}
		if len(row) > width {
			width = len(row)
		}
		s.Buffer[y] = row
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
	}
	if targetHeight < len(s.Buffer) {
		s.Buffer = s.Buffer[:targetHeight]
	} else if targetHeight > len(s.Buffer) {
		for y := len(s.Buffer); y < targetHeight; y++ {
			s.Buffer = append(s.Buffer, make([]rune, s.Width))
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

func decodeLineTokens(tokens []string, y int, formatted bool, s *Screen, state *decodeState) ([]rune, error) {
	// Pre-allocate result to avoid allocations during append.
	// Each token maps to exactly one rune (either a character or a space for SF tokens).
	result := make([]rune, 0, len(tokens))
	index := 0

	for _, token := range tokens {
		if strings.HasPrefix(token, "SA(") {
			// Set Attribute order does not consume a screen position.
			continue
		}
		if strings.HasPrefix(token, "SF(") || strings.HasPrefix(token, "SFE(") {
			if !formatted {
				return nil, fmt.Errorf("format information in unformatted screen")
			}
			result = append(result, ' ')
			processStartField(token, index, y, s, state)
			index++
			continue
		}
		r, err := parseScreenRune(token)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
		index++
	}

	if state.fieldStartX == index && state.fieldStartY == y {
		state.fieldStartX = 0
		state.fieldStartY = y + 1
	}

	return result, nil
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
				extHighlight = int(b)
			}
		case attrKeyForegroundColor:
			if b, err := parseHexByte(val); err == nil {
				color = int(b)
			}
		}
	}

	state.fieldStartX = index + 1
	state.fieldStartY = y
	state.fieldStartCode = startCode
	state.color = color
	state.extHighlight = extHighlight
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

func appendDecodedField(s *Screen, state *decodeState, endX, endY int) {
	// Adjacent field attributes delimit an empty field. Keeping that field
	// would make Substring walk the rest of the screen looking for an end
	// position that precedes its start.
	if endY < state.fieldStartY || (endY == state.fieldStartY && endX < state.fieldStartX) {
		return
	}
	s.Fields = append(s.Fields, NewField(s, state.fieldStartCode, state.fieldStartX, state.fieldStartY, endX, endY, state.color, state.extHighlight))
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
