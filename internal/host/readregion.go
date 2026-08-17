// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"fmt"
	"strconv"
	"strings"
)

// Everything else in this package reads the screen through the parser in
// screen_update.go: the terminal is asked for the display, the bytes are turned
// into a Screen with a buffer and a list of fields, and every caller — the
// browser panel, the HLLAPI endpoint, the chaos engine, workflow playback —
// cuts the region it wants out of that. That is the right arrangement for
// nearly every question, and it is exactly the wrong one for a single question:
// whether the parse is the thing that is wrong.
//
// A code page complaint is that question. A pound sign arrives as a hash, a
// field looks a column wider than the application says it is, an umlaut turns
// into two cells — and the first thing worth knowing is whether the host sent
// what the operator thinks it sent. The parsed screen cannot answer that,
// because it is downstream of every step that could have caused it. The
// terminal will answer it directly: Ascii() reads a region out of its own
// buffer, Ebcdic() reads the same region as the code points the host actually
// transmitted, and ReadBuffer() describes the buffer with the field structure
// still attached.
//
// So these are diagnostic reads, and they are deliberately not wired into the
// paths that serve screens. Nothing here is a faster way to read a screen —
// it is a second opinion, and a second opinion is only worth having when it
// comes from somewhere other than the thing being checked.
//
// Coordinates in this file are 0-based, like MoveCursor and WriteStringAt on
// the same type, and like the terminal's own default origin. The one-based
// counting an operator uses belongs at the HTTP boundary, where every other
// conversion in this codebase already happens.

// MaxRegionLength bounds a single region read. The largest 3270 display this
// terminal can be asked for is 27 rows of 132, so anything past that cannot be
// a region of a screen — it is a caller that has lost track of its own
// arithmetic, and a bounded refusal is a better answer than a response the
// terminal has to invent most of.
const MaxRegionLength = 27 * 132

// RegionEncoding selects what a region read reports.
type RegionEncoding string

const (
	// RegionASCII reports the region as the display shows it.
	RegionASCII RegionEncoding = "ascii"
	// RegionEBCDIC reports the host's own code points, one hex byte per cell,
	// before anything on this side interpreted them.
	RegionEBCDIC RegionEncoding = "ebcdic"
)

// RegionRead is one region of the display as the terminal reports it.
type RegionRead struct {
	Row      int    `json:"row"`
	Col      int    `json:"col"`
	Length   int    `json:"length"`
	Encoding string `json:"encoding"`
	// Text is the region as characters. Set for an ASCII read.
	Text string `json:"text,omitempty"`
	// Codes is one hex code point per cell, in reading order. Set for an
	// EBCDIC read.
	Codes []string `json:"codes,omitempty"`
}

// regionCommand builds the action for one region read. It is separate from the
// method that sends it because the arithmetic — which origin, which argument
// order, how a length is spelled — is the part worth pinning in a test, and the
// part that cannot be tested through a subprocess that is not installed on
// every machine this is built on.
func regionCommand(enc RegionEncoding, row, col, length int) (string, error) {
	if row < 0 || col < 0 {
		return "", fmt.Errorf("row and column must not be negative")
	}
	if length <= 0 {
		return "", fmt.Errorf("length must be at least 1")
	}
	if length > MaxRegionLength {
		return "", fmt.Errorf("length %d is larger than the largest screen this terminal supports (%d cells)", length, MaxRegionLength)
	}
	switch enc {
	case RegionASCII, "":
		return fmt.Sprintf("Ascii(%d,%d,%d)", row, col, length), nil
	case RegionEBCDIC:
		return fmt.Sprintf("Ebcdic(%d,%d,%d)", row, col, length), nil
	}
	return "", fmt.Errorf("unsupported encoding %q (allowed: ascii, ebcdic)", enc)
}

