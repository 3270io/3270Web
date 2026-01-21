# h3270

Web-based 3270 terminal interface in Go with session recording to a 3270Connect-compatible workflow.

## Features
- Web UI for 3270 sessions
- Embedded s3270 binary support (Windows)
- Record sessions to workflow.json (Connect/FillString/Press keys/Disconnect)
- Docker image and GHCR workflow
- Windows build script

## Requirements
- Go 1.22+
- Access to a 3270 host

## Run locally
```bash
go run ./cmd/h3270-web
```
Then open http://localhost:8080

## Build Windows EXE
```powershell
.\scripts\build-windows.ps1
```
This produces `h3270-web.exe` in the repo root.

## Docker
```bash
docker build -t h3270 .
docker run -p 8080:8080 h3270
```
The Docker image installs the `s3270` package so it is available at `/usr/bin/s3270`.

## Recording workflow.json
1. Connect to a host.
2. Click **Start Recording**.
3. Interact with the screen (edits + Enter/PF keys).
4. Click **Stop Recording**.
5. Download `workflow.json`.

The output matches the 3270Connect workflow format.

## Configuration
The app loads `webapp/WEB-INF/h3270-config.xml` if present. If missing, defaults are used.

## GitHub Actions
A workflow is included to build and push images to GHCR on pushes to `main`.
