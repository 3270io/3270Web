// SPDX-License-Identifier: AGPL-3.0-or-later

package host

// A scripted TN3270 host, for testing this package against the terminal it
// actually drives rather than against a transcript of what the terminal once
// said.
//
// The difference matters more here than it looks. Every other test in this
// package feeds the decoder a captured ReadBuffer response, which answers
// "does this parse" and nothing else — a captured line cannot tell you that
// the terminal spells blink 41=f1 rather than 0x80, because the capture was
// written by whoever wrote the test and agrees with them by construction. A
// terminal-fidelity bug is precisely a disagreement between what the terminal
// says and what this code believes it says, so it survives that kind of test
// indefinitely.
//
// What this harness does instead is write a real 3270 data stream at a real
// s3270, read the screen back through this package's own S3270 type, and
// assert on the decoded result. It also keeps every inbound record the
// terminal sends, so the other half of the conversation — which fields report
// as modified, which AID byte a key produces, where the cursor was said to be
// — is testable too.
//
// A build with no s3270 to run skips rather than fails. The binary is looked
// for in the repository, on PATH, and wherever S3270_TEST_BINARY points.

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Telnet, only as much of it as a 3270 session needs.
const (
	tnIAC  = 255
	tnDONT = 254
	tnDO   = 253
	tnWONT = 252
	tnWILL = 251
	tnSB   = 250
	tnSE   = 240
	tnEOR  = 239

	tnOptBinary   = 0
	tnOptEOR      = 25
	tnOptTermType = 24
)

// 3270 commands and orders.
const (
	cmdEraseWrite    = 0xF5
	cmdEraseWriteAlt = 0x7E
	cmdWrite         = 0xF1

	orderSBA = 0x11
	orderSF  = 0x1D
	orderSFE = 0x29
	orderSA  = 0x28
	orderGE  = 0x08
	orderIC  = 0x13
	orderPT  = 0x05
	orderRA  = 0x3C
	orderEUA = 0x12

	// wccRestoreReset unlocks the keyboard and clears every modified-data tag,
	// which is what a host writes when it wants a fresh screen rather than an
	// addition to the last one.
	wccRestoreReset = 0xC3
)

// AID bytes, as the terminal puts them at the front of an inbound record.
const (
	aidEnter  = 0x7D
	aidClear  = 0x6D
	aidPA1    = 0x6C
	aidPA2    = 0x6E
	aidPA3    = 0x6B
	aidPF1    = 0xF1
	aidPF3    = 0xF3
	aidPF12   = 0x7C
	aidPF13   = 0xC1
	aidPF24   = 0x4C
	aidNoAID  = 0x60
	aidSysReq = 0xF0
)

// bufferAddressCode maps the six bits of half a buffer address onto the byte
// the data stream carries it in.
var bufferAddressCode = []byte{
	0x40, 0xC1, 0xC2, 0xC3, 0xC4, 0xC5, 0xC6, 0xC7,
	0xC8, 0xC9, 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F,
	0x50, 0xD1, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7,
	0xD8, 0xD9, 0x5A, 0x5B, 0x5C, 0x5D, 0x5E, 0x5F,
	0x60, 0x61, 0xE2, 0xE3, 0xE4, 0xE5, 0xE6, 0xE7,
	0xE8, 0xE9, 0x6A, 0x6B, 0x6C, 0x6D, 0x6E, 0x6F,
	0xF0, 0xF1, 0xF2, 0xF3, 0xF4, 0xF5, 0xF6, 0xF7,
	0xF8, 0xF9, 0x7A, 0x7B, 0x7C, 0x7D, 0x7E, 0x7F,
}

// decodeBufferAddress reverses it, for reading the addresses in an inbound
// record. Twelve-bit addressing is what a model 2 through 5 uses.
func decodeBufferAddress(hi, lo byte) int {
	index := func(b byte) int {
		for i, c := range bufferAddressCode {
			if c == b {
				return i
			}
		}
		// A fourteen-bit address carries its bits directly rather than through
		// the table.
		return int(b & 0x3F)
	}
	return index(hi)<<6 | index(lo)
}

