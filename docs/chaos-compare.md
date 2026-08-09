---
description: >-
  Diff two exported chaos mind maps to surface divergence between two hosts
  or two builds, and confirm that a fix or migration did not regress a
  transaction.
---

# Chaos Mind-Map Compare

`POST /chaos/mindmap/compare` diffs two previously-exported chaos mind
maps and returns the deltas. Use it to surface divergence between two
hosts (typically an IBM z/OS mainframe and a Rocket Enterprise Server
stand-in, or two builds of the same host) or to confirm that a fix or
migration did not regress an existing transaction.

The endpoint is a pure function on two supplied JSON blobs — no active
chaos run is required. Pair it with
[Host Compatibility Profiler](host-profiler.md) for the wire-level view
of the same comparison.

## Capturing the inputs

Run chaos exploration against each host and export its mind map:

```sh
# Baseline (z/OS)
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:3270/chaos/mindmap/export > baseline.json

# Candidate (Rocket Enterprise Server, after switching the connection)
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:3270/chaos/mindmap/export > candidate.json
```

## Request

```sh
curl -X POST \
  -H "Content-Type: application/json" \
  -d "{\"baseline\": $(cat baseline.json), \"candidate\": $(cat candidate.json)}" \
  http://127.0.0.1:3270/chaos/mindmap/compare
```

Areas are matched on their **stable dedup signature**, not on the raw
hash, so cosmetic differences between hosts do not cause false-positive
divergence.

## Response format

Default: JSON. Pass `Accept: text/html` (or `?format=html` in the
query string) to render the same diff as an HTML report.

```json
{
  "schema_version": "1.0.0",
  "generated_at": "2026-05-18T10:14:22Z",
  "only_in_baseline": [
    { "hash": "...", "signature": "...", "label": "PROFILE", "preview": "..." }
  ],
  "only_in_candidate": [
    { "hash": "...", "signature": "...", "label": "ACCOUNTS", "preview": "..." }
  ],
  "common": [
    {
      "baseline_hash": "...",
      "candidate_hash": "...",
      "signature": "...",
      "label": "MAIN MENU",
      "field_deltas": [
        { "row": 5, "col": 10, "length": 8, "change": "added", "after": { "row": 5, "col": 10, "length": 8 } }
      ],
      "transition_deltas": [
        { "key": "PF3", "baseline_target": "...", "candidate_target": "", "change": "removed" }
      ]
    }
  ],
  "summary": {
    "baseline_areas": 12,
    "candidate_areas": 11,
    "common_areas": 10,
    "areas_only_in_baseline": 2,
    "areas_only_in_candidate": 1,
    "field_changes": 3,
    "transition_changes": 2
  }
}
```

### Fields

- `only_in_baseline` / `only_in_candidate` — areas that exist in one
  mind map but not the other, with a preview to help identify the
  screen.
- `common` — areas that match by signature, with per-area
  `field_deltas` (added/removed/modified) and `transition_deltas`
  (added/removed/redirected AID-key destinations).
- `summary` — counters useful for at-a-glance regressions.

All slice fields are sorted deterministically so two callers that pass
equal inputs always receive byte-equal JSON.

## Pairing with 3270Connect

`3270Connect -profile` produces the matching
[CompatibilityProfile](compatibility-profile-schema.md) for the
wire-level layer; this endpoint covers the discovered-application layer.
Use them together for full migration-readiness coverage:

1. `3270Connect -profile` from CI for the wire-level baseline.
2. Chaos exploration from 3270Web for the application-level baseline.
3. Repeat against the candidate host.
4. Diff both layers — wire compatibility via the profile, application
   compatibility via this endpoint.
