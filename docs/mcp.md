# MCP Server

3270Web can act as a **Model Context Protocol** server, so an AI client drives
a 3270 session directly — reading screens, filling fields, pressing keys,
running chaos exploration and building business understanding. The same tools
the [AI Chat panel](ai-chat.md) uses, from a client outside the browser.

It is the same binary. There is nothing extra to install.

```bash
3270Web mcp
```

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

    > Connect to `sampleapp:app1:3270` and tell me what is on the screen.

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
3270Web mcp --url http://terminal.example.com:8080 --token "$API_TOKEN"
```

The target must have the same `API_TOKEN` set. See the [REST API](rest-api.md)
for that surface.

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
connect to sampleapp:app1:3270
```

## Skills

The procedures the assistant follows are files, not prose baked into a prompt.
`list_skills` shows what is available; `load_skill` fetches one. The built-in
set covers chaos exploration, business understanding, whole-application
overview and performing a catalogued business function.

You can add your own, or replace a built-in with one that knows your
application — and an extension can contribute saved tasks as well as skills.
See [Skills and Extensions](skills.md).

## Remote clients

For a client that cannot launch a local process, the running server also
speaks MCP over HTTP at `/api/v1/mcp`, behind the same `API_TOKEN` as the rest
of [`/api/v1`](rest-api.md).

## Settings

| Variable | Default | What it does |
|---|---|---|
| `MCP_TOOLS` | `interactive` | Tool tier: `readonly`, `interactive` or `full` |
| `API_TOKEN` | unset | Bearer token for `/api/v1` and for attaching to a running instance |
| `ALLOW_SAMPLE_APPS` | off | Let the headless API open sessions against bundled sample apps |
| `MCP_ALLOWED_HOSTS` | unset | Comma-separated glob list of hosts that may be connected to |

These live in the `.env` file beside the binary, or in the client's `env`
block. See [Connect and Use 3270Web](configuration.md).
