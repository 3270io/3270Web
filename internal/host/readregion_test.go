// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"strings"
	"testing"
)

func TestRegionCommandUsesTheTerminalsOwnOrigin(t *testing.T) {
	got, err := regionCommand(RegionASCII, 0, 0, 80)
	if err != nil {
		t.Fatalf("regionCommand: %v", err)
	}
	if got != "Ascii(0,0,80)" {
		t.Errorf("ascii command = %q, want Ascii(0,0,80)", got)
	}

	got, err = regionCommand(RegionEBCDIC, 5, 12, 8)
	if err != nil {
		t.Fatalf("regionCommand: %v", err)
	}
	if got != "Ebcdic(5,12,8)" {
		t.Errorf("ebcdic command = %q, want Ebcdic(5,12,8)", got)
	}
}

// An empty encoding is the caller not having said, and the display is what
// anyone who did not say wanted.
func TestRegionCommandDefaultsToTheDisplay(t *testing.T) {
	got, err := regionCommand("", 1, 1, 4)
	if err != nil {
		t.Fatalf("regionCommand: %v", err)
	}
	if !strings.HasPrefix(got, "Ascii(") {
		t.Errorf("default command = %q, want an Ascii read", got)
	}
}

func TestRegionCommandRefusesWhatCannotBeARegion(t *testing.T) {
	cases := []struct {
		name             string
		enc              RegionEncoding
		row, col, length int
	}{
		{"negative row", RegionASCII, -1, 0, 4},
		{"negative col", RegionASCII, 0, -1, 4},
		{"zero length", RegionASCII, 0, 0, 0},
		{"negative length", RegionASCII, 0, 0, -8},
		{"longer than any screen", RegionASCII, 0, 0, MaxRegionLength + 1},
		{"unknown encoding", RegionEncoding("utf8"), 0, 0, 4},
	}
	for _, tc := range cases {
		if got, err := regionCommand(tc.enc, tc.row, tc.col, tc.length); err == nil {
			t.Errorf("%s: regionCommand = %q, want a refusal", tc.name, got)
		}
	}
}

// The codes have to line up cell for cell with the region they came from —
// that alignment is the only reason anybody asked for them. A token that is not
// a hex byte is kept in place rather than dropped, so a caller counting along a
// field to find the cell that went wrong is not counting against a list with a
// hole in it.
func TestParseHexCodesKeepsOneTokenPerCell(t *testing.T) {
	got := parseHexCodes([]string{"c8 85 93", "93 96"})
	want := []string{"c8", "85", "93", "93", "96"}
	if len(got) != len(want) {
		t.Fatalf("parseHexCodes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("code %d = %q, want %q", i, got[i], want[i])
		}
	}

	// A build that drops the leading zero on a low code point still describes
	// one cell, and a two-character answer keeps every reader's arithmetic the
	// same across builds.
	if got := parseHexCodes([]string{"5 40"}); got[0] != "05" {
		t.Errorf("short code = %q, want 05", got[0])
	}
	if got := parseHexCodes([]string{"?? 40"}); len(got) != 2 || got[0] != "??" {
		t.Errorf("unreadable token = %v, want it kept in place", got)
	}
}

// SF() and GE() each occupy a buffer position; SA() does not — it modifies the
// characters that follow it. Counting SA as a cell puts everything after the
// first coloured run one column left of where the host wrote it, which is the
// exact class of bug this endpoint exists to detect in somebody else's code.
func TestParseReadBufferCountsOnlyThePositionsThatAreOne(t *testing.T) {
	rows := parseReadBuffer([]string{
		"SF(c0=e8) 41 42 SA(42=f2) 43 GE(44) 45",
	})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	cells := rows[0]
	if len(cells) != 6 {
		t.Fatalf("cells = %d, want 6 (SA occupies no position)", len(cells))
	}
	if !cells[0].startField || cells[0].attr != 0xe8 {
		t.Errorf("cell 0 = %+v, want a start field with attribute e8", cells[0])
	}
	if cells[3].code != "43" {
		t.Errorf("cell 3 = %q, want 43 — the SA token should not have shifted it", cells[3].code)
	}
	if cells[4].code != "44" {
		t.Errorf("cell 4 = %q, want the graphic-escape character 44", cells[4].code)
	}
}

