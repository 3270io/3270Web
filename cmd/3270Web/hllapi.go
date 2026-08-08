package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
)

// A compatibility surface for screen-scrapers written against HLLAPI.
//
// There is a great deal of working code out there that drives a terminal by
// calling numbered functions with a presentation-space position and reading a
// return code. Rewriting it against a REST API is not hard, but it is a
// rewrite of every call site, and the reason those programs still exist is
// that nobody has time to rewrite them. This endpoint lets them be ported by
// changing how a call is made rather than what the program does.
//
// What it reproduces is the *shape*, because the shape is what the calling
// code is built around:
//
//   - Positions are one-based and linear. Position 81 on a 24x80 screen is
//     row 2, column 1. Every HLLAPI program does this arithmetic, and giving
//     it a different origin would mean auditing every offset in it.
//   - Every call answers with a return code. 0 is success, 24 is "string not
//     found", 4 is "the keyboard is locked". A program that branches on those
//     numbers keeps its branches.
//   - SendKey takes a string of text with mnemonics embedded in it, so
//     "SMITH@E" still means what it always meant.
//
// What it does not reproduce is a session-management model — a 3270Web
// session is named in the path, so Connect and Disconnect have nothing left
// to do — or any function whose meaning depends on a particular vendor's
// emulator. A function this endpoint does not implement is refused by number,
// which is the one answer a caller can act on; guessing at it would produce a
// return code of 0 for something that did not happen.

// HLLAPI return codes. These are the classic values, and the values are the
// interface: a program tests for 24 to mean "not found", so 24 is what a
// failed search has to answer.
const (
	hllapiOK              = 0  // the call did what was asked
	hllapiNotConnected    = 1  // there is no presentation space
	hllapiParameterError  = 2  // a parameter was missing, malformed or out of range
	hllapiInhibited       = 4  // the keyboard is locked; the host has not finished
	hllapiInvalidPosition = 7  // the position is outside the presentation space
	hllapiSystemError     = 9  // the terminal could not carry the call out
	hllapiNotFound        = 24 // the string was not found
	hllapiUnsupported     = 25 // this function is not implemented here
)

// hllapiFunction is one supported call.
type hllapiFunction struct {
	Number int
	Name   string
}

// hllapiFunctions is what this endpoint implements, by number and by name.
// Names are the conventional ones so that a port can search for the call it
// used to make.
var hllapiFunctions = []hllapiFunction{
	{1, "ConnectPresentationSpace"},
	{2, "DisconnectPresentationSpace"},
	{3, "SendKey"},
	{4, "Wait"},
	{5, "CopyPresentationSpace"},
	{6, "SearchPresentationSpace"},
	{7, "QueryCursorLocation"},
	{8, "CopyPresentationSpaceToString"},
	{15, "CopyStringToPresentationSpace"},
	{31, "FindFieldPosition"},
	{32, "FindFieldLength"},
	{33, "CopyStringToField"},
	{34, "CopyFieldToString"},
	{40, "SetCursor"},
}

// resolveHLLAPIFunction accepts either the number or the name, because ported
// code has one and a person reading the port wants the other.
func resolveHLLAPIFunction(raw interface{}) (hllapiFunction, bool) {
	switch v := raw.(type) {
	case float64:
		for _, f := range hllapiFunctions {
			if f.Number == int(v) {
				return f, true
			}
		}
	case string:
		trimmed := strings.TrimSpace(v)
		if n, err := strconv.Atoi(trimmed); err == nil {
			return resolveHLLAPIFunction(float64(n))
		}
		for _, f := range hllapiFunctions {
			if strings.EqualFold(f.Name, trimmed) {
				return f, true
			}
		}
	}
	return hllapiFunction{}, false
}

// hllapiMnemonics is the subset of the mnemonic alphabet this endpoint
// translates.
//
// It is a subset on purpose. The mnemonic tables differ between vendors past
// the common core, and a mnemonic mapped to the wrong key is worse than one
// that is not mapped at all: the wrong key reaches the host and the program
// carries on believing it did what it asked. So the core is translated,
// anything else is refused with a parameter error, and a caller that needs a
// key outside the table can name it — "PF13", "SysReq" — which the rest of
// this application already understands.
var hllapiMnemonics = map[string]string{
	"@E": "Enter",
	"@C": "Clear",
	"@T": "Tab",
	"@B": "BackTab",
	"@R": "Reset",
	"@F": "EraseEOF",
	"@1": "PF1", "@2": "PF2", "@3": "PF3", "@4": "PF4",
	"@5": "PF5", "@6": "PF6", "@7": "PF7", "@8": "PF8",
	"@9": "PF9", "@a": "PF10", "@b": "PF11", "@c": "PF12",
	"@x": "PA1", "@y": "PA2", "@z": "PA3",
}

