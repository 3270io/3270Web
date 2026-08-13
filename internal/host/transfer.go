package host

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// IND$FILE file transfer, via s3270's Transfer() action.
//
// Getting a dataset off the host and into a spreadsheet, or a file up to it,
// is routine work in a 3270 shop, and a standard capability operators expect
// of a terminal.
//
// Transfer() takes comma-separated key=value options. Those go down the same
// pipe as every other s3270 action, so a value containing a comma, a
// parenthesis or a newline would not merely be wrong — it would let a
// caller append a second action. Everything is validated before it is
// assembled, and TransferOptions.LocalFile is never allowed to come from a
// request (see the note on that field).

// TransferDirection is which way the file moves.
type TransferDirection string

const (
	TransferSend    TransferDirection = "send"
	TransferReceive TransferDirection = "receive"
)

// TransferOptions describes one IND$FILE transfer.
type TransferOptions struct {
	Direction TransferDirection
	// HostFile is the dataset or file name on the host. Caller-supplied and
	// therefore validated.
	HostFile string
	// LocalFile is the path on the server. This must ALWAYS be a path the
	// server chose (a temp file), never one derived from a request: it is
	// the difference between a file transfer feature and an arbitrary
	// file-read/write primitive.
	LocalFile string
	// HostType is tso, vm or cics. Empty means s3270's default.
	HostType string
	// Mode is ascii (with translation) or binary.
	Mode string
	// Cr controls carriage-return handling for ascii transfers:
	// remove, add or keep.
	Cr string
	// Exist is what to do when the destination exists: keep, replace, append.
	Exist string
	// Recfm is fixed, variable or undefined (send to TSO/VM only).
	Recfm string
	// Lrecl and Blksize are record and block sizes (send only, 0 = omit).
	Lrecl   int
	Blksize int
	// PrimarySpace and SecondarySpace allocate a new TSO dataset
	// (0 = omit). Units come from Allocation.
	PrimarySpace   int
	SecondarySpace int
	// Allocation is tracks, cylinders or avblock.
	Allocation string
}

// transferOptionValues is the set of accepted values for each enumerated
// option. Validating against an allowlist rather than a character filter is
// what makes these safe: nothing outside these exact strings ever reaches
// the action.
var transferOptionValues = map[string][]string{
	"HostType":   {"tso", "vm", "cics"},
	"Mode":       {"ascii", "binary"},
	"Cr":         {"remove", "add", "keep"},
	"Exist":      {"keep", "replace", "append"},
	"Recfm":      {"fixed", "variable", "undefined"},
	"Allocation": {"tracks", "cylinders", "avblock"},
}

func allowedValue(field, value string) error {
	if value == "" {
		return nil
	}
	for _, candidate := range transferOptionValues[field] {
		if strings.EqualFold(value, candidate) {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s", field, strings.Join(transferOptionValues[field], ", "))
}

// validateHostFile rejects anything that could break out of the action.
//
// Parentheses are deliberately ALLOWED: "USER.DATA(MEMBER)" is the standard
// way to name a PDS member and by far the most common thing anyone
// transfers, so banning them would make this feature useless for TSO. They
// are safe because the value is emitted quoted (see quoteActionArg) — which
// is also why the quote and backslash characters, the only two that could
// escape the quoting, are the ones rejected.
func validateHostFile(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("a host file name is required")
	}
	if len(trimmed) > 128 {
		return fmt.Errorf("host file name is too long (max 128 characters)")
	}
	if strings.ContainsAny(trimmed, "\n\r\t;,\"'\\") {
		return fmt.Errorf("host file name contains characters that are not allowed")
	}
	return nil
}

func validateLocalFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("a local file path is required")
	}
	// This is always a server-chosen temp path, so these can only trip on a
	// bug rather than on input — but the check is cheap and the consequence
	// of being wrong is command injection.
	if strings.ContainsAny(path, "\n\r\t;,\"'") {
		return fmt.Errorf("local file path contains characters that are not allowed")
	}
	return nil
}

// quoteActionArg wraps a value in the double quotes x3270's action parser
// understands, so a value containing parentheses stays one value instead of
// terminating the action. Callers must already have rejected quotes and
// backslashes, which is what makes the quoting unescapable.
func quoteActionArg(value string) string {
	return "\"" + value + "\""
}

