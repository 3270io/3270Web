---
seo_title: "Compare two chaos mind maps to find host differences"
description: >-
  Diff two exported chaos mind maps to surface divergence between two hosts
  or two builds, and confirm that a fix or a migration did not regress a
  transaction.
---

# Chaos Mind-Map Compare

`POST /chaos/mindmap/compare` diffs two chaos mind maps and returns the
deltas. Use it to surface divergence between two hosts (typically a
mainframe and the platform standing in for it, or two builds of the same
host) or to confirm that a fix or migration did not regress an existing
transaction.

No active chaos run is required: the comparison works on saved runs and on
exported files. Pair it with
[Host Compatibility Profiler](host-profiler.md) for the wire-level view
of the same comparison.

## In the browser

1. Explore the first host. The run is saved when it ends.
2. Connect to the second host and explore it the same way.
3. Open **Automation → Saved chaos runs**. Each run shows the host it
   explored; give the two a name if that makes them easier to tell apart.
4. Select both and choose **Compare**.

## Capturing the inputs over HTTP

There are two ways to say what to compare.

**By saved run.** Every run is saved when it ends, and `GET /chaos/runs`
lists them with the host each explored. Nothing needs exporting: note the
two IDs.

```sh
curl -H "Authorization: Bearer $API_TOKEN" http://127.0.0.1:3270/chaos/runs
```

**By exported map.** Export the map after each run. This works once the
run has finished — it does not have to be caught while the run is still
going — and for a run loaded from the saved list.

```sh
# Baseline: explore the first host, then
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:3270/chaos/mindmap/export > baseline.json

# Candidate: connect to the second host, explore it, then
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:3270/chaos/mindmap/export > candidate.json
```

Exported maps are the form to use when the two runs were made on
different 3270Web deployments, or when the baseline is kept in version
control. Values typed into hidden fields are left out of an export, so the
files are safe to store alongside code.

## Request

Two saved runs:

```sh
curl -X POST \
  -H "Content-Type: application/json" \
  -d '{"baseRunId": "20260301-090000-ab12cd34", "candidateRunId": "20260308-090000-ef34ab56"}' \
  http://127.0.0.1:3270/chaos/mindmap/compare
```

Two exported maps:

```sh
curl -X POST \
  -H "Content-Type: application/json" \
  -d "{\"baseline\": $(cat baseline.json), \"candidate\": $(cat candidate.json)}" \
  http://127.0.0.1:3270/chaos/mindmap/compare
```

The forms mix — a stored baseline file against the run that has just
finished, for example:

```sh
curl -X POST \
  -H "Content-Type: application/json" \
  -d "{\"baseline\": $(cat baseline.json), \"candidateRunId\": \"current\"}" \
  http://127.0.0.1:3270/chaos/mindmap/compare
```

| Field | Meaning |
|---|---|
| `baseline`, `candidate` | A mind map, as returned by `GET /chaos/mindmap/export`. |
| `baseRunId`, `candidateRunId` | The ID of a saved run, or `current` for the run the session holds. |

A document wins when both are given for the same side. A run ID that does
not exist answers `404`, naming the side.

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

## Use the diff in a migration review

Follow [Migration Testing](migration-testing.md) to align accounts and terminal settings, capture comparable maps, investigate deltas and replay representative business outcomes.
