// Package profiler probes a connected 3270 host (typically via s3270 Query
// actions) and emits a structured CompatibilityProfile describing the host's
// terminal model, protocol capabilities, and timing characteristics.
//
// The CompatibilityProfile JSON shape is shared between 3270Web and
// 3270Connect. The schema_version field follows semver — breaking changes
// bump major, additive fields bump minor. Consumers MUST tolerate unknown
// fields.
package profiler

import "time"

// SchemaVersion is the semver version of the CompatibilityProfile JSON shape.
// Bump the minor when adding fields; bump the major for breaking changes.
const SchemaVersion = "1.0.0"

// TriState captures a probe outcome that may be unknown if the host did not
// answer the underlying query.
type TriState string

const (
	TriUnknown TriState = "unknown"
	TriYes     TriState = "yes"
	TriNo      TriState = "no"
)

// CompatibilityProfile is the top-level document returned by Probe().
type CompatibilityProfile struct {
	SchemaVersion string            `json:"schema_version"`
	RunID         string            `json:"run_id"`
	Timestamp     time.Time         `json:"timestamp"`
	Profiler      ProfilerMeta      `json:"profiler"`
	Host          HostIdentity      `json:"host"`
	Device        DeviceProfile     `json:"device"`
	Protocol      ProtocolProfile   `json:"protocol"`
	Capabilities  CapabilityProfile `json:"capabilities"`
	Timing        TimingProfile     `json:"timing"`
	Raw           map[string]string `json:"raw,omitempty"`
}

// ProfilerMeta identifies which tool produced the profile.
type ProfilerMeta struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
}

// HostIdentity is the network-side identity of the host plus a fingerprint of
// the first screen for cross-environment comparison.
type HostIdentity struct {
	Host            string `json:"host"`
	Port            int    `json:"port,omitempty"`
	TLS             bool   `json:"tls"`
	BannerSignature string `json:"banner_signature,omitempty"`
	BannerPreview   string `json:"banner_preview,omitempty"`
}

// DeviceProfile describes the negotiated terminal model.
type DeviceProfile struct {
	TerminalType       string `json:"terminal_type,omitempty"`
	Rows               int    `json:"rows,omitempty"`
	Cols               int    `json:"cols,omitempty"`
	Color              bool   `json:"color"`
	ExtendedAttributes bool   `json:"extended_attributes"`
	AltScreen          bool   `json:"alt_screen"`
}

// ProtocolProfile describes the TN3270 negotiation.
type ProtocolProfile struct {
	TN3270E             bool     `json:"tn3270e"`
	BindImagePresent    bool     `json:"bind_image_present"`
	StructuredFields    bool     `json:"structured_fields"`
	LUName              string   `json:"lu_name,omitempty"`
	NegotiatedFunctions []string `json:"negotiated_functions,omitempty"`
}

// CapabilityProfile captures higher-level capabilities discovered at probe
// time. The Unknown list names queries the host did not answer, which is the
// primary signal callers use to spot Rocket-vs-IBM divergence.
type CapabilityProfile struct {
	INDFile       TriState `json:"ind_file"`
	QueryReplyIDs []string `json:"query_reply_ids,omitempty"`
	Unknown       []string `json:"unknown,omitempty"`
}

// TimingProfile records connect and first-screen latencies in milliseconds.
type TimingProfile struct {
	ConnectMS        int64 `json:"connect_ms"`
	FirstScreenMS    int64 `json:"first_screen_ms"`
	KeyboardUnlockMS int64 `json:"keyboard_unlock_ms"`
}
