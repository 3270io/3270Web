// Package printer runs a 3287 printer session beside a terminal session.
//
// A 3270 shop's batch output does not arrive on the display. It arrives on a
// printer LU: the host binds a separate session, sends it an SCS (LU1) or a
// 3270 data stream (LU3), and ends the job. A terminal that cannot bind one is
// not a replacement for the emulator on the desk, however good its screen
// handling is — the report still has to come out somewhere.
//
// The protocol half of that is already written. `pr3287` ships alongside
// `s3270`, speaks both datastreams, and is maintained by the same authors (see
// Acknowledgements). What is left is the part a browser-delivered terminal has
// to answer for itself: where the paper goes when there is no desk to put a
// printer on. So each job is spooled to a file the session owns, and the
// operator downloads it — see spool.go.
//
// This package knows nothing about HTTP, sessions or accounts. It builds the
// argument list, runs the process, and reports what happened to it.
package printer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Options describes one printer session.
//
// Host and Port are not the caller's to choose: cmd/3270Web fills them from
// the terminal session this printer belongs to. That is a deliberate
// restriction rather than an omission — an endpoint that took an arbitrary
// host would be a way to make the server open connections anywhere it can
// reach, and the real case is always "the printer LU on the mainframe I am
// already signed in to".
type Options struct {
	Host string
	Port int
	// TLS wraps the connection, matching the terminal's own link. Verify
	// asks pr3287 to check the host certificate; unlike s3270, pr3287 does
	// not verify unless told to, so this is opt-in on the wire as well as
	// in the struct.
	TLS    bool
	Verify bool

	// LU is the printer LU to bind, when the host has one by name.
	LU string
	// Associate is the *display* LU whose associated printer to bind, for
	// hosts that pair a printer with a terminal rather than naming it. The
	// two are alternatives: a session binds by name or by association, never
	// both.
	Associate string

	// CodePage is an alternate EBCDIC code page (pr3287's -charset).
	CodePage string
	// MPP is the maximum presentation position — the line length — for LU3
	// printing. Zero leaves pr3287's own default of 132.
	MPP int
	// CRLF ends each line with CR/LF rather than LF, which is what a reader
	// on Windows expects of a downloaded file.
	CRLF bool
	// BlankLines keeps blank lines that LU3 formatted mode would otherwise
	// suppress.
	BlankLines bool
	// FFSkip ignores a formfeed at the top of a page; FFThru passes one
	// through in SCS mode rather than interpreting it.
	FFSkip bool
	FFThru bool
	// IgnoreEOJ ignores the host's PRINT-EOJ, for hosts that never send one;
	// EOJTimeout then ends a job after that many idle seconds instead. Zero
	// leaves pr3287's default.
	IgnoreEOJ  bool
	EOJTimeout int
	// SkipCC drops ASA carriage-control characters from the first column.
	SkipCC bool

	// SpoolCommand is what pr3287 runs for each finished job, with the job's
	// bytes on its standard input. Always built by the server (see
	// SpoolCommandFor); never assembled from a request.
	SpoolCommand string
}

// maxMPP and minMPP bound the line length pr3287 accepts.
const (
	minMPP = 40
	maxMPP = 256
)

// maxEOJTimeout bounds the idle wait. A job that has not ended after an hour
// is a misconfiguration rather than a long job, and a larger number here only
// delays finding that out.
const maxEOJTimeout = 3600

// luNamePattern is what an LU name may contain.
//
// The value reaches an argument vector rather than a shell, so this is not
// quoting — it is a check that the thing being named is a name. A leading "-"
// is rejected separately below, because pr3287 would read it as an option and
// a caller must not be able to add one.
var luNamePattern = regexp.MustCompile(`^[A-Za-z0-9@$#._-]{1,64}$`)

// codePagePattern matches the code page names pr3287 knows ("cp037",
// "bracket", "belgian-euro").
var codePagePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// hostPattern is a last line of defence on a value the server supplies. If a
// hostname ever reaches here with a space, a leading dash or a stray colon in
// it, something upstream is wrong and the process should not start.
//
// The colon is the interesting one, because the target is "host:port" and an
// extra one silently changes which port is meant. An IPv6 literal is the
// legitimate case and has to arrive bracketed, which is also how it has to be
// written in a target.
var hostPattern = regexp.MustCompile(`^([A-Za-z0-9._-]{1,253}|\[[0-9A-Fa-f:.]{2,45}\])$`)

