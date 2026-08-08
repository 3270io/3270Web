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
| `--port <port>` | `8080` | Host port to serve on |
| `--bind <address>` | `127.0.0.1` | Host interface to publish on |
| `--dir <path>` | `./3270web` | Compose project directory |
| `--system` | off | Binary install to `/opt` + `/usr/local/bin` |
| `--user` | on | Binary install under `$HOME` |
| `--theme <grn\|amb\|ice\|day>` | `grn` | Installer palette, matching the docs themes |
| `--no-color` / `--color` | auto | Force colour off or on |
| `--yes`, `-y` | off | Accept every prompt — use in CI |
| `--dry-run` | off | Report what would happen, change nothing |
| `--help`, `-h` | | Usage |

Non-interactive by design: with no TTY (a CI job, a provisioning script) the
installer stops asking and picks the binary on `amd64`, Docker elsewhere.

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

Whichever you choose, 3270Web listens on **port 8080** by default. Open
[http://localhost:8080](http://localhost:8080) once it is up and continue with
[Connect and Use 3270Web](configuration.md).

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
# → serving on http://localhost:8080  (Ctrl+C / SIGTERM to stop)
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
as a non-root `app` user, exposes port 8080, and ships a container `HEALTHCHECK`.

### Use the published image

```bash
docker run --rm -p 8080:8080 ghcr.io/3270io/3270web:latest
```

### Build it yourself

```bash
docker build -t 3270web .
docker run --rm -p 8080:8080 3270web
```

### Environment variables

Pass `s3270`/app options with `-e`:

```bash
docker run --rm -p 8080:8080 \
  -e GIN_MODE=release \
  -e S3270_MODEL=3279-2-E \
  -e S3270_CODE_PAGE=bracket \
  ghcr.io/3270io/3270web:latest
```

### Health check

The container probes `GET /healthz`, which returns:

```json
{ "status": "ok", "version": "3.1.0" }
```

Use it as a liveness/readiness probe in orchestrators, or inspect it directly:

```bash
docker inspect --format '{{.State.Health.Status}}' <container>   # healthy
```

### Persisting chaos output

The app writes chaos runs to `/app/chaos-runs` inside the container. Mount a
volume there to keep them across restarts:

```bash
docker run --rm -p 8080:8080 \
  -v 3270web-chaos:/app/chaos-runs \
  ghcr.io/3270io/3270web:latest
```

!!! tip
    Configure 3270Web through environment variables rather than bind-mounting all
    of `/app` — the binary and embedded `web/` assets live there, and a mount over
    `/app` would shadow them.

### Listen address

| Variable | Default | Meaning |
|---|---|---|
| `WEBUI_BIND` | `127.0.0.1` (`0.0.0.0` in the Docker image) | Interface to listen on |
| `WEBUI_PORT` | `8080` | Port to listen on |

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
    network, restrict the **host** side of the mapping — `"127.0.0.1:8080:8080"` —
    not the bind address inside the container.

---

## Run with Docker Compose

The repository ships a `docker-compose.yml` that builds the image locally and
binds it to `127.0.0.1:8080`.

### Quick start

```bash
docker compose up --build
# → http://127.0.0.1:8080
```

The shipped file:

```yaml
services:
  3270Web:
    build: .
    image: ghcr.io/3270io/3270web:local
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      - GIN_MODE=release
    restart: unless-stopped
```

The container `HEALTHCHECK` is inherited automatically — `docker compose ps`
shows the health column.

### Use the published image instead of building

To run the prebuilt image rather than building from source, drop `build:` and
point `image:` at the published tag:

```yaml
services:
  3270Web:
    image: ghcr.io/3270io/3270web:latest
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      - GIN_MODE=release
    restart: unless-stopped
```

```bash
docker compose pull
docker compose up -d
```

### Customising

- **Expose beyond localhost** — change the port mapping to `"8080:8080"`. Change
  the *host* side of the mapping only; leave `WEBUI_BIND` at the image default.
- **Add options** — list more `S3270_*` variables under `environment:`.
- **Persist chaos runs** — add a volume:

    ```yaml
    volumes:
      - 3270web-chaos:/app/chaos-runs
    ```

---

## Verify it's running

```bash
curl -fsS http://localhost:8080/healthz   # {"status":"ok",...}
```

Then open [http://localhost:8080](http://localhost:8080) and head to
[Connect and Use 3270Web](configuration.md) to connect to your first host.
