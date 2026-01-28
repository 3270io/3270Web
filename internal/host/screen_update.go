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
		`^[ULE] [FU] [PU] (?:C\([^)]*\)|N) [ILCN] [2-5] [0-9]+ [0-9]+ ([0-9]+) ([0-9]+) 0x0 (?:[0-9.]+|-)$`,
	)
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

	if rows, cols, ok := screenDimensionsFromStatus(status); ok {
		lines = normalizeScreenLines(lines, rows, cols)
	}

	if err := s.updateBuffer(lines); err != nil {
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

func screenDimensionsFromStatus(status string) (int, int, bool) {
	if status == "" {
		return 0, 0, false
	}
	parts := strings.Fields(status)
	if len(parts) < 8 {
		return 0, 0, false
	}
	rows, err := strconv.Atoi(parts[6])
	if err != nil || rows <= 0 {
		return 0, 0, false
	}
	cols, err := strconv.Atoi(parts[7])
	if err != nil || cols <= 0 {
		return 0, 0, false
	}
	return rows, cols, true
}

func normalizeScreenLines(lines []string, rows, cols int) []string {
	if rows <= 0 || cols <= 0 || len(lines) != 1 {
		return lines
	}
	line := strings.TrimSpace(lines[0])
	if strings.HasPrefix(line, "data:") {
		line = strings.TrimSpace(line[len("data:"):])
	}
	tokens := extractTokens(line)
	if len(tokens) < cols || len(tokens)%cols != 0 {
		return lines
	}
	totalRows := len(tokens) / cols
	if totalRows < rows {
		return lines
	}
	if totalRows == rows {
		return splitScreenLines(tokens, rows, cols)
	}
	if totalRows%rows != 0 {
		return lines
	}
	if !repeatsScreen(tokens, rows, cols, totalRows) {
		return lines
	}
	return splitScreenLines(tokens, rows, cols)
}

func normalizeScreenLinesForTest(lines []string, rows, cols int) []string {
	return normalizeScreenLines(lines, rows, cols)
}

func repeatsScreenForTest(tokens []string, rows, cols, totalRows int) bool {
	return repeatsScreen(tokens, rows, cols, totalRows)
}

func splitScreenLines(tokens []string, rows, cols int) []string {
	if rows <= 0 || cols <= 0 {
		return nil
	}
	normalized := make([]string, 0, rows)
	for i := 0; i < rows; i++ {
		start := i * cols
		end := start + cols
		if end > len(tokens) {
			break
		}
		normalized = append(normalized, "data: "+strings.Join(tokens[start:end], " "))
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
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

func (s *Screen) updateBuffer(lines []string) error {
	s.Height = len(lines)
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
	for y, line := range lines {
		state.width = width
		row, err := decodeLine(line, y, s.IsFormatted, s, state)
		if err != nil {
			return err
		}
		if len(row) > width {
			width = len(row)
		}
		s.Buffer[y] = row
	}
	s.Width = width

	if state.fieldStartX >= 0 && s.Width > 0 && s.Height > 0 {
		endX := s.Width - 1
		endY := s.Height - 1
		if endX >= 0 && endY >= 0 {
			s.Fields = append(s.Fields, NewField(s, state.fieldStartCode, state.fieldStartX, state.fieldStartY, endX, endY, state.color, state.extHighlight))
		}
	}

	return nil
}

func decodeLine(line string, y int, formatted bool, s *Screen, state *decodeState) ([]rune, error) {
	if strings.HasPrefix(line, "data:") {
		line = strings.TrimSpace(line[len("data:"):])
	}

	tokens := extractTokens(line)

	var result []rune
	index := 0

	for _, token := range tokens {
		if strings.HasPrefix(token, "SF(") {
			if !formatted {
				return nil, fmt.Errorf("format information in unformatted screen")
			}

			result = append(result, ' ')

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
			startCode := state.fieldStartCode
			color := AttrColDefault
			extHighlight := AttrEhDefault

			attrs := strings.Split(inner, ",")
			for _, attr := range attrs {
				parts := strings.SplitN(attr, "=", 2)
				if len(parts) != 2 {
					continue
				}
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])

				switch key {
				case "c0":
					if b, err := parseHexByte(val); err == nil {
						startCode = b
					}
				case "41":
					if b, err := parseHexByte(val); err == nil {
						extHighlight = int(b)
					}
				case "42":
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

func parseHexByte(s string) (byte, error) {
	v, err := strconv.ParseUint(s, 16, 8)
	if err != nil {
		return 0, err
	}
	return byte(v), nil
}
