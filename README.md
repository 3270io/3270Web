# 3270Web

Web-based 3270 terminal interface in Go with session recording to a 3270Connect-compatible workflow.

## Features
- Web UI for 3270 sessions
- Embedded s3270 binary support
- Record sessions to workflow.json (Connect/FillString/Press keys/Disconnect)
- Docker image and GHCR workflow
- Windows build script

## Requirements
- Go 1.22+
- Access to a 3270 host

## Run locally
```bash
go run ./cmd/3270Web
```
Then open http://localhost:8080

## Build Windows EXE
```powershell
.\scripts\build-windows.ps1
```
This produces `3270Web.exe` in the repo root.

## Docker
```bash
docker build -t 3270Web .
docker run -p 8080:8080 3270Web
```

## Recording workflow.json
1. Connect to a host.
2. Click **Start Recording**.
3. Interact with the screen (edits + Enter/PF keys).
4. Click **Stop Recording**.
5. Download `workflow.json`.

The output matches the 3270Connect workflow format.

## Configuration
The app loads `webapp/WEB-INF/3270Web-config.xml` if present. If missing, defaults are used.

## GitHub Actions
A workflow is included to build and push images to GHCR on pushes to `main`.
