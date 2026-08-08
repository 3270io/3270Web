# REST API (v1)

3270Web exposes a small JSON HTTP API for non-browser clients (RPA bots,
CI jobs, integration scripts). The API is versioned and gated by a
Bearer token so it can be enabled per-deployment.

## Enabling the API

Set the `API_TOKEN` environment variable to a non-empty secret before
starting 3270Web. The simplest way is to add a line to the `.env` file
that 3270Web reads on startup:

```
API_TOKEN=replace-with-a-long-random-string
```

When `API_TOKEN` is unset or empty, every `/api/v1/*` request returns
`503 Service Unavailable` with `{"error": "API disabled: API_TOKEN not
configured"}`. This is the default so the API can't be accidentally
exposed.

The 3270Web server binds to `127.0.0.1:8080` by default, so the API is
only reachable from the local host. The Bearer token is additional
defense-in-depth for any deployment that changes the bind address via
`WEBUI_BIND` — including the Docker image, which sets `WEBUI_BIND=0.0.0.0`
so that published ports work at all. In a container, what the API is
reachable from is decided by the port mapping, not the bind address.

## Authentication

Every request must include an `Authorization: Bearer <token>` header.
Bad or missing tokens get `401 Unauthorized`.

```sh
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:8080/api/v1/sessions
```

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/sessions` | List active sessions |
| `POST` | `/api/v1/sessions` | Start a new host session |
| `DELETE` | `/api/v1/sessions/:id` | Disconnect and remove a session |
| `GET` | `/api/v1/sessions/:id/screen` | Refresh and read the current screen |
| `POST` | `/api/v1/sessions/:id/key` | Send an AID or navigation key |
| `POST` | `/api/v1/sessions/:id/field` | Write text into a field |
| `POST` | `/api/v1/sessions/:id/submit` | Submit modified fields + send Enter (or another AID) |
| `POST` | `/api/v1/sessions/:id/profile` | Run a host compatibility probe and return the `CompatibilityProfile` JSON |
| `GET` | `/api/v1/sessions/:id/profile` | Return the cached `CompatibilityProfile` from the last probe |

### `POST /api/v1/sessions`

Create and start a host session. Sample-app pseudo-hostnames (`mock`,
`demo`, `sampleapp:appN`) are rejected by the API — those are reserved
for the browser UI.

```sh
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"host":"mainframe.example.com:23"}' \
  http://127.0.0.1:8080/api/v1/sessions
```

Response:

```json
{ "id": "f1c5...", "host": "mainframe.example.com", "port": 23 }
```

### `GET /api/v1/sessions/:id/screen`

Refreshes the screen and returns its full structure.

```sh
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:8080/api/v1/sessions/$ID/screen
```

Response:

```json
{
  "width": 80,
  "height": 24,
  "text": "...screen contents...\n...\n",
  "formatted": true,
  "kbd_lock": "U",
  "cursor": { "row": 5, "col": 12 },
  "fields": [
    {
      "start_row": 1, "start_col": 0,
      "end_row": 1, "end_col": 19,
      "value": "USERID",
      "protected": true, "numeric": false, "hidden": false,
      "length": 20
    }
  ],
  "status": "U F P C(mainframe.example.com) I 4 24 80 5 12 0x0 0.000"
}
```

`kbd_lock` is `"U"` (unlocked), `"L"` (locked), or `"E"` (error). The
field is omitted when 3270Web could not parse the status line.

### `POST /api/v1/sessions/:id/key`

Send a single key. The key name follows the same vocabulary the Copilot
side panel uses: `Enter`, `PF1`..`PF24`, `PA1`..`PA3`, `Tab`, `BackTab`,
`Clear`, `Reset`, `EraseEOF`, `EraseInput`, `Home`, `Up`, `Down`,
`Left`, `Right`.

```sh
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"key":"PF3"}' \
  http://127.0.0.1:8080/api/v1/sessions/$ID/key
```

### `POST /api/v1/sessions/:id/field`

Write text into the input field that contains `(row, col)`. Coordinates
are 0-indexed. Text containing CR, LF, or TAB is rejected.

```sh
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"row":3,"col":10,"text":"USER01"}' \
  http://127.0.0.1:8080/api/v1/sessions/$ID/field