// parseHexCodes splits an Ebcdic() response into one token per cell.
//
// A token that is not a hex byte is kept rather than dropped. Dropping it would
// keep the list tidy and lose the alignment between the codes and the cells
// they came from, which is the only reason anybody asked for them — a caller
// counting along a field to find the cell that went wrong would be counting
// against a list with a hole in it and never know.
func parseHexCodes(lines []string) []string {
	out := []string{}
	for _, line := range lines {
		for _, token := range strings.Fields(line) {
			if len(token) == 1 {
				// Some builds drop the leading zero on a low code point.
				token = "0" + token
			}
			out = append(out, strings.ToLower(token))
		}
	}
	return out
}

// ReadRegion reads length cells starting at (row, col) — 0-based — straight
// out of the terminal's buffer, in the requested encoding.
//
// A region that runs past the end of a row continues on the next one, which is
// the terminal's own behaviour and the 3270 buffer's: the display is a single
// linear buffer that happens to be drawn as a rectangle, and a field is
// perfectly entitled to straddle the right-hand edge.
func (h *S3270) ReadRegion(row, col, length int, enc RegionEncoding) (*RegionRead, error) {
	cmd, err := regionCommand(enc, row, col, length)
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	data, status, err := h.doCommandLocked(cmd)
	if err != nil {
		return nil, err
	}
	if isS3270Error(status, data) {
		return nil, fmt.Errorf("s3270 %s error: %s", cmd, strings.TrimSpace(status))
	}

	out := &RegionRead{Row: row, Col: col, Length: length, Encoding: string(enc)}
	if out.Encoding == "" {
		out.Encoding = string(RegionASCII)
	}
	lines := dataLines(data)
	if enc == RegionEBCDIC {
		out.Codes = parseHexCodes(lines)
		return out, nil
	}
	// Joined rather than taking the first line: a region long enough to wrap
	// comes back as one line per row on some builds, and silently keeping the
	// first would return part of the region and report the whole length.
	out.Text = strings.Join(lines, "\n")
	return out, nil
}

// BufferField is one field as the terminal's own buffer describes it, rather
// than as this package's parser reconstructed it. Row and Col are 0-based and
// point at the field's first content cell, not at its attribute byte — the
// attribute occupies a buffer position of its own, and reporting that position
// as the start of the field is the off-by-one every 3270 field calculation
// gets wrong at least once.
type BufferField struct {
	Row       int    `json:"row"`
	Col       int    `json:"col"`
	Length    int    `json:"length"`
	Attribute string `json:"attribute"`

	Protected   bool `json:"protected"`
	Numeric     bool `json:"numeric"`
	Hidden      bool `json:"hidden"`
	Intensified bool `json:"intensified"`
	Modified    bool `json:"modified"`

	// Formatted is false when the screen carries no field structure at all. An
	// unformatted screen has no fields, so the whole buffer is reported as one
	// unprotected field — that being what an unformatted 3270 screen is, rather
	// than an error to hand back to a caller that asked a reasonable question.
	Formatted bool `json:"formatted"`

	// Text is the field's content, decoded from the code points the terminal
	// reported for each cell.
	Text string `json:"text"`
	// Codes is the host's own EBCDIC code point for each content cell, in
	// reading order. Absent on a build that would not report them, rather than
	// filled in from the display's code points — the difference between those
	// two is the whole question this read exists to answer.
	Codes []string `json:"codes,omitempty"`
}

// AttrModified is the field attribute's modified-data-tag bit. The other bits
// this file reads already have names in types.go.
const AttrModified = 0x01

// bufferCell is one position of the buffer as ReadBuffer described it.
type bufferCell struct {
	startField bool
	attr       byte
	code       string
}