// ebcdicByASCII is the host code page for the characters these tests write.
// It is deliberately partial: a conformance test that needs a character
// outside this set is a test about code pages, and belongs with the code page
// tests rather than here.
var ebcdicByASCII = map[byte]byte{
	' ': 0x40, '.': 0x4B, '<': 0x4C, '(': 0x4D, '+': 0x4E, '|': 0x4F,
	'&': 0x50, '!': 0x5A, '$': 0x5B, '*': 0x5C, ')': 0x5D, ';': 0x5E,
	'-': 0x60, '/': 0x61, ',': 0x6B, '%': 0x6C, '_': 0x6D, '>': 0x6E,
	'?': 0x6F, ':': 0x7A, '#': 0x7B, '@': 0x7C, '\'': 0x7D, '=': 0x7E,
	'"': 0x7F,
}

func init() {
	for i := byte(0); i < 9; i++ {
		ebcdicByASCII['A'+i] = 0xC1 + i
		ebcdicByASCII['a'+i] = 0x81 + i
	}
	for i := byte(0); i < 9; i++ {
		ebcdicByASCII['J'+i] = 0xD1 + i
		ebcdicByASCII['j'+i] = 0x91 + i
	}
	for i := byte(0); i < 8; i++ {
		ebcdicByASCII['S'+i] = 0xE2 + i
		ebcdicByASCII['s'+i] = 0xA2 + i
	}
	for i := byte(0); i < 10; i++ {
		ebcdicByASCII['0'+i] = 0xF0 + i
	}
	for a, e := range ebcdicByASCII {
		asciiByEBCDIC[e] = a
	}
}

// asciiByEBCDIC is the reverse, for reading what the terminal sent back. It is
// filled in the same init as the letters and digits above, because a package
// variable is initialised before any init runs and would otherwise hold only
// the punctuation.
var asciiByEBCDIC = map[byte]byte{}

func toEBCDIC(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		b, ok := ebcdicByASCII[s[i]]
		if !ok {
			panic(fmt.Sprintf("tn3270 test host: no host code point for %q", s[i]))
		}
		out = append(out, b)
	}
	return out
}

func fromEBCDIC(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if a, ok := asciiByEBCDIC[c]; ok {
			sb.WriteByte(a)
			continue
		}
		if c == 0x00 {
			sb.WriteByte(' ')
			continue
		}
		sb.WriteString(fmt.Sprintf("<%02x>", c))
	}
	return sb.String()
}

// dataStream builds one outbound 3270 write. The methods chain, so a screen
// reads in the order the host writes it.
type dataStream struct {
	b    []byte
	cols int
}

// newScreen starts an Erase/Write: the whole display is cleared and rewritten,
// which is what an application does when it puts up a new panel.
func newScreen(cols int) *dataStream {
	return &dataStream{b: []byte{cmdEraseWrite, wccRestoreReset}, cols: cols}
}

// newAlternateScreen starts an Erase/Write Alternate, which additionally
// switches the display to its alternate size — the larger of the two a model
// has, and the one an oversize setting enlarges.
func newAlternateScreen(cols int) *dataStream {
	return &dataStream{b: []byte{cmdEraseWriteAlt, wccRestoreReset}, cols: cols}
}

// newUpdate starts a Write, which leaves the display alone except where it
// writes.
func newUpdate(cols int) *dataStream {
	return &dataStream{b: []byte{cmdWrite, wccRestoreReset}, cols: cols}
}

func (d *dataStream) at(row, col int) *dataStream {
	addr := row*d.cols + col
	d.b = append(d.b, orderSBA, bufferAddressCode[(addr>>6)&0x3F], bufferAddressCode[addr&0x3F])
	return d
}

// field opens a field with a plain attribute byte. The attribute occupies the
// position it is written at, which is why a field's text starts one column
// later than the order.
func (d *dataStream) field(attr byte) *dataStream {
	d.b = append(d.b, orderSF, bufferAddressCode[attr&0x3F])
	return d
}