func TestParseFieldAttributeReadsTheC0Byte(t *testing.T) {
	if got := parseFieldAttribute("SF(c0=e8)"); got != 0xe8 {
		t.Errorf("attribute = %02x, want e8", got)
	}
	if got := parseFieldAttribute("SF(c0=c0,41=f2)"); got != 0xc0 {
		t.Errorf("attribute with extras = %02x, want c0", got)
	}
	// A token carrying no c0 reads as zero: unprotected, normal intensity. That
	// is what a caller would conclude from a screen with no attributes at all,
	// and safer than inventing a protection nobody stated.
	if got := parseFieldAttribute("SF(41=f2)"); got != 0 {
		t.Errorf("attribute without c0 = %02x, want 0", got)
	}
}

// A zero code is the buffer saying nothing was written here. Rendering it as a
// space is what keeps a field's text the width the application drew it.
func TestDecodeCellsRendersUnwrittenCellsAsSpaces(t *testing.T) {
	if got := decodeCells([]string{"48", "49", "00", "21"}); got != "HI !" {
		t.Errorf("decodeCells = %q, want %q", got, "HI !")
	}
	if got := decodeCells([]string{"zz"}); got != " " {
		t.Errorf("unreadable code decoded as %q, want a space", got)
	}
}

// readBufferFixture is a two-row screen holding three fields: a protected
// label reading "HI", an unprotected one holding "ID", and a protected one
// opening the second row and running to the bottom of the buffer.
//
//	row 0:  SF(protected) H I SF(unprotected) I D
//	row 1:  SF(protected) then five unwritten cells
func readBufferFixture() [][]bufferCell {
	return parseReadBuffer([]string{
		"SF(c0=f0) 48 49 SF(c0=c0) 49 44",
		"SF(c0=f0) 00 00 00 00 00",
	})
}

func TestFieldAtReportsContentStartNotTheAttributeByte(t *testing.T) {
	field, err := fieldAt(readBufferFixture(), nil, 0, 4)
	if err != nil {
		t.Fatalf("fieldAt: %v", err)
	}
	// The attribute sits at column 3; the field's content starts at column 4.
	// Reporting the attribute's position as the start of the field is the
	// off-by-one every 3270 field calculation gets wrong at least once.
	if field.Row != 0 || field.Col != 4 {
		t.Errorf("start = row %d col %d, want row 0 col 4", field.Row, field.Col)
	}
	if field.Text != "ID" {
		t.Errorf("text = %q, want ID", field.Text)
	}
	if field.Attribute != "c0" {
		t.Errorf("attribute = %q, want c0", field.Attribute)
	}
	if field.Protected {
		t.Error("c0 has no protect bit set; the field should be reported unprotected")
	}
	if !field.Formatted {
		t.Error("a screen with attribute bytes in it is formatted")
	}
}

// A 3270 field runs to the cell before the next attribute byte, wrapping past
// the bottom of the display and back to the top. The field a position belongs
// to is genuinely the one that started before it, even when "before" means the
// last row.
func TestFieldAtWrapsPastTheEndOfTheBuffer(t *testing.T) {
	field, err := fieldAt(readBufferFixture(), nil, 1, 3)
	if err != nil {
		t.Fatalf("fieldAt: %v", err)
	}
	if field.Row != 1 || field.Col != 1 {
		t.Errorf("start = row %d col %d, want the last field at row 1 col 1", field.Row, field.Col)
	}
	// Five content cells to the bottom of the buffer, and then it stops — the
	// next attribute byte is the one at the very top, which the field wraps
	// around to rather than running past.
	if field.Length != 5 {
		t.Errorf("length = %d, want 5 — the field ends at the attribute byte it wraps back around to", field.Length)
	}
}

