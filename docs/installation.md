---
seo_title: "Install and run 3270Web on Linux, Docker or Windows"
description: >-
  Install and run 3270Web four ways: the one-line Linux installer, the
  Docker image, the Windows executable, or a build from source with the Go
  toolchain.
---

# Install and Run

## One-line install

```bash
curl -fsSL https://3270Web.3270.io/install.sh | bash
```

That is the whole thing. The installer asks which of the three methods below you
want, checks the host for what it needs, installs it, and waits for the health
endpoint before reporting success.

```
  ╭────────────────────────────────────────────────────────────────╮
  │  ● 3270Web installer                            linux · x86_64 │
  ╰────────────────────────────────────────────────────────────────╯

  ▌ PREFLIGHT

  › host       Linux x86_64                        [ok]
  › docker     Docker version 27.3.1               [ok]
  › compose    docker compose                      [ok]
  › release    v0.3.2                              [ok]

  ▌ PICK A DOOR

   [1]  Binary         self-contained, no runtime deps
         ─ s3270 bundled · installs to ~/.local/share/3270web

   [2]  Docker         one container, multi-arch image
         ─ docker detected · ghcr.io/3270io/3270web

   [3]  Compose        a stack you can edit and re-up
         ─ docker compose · writes ./3270web/docker-compose.yml

  › Select 1-3 [1]
```

### Skip the questions

```bash
curl -fsSL https://3270Web.3270.io/install.sh | bash -s -- --method docker --yes
```

| Flag | Default | Meaning |
|---|---|---|
| `--method <binary\|docker\|compose>` | ask | Installation method |
| `--version <tag>` | `latest` | Release tag, for example `v0.3.2` |
| `--port <port>` | `3270` | Host port to serve on |
| `--bind <address>` | `127.0.0.1` | Host interface to publish on |
| `--dir <path>` | `./3270web` | Compose project directory |
| `--auth <none\|local>` | ask | Require a sign-in and give each person an account (`local`), or run with one operator (`none`). See [Running a shared instance](multi-user.md) |
| `--api-token <value\|auto>` | off | Turn on `/api/v1` and [MCP over HTTP](mcp.md); `auto` generates a token |
| `--mcp-tools <readonly\|interactive\|full>` | `interactive` | MCP tool tier |
| `--system` | off | Binary install to `/opt` + `/usr/local/bin` |
| `--user` | on | Binary install under `$HOME` |
| `--theme <grn\|amb\|ice\|day>` | `grn` | Installer palette, matching the docs themes |
| `--no-color` / `--color` | auto | Force colour off or on |
| `--yes`, `-y` | off | Accept every prompt — use in CI |
| `--dry-run` | off | Report what would happen, change nothing |
| `--help`, `-h` | | Usage |