// hllapiToken is one piece of a SendKey string: either literal text to type
// or a key to press.
type hllapiToken struct {
	Text string
	Key  string
}

// parseHLLAPIKeys splits a SendKey string into what to type and what to
// press, in the order they were given. "SMITH@E" is two tokens, and the order
// is the whole meaning of the call.
func parseHLLAPIKeys(s string) ([]hllapiToken, error) {
	tokens := make([]hllapiToken, 0, 4)
	var literal strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '@' {
			literal.WriteRune(runes[i])
			continue
		}
		if i+1 >= len(runes) {
			return nil, fmt.Errorf("the string ends with a bare @; a mnemonic needs a letter after it")
		}
		// "@@" is a literal at-sign, which is how a program types an email
		// address into a field.
		if runes[i+1] == '@' {
			literal.WriteRune('@')
			i++
			continue
		}
		mnemonic := string(runes[i : i+2])
		key, known := hllapiMnemonics[mnemonic]
		if !known {
			return nil, fmt.Errorf("%q is not a mnemonic this endpoint translates; name the key instead, e.g. \"PF13\"", mnemonic)
		}
		if literal.Len() > 0 {
			tokens = append(tokens, hllapiToken{Text: literal.String()})
			literal.Reset()
		}
		tokens = append(tokens, hllapiToken{Key: key})
		i++
	}
	if literal.Len() > 0 {
		tokens = append(tokens, hllapiToken{Text: literal.String()})
	}
	return tokens, nil
}

// hllapiScreenSize returns the geometry the position arithmetic is done
// against.
func hllapiScreenSize(s *host.Screen) (rows, cols int) {
	rows, cols = s.Height, s.Width
	if rows <= 0 {
		rows = 24
	}
	if cols <= 0 {
		cols = 80
	}
	return rows, cols
}

// positionToRowCol converts a one-based linear position into the zero-based
// row and column the rest of this application uses.
func positionToRowCol(pos, cols int) (row, col int) {
	zero := pos - 1
	return zero / cols, zero % cols
}

// rowColToPosition is the inverse.
func rowColToPosition(row, col, cols int) int {
	return row*cols + col + 1
}

// screenRunes flattens the screen into one row-major slice, padded to the
// full rectangle. HLLAPI addresses the presentation space as a single string,
// so this is the form every position in this file indexes into.
func screenRunes(s *host.Screen) []rune {
	rows, cols := hllapiScreenSize(s)
	out := make([]rune, rows*cols)
	for i := range out {
		out[i] = ' '
	}
	for y := 0; y < rows && y < len(s.Buffer); y++ {
		for x := 0; x < cols && x < len(s.Buffer[y]); x++ {
			r := s.Buffer[y][x]
			if r == 0 {
				r = ' '
			}
			out[y*cols+x] = r
		}
	}
	return out
}

// hllapiRequest is the body every call sends. Only the fields a given
// function uses are read.
type hllapiRequest struct {
	Function interface{} `json:"function"`
	Text     string      `json:"text"`
	Position int         `json:"position"`
	Length   int         `json:"length"`
}

// APIHLLAPI handles POST /api/v1/sessions/:id/hllapi.
//
// It answers 200 with a return code in the body rather than mapping return
// codes onto HTTP statuses. That is the point of the endpoint: a caller
// ported from HLLAPI branches on the return code, and a transport-level
// failure for "the string was not found" would be a different program. The
// HTTP status is about whether the call was delivered; `rc` is about what the
// terminal said.
func (app *App) APIHLLAPI(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	var body hllapiRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	fn, known := resolveHLLAPIFunction(body.Function)
	if !known {
		names := make([]string, 0, len(hllapiFunctions))
		for _, f := range hllapiFunctions {
			names = append(names, fmt.Sprintf("%d %s", f.Number, f.Name))
		}
		c.JSON(http.StatusOK, gin.H{
			"rc":        hllapiUnsupported,
			"error":     fmt.Sprintf("function %v is not implemented here", body.Function),
			"functions": names,
		})
		return
	}

	h := app.sessionHost(s)
	if h == nil || !h.IsConnected() {
		c.JSON(http.StatusOK, gin.H{"rc": hllapiNotConnected, "function": fn.Name, "error": errSessionNotConnected.Error()})
		return
	}

	result := app.runHLLAPI(h, fn, body)
	result["function"] = fn.Name
	result["number"] = fn.Number
	c.JSON(http.StatusOK, result)
}

