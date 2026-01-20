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
	// Example: U F U C(hostname) I 3 32 80 22 15 0x0 -
	statusPattern = regexp.MustCompile(`^[ULE] [FU] [PU] (?:C\([^)]*\)|N) [ILCN] [2-5] [0-9]+ [0-9]+ ([0-9]+) ([0-9]+) 0x0 (?:[0-9.]+|-)`)

	// SF(c0=e0) or hex like 53
	formattedCharPattern = regexp.MustCompile(`SF\((..)=(..)(,(..)=(..)(,(..)=(..))?)?\)|[0-9a-fA-F]{2}`)

	// SA(..=..) removal
	saPattern = regexp.MustCompile(`SA\(..=..\)`)

	// Status detection
	statusLineCheck = regexp.MustCompile(`^[ULE] [UF] [UC] .*`)
)

// NewScreenFromDump creates a screen from a dump reader (file or string).
func NewScreenFromDump(r io.Reader) (*Screen, error) {
	s := &Screen{IsFormatted: true}
	err := s.UpdateFromDump(r)
	return s, err
}

// UpdateFromDump reads lines from the reader and updates the screen.
func (s *Screen) UpdateFromDump(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	var lines []string
	var status string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			lines = append(lines, line)
		} else if statusLineCheck.MatchString(line) {
			status = line
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	if status == "" {
		// Fallback or error? specific dumps might lack status line or conform differently.
		// For now, proceed if we have data.
		if len(lines) == 0 {
			return fmt.Errorf("no data found in dump")
		}
		// Default status?
		status = "U F U C(unknown) I 4 24 80 0 0 0x0 -"
	}

	return s.Update(status, lines)
}

// Update parses the s3270 output (status line + data lines) and updates the screen state.
func (s *Screen) Update(status string, bufferData []string) error {
	s.Status = status

	// Parse status line
	if len(status) > 2 {
		if status[2] == 'F' {
			s.IsFormatted = true
		} else {
			s.IsFormatted = false
		}
	}

	matches := statusPattern.FindStringSubmatch(status)
	if len(matches) >= 3 {
		y, _ := strconv.Atoi(matches[1])
		x, _ := strconv.Atoi(matches[2])
		s.CursorX = x
		s.CursorY = y
	} else {
		s.CursorX = 0
		s.CursorY = 0
	}

	return s.updateBuffer(bufferData)
}

func (s *Screen) updateBuffer(bufferData []string) error {
	s.Height = len(bufferData)
	s.Width = 0
	s.Buffer = make([][]rune, s.Height)
	s.Fields = make([]*Field, 0)

	// Parsing state
	var (
		fieldStartX    = 0
		fieldStartY    = 0
		fieldStartCode = byte(0xe0)
		color          = AttrColDefault
		extHighlight   = AttrEhDefault
	)

	for y := 0; y < s.Height; y++ {
		lineStr := bufferData[y]

		// Decode line
		rowChars, nextFx, nextFy, nextCode, nextCol, nextEh, err := s.decodeLine(
			lineStr, y,
			&fieldStartX, &fieldStartY, &fieldStartCode, &color, &extHighlight,
		)
		if err != nil {
			return err
		}

		if len(rowChars) > s.Width {
			s.Width = len(rowChars)
		}
		s.Buffer[y] = rowChars

		// Update state for next line
		fieldStartX = nextFx
		fieldStartY = nextFy
		fieldStartCode = nextCode
		color = nextCol
		extHighlight = nextEh
	}

	// Add the final field
	s.Fields = append(s.Fields, NewField(
		s, fieldStartCode, fieldStartX, fieldStartY,
		s.Width-1, s.Height-1, color, extHighlight,
	))

	// Update focus based on cursor
	if focused := s.GetInputFieldAt(s.CursorX, s.CursorY); focused != nil {
		focused.Focused = true
	}

	return nil
}