// fieldExtended opens a field carrying extended attributes, given as
// type/value pairs — 0x41 highlighting, 0x42 foreground, 0x45 background.
func (d *dataStream) fieldExtended(attr byte, pairs ...byte) *dataStream {
	if len(pairs)%2 != 0 {
		panic("fieldExtended wants type/value pairs")
	}
	d.b = append(d.b, orderSFE, byte(len(pairs)/2+1), 0xC0, bufferAddressCode[attr&0x3F])
	d.b = append(d.b, pairs...)
	return d
}

// setAttribute changes the attributes of the characters written after it,
// without opening a field and without occupying a position.
func (d *dataStream) setAttribute(typ, value byte) *dataStream {
	d.b = append(d.b, orderSA, typ, value)
	return d
}

func (d *dataStream) text(s string) *dataStream {
	d.b = append(d.b, toEBCDIC(s)...)
	return d
}

// graphicEscape writes one character from the alternate character set — the
// box drawing and APL glyphs a panel is ruled with.
func (d *dataStream) graphicEscape(code byte) *dataStream {
	d.b = append(d.b, orderGE, code)
	return d
}

// cursor places the cursor where the stream currently is.
func (d *dataStream) cursor() *dataStream {
	d.b = append(d.b, orderIC)
	return d
}

// repeat fills to a position with one character, which is how a host rules a
// line without sending eighty bytes.
func (d *dataStream) repeat(toRow, toCol int, ch byte) *dataStream {
	addr := toRow*d.cols + toCol
	d.b = append(d.b, orderRA, bufferAddressCode[(addr>>6)&0x3F], bufferAddressCode[addr&0x3F], ch)
	return d
}

func (d *dataStream) bytes() []byte { return d.b }

// Field attribute bytes, spelled as the tests need them. The two high bits are
// set by the address encoding rather than carried here.
const (
	faProtected   = 0x20
	faNumeric     = 0x10
	faIntensified = 0x08
	faHidden      = 0x0C
	faModified    = 0x01
	faSkip        = faProtected | faNumeric
)

// inboundRecord is one thing the terminal sent: the key that was pressed,
// where it said the cursor was, and the fields it reported as modified.
type inboundRecord struct {
	AID    byte
	Cursor int
	Fields []inboundField
	Raw    []byte
}

type inboundField struct {
	Address int
	Text    string
	Raw     []byte
}

// parseInbound reads a Read Modified reply: an AID byte, the cursor address,
// then a run of SBA-addressed field contents.
func parseInbound(raw []byte) inboundRecord {
	rec := inboundRecord{Raw: append([]byte(nil), raw...)}
	if len(raw) == 0 {
		return rec
	}
	rec.AID = raw[0]
	// A short-read AID — PA keys and Clear — carries no cursor and no fields.
	if len(raw) < 3 {
		return rec
	}
	rec.Cursor = decodeBufferAddress(raw[1], raw[2])

	i := 3
	for i < len(raw) {
		if raw[i] != orderSBA || i+2 >= len(raw) {
			i++
			continue
		}
		addr := decodeBufferAddress(raw[i+1], raw[i+2])
		i += 3
		start := i
		for i < len(raw) && raw[i] != orderSBA {
			i++
		}
		rec.Fields = append(rec.Fields, inboundField{
			Address: addr,
			Text:    fromEBCDIC(raw[start:i]),
			Raw:     append([]byte(nil), raw[start:i]...),
		})
	}
	return rec
}

// conformanceHost is a TN3270 host that writes one scripted screen and keeps
// whatever the terminal says back.
type conformanceHost struct {
	t        *testing.T
	listener net.Listener

	mu       sync.Mutex
	inbound  []inboundRecord
	conns    []net.Conn
	closed   bool
	screens  [][]byte // written in order: the first on connect, the rest on each AID
	nextSend int
}

