package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// Every other screen-reading endpoint here serves the parsed screen: the
// terminal is asked for the display, this side turns it into a buffer and a
// list of fields, and the response is cut out of that. These two serve the
// terminal's own buffer instead, and they exist for the one question the parsed
// screen cannot answer — whether the parse is what went wrong.
//
// That question comes up as a code page complaint, and it always arrives the
// same way: a pound sign showing as a hash, an umlaut taking two cells, a field
// reported one column wider than the application drew it. What settles it is
// the byte the host actually transmitted, and only the terminal has that.
// /buffer returns a region as the display shows it or as the host's own code
// points; /buffer/field returns the field a position falls in, with the
// attribute byte the terminal read off the wire rather than the one this side
// reconstructed.
//
// Nothing here is a faster way to read a screen, and none of it is wired into
// the paths that serve screens. A second opinion is only worth having when it
// comes from somewhere other than the thing being checked.
//
// One rule from the ordinary path does carry over, and has to. 3270 "hidden"
// only suppresses local echo on a real terminal — the characters an operator
// typed into a password field are still in the buffer, so a read that went
// straight to the terminal would be the one way around the redaction every
// other serializer applies. See screen_redaction.go; the masking below is that
// file's rule, applied to a region rather than to a screen.

// redactedCodePlaceholder stands in for the code point of a hidden cell. Two
// characters, so the list still lines up cell for cell with the region that was
// asked for, and not hex, so nothing downstream can mistake it for a byte the
// host sent.
const redactedCodePlaceholder = "**"

// bufferReader is the part of a host that can read its own buffer back.
// Declared here rather than added to host.Host so the doubles in other
// packages, which only ever drive a terminal, do not have to grow two methods
// to satisfy it.
type bufferReader interface {
	ReadRegion(row, col, length int, enc host.RegionEncoding) (*host.RegionRead, error)
	ReadFieldAt(row, col int) (*host.BufferField, error)
}

// sessionBufferReader resolves the session's host to a bufferReader, or writes
// the reason it could not and returns nil.
func (app *App) sessionBufferReader(c *gin.Context, s *session.Session) bufferReader {
	h := app.sessionHost(s)
	if h == nil {
		c.JSON(http.StatusConflict, gin.H{"error": errSessionHostMissing.Error()})
		return nil
	}
	if !h.IsConnected() {
		c.JSON(http.StatusConflict, gin.H{"error": errSessionNotConnected.Error()})
		return nil
	}
	r, ok := h.(bufferReader)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "this terminal cannot report its own buffer"})
		return nil
	}
	return r
}

// oneBasedParam reads a coordinate from the query string.
//
// Positions on this API are one-based, as they are everywhere an operator or a
// recording quotes one — row 1 column 1 is the top-left cell. The terminal
// counts from zero, and the conversion happens here rather than in the wrapper,
// so there is exactly one place in the request path where the origin changes.
func oneBasedParam(c *gin.Context, name string) (int, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", name)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s must be 1 or greater — positions here are one-based", name)
	}
	return n - 1, nil
}

// APIReadBuffer handles GET /api/v1/sessions/:id/buffer.
//
// ?row=&col=&length= names the region, one-based, and ?encoding=ebcdic asks for
// the host's own code points instead of the characters the display is showing.
func (app *App) APIReadBuffer(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	row, err := oneBasedParam(c, "row")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	col, err := oneBasedParam(c, "col")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	length, err := strconv.Atoi(strings.TrimSpace(c.Query("length")))
	if err != nil || length < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "length must be a number of 1 or greater"})
		return
	}
	// The wrapper refuses this too, but its refusal is a plain error rather than
	// a refusal from the terminal, and everything below treats those as a
	// failure of the terminal. Checking here keeps a caller's arithmetic mistake
	// a 400 with the ceiling in it rather than a 502 blaming the host.
	if length > host.MaxRegionLength {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("length %d is larger than the largest screen this terminal supports (%d cells)", length, host.MaxRegionLength),
		})
		return
	}

	enc := host.RegionASCII
	switch strings.ToLower(strings.TrimSpace(c.Query("encoding"))) {
	case "", "ascii":
	case "ebcdic":
		enc = host.RegionEBCDIC
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unsupported encoding %q (allowed: ascii, ebcdic)", c.Query("encoding"))})
		return
	}

	reader := app.sessionBufferReader(c, s)
	if reader == nil {
		return
	}
	region, err := reader.ReadRegion(row, col, length, enc)
	if err != nil {
		// A refusal from the terminal is the answer to the question, not a
		// failure of this server: a region past the end of the screen is a
		// caller's arithmetic, and saying so with the terminal's own words is
		// more use than a 502.
		if host.IsActionError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	if !redactRegion(region, app.sessionHost(s).GetScreenSnapshot()) {
		c.JSON(http.StatusConflict, gin.H{
			"error": "this session has no parsed screen to check the region against, so it cannot be shown to contain no hidden field; read the screen first",
		})
		return
	}
	c.JSON(http.StatusOK, regionResponse(region))
}