// Validate reports what is wrong with the options, in the words the operator
// who filled the form needs rather than the ones pr3287 would use.
func (o Options) Validate() error {
	host := strings.TrimSpace(o.Host)
	if host == "" {
		return fmt.Errorf("this session has no host to open a printer session against")
	}
	if strings.HasPrefix(host, "-") || !hostPattern.MatchString(host) {
		return fmt.Errorf("the session's host name cannot be used for a printer session")
	}
	if o.Port <= 0 || o.Port > 65535 {
		return fmt.Errorf("the session's port is not a usable port number")
	}

	lu := strings.TrimSpace(o.LU)
	assoc := strings.TrimSpace(o.Associate)
	switch {
	case lu == "" && assoc == "":
		return fmt.Errorf("name the printer LU to bind, or associate the printer with this session's display LU")
	case lu != "" && assoc != "":
		return fmt.Errorf("a printer session binds an LU by name or by association, not both")
	}
	for label, value := range map[string]string{"printer LU": lu, "display LU": assoc} {
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "-") || !luNamePattern.MatchString(value) {
			return fmt.Errorf("%s %q is not a valid LU name", label, value)
		}
	}

	if cp := strings.TrimSpace(o.CodePage); cp != "" {
		if strings.HasPrefix(cp, "-") || !codePagePattern.MatchString(cp) {
			return fmt.Errorf("code page %q is not a valid name", cp)
		}
	}
	if o.MPP != 0 && (o.MPP < minMPP || o.MPP > maxMPP) {
		return fmt.Errorf("line length must be between %d and %d characters", minMPP, maxMPP)
	}
	if o.EOJTimeout < 0 || o.EOJTimeout > maxEOJTimeout {
		return fmt.Errorf("the end-of-job timeout must be between 0 and %d seconds", maxEOJTimeout)
	}
	if strings.TrimSpace(o.SpoolCommand) == "" {
		return fmt.Errorf("no spool command was configured for this printer session")
	}
	return nil
}

// Target renders the connection as pr3287 wants it: [L:][lu@]host:port.
//
// Certificate verification is an option rather than part of the prefix, so
// there is no "Y:" here even though s3270 targets elsewhere in 3270Web use
// one. See Args.
func (o Options) Target() string {
	var sb strings.Builder
	if o.TLS {
		sb.WriteString("L:")
	}
	if lu := strings.TrimSpace(o.LU); lu != "" {
		sb.WriteString(lu)
		sb.WriteString("@")
	}
	sb.WriteString(strings.TrimSpace(o.Host))
	sb.WriteString(":")
	sb.WriteString(strconv.Itoa(o.Port))
	return sb.String()
}

// Args returns the argument vector for pr3287, target last.
//
// Validate is called first: every value below has been checked to be the kind
// of thing it claims to be, which is what makes it safe to hand a
// caller-influenced LU name to a process.
func (o Options) Args() ([]string, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}

	args := []string{"-command", o.SpoolCommand}

	if assoc := strings.TrimSpace(o.Associate); assoc != "" {
		args = append(args, "-assoc", assoc)
	}
	if cp := strings.TrimSpace(o.CodePage); cp != "" {
		// -codepage is the current name and what s3270 is given elsewhere in
		// 3270Web. Builds before x3270 4.0 called it -charset, and 4.x still
		// answers to that; the documented one is the one to send.
		args = append(args, "-codepage", cp)
	}
	if o.MPP != 0 {
		args = append(args, "-mpp", strconv.Itoa(o.MPP))
	}
	if o.CRLF {
		args = append(args, "-crlf")
	}
	if o.BlankLines {
		args = append(args, "-blanklines")
	}
	if o.FFSkip {
		args = append(args, "-ffskip")
	}
	if o.FFThru {
		args = append(args, "-ffthru")
	}
	if o.IgnoreEOJ {
		args = append(args, "-ignoreeoj")
	}
	if o.EOJTimeout > 0 {
		args = append(args, "-eojtimeout", strconv.Itoa(o.EOJTimeout))
	}
	if o.SkipCC {
		args = append(args, "-skipcc")
	}
	// Said either way round, never left to the default.
	//
	// Which default that is has changed: pr3287 before x3270 4.0 did not check
	// the host certificate unless asked, and 4.x checks unless asked not to.
	// A printer session that inherited whichever default the installed build
	// happens to have would be on different terms from the display session
	// beside it — and would change terms under a package upgrade, silently,
	// which is the worst way for a TLS decision to move.
	if o.TLS {
		if o.Verify {
			args = append(args, "-verifycert")
		} else {
			args = append(args, "-noverifycert")
		}
	}

	return append(args, o.Target()), nil
}
