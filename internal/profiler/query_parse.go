package profiler

import (
	"strconv"
	"strings"
)

// applyQueryHost parses the response of `Query(Host)`. The s3270/x3270 wire
// format varies across versions; common shapes include:
//
//	host hostname port tls
//	Connect hostname port
//	hostname:port
//
// All are best-effort; whatever fields we can pull go in.
func applyQueryHost(p *CompatibilityProfile, resp string) {
	resp = strings.TrimSpace(resp)
	if resp == "" {
		return
	}
	// Drop a leading "host" or "Connect" verb if present.
	fields := strings.Fields(resp)
	if len(fields) == 0 {
		return
	}
	switch strings.ToLower(fields[0]) {
	case "host", "connect":
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return
	}
	// Field 0 may be either "hostname" or "hostname:port".
	hostField := fields[0]
	if strings.Contains(hostField, ":") {
		parts := strings.SplitN(hostField, ":", 2)
		hostField = parts[0]
		if port, err := strconv.Atoi(parts[1]); err == nil && p.Host.Port == 0 {
			p.Host.Port = port
		}
	}
	if p.Host.Host == "" {
		p.Host.Host = hostField
	}
	for _, f := range fields[1:] {
		lower := strings.ToLower(f)
		if lower == "tls" || lower == "ssl" || lower == "secure" {
			p.Host.TLS = true
			continue
		}
		if port, err := strconv.Atoi(f); err == nil && p.Host.Port == 0 {
			p.Host.Port = port
		}
	}
}

// applyQueryConnectionState parses `Query(ConnectionState)`. Typical shapes:
//
//	tn3270e host
//	tn3270 host
//	connected initial
func applyQueryConnectionState(p *CompatibilityProfile, resp string) {
	resp = strings.TrimSpace(strings.ToLower(resp))
	if resp == "" {
		return
	}
	if strings.Contains(resp, "tn3270e") {
		p.Protocol.TN3270E = true
	}
	// Structured fields are part of the TN3270E function set negotiated
	// when BIND-IMAGE is present; default to TN3270E presence as a coarse
	// proxy and refine in applyQueryTn3270eFunctions / applyQueryBind.
	if p.Protocol.TN3270E {
		p.Protocol.StructuredFields = true
	}
}

// applyQueryBind parses `Query(Bind)`. The presence of a BIND image strongly
// implies a real TN3270E session with structured-field support. Newer
// s3270 versions print rows/cols/alt-screen as part of the image.
func applyQueryBind(p *CompatibilityProfile, resp string) {
	resp = strings.TrimSpace(resp)
	if resp == "" {
		return
	}
	p.Protocol.BindImagePresent = true
	p.Protocol.StructuredFields = true
	// Look for "rows N" / "cols N" / "alt" tokens.
	lower := strings.ToLower(resp)
	if rows := tokenValue(lower, "rows"); rows > 0 {
		p.Device.Rows = rows
	}
	if cols := tokenValue(lower, "cols"); cols > 0 {
		p.Device.Cols = cols
	}
	if strings.Contains(lower, "alt") {
		p.Device.AltScreen = true
	}
	if strings.Contains(lower, "color") || strings.Contains(lower, "colour") {
		p.Device.Color = true
	}
	if strings.Contains(lower, "extended") || strings.Contains(lower, "ext-attr") {
		p.Device.ExtendedAttributes = true
	}
}

// applyQueryCursor is a liveness check — we only care that the host answered.
// The value itself is already tracked via Screen.StatusCursor at a higher
// level.
func applyQueryCursor(p *CompatibilityProfile, resp string) {
	_ = resp
}

// applyQueryModel parses `Query(Model)`. Example: "3279-2-E" or "IBM-3279-4-E".
// Color is inferred from the 3279 family, extended attributes from the "-E"
// suffix.
func applyQueryModel(p *CompatibilityProfile, resp string) {
	resp = strings.TrimSpace(resp)
	if resp == "" {
		return
	}
	// Take the first whitespace-delimited token to drop any trailing notes.
	parts := strings.Fields(resp)
	model := parts[0]
	if p.Device.TerminalType == "" {
		p.Device.TerminalType = model
	}
	lower := strings.ToLower(model)
	if strings.Contains(lower, "3279") {
		p.Device.Color = true
	}
	if strings.HasSuffix(lower, "-e") {
		p.Device.ExtendedAttributes = true
	}
}

// applyQueryBindPluName parses `Query(BindPluName)`. The response is the LU
// name as a single token.
func applyQueryBindPluName(p *CompatibilityProfile, resp string) {
	name := strings.TrimSpace(resp)
	if name == "" {
		return
	}
	p.Protocol.LUName = name
}

// applyQueryTn3270eFunctions parses `Query(Tn3270eFunctions)`. The response
// lists negotiated function names (one per line, or whitespace-separated),
// e.g. "BIND-IMAGE RESPONSES SYSREQ".
func applyQueryTn3270eFunctions(p *CompatibilityProfile, resp string) {
	if strings.TrimSpace(resp) == "" {
		return
	}
	seen := make(map[string]struct{})
	var out []string
	for _, field := range strings.Fields(strings.ReplaceAll(resp, ",", " ")) {
		f := strings.ToUpper(strings.TrimSpace(field))
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	if len(out) > 0 {
		p.Protocol.NegotiatedFunctions = out
		p.Protocol.TN3270E = true
		p.Protocol.StructuredFields = true
	}
}

// applyQueryScreenCurSize parses `Query(ScreenCurSize)`. Format: "rows cols".
func applyQueryScreenCurSize(p *CompatibilityProfile, resp string) {
	parts := strings.Fields(resp)
	if len(parts) < 2 {
		return
	}
	if rows, err := strconv.Atoi(parts[0]); err == nil && p.Device.Rows == 0 {
		p.Device.Rows = rows
	}
	if cols, err := strconv.Atoi(parts[1]); err == nil && p.Device.Cols == 0 {
		p.Device.Cols = cols
	}
}

// tokenValue scans s for a "label N" token pair (whitespace- or
// equals-separated) and returns N or 0 if not found / not numeric.
func tokenValue(s, label string) int {
	idx := strings.Index(s, label)
	if idx < 0 {
		return 0
	}
	rest := strings.TrimLeft(s[idx+len(label):], " \t=:")
	if rest == "" {
		return 0
	}
	end := len(rest)
	for i, r := range rest {
		if r < '0' || r > '9' {
			end = i
			break
		}
	}
	if end == 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0
	}
	return n
}