!!! tip "Re-running it is the way to upgrade"
    Run the installer again to take a new image or change a setting. It
    updates the install you already have rather than replacing it, and it does
    not matter which directory you run it from: it asks Docker where 3270Web
    is running and updates *that* stack, because the project directory
    defaults to `./3270web` relative to wherever you are and a second one
    elsewhere would not be a second install — Compose names its project after
    that directory, so bringing it up recreates the container that is running.

    Settings you do not pass are read back off the existing stack and the
    running container: the accounts setting, the published port and address,
    a pinned `--version`, the MCP tool tier, and the API token — regenerating
    one would break every client holding it. The stack file it replaces is
    kept beside the new one as `docker-compose.yml.bak`.

    Above all, **the data folder is carried forward exactly as it is** —
    whether that is `./data`, an absolute path or a Docker volume. Accounts,
    API tokens, the audit trail and everyone's saved work live there, and a
    container started against a different folder does not find them: it comes
    up in [first-run setup](authentication.md) for whoever reaches it, with
    all of it still sitting in the folder it stopped reading. If anything
    would move that folder, the installer stops and says so instead of
    restarting into it.

    `--auth local` and `--api-token` cannot be combined. One shared token
    would reach every account's sessions, so an instance with accounts refuses
    to start with one set; the installer says so and drops the token rather
    than writing a stack that will not come up. With accounts, each client
    gets [its own token](multi-user.md#api-tokens).

Non-interactive by design: with no TTY (a CI job, a provisioning script) the
installer stops asking and picks the binary on `amd64`, Docker elsewhere.

### Install with the API and MCP switched on

`/api/v1` — and with it [MCP over HTTP](mcp.md), the endpoint an AI client
connects to — is off until `API_TOKEN` is set. `--api-token` sets it for the
Docker and Compose methods:

```bash
curl -fsSL https://3270Web.3270.io/install.sh | bash -s -- \
  --method compose --api-token auto --yes
```

`auto` generates a random token; pass a value of your own instead if you have
one. The Compose method writes it to a `.env` file (mode `0600`) beside the
generated `docker-compose.yml` and interpolates it from there, so the stack
file itself carries no secret. Either way the installer prints the token and
the MCP URL when it finishes:

```
  › mcp url    http://localhost:3270/api/v1/mcp
  › token      e436b3d930c2ce7f51c9a05fb1a9494…
```

Without the flag, both methods write the same variables commented out with a
note on how to turn them on. The binary method configures `API_TOKEN` in the
`.env` it generates itself, so `--api-token` does not apply there.

!!! tip "Read before you pipe"
    Piping a script from the internet into a shell deserves a look first:

    ```bash
    curl -fsSL https://3270Web.3270.io/install.sh -o install.sh
    less install.sh && bash install.sh
    ```

    The script downloads only from `github.com/3270io/3270Web` releases and
    `ghcr.io/3270io/3270web`, verifies the release checksum when GitHub
    publishes one, and writes only to the directories it prints.

### What the binary method installs where

3270Web keeps its runtime state in the directory holding the executable, so the
installer gives the binary a directory of its own and links it onto `PATH`:

| Path | What |
|---|---|
| `~/.local/share/3270web/3270Web` | The executable |
| `~/.local/share/3270web/.env` | Generated configuration |
| `~/.local/share/3270web/3270Web.log` | Application log |
| `~/.local/share/3270web/chaos-runs/` | Chaos exploration output |
| `~/.local/bin/3270web` | Symlink you actually run |

With `--system` those become `/opt/3270web/…` and `/usr/local/bin/3270web`.
Re-running the installer upgrades in place: the new build is renamed over the
old one, so an upgrade works even while 3270Web is serving. Restart it to pick
up the new build.

To remove a binary install, delete those two paths. To remove a container
install, `docker rm -f 3270web`; for Compose, `docker compose down` in the
project directory.

---

## The three methods

| Method | Best for | s3270 needed? |
|---|---|---|
| [Native binary](#run-the-linux-binary) | Bare-metal / VM installs, quick local use | Bundled on `linux/amd64`; install it for other arches |
| [Docker](#run-with-docker) | Containerized / cloud deployments | No — the image installs it |
| [Docker Compose](#run-with-docker-compose) | One-command local stack | No — the image installs it |

Whichever you choose, 3270Web listens on **port 3270** by default. Open
[http://localhost:3270](http://localhost:3270) once it is up and continue with
[Connect and Use 3270Web](configuration.md).

!!! warning "Upgrading from a build that served 8080"

    The default listen port changed from `8080` to `3270`. A container
    published as `"8080:8080"` maps the host port onto a container port
    nothing is listening on any more, so the page stops loading after the
    pull even though the container is running.

    Re-running the installer rewrites the mapping for you. To fix a stack you
    edit by hand, change the container side of the mapping and restart:

    ```yaml
    ports:
      - "127.0.0.1:8080:3270"   # keep the host port you already use
      - "127.0.0.1:3270:3270"   # or move to the new one
    ```

    A binary install takes the port from `WEBUI_PORT`; set it to `8080` in
    that install's `.env` to stay where you were.

    3270 is also the well-known TN3270 port, so the bundled sample apps moved
    up one — they now offer 3271–3278, with 3271 as the default — and the two
    no longer collide on a host running both. Nothing about connecting to a
    *real* mainframe changed: that port comes from the host you type in.

The rest of this page is what the installer does, by hand — useful when you want
a different layout, are packaging 3270Web yourself, or are working offline.

!!! note "Windows"
    Windows ships as a signed `3270Web.exe` built with
    `scripts\build-windows.ps1`. This page covers Linux/Unix and containers; see
    the project `README` for the Windows desktop build.

---

## Run the Linux binary

### Build it

```bash
# From a clone of the repository
./scripts/build-linux.sh
```

This produces a `3270Web` executable in the repository root. Equivalent manual
builds:

```bash
go build -trimpath -ldflags "-s -w" -o 3270Web ./cmd/3270Web   # release-style
go run ./cmd/3270Web                                           # run without building
```

Set `GOARCH`/`GOOS` to cross-compile, e.g. `GOARCH=arm64 ./scripts/build-linux.sh 3270Web-arm64`.

PowerShell users can run the equivalent wrapper:

```powershell
.\scripts\build-linux.ps1
```
Use `-Goarch arm64` or `-Goos linux -Goarch arm64` for cross-compiles.

### Run it

```bash
./3270Web
# → serving on http://localhost:3270  (Ctrl+C / SIGTERM to stop)
```

The process runs in the foreground and serves the full UI and REST API. There is
no desktop window on Linux — just point a browser at the address above.

### The binary is self-contained

The build embeds everything needed to serve a session:

- **Web UI** — all HTML templates, JavaScript, fonts, and images are compiled
  into the binary, so a single file serves the complete interface.
- **`s3270` (on `linux/amd64`)** — a matching `s3270` is bundled and extracted to
  a temporary directory on first launch, so no separate install is required.

!!! warning "Non-amd64 architectures"
    The bundled `s3270` is `linux/amd64` only. On other architectures (for
    example `arm64`), install `s3270` from your package manager and 3270Web will
    pick it up from `PATH`:

    ```bash
    sudo apt-get install s3270      # Debian/Ubuntu
    ```

    You can also point 3270Web at a specific `s3270` via the **Automation/Startup
    → Exec command** setting (see [Connect and Use 3270Web](configuration.md)).

### Files it writes

On startup the binary writes runtime state **next to the executable** (its own
directory):

- `.env` — generated default configuration (see below)
- `3270Web.log` — application log
- `chaos-runs/`, `chaos-hints.json` — chaos exploration output

Run it from a writable directory so these can be created.

### Configuration

3270Web reads `s3270` and application options from environment variables and the
generated `.env` file. For example:

```bash
GIN_MODE=release S3270_MODEL=3279-2-E S3270_CODE_PAGE=bracket ./3270Web
```

The full list of options and their meanings is described in
[Connect and Use 3270Web](configuration.md); they can also be edited live from the
in-app **Settings** modal.

---

## Run with Docker

The image is published **multi-arch** (`linux/amd64`, `linux/arm64`) to
`ghcr.io/3270io/3270web`. It installs the `s3270` package (`/usr/bin/s3270`), runs
as a non-root `app` user, exposes port 3270, and ships a container `HEALTHCHECK`.

### Use the published image

```bash
docker run --rm -p 3270:3270 ghcr.io/3270io/3270web:latest
```

### Build it yourself

```bash
docker build -t 3270web .
docker run --rm -p 3270:3270 3270web
```

### Environment variables

Pass `s3270`/app options with `-e`:

```bash
docker run --rm -p 3270:3270 \
  -e GIN_MODE=release \
  -e S3270_MODEL=3279-2-E \
  -e S3270_CODE_PAGE=bracket \
  ghcr.io/3270io/3270web:latest
```

To serve the [REST API](rest-api.md) and [MCP](mcp.md) as well as the browser
UI, add a token:

```bash
docker run --rm -p 3270:3270 \
  -e GIN_MODE=release \
  -e API_TOKEN="$API_TOKEN" \
  -e MCP_TOOLS=interactive \
  ghcr.io/3270io/3270web:latest
```

Without `API_TOKEN` every `/api/v1/*` request — MCP included — answers
`503`, so a published port is a browser UI and nothing more.

### Health check

The container probes `GET /healthz`, which returns:

```json
{ "status": "ok", "version": "3.1.0" }
```

Use it as a liveness/readiness probe in orchestrators, or inspect it directly:

```bash
docker inspect --format '{{.State.Health.Status}}' <container>   # healthy
```

### Persisting the data

The image sets `DATA_DIR=/data`, and everything a deployment accumulates goes
there: accounts, API tokens, the audit trail, chaos runs, saved tasks,
profiles and themes. Mount a folder on it, or a container replacement takes
all of it:

```bash
mkdir -p ./data && sudo chown -R 10001:10001 ./data

docker run -d -p 3270:3270 \
  -v ./data:/data \
  ghcr.io/3270io/3270web:latest
```

The `chown` is needed because 3270Web runs unprivileged as uid 10001 and a
bind mount keeps the host's ownership. Without it the container refuses to
start and tells you the command to run — deliberately, because the
alternative is writing state somewhere the next deploy deletes.

!!! tip
    Mount the data folder on `/data`, never on `/app`: the binary and the
    `web/` assets live there, and a mount over `/app` would shadow them.
    Everything else is configured through environment variables.

!!! warning "The sign-in page turned into a setup page"
    Nothing has been deleted. A container that starts against a data folder
    with no accounts in it has no way to tell "new install" from "wrong
    folder", so it does the only safe thing and offers first-run setup. The
    accounts are in the folder it *stopped* reading.

    Do not complete the setup form — it would create a second administrator
    on an empty instance. Find where the container is reading from and where
    it used to:

    ```bash
    docker inspect -f '{{range .Mounts}}{{.Source}} -> {{.Destination}}{{"\n"}}{{end}}' 3270web
    ```

    Then point the `:/data` mapping in `docker-compose.yml` back at the folder
    holding `users.json` and restart. Versions of the installer before this
    behaviour existed could repoint it when re-run from a different directory;
    the current one carries the folder forward and refuses to start against a
    different one.

!!! note "Upgrading from a version without `/data`"
    Older instances kept their files beside the program, and older stacks
    persisted only chaos runs, in a volume on `/app/chaos-runs`. On the first
    start with a data folder, 3270Web **moves anything it finds beside the
    program into it** and logs what it moved, so accounts and runs survive the
    change. If your stack mounted the old chaos volume, leave it mounted for
    that one start; afterwards it is empty and can be removed with
    `docker volume rm 3270web-chaos`. The installer does all of this for you.

### Listen address

| Variable | Default | Meaning |
|---|---|---|
| `WEBUI_BIND` | `127.0.0.1` (`0.0.0.0` in the Docker image) | Interface to listen on |
| `WEBUI_PORT` | `3270` | Port to listen on |

Outside a container the server binds to loopback, so the UI — which has no
password of its own — is not published to your local network by default.

The image overrides this with `WEBUI_BIND=0.0.0.0`, and it has to. A published
port forwards to the container's **external** interface, so a loopback-only
listener inside the container refuses every connection from the host — while the
container still reports healthy, because its `HEALTHCHECK` curls `127.0.0.1`
from *inside* the container. Control exposure with the port mapping instead.

!!! warning
    Do not set `WEBUI_BIND=127.0.0.1` in a container. The container will start and
    pass its healthcheck, but the browser will get a connection refused / "empty
    response" no matter how the ports are mapped. To keep the terminal off your
    network, restrict the **host** side of the mapping — `"127.0.0.1:3270:3270"` —
    not the bind address inside the container.

---

## Run with Docker Compose

The repository ships a `docker-compose.yml` that builds the image locally,
publishes port 3270 on this host, and requires a sign-in.

### Quick start

```bash
mkdir -p ./data && sudo chown -R 10001:10001 ./data
docker compose up --build
docker compose logs 3270Web    # prints the setup code for the first administrator
# → http://localhost:3270
```

The shipped file:

```yaml
services:
  3270Web:
    build: .
    image: ghcr.io/3270io/3270web:local
    ports:
      - "3270:3270"
    environment:
      - GIN_MODE=release
      - AUTH_MODE=local
      # - API_TOKEN=${API_TOKEN}
      # - MCP_TOOLS=interactive
    volumes:
      - ./data:/data
    restart: unless-stopped
```

The port mapping has no host address on it, so 3270 is published on every
interface — which is what makes the terminal reachable from a phone on the same
network, and equally what makes it reachable from everything else on that
network. `AUTH_MODE=local` is on for that reason: without a sign-in every
request arrives as the local operator, which carries the administrator role.

**Running it on your own machine, for yourself alone?** Drop the `AUTH_MODE`
line and change the mapping to `"127.0.0.1:3270:3270"`, so the listener is not
on the network in the first place. Do one or the other — the combination to
avoid is an open port with no sign-in. The server says so in its own log if it
ever finds itself in that state.

The container `HEALTHCHECK` is inherited automatically — `docker compose ps`
shows the health column.

### Serve the API and MCP from the stack

The two commented lines are the whole of it. `/api/v1` is off until
`API_TOKEN` is set — and [MCP over HTTP](mcp.md) lives under that prefix, at
`POST /api/v1/mcp`, so an AI client cannot reach a stack without one.

Put the token in a `.env` file beside `docker-compose.yml`, where Compose
picks it up for `${API_TOKEN}` without it being written into the stack file:

```bash
printf 'API_TOKEN=%s\n' "$(openssl rand -hex 24)" > .env
chmod 600 .env
```

Then uncomment both lines and bring the stack up:

```yaml
    environment:
      - GIN_MODE=release
      - API_TOKEN=${API_TOKEN}
      - MCP_TOOLS=interactive          # readonly | interactive | full
      # - MCP_ALLOWED_HOSTS=*.test.example.com
```

```bash
docker compose up -d
set -a; . ./.env; set +a
curl -sS -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:3270/api/v1/sessions
```

`200` means the API is live and MCP with it, at
`http://127.0.0.1:3270/api/v1/mcp`. `503` means the token is not reaching the
container; `401` means it is, but not the value you sent.
[MCP Server](mcp.md#docker-compose-and-remote-clients) covers connecting a
client to that endpoint, and the tool tiers `MCP_TOOLS` selects.

!!! warning "A token is a password for the terminal"
    An MCP client holding it can open sessions, type into fields and press
    keys on whatever host the stack can reach. Keep the port mapping on
    `127.0.0.1` unless the stack is behind TLS, set `MCP_ALLOWED_HOSTS` to
    fence off the hosts that matter, and treat the token like the mainframe
    credential it is standing next to.

### Use the published image instead of building

To run the prebuilt image rather than building from source, drop `build:` and
point `image:` at the published tag:

```yaml
services:
  3270Web:
    image: ghcr.io/3270io/3270web:latest
    ports:
      - "127.0.0.1:3270:3270"
    environment:
      - GIN_MODE=release
    restart: unless-stopped
```

```bash
docker compose pull
docker compose up -d
```

### Customising

- **Keep it off the network** — change the port mapping to
  `"127.0.0.1:3270:3270"`. Change the *host* side of the mapping only; leave
  `WEBUI_BIND` at the image default, or the container becomes unreachable
  however the ports are mapped.

- **Publish it, and require a sign-in** — a mapping with no host address on it
  (`"3270:3270"`) publishes on every interface, which is what the shipped file
  does so a phone on the same network can reach the terminal. Set
  `AUTH_MODE=local` alongside it.

    !!! warning "An open port with no sign-in is an open administrator"
        The UI has no password of its own until you set `AUTH_MODE=local`;
        until then every request arrives as the local operator, which carries
        the administrator role — sessions, settings, the audit log and restart.
        Publishing the port without it puts that, and whatever hosts the
        terminal can reach, on your network. The server prints a warning at
        startup if it finds itself listening that way. See
        [User Accounts and Sign-In](authentication.md).

- **Add options** — list more `S3270_*` variables under `environment:`.
- **Persist the data** — required for anything but a trial. Accounts, tokens,
  the audit trail and everyone's saved work live here:

    ```yaml
    volumes:
      - ./data:/data
    ```

    First: `mkdir -p ./data && sudo chown -R 10001:10001 ./data`. See
    [Keeping the state](multi-user.md#keeping-the-state).

---

## Verify it's running

```bash
curl -fsS http://localhost:3270/healthz   # {"status":"ok",...}
```

Then open [http://localhost:3270](http://localhost:3270) and head to
[Connect and Use 3270Web](configuration.md) to connect to your first host.
