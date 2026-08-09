---
description: >-
  Probe the active 3270 session for a CompatibilityProfile: the negotiated
  terminal model, protocol options, discovered capabilities and timing.
---

# Host Compatibility Profiler

The host compatibility profiler probes the active 3270 session and
returns a `CompatibilityProfile` JSON document describing the negotiated
terminal model, protocol options, discovered capabilities, and timing.
The schema is shared byte-for-byte with the `3270Connect -profile` CLI,
so profiles produced by either tool can be diffed against each other —
for example, to spot divergence between an IBM z/OS host and a Rocket
Enterprise Server stand-in before a migration.

See [Compatibility Profile Schema](compatibility-profile-schema.md) for
the full field reference, and [Chaos Mind-Map Compare](chaos-compare.md)
for the companion divergence-report endpoint.

## Endpoints

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `POST` | `/profile` | Session cookie | Run a probe against the caller's active session and return the profile. |
| `GET` | `/profile` | Session cookie | Return the cached profile from the last probe in this session, or `404` if no probe has run. |
| `POST` | `/api/v1/sessions/:id/profile` | `Authorization: Bearer $API_TOKEN` | Same as `POST /profile` for external automation. |
| `GET` | `/api/v1/sessions/:id/profile` | `Authorization: Bearer $API_TOKEN` | Cached profile for the given session id. |

The probe uses a 30-second total timeout. Failures surface as
`502 Bad Gateway`; timeouts surface as `504 Gateway Timeout`.

## Request body

All four endpoints accept the same optional JSON body:

```json
{
  "ind_file_probe": false,
  "collect_raw": false,
  "per_action_timeout_ms": 0
}
```

| Field | Default | Description |
|---|---|---|
| `ind_file_probe` | `false` | Opt in to the mildly-destructive IND$FILE probe. Sets `capabilities.ind_file` to `yes`/`no` instead of `unknown`. |
| `collect_raw` | `false` | Include raw s3270 `Query` responses under the `raw` block. Useful when debugging parser drift against new s3270 versions. |
| `per_action_timeout_ms` | server default | Override the per-`Query` timeout in milliseconds. |

## Example: probe via the API

```sh
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"collect_raw": true}' \
  http://127.0.0.1:3270/api/v1/sessions/$SESSION_ID/profile
```

Response:

```json
{
  "schema_version": "1.0.0",
  "run_id": "8f3b2c1a...",
  "timestamp": "2026-05-18T10:11:12Z",
  "profiler": { "tool": "3270Web", "version": "1.0.0" },
  "host": {
    "host": "mvs01.example.com",
    "port": 992,
    "tls": true,
    "banner_signature": "9b6f...",
    "banner_preview": "WELCOME TO MVS01 ..."
  },
  "device": {
    "terminal_type": "IBM-3279-2-E",
    "rows": 24, "cols": 80,
    "color": true, "extended_attributes": true, "alt_screen": false
  },
  "protocol": { "tn3270e": true, "bind_image_present": true, "structured_fields": true },
  "capabilities": { "ind_file": "unknown", "query_reply_ids": ["..."], "unknown": [] },
  "timing": { "connect_ms": 142, "first_screen_ms": 38, "keyboard_unlock_ms": 38 }
}
```

## Cached read

The most recent probe per session is cached server-side. `GET` either
endpoint to read it back without re-querying the host:

```sh
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:3270/api/v1/sessions/$SESSION_ID/profile
```

The cache is cleared when the session ends.

## When to use which

- **`POST /profile`** — interactive use from the browser session.
- **`POST /api/v1/sessions/:id/profile`** — external automation that
  already authenticates with `API_TOKEN`.
- **`3270Connect -profile`** — shell-friendly one-shot probe with no
  web UI involved. Use this from CI for nightly fleet sweeps; the JSON
  output is identical and can be fed into
  [Chaos Mind-Map Compare](chaos-compare.md) alongside profiles
  collected here.

## Cross-environment workflow

A typical migration-readiness pipeline looks like:

1. Capture a baseline profile from the legacy host (z/OS):
   `3270Connect -profile -profileHost mvs01 -profilePort 992 -profileTLS -profileOut baseline.json`
2. Capture a candidate profile from the new host (Rocket Enterprise
   Server) by connecting in 3270Web and calling `POST /profile`.
3. Run mind-map exploration against both hosts (see
   [Chaos Mode](chaos-mode.md)) and export each mind map.
4. Diff the two mind maps via
   [`POST /chaos/mindmap/compare`](chaos-compare.md) to surface field
   and transition divergence.
