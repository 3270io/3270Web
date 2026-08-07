# Install and Run

There are three supported ways to run 3270Web on Linux/Unix:

| Method | Best for | s3270 needed? |
|---|---|---|
| [Native binary](#run-the-linux-binary) | Bare-metal / VM installs, quick local use | Bundled on `linux/amd64`; install it for other arches |
| [Docker](#run-with-docker) | Containerized / cloud deployments | No — the image installs it |
| [Docker Compose](#run-with-docker-compose) | One-command local stack | No — the image installs it |

Whichever you choose, 3270Web listens on **port 8080** by default. Open
[http://localhost:8080](http://localhost:8080) once it is up and continue with
[Connect and Use 3270Web](configuration.md).

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

- **Expose beyond localhost** — change the port mapping to `"8080:8080"`.
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