// parseReadBuffer turns a ReadBuffer response into a rectangle of cells.
//
// The response is one line per row, and each row is a series of whitespace-
// separated tokens: a bare hex number for an ordinary character, SF(c0=xx) for
// a start-of-field position, GE(xx) for a character from the graphic-escape
// set, and SA(nn=xx) for an extended attribute. The first three each occupy a
// buffer position; SA does not — it modifies the characters that follow it —
// and counting it as one is what would put every cell after the first coloured
// run one column to the left of where the host wrote it.
func parseReadBuffer(lines []string) [][]bufferCell {
	rows := make([][]bufferCell, 0, len(lines))
	for _, line := range lines {
		tokens := strings.Fields(line)
		if len(tokens) == 0 {
			continue
		}
		cells := make([]bufferCell, 0, len(tokens))
		for _, token := range tokens {
			switch {
			case strings.HasPrefix(token, "SA("):
				// Occupies no position.
			case strings.HasPrefix(token, "SF("):
				cells = append(cells, bufferCell{startField: true, attr: parseFieldAttribute(token)})
			case strings.HasPrefix(token, "GE("):
				inner := strings.TrimSuffix(strings.TrimPrefix(token, "GE("), ")")
				cells = append(cells, bufferCell{code: strings.ToLower(inner)})
			default:
				cells = append(cells, bufferCell{code: strings.ToLower(token)})
			}
		}
		rows = append(rows, cells)
	}
	return rows
}

// parseFieldAttribute pulls the c0 byte out of an SF(...) token. A token that
// does not carry one yields zero, which reads as an unprotected, normal-
// intensity field — the same thing a caller would conclude from a screen with
// no attributes at all, and safer than guessing at protection that was never
// stated.
func parseFieldAttribute(token string) byte {
	inner := strings.TrimSuffix(strings.TrimPrefix(token, "SF("), ")")
	for _, part := range strings.Split(inner, ",") {
		name, value, found := strings.Cut(part, "=")
		if !found || !strings.EqualFold(strings.TrimSpace(name), "c0") {
			continue
		}
		if n, err := strconv.ParseUint(strings.TrimSpace(value), 16, 8); err == nil {
			return byte(n)
		}
	}
	return 0
}

// decodeCells turns the hex code points of an ASCII ReadBuffer into text. A
// zero code is the buffer's own way of saying "nothing has been written here",
// and it is rendered as a space so a field's text lines up with the width the
// application drew.
func decodeCells(codes []string) string {
	var sb strings.Builder
	for _, code := range codes {
		n, err := strconv.ParseUint(code, 16, 32)
		if err != nil || n == 0 {
			sb.WriteRune(' ')
			continue
		}
		sb.WriteRune(rune(n))
	}
	return sb.String()
}