```

### `POST /api/v1/sessions/:id/submit`

Submit any modified fields and send an AID key. The default AID is
`Enter`; pass `{"aid": "PF3"}` to use a different key. The response
includes the updated screen.

```sh
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"aid":"Enter"}' \
  http://127.0.0.1:8080/api/v1/sessions/$ID/submit
```

### `DELETE /api/v1/sessions/:id`

Disconnect from the host and remove the session.

```sh
curl -X DELETE \
  -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:8080/api/v1/sessions/$ID
```

### `POST /api/v1/sessions/:id/profile`

Probe the session's connected host and return a `CompatibilityProfile`
JSON document. The schema is shared byte-for-byte with `3270Connect
-profile` output, so profiles from either tool can be diffed against
each other. Body is optional.

```sh
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"collect_raw": true}' \
  http://127.0.0.1:8080/api/v1/sessions/$ID/profile
```

Supported body fields: `ind_file_probe`, `collect_raw`,
`per_action_timeout_ms`. See the
[Host Compatibility Profiler](host-profiler.md) page for the full
walkthrough and the
[Compatibility Profile Schema](compatibility-profile-schema.md) for the
response shape.

### `GET /api/v1/sessions/:id/profile`

Return the cached `CompatibilityProfile` from the last probe in this
session. `404 Not Found` if no probe has run.

```sh
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:8080/api/v1/sessions/$ID/profile
```

## Browser-session endpoints

These endpoints reuse the session cookie set by the connect flow rather
than `API_TOKEN`. They are useful for in-browser callers and for tools
that already drive 3270Web through the cookie.

| Method | Path | Description |
|---|---|---|
| `POST` | `/profile` | Probe the current session and return the `CompatibilityProfile` JSON. |
| `GET` | `/profile` | Return the cached profile for the current session. |
| `POST` | `/chaos/report` | Markdown discovery report for the active chaos run (ASCII screen graph, per-screen stats, suggested experiments). |
| `POST` | `/chaos/mindmap/compare` | Diff two previously-exported chaos mind maps. JSON by default; pass `Accept: text/html` (or `?format=html`) for the HTML report. See [Chaos Mind-Map Compare](chaos-compare.md). |
| `GET` | `/chaos/screens` | Every screen discovered by chaos: fields, learned values, key destinations, business annotations, and a truncated preview. `?include_previews=false` omits previews. |
| `POST` | `/chaos/screens/annotate` | Record a screen's business purpose and field semantics. Body: `{"screen_hash", "business_purpose", "notes", "field_semantics": {"R5C20L8": {"name", "description", "example", "sensitive"}}}`. |
| `GET` | `/chaos/business/functions` | List cataloged business functions (name, description, steps, parameters). |
| `POST` | `/chaos/business/functions` | Upsert a business function. Body: `{"name", "description", "entry_screen_hash", "steps": [{"screen_hash", "inputs": [{"field_key", "value", "parameter"}], "aid_key", "expect_hash"}], "parameters": [{"name", "description", "screen_hash", "field_key", "example", "required"}]}`. |
| `POST` | `/chaos/business/generate-workflow` | Generate a business-focused workflow JSON from a cataloged function. Body: `{"name", "parameters": {"param": "value"}, "host", "port"}`. Returns a playback-compatible workflow document with `Name`/`Description`/`BusinessFunction`/`Parameters` metadata. |

## System endpoints

Unauthenticated and not session-scoped — intended for liveness/readiness probes.

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthz` | Returns `200 OK` with `{"status":"ok","version":"<app version>"}`. Used by the Docker `HEALTHCHECK` and orchestrators. Performs no session or `s3270` work. See [Install and Run](installation.md). |

## Errors

| Status | Meaning |
|---|---|
| `400 Bad Request` | Bad input — e.g. missing host, CR/LF/TAB in field text |
| `401 Unauthorized` | Missing or bad `Authorization` header |
| `404 Not Found` | Session id does not exist |
| `502 Bad Gateway` | The host or s3270 subprocess returned an error |
| `503 Service Unavailable` | `API_TOKEN` not configured — API is disabled |

## Out of scope (v1)

The following are deliberately not part of v1. Some of them are tracked
in the [Feature Roadmap](feature-roadmap.md).

- WebSocket / SSE streaming of screen changes
- File transfer endpoints (`IND$FILE`)
- OAuth / OIDC / SAML auth
- Multiple tokens, per-token scopes, rate limiting
- API token rotation