// startConformanceHost begins listening and returns the host. screens[0] is
// written once the session is negotiated; each further screen is written in
// reply to an inbound record, so a test can drive a two-screen flow.
func startConformanceHost(t *testing.T, screens ...[]byte) *conformanceHost {
	t.Helper()
	if len(screens) == 0 {
		t.Fatal("a conformance host needs at least one screen to write")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	h := &conformanceHost{t: t, listener: listener, screens: screens}
	go h.accept()
	t.Cleanup(h.close)
	return h
}

func (h *conformanceHost) addr() string {
	return h.listener.Addr().String()
}

func (h *conformanceHost) close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	conns := h.conns
	h.conns = nil
	h.mu.Unlock()

	_ = h.listener.Close()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (h *conformanceHost) accept() {
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			return
		}
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			_ = conn.Close()
			return
		}
		h.conns = append(h.conns, conn)
		h.mu.Unlock()
		go h.serve(conn)
	}
}

// serve negotiates a 3270 session and then relays screens and records.
func (h *conformanceHost) serve(conn net.Conn) {
	defer conn.Close()

	if _, err := conn.Write([]byte{tnIAC, tnDO, tnOptTermType}); err != nil {
		return
	}

	reader := &byteReader{conn: conn}
	binaryAgreed, eorAgreed, sent := false, false, false
	var record []byte

	for {
		c, ok := reader.next()
		if !ok {
			return
		}
		if c != tnIAC {
			record = append(record, c)
			continue
		}
		verb, ok := reader.next()
		if !ok {
			return
		}
		switch verb {
		case tnIAC:
			// An escaped 0xFF in the data stream.
			record = append(record, tnIAC)
			continue
		case tnEOR:
			if len(record) > 0 {
				h.mu.Lock()
				h.inbound = append(h.inbound, parseInbound(record))
				h.nextSend++
				next := h.nextSend
				h.mu.Unlock()
				record = nil
				if next < len(h.screens) {
					h.writeScreen(conn, h.screens[next])
				}
			}
			continue
		case tnWILL, tnWONT, tnDO, tnDONT:
			opt, ok := reader.next()
			if !ok {
				return
			}
			if verb == tnWILL && opt == tnOptTermType {
				// Ask for the terminal type, which is the reply that tells us
				// the client is ready to be offered binary and EOR.
				_, _ = conn.Write([]byte{tnIAC, tnSB, tnOptTermType, 1, tnIAC, tnSE})
			}
			if opt == tnOptBinary && (verb == tnWILL || verb == tnDO) {
				binaryAgreed = true
			}
			if opt == tnOptEOR && (verb == tnWILL || verb == tnDO) {
				eorAgreed = true
			}
		case tnSB:
			// The terminal-type reply. Drain it and offer the two options a
			// 3270 session runs on.
			for {
				c, ok := reader.next()
				if !ok {
					return
				}
				if c != tnIAC {
					continue
				}
				n, ok := reader.next()
				if !ok {
					return
				}
				if n == tnSE {
					break
				}
			}
			_, _ = conn.Write([]byte{
				tnIAC, tnDO, tnOptEOR, tnIAC, tnWILL, tnOptEOR,
				tnIAC, tnDO, tnOptBinary, tnIAC, tnWILL, tnOptBinary,
			})
		}
		if binaryAgreed && eorAgreed && !sent {
			h.writeScreen(conn, h.screens[0])
			sent = true
		}
	}
}

func (h *conformanceHost) writeScreen(conn net.Conn, screen []byte) {
	out := make([]byte, 0, len(screen)+2)
	for _, b := range screen {
		out = append(out, b)
		if b == tnIAC {
			out = append(out, tnIAC)
		}
	}
	out = append(out, tnIAC, tnEOR)
	_, _ = conn.Write(out)
}

// records returns what the terminal has sent so far.
func (h *conformanceHost) records() []inboundRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]inboundRecord(nil), h.inbound...)
}