// buildTransferCommand assembles the Transfer() action. Options are emitted
// in a stable order so the command is reproducible in logs and tests.
func buildTransferCommand(opts TransferOptions) (string, error) {
	switch opts.Direction {
	case TransferSend, TransferReceive:
	default:
		return "", fmt.Errorf("direction must be %q or %q", TransferSend, TransferReceive)
	}
	if err := validateHostFile(opts.HostFile); err != nil {
		return "", err
	}
	if err := validateLocalFile(opts.LocalFile); err != nil {
		return "", err
	}
	for field, value := range map[string]string{
		"HostType":   opts.HostType,
		"Mode":       opts.Mode,
		"Cr":         opts.Cr,
		"Exist":      opts.Exist,
		"Recfm":      opts.Recfm,
		"Allocation": opts.Allocation,
	} {
		if err := allowedValue(field, value); err != nil {
			return "", err
		}
	}
	for field, value := range map[string]int{
		"Lrecl":          opts.Lrecl,
		"Blksize":        opts.Blksize,
		"PrimarySpace":   opts.PrimarySpace,
		"SecondarySpace": opts.SecondarySpace,
	} {
		if value < 0 || value > 999999999 {
			return "", fmt.Errorf("%s is out of range", field)
		}
	}

	pairs := map[string]string{
		"Direction": string(opts.Direction),
		// Quoted so a PDS member name like USER.DATA(MEMBER) cannot close
		// the action early. Everything else is an enumerated value or a
		// number and needs no quoting.
		"HostFile":  quoteActionArg(strings.TrimSpace(opts.HostFile)),
		"LocalFile": quoteActionArg(opts.LocalFile),
	}
	addIf := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			pairs[key] = strings.ToLower(strings.TrimSpace(value))
		}
	}
	addIf("Host", opts.HostType)
	addIf("Mode", opts.Mode)
	addIf("Cr", opts.Cr)
	addIf("Exist", opts.Exist)
	addIf("Recfm", opts.Recfm)
	addIf("Allocation", opts.Allocation)
	addNum := func(key string, value int) {
		if value > 0 {
			pairs[key] = strconv.Itoa(value)
		}
	}
	addNum("Lrecl", opts.Lrecl)
	addNum("Blksize", opts.Blksize)
	addNum("PrimarySpace", opts.PrimarySpace)
	addNum("SecondarySpace", opts.SecondarySpace)

	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+pairs[k])
	}
	return "Transfer(" + strings.Join(parts, ",") + ")", nil
}

// Transfer runs an IND$FILE transfer and blocks until s3270 reports the
// outcome. The host side needs the IND$FILE program available and the
// session sitting at a command prompt that can run it — a transfer started
// from the middle of an application screen will fail, and the error s3270
// returns is the useful thing to show the operator.
func (h *S3270) Transfer(opts TransferOptions) (string, error) {
	cmd, err := buildTransferCommand(opts)
	if err != nil {
		return "", err
	}

	// Marked before the lock is taken, so a screen read arriving between the two
	// is told the session is busy rather than joining a queue that will not move
	// for half an hour. See lockForInteraction.
	h.transferring.Store(true)
	defer h.transferring.Store(false)

	h.mu.Lock()
	defer h.mu.Unlock()

	// Its own budget, not the general one — see transferTimeout.
	data, status, err := h.executeCommandLockedTimeout(cmd, cmd, transferTimeout)
	h.logAction(cmd, status)
	if actionErr, refused := AsActionError(err); refused {
		message := strings.TrimSpace(actionErr.Detail)
		if message == "" {
			message = strings.TrimSpace(actionErr.Status)
		}
		return "", fmt.Errorf("file transfer failed: %s", message)
	}
	if err != nil {
		return "", err
	}
	message := strings.TrimSpace(strings.Join(stripDataPrefix(data), "\n"))
	if isS3270Error(status, data) {
		if message == "" {
			message = status
		}
		return "", fmt.Errorf("file transfer failed: %s", message)
	}
	return message, nil
}

func stripDataPrefix(data []string) []string {
	out := make([]string, 0, len(data))
	for _, line := range data {
		out = append(out, strings.TrimPrefix(line, "data: "))
	}
	return out
}
