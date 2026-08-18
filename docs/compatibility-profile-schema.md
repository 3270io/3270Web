---
seo_title: "CompatibilityProfile schema as 3270Web writes it"
description: >-
  The CompatibilityProfile document 3270Web captures from a browser session,
  field by field, so it can be diffed against one 3270Connect produced.
---

# CompatibilityProfile schema (v1.0.0)

The `CompatibilityProfile` JSON document is the shared output format
produced by both `3270Web` (via the `internal/profiler` package, exposed
as `POST /profile` and `POST /api/v1/sessions/:id/profile`) and
`3270Connect` (via `3270Connect -profile`). Both repositories carry
**identical bytes** of this document; bump in lock-step.

See [Host Compatibility Profiler](host-profiler.md) for the 3270Web
endpoints that produce this document, and
[Chaos Mind-Map Compare](chaos-compare.md) for the application-layer
companion diff.

## Versioning

- `schema_version` follows semver. Breaking changes bump the major;
  additive fields bump the minor.
- **Consumers MUST tolerate unknown fields.** New fields can be added in
  minor versions without breaking older readers.
- Current: `1.0.0`.

## Top-level fields

| Field | Type | Notes |
|---|---|---|
| `schema_version` | string | Always `"1.0.0"` for this revision. |
| `run_id` | string | Random opaque identifier; lets a fleet probe correlate logs. |
| `timestamp` | RFC3339 UTC | When the probe ran. |
| `profiler` | object | Tool metadata — see below. |
| `host` | object | Network-side identity + first-screen fingerprint. |
| `device` | object | Negotiated terminal model. |
| `protocol` | object | TN3270 negotiation outcome. |
| `capabilities` | object | Discovered capabilities + list of unanswered queries. |
| `timing` | object | Connect / first-screen / keyboard-unlock latencies (ms). |
| `raw` | object | Optional; raw s3270 `Query` responses keyed by query name. |

### `profiler`

```json
{"tool": "3270Web" | "3270Connect", "version": "<semver>"}
```

### `host`

```json
{
  "host": "mvs01.example.com",
  "port": 992,
  "tls": true,
  "banner_signature": "<sha256 hex>",
  "banner_preview": "first ~240 chars of first screen, normalised"
}
```

- `banner_signature` is the sha256 of the first screen after collapsing
  whitespace, stripping control characters, and ASCII-folding
  non-printable codepoints. Two hosts that show the same logon banner
  will have the same signature.
- `host` and `port` are populated from `Query(Host)` when the host
  answers, falling back to the caller-supplied identity otherwise.

### `device`

```json
{
  "terminal_type": "IBM-3279-2-E",
  "rows": 24,
  "cols": 80,
  "color": true,
  "extended_attributes": true,
  "alt_screen": false
}
```

### `protocol`

```json
{
  "tn3270e": true,
  "bind_image_present": true,
  "structured_fields": true,
  "lu_name": "LU01",
  "negotiated_functions": ["BIND-IMAGE", "RESPONSES", "SYSREQ"]
}
```

### `capabilities`

```json
{
  "ind_file": "yes" | "no" | "unknown",
  "query_reply_ids": ["..."],
  "unknown": ["BindPluName", "Tn3270eOptions"]
}
```

- `ind_file` defaults to `"unknown"`. The mildly-destructive probe is
  opt-in via the `ind_file_probe` request flag (3270Web) or
  `-indFileProbe` (3270Connect — when added in a future slice).
- `unknown` lists queries the host did not answer. This list is the
  **primary signal** for spotting mainframe-vs-Rocket divergence: a
  query present in one environment's `unknown` and absent from the
  other's highlights a capability gap.
- Only queries the emulator actually has belong in the probe sequence.
  One that it does not recognise is indistinguishable here from a host
  declining to answer, so it lands in `unknown` on every host and
  dilutes the signal the list exists to carry.

### `timing`

```json
{"connect_ms": 142, "first_screen_ms": 38, "keyboard_unlock_ms": 38}
```

Zero indicates the value was not measured (e.g. the host does not expose
connect timing).

### `raw` (optional)

```json
{"ScreenSizeMax": "rows 43 columns 80", "Model": "IBM-3279-2-E"}
```

Only emitted when the caller passes `collect_raw: true` (3270Web) or
`-profileCollectRaw` (3270Connect). Useful for debugging parser drift
against new s3270 versions.

## Cross-environment comparison

The canonical comparison key is the tuple
`(device.terminal_type, host.banner_signature, protocol.tn3270e,
protocol.structured_fields)`.

Two profiles whose tuples match are compatible at the wire level even if
their `capabilities.unknown` lists differ — the latter is a hint that
the hosts implement different optional features (e.g. IND$FILE on z/OS
but not on a stripped-down Rocket Enterprise Server image).