// awaitRecord waits for the nth inbound record (1-based) and returns it.
func (h *conformanceHost) awaitRecord(n int) inboundRecord {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got := h.records()
		if len(got) >= n {
			return got[n-1]
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatalf("the terminal sent %d records, waiting for %d", len(h.records()), n)
	return inboundRecord{}
}

// byteReader reads the connection one byte at a time, which is the simplest
// correct way to walk a telnet stream.
type byteReader struct {
	conn net.Conn
	buf  [1]byte
}

func (r *byteReader) next() (byte, bool) {
	n, err := r.conn.Read(r.buf[:])
	if err != nil || n == 0 {
		return 0, false
	}
	return r.buf[0], true
}

// terminalBinary finds an s3270 to run, or "" when there is none.
//
// The repository ships one for the platform it is built for, which is what
// makes this suite run in CI without a package install. A build on some other
// platform, or one whose bundled binary will not execute, skips instead.
func terminalBinary() string {
	if path := strings.TrimSpace(os.Getenv("S3270_TEST_BINARY")); path != "" {
		return path
	}
	name := "s3270"
	if runtime.GOOS == "windows" {
		name = "s3270.exe"
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	bundled := map[string]string{
		"linux/amd64":   "s3270-linux-amd64",
		"windows/amd64": "s3270.exe",
	}[runtime.GOOS+"/"+runtime.GOARCH]
	if bundled == "" {
		return ""
	}
	path, err := filepath.Abs(filepath.Join("..", "..", "s3270-bin", bundled))
	if err != nil {
		return ""
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return ""
	}
	return path
}

// connectTerminal starts a real terminal against the scripted host and reads
// the first screen.
//
// The extra arguments are the ones under test — a model, an oversize, a code
// page. Everything else is left at the terminal's own defaults, so a test
// asserts the behaviour a deployment gets rather than the behaviour a
// carefully-configured one does.
func connectTerminal(t *testing.T, h *conformanceHost, args ...string) *S3270 {
	t.Helper()
	exe := terminalBinary()
	if exe == "" {
		t.Skip("no s3270 available to test against")
	}

	term := NewS3270(exe, append(append([]string{}, args...), h.addr())...)
	if err := term.Start(); err != nil {
		t.Skipf("could not start the terminal at %s: %v", exe, err)
	}
	t.Cleanup(func() { _ = term.Stop() })

	// The terminal is given a moment to negotiate and take the first write.
	// UpdateScreen retries a locked keyboard on its own; what it does not wait
	// for is a session that has not finished connecting.
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := term.UpdateScreen()
		if err == nil {
			s := term.GetScreenSnapshot()
			if s != nil && s.Width > 0 && len(s.Fields) > 0 {
				return term
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("the terminal never produced a screen: %v", err)
			}
			t.Fatal("the terminal never produced a formatted screen")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// screenRow returns one row of the decoded screen as text, with the nulls a
// terminal leaves in untouched positions shown as spaces.
func screenRow(s *Screen, y int) string {
	if s == nil || y < 0 || y >= len(s.Buffer) {
		return ""
	}
	var sb strings.Builder
	for _, r := range s.Buffer[y] {
		if r == 0 {
			r = ' '
		}
		sb.WriteRune(r)
	}
	return strings.TrimRight(sb.String(), " ")
}

// fieldStartingAt returns the decoded field whose text starts at (col,row).
func fieldStartingAt(s *Screen, col, row int) *Field {
	for _, f := range s.Fields {
		if f.StartX == col && f.StartY == row {
			return f
		}
	}
	return nil
}

// addressOf turns a row and column into the buffer address the terminal
// reports in an inbound record.
func addressOf(row, col, cols int) int { return row*cols + col }

func init() {
	// Keep the linters quiet about constants a future test will want but no
	// current one names.
	_ = []interface{}{
		bytes.Equal, strconv.Itoa, orderPT, orderEUA, aidPF13, aidPF24, aidNoAID,
		aidSysReq, aidPA2, aidPA3, aidPF12, faModified, newUpdate,
	}
}

// bytesWith appends raw host code points, for a test that is about the code
// page rather than about the text.
func (d *dataStream) bytesWith(raw ...byte) *dataStream {
	d.b = append(d.b, raw...)
	return d
}