// regionResponse quotes the region back one-based, matching how it was asked
// for. A response that answered a question about row 5 with "row 4" would be
// correct and useless.
func regionResponse(r *host.RegionRead) gin.H {
	out := gin.H{
		"row":      r.Row + 1,
		"col":      r.Col + 1,
		"length":   r.Length,
		"encoding": r.Encoding,
	}
	if r.Codes != nil {
		out["codes"] = r.Codes
	} else {
		out["text"] = r.Text
	}
	return out
}

// redactRegion masks the cells of a region that fall inside a hidden field, and
// reports whether it was able to decide.
//
// The extents come from the parsed screen, which is the only thing on this side
// that knows where the hidden fields are — the region read deliberately does
// not go through it. So the two views are reconciled here: the terminal says
// what is in the cells, and the parse says which of them may be shown.
//
// When there is no usable parse the answer is false rather than a region served
// unmasked. This side not knowing where the password field is does not make it
// safe to publish the cells it might be in, and "I could not check" is a better
// thing for a caller to receive than a plausible-looking region that might have
// somebody's password in the middle of it.
func redactRegion(r *host.RegionRead, screen *host.Screen) bool {
	if r == nil {
		return true
	}
	if screen == nil || screen.Width <= 0 || screen.Height <= 0 {
		return false
	}
	hidden := hiddenCells(screen)
	if len(hidden) == 0 {
		return true
	}
	size := screen.Width * screen.Height
	start := r.Row*screen.Width + r.Col
	// Modulo the buffer size: a region long enough to run off the bottom of the
	// display carries on from the top, as the terminal's own buffer does, and an
	// unwrapped index would stop matching hidden cells at exactly the point the
	// region started covering them again.
	at := func(i int) bool { return hidden[((start+i)%size+size)%size] }

	if r.Codes != nil {
		for i := range r.Codes {
			if at(i) {
				r.Codes[i] = redactedCodePlaceholder
			}
		}
		return true
	}
	// Text is masked rune by rune. The newlines a wrapped region comes back
	// with are separators rather than cells, so they are stepped over without
	// consuming a buffer position — counting them as cells would shift the mask
	// one place further left for every row the region crosses.
	runes := []rune(r.Text)
	cell := 0
	for i, ch := range runes {
		if ch == '\n' {
			continue
		}
		if at(cell) {
			runes[i] = '*'
		}
		cell++
	}
	r.Text = string(runes)
	return true
}

// APIReadBufferField handles GET /api/v1/sessions/:id/buffer/field.
//
// ?row=&col= names a position, one-based, and the answer is the field that
// position falls in as the terminal's own buffer describes it — where it
// starts, how long it is, and the attribute byte that opens it.
func (app *App) APIReadBufferField(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	row, err := oneBasedParam(c, "row")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	col, err := oneBasedParam(c, "col")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	reader := app.sessionBufferReader(c, s)
	if reader == nil {
		return
	}
	field, err := reader.ReadFieldAt(row, col)
	if err != nil {
		if host.IsActionError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, bufferFieldResponse(field))
}

// bufferFieldResponse quotes a field back one-based, and withholds the contents
// of a hidden one.
//
// The hiding is decided by the attribute byte the terminal reported, not by the
// parsed screen: this endpoint's whole claim is that it reads the buffer
// directly, and a password field would be the worst possible place for that
// claim to hold in one direction only.
func bufferFieldResponse(f *host.BufferField) gin.H {
	out := gin.H{
		"row":         f.Row + 1,
		"col":         f.Col + 1,
		"length":      f.Length,
		"attribute":   f.Attribute,
		"protected":   f.Protected,
		"numeric":     f.Numeric,
		"hidden":      f.Hidden,
		"intensified": f.Intensified,
		"modified":    f.Modified,
		"formatted":   f.Formatted,
	}
	if f.Hidden {
		out["text"] = strings.Repeat("*", f.Length)
		return out
	}
	out["text"] = f.Text
	if len(f.Codes) > 0 {
		out["codes"] = f.Codes
	}
	return out
}
