package host

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
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
	fields := strings.Fields(line)
	tokens := make([]string, 0, len(fields))

	for _, field := range fields {
		if strings.HasPrefix(field, "SA(") {
			continue
		}
		// formattedTokenPattern is SF(...) or 2 hex chars.
		if strings.HasPrefix(field, "SF(") || (len(field) == 2 && isHex(field)) {
			tokens = append(tokens, field)
		}
	}
	return tokens
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
	s.Height = len(tokenRows)
	if s.Height == 0 {
		s.Width = 0
		s.Buffer = nil
		s.Fields = nil
		return nil
	}

	s.Buffer = make([][]rune, s.Height)
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
	// Use enforced dimensions if available, otherwise use calculated width
	if enforcedCols > 0 && width > enforcedCols {
		s.Width = enforcedCols
	} else {
		s.Width = width
	}
	if enforcedRows > 0 && s.Height > enforcedRows {
		s.Height = enforcedRows
	}

	if state.fieldStartX >= 0 && s.Width > 0 && s.Height > 0 {
		endX := s.Width - 1
		endY := s.Height - 1
		if endX >= 0 && endY >= 0 {
			s.Fields = append(s.Fields, NewField(s, state.fieldStartCode, state.fieldStartX, state.fieldStartY, endX, endY, state.color, state.extHighlight))
		}
	}

	return nil
}

func decodeLineTokens(tokens []string, y int, formatted bool, s *Screen, state *decodeState) ([]rune, error) {
	var result []rune
	index := 0

	for _, token := range tokens {
		if strings.HasPrefix(token, "SF(") {
			if !formatted {
				return nil, fmt.Errorf("format information in unformatted screen")
			}
			result = append(result, ' ')
			processStartField(token, index, y, s, state)
		} else {
			b, err := parseHexByte(token)
			if err != nil {
				return nil, err
			}
			result = append(result, rune(b))
		}
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
			s.Fields = append(s.Fields, NewField(s, state.fieldStartCode, state.fieldStartX, state.fieldStartY, endX, endY, state.color, state.extHighlight))
		}
	}

	inner := strings.TrimSuffix(strings.TrimPrefix(token, "SF("), ")")
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