// runHLLAPI dispatches one call and returns the body to send back. Every path
// out of it sets "rc".
func (app *App) runHLLAPI(h host.Host, fn hllapiFunction, body hllapiRequest) gin.H {
	// Connect and Disconnect are the two functions with nothing to do: the
	// session is named in the path, so it is already connected by the time
	// this runs, and disconnecting it is what DELETE /sessions/:id is for.
	// They answer 0 rather than "unsupported" so that ported code's opening
	// and closing calls do not have to be deleted before it will run.
	switch fn.Number {
	case 1, 2:
		return gin.H{"rc": hllapiOK}
	}

	if err := h.UpdateScreen(); err != nil {
		return gin.H{"rc": hllapiSystemError, "error": err.Error()}
	}
	screen := hostScreenSnapshot(h)
	if screen == nil {
		return gin.H{"rc": hllapiSystemError, "error": "the screen is unavailable"}
	}
	rows, cols := hllapiScreenSize(screen)
	size := rows * cols

	switch fn.Number {
	case 3: // SendKey
		tokens, err := parseHLLAPIKeys(body.Text)
		if err != nil {
			return gin.H{"rc": hllapiParameterError, "error": err.Error()}
		}
		if len(tokens) == 0 {
			return gin.H{"rc": hllapiParameterError, "error": "text is required"}
		}
		for _, token := range tokens {
			if token.Key != "" {
				key, ok := normalizeKey(token.Key)
				if !ok {
					return gin.H{"rc": hllapiParameterError, "error": fmt.Sprintf("unrecognized key %q", token.Key)}
				}
				// Field writes are local until an AID key is sent, so a
				// pending write has to be submitted with it — otherwise
				// "SMITH@E" types SMITH and then presses Enter on a screen
				// that never received it.
				if err := h.SubmitScreen(); err != nil {
					return gin.H{"rc": hllapiSystemError, "error": err.Error()}
				}
				if err := h.SendKey(key); err != nil {
					return gin.H{"rc": hllapiSystemError, "error": err.Error()}
				}
				continue
			}
			if host.ContainsForbiddenFieldText(token.Text) {
				return gin.H{"rc": hllapiParameterError, "error": "text must not contain CR/LF/TAB"}
			}
			row, col, ok := screen.StatusCursor()
			if !ok {
				row, col = screen.CursorY, screen.CursorX
			}
			if err := h.WriteStringAt(row, col, token.Text); err != nil {
				return gin.H{"rc": hllapiSystemError, "error": err.Error()}
			}
		}
		return gin.H{"rc": hllapiOK}

	case 4: // Wait — for the host to finish and the keyboard to unlock
		if err := h.UpdateScreen(); err != nil {
			return gin.H{"rc": hllapiSystemError, "error": err.Error()}
		}
		fresh := hostScreenSnapshot(h)
		if locked, known := fresh.StatusKeyboardLocked(); known && locked {
			return gin.H{"rc": hllapiInhibited}
		}
		return gin.H{"rc": hllapiOK}

	case 5, 8: // CopyPresentationSpace / ...ToString
		start := 1
		length := size
		if fn.Number == 8 {
			if body.Position != 0 {
				start = body.Position
			}
			if body.Length > 0 {
				length = body.Length
			}
		}
		if start < 1 || start > size {
			return gin.H{"rc": hllapiInvalidPosition, "error": positionOutOfRange(start, size)}
		}
		if start+length-1 > size {
			length = size - start + 1
		}
		runes := screenRunes(screen)
		return gin.H{
			"rc":       hllapiOK,
			"data":     string(runes[start-1 : start-1+length]),
			"position": start,
			"length":   length,
			"rows":     rows,
			"cols":     cols,
		}

	case 6: // SearchPresentationSpace
		if body.Text == "" {
			return gin.H{"rc": hllapiParameterError, "error": "text is required"}
		}
		start := 1
		if body.Position != 0 {
			start = body.Position
		}
		if start < 1 || start > size {
			return gin.H{"rc": hllapiInvalidPosition, "error": positionOutOfRange(start, size)}
		}
		haystack := string(screenRunes(screen))
		// Indexing over runes rather than bytes, so a position is a screen
		// cell rather than a UTF-8 offset.
		hay := []rune(haystack)
		idx := indexRunes(hay[start-1:], []rune(body.Text))
		if idx < 0 {
			return gin.H{"rc": hllapiNotFound}
		}
		return gin.H{"rc": hllapiOK, "position": start + idx}

	case 7: // QueryCursorLocation
		row, col, ok := screen.StatusCursor()
		if !ok {
			row, col = screen.CursorY, screen.CursorX
		}
		return gin.H{
			"rc":       hllapiOK,
			"position": rowColToPosition(row, col, cols),
			"row":      row + 1,
			"col":      col + 1,
		}

	case 15, 33: // CopyStringToPresentationSpace / ...ToField
		if body.Position < 1 || body.Position > size {
			return gin.H{"rc": hllapiInvalidPosition, "error": positionOutOfRange(body.Position, size)}
		}
		if body.Text == "" {
			return gin.H{"rc": hllapiParameterError, "error": "text is required"}
		}
		if host.ContainsForbiddenFieldText(body.Text) {
			return gin.H{"rc": hllapiParameterError, "error": "text must not contain CR/LF/TAB"}
		}
		row, col := positionToRowCol(body.Position, cols)
		if fn.Number == 33 {
			// CopyStringToField writes from the *start* of the field the
			// position falls in, which is what makes it a different function
			// from writing at the position itself.
			field := screen.GetInputFieldAt(col, row)
			if field == nil {
				return gin.H{"rc": hllapiInvalidPosition, "error": "there is no input field at that position"}
			}
			row, col = field.StartY, field.StartX
		}
		if err := h.WriteStringAt(row, col, body.Text); err != nil {
			return gin.H{"rc": hllapiSystemError, "error": err.Error()}
		}
		return gin.H{"rc": hllapiOK, "position": rowColToPosition(row, col, cols)}

	case 31, 32, 34: // FindFieldPosition / FindFieldLength / CopyFieldToString
		if body.Position < 1 || body.Position > size {
			return gin.H{"rc": hllapiInvalidPosition, "error": positionOutOfRange(body.Position, size)}
		}
		row, col := positionToRowCol(body.Position, cols)
		field := screen.GetInputFieldAt(col, row)
		if field == nil {
			return gin.H{"rc": hllapiNotFound, "error": "there is no input field at that position"}
		}
		start := rowColToPosition(field.StartY, field.StartX, cols)
		length := rowColToPosition(field.EndY, field.EndX, cols) - start + 1
		switch fn.Number {
		case 31:
			return gin.H{"rc": hllapiOK, "position": start, "length": length}
		case 32:
			return gin.H{"rc": hllapiOK, "length": length}
		}
		// A hidden field is a password field. Its contents are redacted
		// everywhere else this application hands a screen out, and this
		// endpoint is reachable by any token holder, so it is redacted here
		// too rather than becoming the one way around that.
		if field.IsHidden() {
			return gin.H{"rc": hllapiOK, "position": start, "length": length, "data": strings.Repeat("*", length), "hidden": true}
		}
		return gin.H{"rc": hllapiOK, "position": start, "length": length, "data": field.GetValue()}

	case 40: // SetCursor
		if body.Position < 1 || body.Position > size {
			return gin.H{"rc": hllapiInvalidPosition, "error": positionOutOfRange(body.Position, size)}
		}
		row, col := positionToRowCol(body.Position, cols)
		if err := h.MoveCursor(row, col); err != nil {
			return gin.H{"rc": hllapiSystemError, "error": err.Error()}
		}
		return gin.H{"rc": hllapiOK, "position": body.Position}
	}

	return gin.H{"rc": hllapiUnsupported}
}

// positionOutOfRange phrases the one mistake every port makes: an off-by-one
// from a zero-based API, or a position computed for a different screen size.
func positionOutOfRange(pos, size int) string {
	return fmt.Sprintf("position %d is outside the presentation space, which is 1..%d — positions here are one-based", pos, size)
}

// indexRunes is strings.Index over runes, so the answer is a count of screen
// cells rather than of bytes.
func indexRunes(haystack, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