// decodeLine decodes a single "data: ..." line.
// It returns the characters, and the updated field state variables.
func (s *Screen) decodeLine(
	line string, y int,
	fieldStartX, fieldStartY *int, fieldStartCode *byte, color, extHighlight *int,
) ([]rune, int, int, byte, int, int, error) {

	if strings.HasPrefix(line, "data: ") {
		line = line[6:]
	}

	// Workaround: remove SA(..)
	line = saPattern.ReplaceAllString(line, "")

	var result []rune
	index := 0 // Column index

	// Local copies of state
	fx := *fieldStartX
	fy := *fieldStartY
	fCode := *fieldStartCode
	fColor := *color
	fEh := *extHighlight

	// Iterate matches
	allMatches := formattedCharPattern.FindAllStringSubmatch(line, -1)

	for _, m := range allMatches {
		code := m[0]

		if strings.HasPrefix(code, "SF") {
			if !s.IsFormatted {
				return nil, 0, 0, 0, 0, 0, fmt.Errorf("format info in unformatted screen")
			}

			result = append(result, ' ') // Field attribute takes up a space (visual blank)

			// Parse attributes in the SF(...) content
			// Groups in regex:
			// 0: Full match
			// 1: Key1, 2: Val1
			// 3: ,Key2=Val2 part
			// 4: Key2, 5: Val2
			// 6: ,Key3=Val3 part
			// 7: Key3, 8: Val3

			var auxStartCode = -1
			var auxColor = -1
			var auxExtHighlight = -1

			// We need to parse manually or use the groups carefully.
			// The regex structure is recursive/nested in the original logic,
			// but here I used a fixed depth regex which matches the Java one roughly.
			// Java logic:
			// while (i <= m.groupCount()) ...

			// Let's iterate over pairs (1,2), (4,5), (7,8)
			pairs := []struct{ k, v string }{
				{m[1], m[2]},
				{m[4], m[5]},
				{m[7], m[8]},
			}

			for _, p := range pairs {
				if p.k == "" {
					continue
				}
				if p.k == "c0" {
					// Field definition
					// If we were in a field, close it
					if fx != -1 {
						fieldEndX := index - 1
						fieldEndY := y
						if fieldEndX == -1 {
							// Wrapped from previous line?
							// If index is 0, fieldEndX is -1.
							// The field ended on the previous line end.
							// But we are processing line y.
							// If index is 0, it means this SF is at the start of the line.
							// The previous field ended at (width-1, y-1).
							// But we don't know width yet fully.
							// However, s.Width is accumulating max width.
							// Actually, if fieldEndX is -1, it means the field was empty on THIS line?
							// No, it means the field end coordinate is before the start of this line.
							// But the logic in Java says:
							// if (fieldEndX == -1) { fieldEndX = width-1; fieldEndY--; }
							// In Java `width` inside updateBuffer loop is `line.length` of the current line?
							// No, `width` is a member variable, init to 0.
							// "if (line.length > width) width = line.length;" happens AFTER decode.
							// So `width` in `decode` refers to the `width` of the screen so far?
							// Wait, `S3270Screen.java` uses `width` in `decode`?
							// Yes: "fieldEndX = width-1;"
							// But `width` is 0 initially.
							// This logic seems susceptible to bug if width isn't set yet.
							// However, usually width is 80.
							// Let's assume standard 80 width for fallback if 0?
							// Or maybe it refers to `s.Width` from previous update?
							// In `updateBuffer`: "width = 0;"
							// So for the first line, width is 0.
							// If `fieldEndX` becomes -1, `fieldEndY--`.
							// This implies the field ended on the previous line.
							// The X coordinate would be the last column of previous line.
							// If we don't know the width of previous line, we have a problem.
							// BUT, `decode` returns char[] and sets `width` later.
							// Ah, `S3270Screen` uses `width` variable.
							// Is it possible `width` is not used until after the loop?
							// No, it's used right there.
							// If this is the first update, width is 0.
							// If `fieldEndX` is -1, it sets `fieldEndX = -1` (0-1).
							// That seems wrong if width is 0.
							// But typical 3270 screens are 80 cols.
							// Let's look at `S3270Screen.java` again.
							// `width` is reset to 0.
							// `fieldEndX = width-1` -> -1.
							// `createField` takes endx.
							// `new InputField`...
							// If endx is -1, that's invalid.
							// Maybe valid dumps don't trigger this condition on the first line?
							// Or maybe `width` keeps the old width?
							// `width = 0;` is explicit.

							// Let's assume for now we can use 79 (80-1) or similar if width is 0, or just let it be -1 and fix later if it crashes.
							// Actually, let's use the length of the *previous* line in Buffer?
							// s.Buffer[y-1] exists if y > 0.
							w := 0
							if y > 0 {
								w = len(s.Buffer[y-1])
							}
							if w == 0 {
								w = 80
							} // Fallback
							fieldEndX = w - 1
							fieldEndY--
						}

						s.Fields = append(s.Fields, NewField(
							s, fCode, fx, fy, fieldEndX, fieldEndY, fColor, fEh,
						))
					}

					// Setup new field
					fx = index + 1
					fy = y

					val, _ := strconv.ParseUint(p.v, 16, 8)
					fCode = byte(val)

					// Reset attributes for new field?
					// Java: "fieldStartCode = ...; ext_highlight = DEFAULT; color = DEFAULT;" (unless overridden by 41/42)
					// Yes, it sets defaults then overrides if found.
					auxStartCode = 1 // Just a flag that we found c0

				} else if p.k == "41" {
					val, _ := strconv.ParseInt(p.v, 16, 32)
					auxExtHighlight = int(val)
				} else if p.k == "42" {
					val, _ := strconv.ParseInt(p.v, 16, 32)
					auxColor = int(val)
				}
			}

			if auxStartCode != -1 {
				if auxExtHighlight != -1 {
					fEh = auxExtHighlight
				} else {
					fEh = AttrEhDefault
				}
				if auxColor != -1 {
					fColor = auxColor
				} else {
					fColor = AttrColDefault
				}
			}

		} else {
			// Hex char
			// code is like "C1"
			val, err := strconv.ParseUint(code, 16, 32)
			if err != nil {
				// Should not happen with regex match
			}
			result = append(result, rune(val))
		}

		index++
	}

	// Check for field wrap at end of line
	// Java: "if (fieldStartX == index && fieldStartY == y) { fieldStartX = 0; fieldStartY++; }"
	if fx == index && fy == y {
		fx = 0
		fy++
	}

	return result, fx, fy, fCode, fColor, fEh, nil
}
