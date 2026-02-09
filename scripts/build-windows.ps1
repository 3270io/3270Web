param(
  [string]$Output = "3270Web.exe"
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$repo = Split-Path -Parent $root

Push-Location $repo
try {
  $env:GOOS = "windows"
  $env:GOARCH = "amd64"
  $env:CGO_ENABLED = "0"

  Write-Host "Building Windows executable..."
  Write-Host "Generating Windows resources (icon)..."
  go run github.com/tc-hib/go-winres@latest simply --icon ./web/static/3270Web_logo.png --out ./cmd/3270Web/rsrc --manifest gui
  if ($LASTEXITCODE -ne 0) {
    throw "go-winres failed with exit code $LASTEXITCODE"
  }
  go build -trimpath -ldflags "-s -w -H=windowsgui" -o $Output ./cmd/3270Web
  if ($LASTEXITCODE -ne 0) {
    throw "go build failed with exit code $LASTEXITCODE. If '$Output' is locked, close any running 3270Web instances and try again."
  }
  Write-Host "Built: $Output"
} finally {
  Pop-Location
}
