---
description: >-
  Run 3270Web as a Model Context Protocol server so an AI client outside the
  browser can read screens, fill fields and press keys. Same binary, nothing
  extra to install.
---

# MCP Server

3270Web can act as a **Model Context Protocol** server, so an AI client drives
a 3270 session directly — reading screens, filling fields, pressing keys,
running chaos exploration and building business understanding. The same tools
the [AI Chat panel](ai-chat.md) uses, from a client outside the browser.

It is the same binary. There is nothing extra to install.

```bash
3270Web mcp
```

Running in Docker or Compose, where there is no local binary for a client to
launch? The server speaks MCP over HTTP itself — skip to [Docker, Compose and
remote clients](#docker-compose-and-remote-clients).

## Check it works first

Before configuring any client, confirm the binary answers:

```bash
3270Web mcp --list-tools
```

That prints the tool catalogue as JSON and exits. It needs no host, no session
and no mainframe. If you see JSON, the wiring is right and anything that fails
afterwards is configuration.

## Setting up Claude Desktop

1. Open **Claude Desktop** → **Settings** → **Developer** → **Edit Config**.
   That opens `claude_desktop_config.json`.

2. Add 3270Web to the `mcpServers` section.

    **Windows**

    ```json
    {
      "mcpServers": {
        "3270web": {
          "command": "C:\\3270Web\\3270Web.exe",
          "args": ["mcp"]
        }
      }
    }
    ```

    **macOS and Linux**

    ```json
    {
      "mcpServers": {
        "3270web": {
          "command": "/opt/3270web/3270Web",
          "args": ["mcp"]
        }
      }
    }
    ```

3. **Restart Claude Desktop.** 3270Web appears in the tools list.

4. Try it with no mainframe at all:

    > Connect to `sampleapp:petstore` and tell me what is on the screen.

Other MCP clients — VS Code, Claude Code, Cursor, Windsurf — take the same
command and arguments in their own configuration format.

## Where the session runs

With no options, `3270Web mcp` starts 3270Web on a loopback port and drives
that, printing the URL to stderr:

```
3270Web UI: http://127.0.0.1:54321
```

Open it. Watching the green screen while the model works is most of the point
— you see what it typed, on which screen, before it moves on.

If 3270Web is **already running** and `API_TOKEN` is set, the MCP server
attaches to that instead, so the model drives the session you already have
open rather than an invisible second copy.

To attach to an instance elsewhere:

```bash
3270Web mcp --url http://terminal.example.com:3270 --token "$API_TOKEN"
```

The target must have the same `API_TOKEN` set. See the [REST API](rest-api.md)
for that surface.

### If the instance has accounts

An MCP client cannot sign in, so on an instance with
[accounts](authentication.md) (`AUTH_MODE=local`) it presents a token issued
to one:

```bash
3270Web token add alice mcp
3270Web mcp --url http://127.0.0.1:3270 --token "$MY_TOKEN"
```

Every tool call then acts as that account — `list_sessions` shows its
sessions, `use_session` reaches its sessions only. Launching `3270Web mcp`
with no `--url` will refuse rather than start a second instance with the
authentication turned off over the same files.

A `--read-only` token will not work over MCP: every tool call is a `POST`,
including the ones that only read. Use the `readonly` tool tier below to limit
what a client can do.

## Tool tiers

The tools are grouped by what they can do, set with the `MCP_TOOLS`
environment variable. **The default is `interactive`.**

| `MCP_TOOLS` | Adds | For |
|---|---|---|
| `readonly` | `get_screen`, `wait_for_unlock`, `chaos_status`, `chaos_report`, `chaos_insights`, `chaos_list_screens`, `chaos_get_hints`, `business_list_functions`, `business_app_overview`, the export tools, `list_skills`, `load_skill`, `list_tasks`, `list_sessions` | Reading and reporting on a session someone else is driving |
| `interactive` *(default)* | + `send_key`, `write_field`, `submit_screen`, `move_cursor`, `connect_session`, `use_session`, the hint and annotation writes, `run_task` and the generated `task_*` tools | Driving a session the way a person at the keyboard would |
| `full` | + `chaos_start`, `chaos_stop`, `chaos_resume` | Automated chaos exploration |

Each tier includes the ones below it.

Chaos is a tier of its own not because a single key press is safer than
another, but because a person pressing keys is watching and an unattended
exploration run is not.

Two keys stop and ask whatever the tier: **PF3** commonly logs the session
off and **Clear** discards what is on the screen. The AI Chat panel will not
press either in Auto Mode without someone clicking Run, and `send_key`'s own
description says so, so a model knows before it builds a plan around one. Over
MCP the equivalent is your client's tool-approval prompt — leave it on.

Set it in the client's config:

```json
{
  "mcpServers": {
    "3270web": {
      "command": "/opt/3270web/3270Web",
      "args": ["mcp"],
      "env": { "MCP_TOOLS": "full" }
    }
  }
}
```

## Sessions

The AI Chat panel's session is whichever browser tab you are in. An MCP client
has no tab, so sessions are explicit:

- `list_sessions` — what is open, and which one is currently being driven.
- `connect_session` — open one against a hostname. This is the only tool that
  works before a session exists.
- `use_session` — attach to an existing one by id. It verifies the session
  before adopting it, so a mistyped id fails where it was made rather than on
  the next call.

Any other tool called with no session refuses and says which of these to use.

## Guided Business Tasks

A [Guided Business Task](business-tasks.md) is a recorded flow with named
inputs and named outputs — "check a balance", "look up an order" — that stops
rather than continuing when a screen is not the one it expected.

Each saved task is offered as **its own tool**, `task_<name>`, with a schema
built from the parameters it declares:

```
task_account_balance_enquiry(account_number: string, ...)
```

So a model sees that checking a balance is a thing this application does, and
what it needs, in the same list as every other tool — rather than having to
know to go looking for a catalogue. Prefer one over driving the screens by
hand: a task guards every step, and a model typing an account number into
whatever field is under the cursor is the failure the guards exist to prevent.

`list_tasks` describes the catalogue, and `run_task` runs one by name for
clients that do not act on `tools/list_changed`. A task saved in the browser
mid-conversation becomes callable without restarting the server.

The generated `task_*` tools are the one thing `3270Web mcp --list-tools` does
not report: that check runs without a target, so there is no catalogue to read.
Everything else it prints is exactly what the server offers.

## What the model sees

Everything read from the host arrives wrapped in `<untrusted-host-data>`
tags. A mainframe screen is text somebody else controls, and it can be made to
read like an instruction — a "system notice" telling the assistant to press a
PF key, for example. The browser panel applies that marking today; over MCP
there is no browser in the loop, so the server does it.

Hidden fields are redacted before anything leaves the server: a password's
value is empty and its characters are replaced in the screen text. See
[Terminal Capabilities](terminal-capabilities.md).

## Sample applications

The bundled sample apps are how you try this without a mainframe. When
`3270Web mcp` starts its own instance it enables them automatically; on a
server you started yourself, set `ALLOW_SAMPLE_APPS=1`.

```
connect to sampleapp:petstore
```

The default sample is a pet store with a counter, a back office and a
command line on every screen, which is enough of an application to ask real
questions of: *"sign on as ADMIN, then tell me which stock lines are below
their reorder level"*. See
[Bundled Sample Applications](sample-apps.md) for its screens and commands.

## Skills

The procedures the assistant follows are files, not prose baked into a prompt.
`list_skills` shows what is available; `load_skill` fetches one. The built-in
set covers chaos exploration, business understanding, whole-application
overview and performing a catalogued business function.

You can add your own, or replace a built-in with one that knows your
application — and an extension can contribute saved tasks as well as skills.
See [Skills and Extensions](skills.md).

## Docker, Compose and remote clients

Everything above assumes a client that can launch `3270Web mcp` as a local
process. A containerised instance is the other case: nothing to launch, so the
running server speaks MCP over HTTP itself, at `POST /api/v1/mcp`, behind the
same `API_TOKEN` as the rest of [`/api/v1`](rest-api.md).

It is under `/api/v1` deliberately. The origin check that protects the browser
UI skips that prefix, and a top-level `/mcp` would be origin-checked — which
rejects every MCP client, all of them being non-browsers.

### Turn it on in the stack

```yaml
services:
  3270Web:
    image: ghcr.io/3270io/3270web:latest
    ports:
      # Loopback until this is behind TLS. See the warning below.
      - "127.0.0.1:3270:3270"
    environment:
      - GIN_MODE=release
      # Interpolated from a .env file beside this one.
      - API_TOKEN=${API_TOKEN}
      - MCP_TOOLS=interactive
      # - ALLOW_SAMPLE_APPS=1
      # - MCP_ALLOWED_HOSTS=*.test.example.com
    restart: unless-stopped
```

```bash
printf 'API_TOKEN=%s\n' "$(openssl rand -hex 24)" > .env
chmod 600 .env
docker compose up -d
```

The [installer](installation.md#install-with-the-api-and-mcp-switched-on)
writes that for you with `--api-token auto`.

There is no separate switch for MCP: `API_TOKEN` turns on `/api/v1`, and MCP
is part of it. With the variable unset the endpoint answers **503**, not 401 —
if a client reports the server as unreachable rather than unauthorised, the
token is not reaching the container.

On a stack with `AUTH_MODE=local`, drop `API_TOKEN` — it is refused at startup
— and give each client a token of its own with
[`3270Web token add`](multi-user.md#api-tokens). Each connection's tool
calls are authorized as the account whose token opened it.

### Check the endpoint before configuring a client

```bash
set -a; . ./.env; set +a
curl -sS -X POST http://127.0.0.1:3270/api/v1/mcp \
  -H "Authorization: Bearer $API_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize",
       "params":{"protocolVersion":"2025-06-18","capabilities":{},
                 "clientInfo":{"name":"curl","version":"0"}}}'
```

A JSON-RPC result is the whole check. Both headers on `Accept` matter — the
transport is Streamable HTTP and may answer either way.

| Answer | Meaning |
|---|---|
| JSON-RPC `result` | Working; anything that fails now is client configuration |
| `503` | `API_TOKEN` is not set in the container |
| `401` | Set, but not the value you sent |
| Connection refused | Port mapping, not MCP — check `docker compose ps` |

### Connect a client

For a client that speaks HTTP transports directly:

```bash
claude mcp add --transport http 3270web \
  https://terminal.example.com/api/v1/mcp \
  --header "Authorization: Bearer $API_TOKEN"
```

Or as project configuration, which VS Code and Cursor take in their own
formats:

```json
{
  "mcpServers": {
    "3270web": {
      "type": "http",
      "url": "https://terminal.example.com/api/v1/mcp",
      "headers": { "Authorization": "Bearer ${API_TOKEN}" }
    }
  }
}
```

A desktop client that only launches local processes reaches the same endpoint
through a stdio bridge:

```json
{
  "mcpServers": {
    "3270web": {
      "command": "npx",
      "args": [
        "-y", "mcp-remote",
        "https://terminal.example.com/api/v1/mcp",
        "--header", "Authorization:${AUTH_HEADER}"
      ],
      "env": { "AUTH_HEADER": "Bearer your-token-here" }
    }
  }
}
```

The `${AUTH_HEADER}` indirection is not decoration: config arguments are split
on spaces, so `Authorization: Bearer …` written inline arrives at the server in
pieces.

If you have the binary to hand, it bridges too, and needs no proxy package:

```bash
3270Web mcp --url http://localhost:3270 --token "$API_TOKEN"
```

That is the same stdio server described above, driving your stack instead of
one it started. Configure it exactly as in [Setting up Claude
Desktop](#setting-up-claude-desktop), with `--url` and `--token` in `args`.

### One conversation, one connection

A fresh MCP server is built per connection, so the current session and the
skills one client has loaded do not leak into another's. What is *not* per
connection is the sessions themselves: two clients pointed at one stack can
`use_session` the same id and type into the same screen. That is occasionally
what you want — a person watching the green screen in the browser while a
model works — and otherwise it is two hands on one keyboard.

### Before you publish the port

A token is a password for the terminal. A client holding it opens sessions,
types into fields and presses keys on whatever host the container can reach,
with no second prompt in between.

- Keep the mapping on `127.0.0.1` until the stack is behind TLS. The token
  travels in a header; over plain HTTP on a shared network it travels in the
  clear.
- Set `MCP_ALLOWED_HOSTS` to a glob list of hosts an AI client may connect to.
  Unset means anything the container can route to, which is right for a lab
  and wrong for a network with production on it.
- Choose the tier deliberately. `MCP_TOOLS=readonly` is a real answer for a
  shared instance: the model reads and reports on a session a person is
  driving, and cannot press anything.
- Leave your client's tool-approval prompt on. It is the only thing standing
  between a plan and a PF3 on a live session.
- `ALLOW_SAMPLE_APPS=1` is what lets a headless client open the bundled sample
  hosts. Useful for trying this out; not something to leave on a stack that
  can see a real mainframe.

## Settings

| Variable | Default | What it does |
|---|---|---|
| `MCP_TOOLS` | `interactive` | Tool tier: `readonly`, `interactive` or `full` |
| `API_TOKEN` | unset | Bearer token for `/api/v1` and for attaching to a running instance |
| `ALLOW_SAMPLE_APPS` | off | Let the headless API open sessions against bundled sample apps |
| `MCP_ALLOWED_HOSTS` | unset | Comma-separated glob list of hosts that may be connected to |

These live in the `.env` file beside the binary, or in the client's `env`
block. See [Connect and Use 3270Web](configuration.md).