func TestFieldAtReadsTheProtectedFieldsAttributes(t *testing.T) {
	field, err := fieldAt(readBufferFixture(), nil, 0, 1)
	if err != nil {
		t.Fatalf("fieldAt: %v", err)
	}
	if !field.Protected {
		t.Error("f0 sets the protect bit; the field should be reported protected")
	}
	if field.Text != "HI" {
		t.Errorf("text = %q, want HI", field.Text)
	}
	if field.Length != 2 {
		t.Errorf("length = %d, want 2", field.Length)
	}
}

// The whole point of the EBCDIC read is that it reports what the host sent
// rather than what the display shows, so the codes have to come from the EBCDIC
// buffer and line up with the field the ASCII one located.
func TestFieldAtTakesItsCodesFromTheHostsBuffer(t *testing.T) {
	ebcdic := parseReadBuffer([]string{
		"SF(c0=f0) c8 c9 SF(c0=c0) c9 c4",
		"SF(c0=f0) 40 40 40 40 40",
	})
	field, err := fieldAt(readBufferFixture(), ebcdic, 0, 4)
	if err != nil {
		t.Fatalf("fieldAt: %v", err)
	}
	if len(field.Codes) != 2 || field.Codes[0] != "c9" || field.Codes[1] != "c4" {
		t.Errorf("codes = %v, want the host's own [c9 c4]", field.Codes)
	}
	if field.Text != "ID" {
		t.Errorf("text = %q, want the display's ID alongside the host's codes", field.Text)
	}
}

// Handing back the display's code points labelled as the host's would be a
// plausible list of bytes under a name that promises where they came from —
// worse than an absent field on the one read whose purpose is telling those two
// apart.
func TestFieldAtOmitsCodesWhenTheHostsBufferIsUnavailable(t *testing.T) {
	field, err := fieldAt(readBufferFixture(), nil, 0, 4)
	if err != nil {
		t.Fatalf("fieldAt: %v", err)
	}
	if len(field.Codes) != 0 {
		t.Errorf("codes = %v, want none when the terminal did not report the host's buffer", field.Codes)
	}
}

// An unformatted screen has no fields. Reporting the whole buffer as one
// unprotected field is what an unformatted 3270 screen actually is, and a
// better answer to a reasonable question than an error.
func TestFieldAtTreatsAnUnformattedScreenAsOneField(t *testing.T) {
	cells := parseReadBuffer([]string{"48 49", "20 21"})
	field, err := fieldAt(cells, nil, 1, 1)
	if err != nil {
		t.Fatalf("fieldAt: %v", err)
	}
	if field.Formatted {
		t.Error("a buffer with no attribute bytes is unformatted")
	}
	if field.Row != 0 || field.Col != 0 || field.Length != 4 {
		t.Errorf("field = row %d col %d length %d, want the whole buffer", field.Row, field.Col, field.Length)
	}
	if field.Protected {
		t.Error("an unformatted screen is not protected")
	}
}

func TestFieldAtRefusesPositionsOffTheScreen(t *testing.T) {
	cells := readBufferFixture()
	if _, err := fieldAt(cells, nil, 9, 0); err == nil {
		t.Error("a row past the bottom of the screen should be refused")
	}
	if _, err := fieldAt(cells, nil, 0, 99); err == nil {
		t.Error("a column past the right edge should be refused")
	}
	if _, err := fieldAt(nil, nil, 0, 0); err == nil {
		t.Error("an empty buffer should be refused rather than reported as a field")
	}
}

// A build that trims trailing blanks off a row would otherwise shift every cell
// below it, and every field boundary with them.
func TestFlattenCellsPadsShortRows(t *testing.T) {
	rows := parseReadBuffer([]string{"41 42 43", "44"})
	flat, cols := flattenCells(rows)
	if cols != 3 {
		t.Fatalf("cols = %d, want 3", cols)
	}
	if len(flat) != 6 {
		t.Fatalf("flat = %d cells, want 6", len(flat))
	}
	if flat[3].code != "44" {
		t.Errorf("first cell of the short row = %q, want 44", flat[3].code)
	}
}