// ReadFieldAt reports the field containing (row, col) — 0-based — as the
// terminal's own buffer describes it.
//
// The cursor is not moved to do this. AsciiField() would have been the shorter
// route and reads the field the cursor happens to be in, which means asking
// about a position requires moving the cursor there and putting it back
// afterwards. That is two mutations of something the operator can see, to
// answer a question that changes nothing — and if the second one fails, the
// session is left with the cursor somewhere nobody put it. ReadBuffer describes
// the whole buffer with its field structure intact, so the field containing a
// position is found by looking rather than by pointing.
func (h *S3270) ReadFieldAt(row, col int) (*BufferField, error) {
	if row < 0 || col < 0 {
		return nil, fmt.Errorf("row and column must not be negative")
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	ascii, err := h.readBufferLocked("Ascii")
	if err != nil {
		return nil, err
	}
	ebcdic, err := h.readBufferLocked("Ebcdic")
	if err != nil {
		// The host's own code points are the second opinion, not the answer.
		// A build that will not give them up is not a reason to withhold the
		// field, so the read carries on without them.
		ebcdic = nil
	}
	return fieldAt(parseReadBuffer(ascii), parseReadBuffer(ebcdic), row, col)
}

// readBufferLocked issues one ReadBuffer and returns its data lines. The caller
// must hold h.mu.
func (h *S3270) readBufferLocked(mode string) ([]string, error) {
	cmd := fmt.Sprintf("ReadBuffer(%s)", mode)
	data, status, err := h.doCommandLocked(cmd)
	if err != nil {
		return nil, err
	}
	if isS3270Error(status, data) {
		return nil, fmt.Errorf("s3270 %s error: %s", cmd, strings.TrimSpace(status))
	}
	return dataLines(data), nil
}

// fieldAt locates the field containing (row, col) in a parsed buffer.
//
// A 3270 field runs from its attribute byte to the cell before the next
// attribute byte, wrapping past the end of the buffer and back to the top —
// the display is one circular buffer drawn as a rectangle, and the field a
// position belongs to is genuinely the one that started before it even when
// "before" means the bottom of the screen.
func fieldAt(cells, ebcdicCells [][]bufferCell, row, col int) (*BufferField, error) {
	flat, cols := flattenCells(cells)
	if len(flat) == 0 {
		return nil, fmt.Errorf("the terminal reported an empty screen buffer")
	}
	if row >= len(cells) {
		return nil, fmt.Errorf("row %d is past the end of a %d-row screen", row+1, len(cells))
	}
	if col >= cols {
		return nil, fmt.Errorf("column %d is past the end of a %d-column screen", col+1, cols)
	}
	pos := row*cols + col
	if pos >= len(flat) {
		return nil, fmt.Errorf("row %d column %d is outside the screen buffer", row+1, col+1)
	}

	ebcdicFlat, _ := flattenCells(ebcdicCells)

	start, formatted := startFieldBefore(flat, pos)
	out := &BufferField{Formatted: formatted}

	// Content begins after the attribute byte on a formatted screen, and at the
	// top of the buffer on one with no fields at all.
	content := start
	if formatted {
		attr := flat[start].attr
		out.Attribute = fmt.Sprintf("%02x", attr)
		marker := &Field{FieldCode: attr}
		out.Protected = marker.IsProtected()
		out.Numeric = marker.IsNumeric()
		out.Hidden = marker.IsHidden()
		out.Intensified = marker.IsIntensified()
		out.Modified = attr&AttrModified != 0
		content = (start + 1) % len(flat)
	}

	length := fieldLengthFrom(flat, content, formatted)
	out.Row = content / cols
	out.Col = content % cols
	out.Length = length

	displayCodes := make([]string, 0, length)
	for i := 0; i < length; i++ {
		displayCodes = append(displayCodes, flat[(content+i)%len(flat)].code)
	}
	out.Text = decodeCells(displayCodes)

	// Codes are the host's code points and nothing else. Falling back to the
	// display's when the terminal would not give up the host's would hand back
	// a plausible list of bytes under a name that promises where they came
	// from, which is worse than an absent field on the one endpoint whose
	// entire purpose is telling those two apart.
	if len(ebcdicFlat) == len(flat) {
		out.Codes = make([]string, 0, length)
		for i := 0; i < length; i++ {
			out.Codes = append(out.Codes, ebcdicFlat[(content+i)%len(flat)].code)
		}
	}
	return out, nil
}

// flattenCells lays the rows out as one buffer and reports the width. Rows
// shorter than the widest are padded, so a build that trims trailing blanks off
// a row cannot shift every cell below it.
func flattenCells(rows [][]bufferCell) ([]bufferCell, int) {
	cols := 0
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols == 0 {
		return nil, 0
	}
	flat := make([]bufferCell, 0, len(rows)*cols)
	for _, row := range rows {
		flat = append(flat, row...)
		for i := len(row); i < cols; i++ {
			flat = append(flat, bufferCell{})
		}
	}
	return flat, cols
}

// startFieldBefore walks backwards from pos to the attribute byte that opens
// the field pos belongs to, wrapping past the top of the buffer. It reports
// false when the buffer holds no attribute bytes at all, which is what an
// unformatted screen looks like from here.
func startFieldBefore(flat []bufferCell, pos int) (int, bool) {
	for i := 0; i < len(flat); i++ {
		at := ((pos-i)%len(flat) + len(flat)) % len(flat)
		if flat[at].startField {
			return at, true
		}
	}
	return 0, false
}

// fieldLengthFrom counts content cells from start up to the next attribute
// byte. On an unformatted screen there is no next attribute byte and the answer
// is the whole buffer.
func fieldLengthFrom(flat []bufferCell, start int, formatted bool) int {
	if !formatted {
		return len(flat)
	}
	for i := 0; i < len(flat); i++ {
		if flat[(start+i)%len(flat)].startField {
			return i
		}
	}
	return len(flat)
}
