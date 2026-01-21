param(
  [string]$Output = "h3270-web.exe"
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
  go build -trimpath -ldflags "-s -w" -o $Output ./cmd/h3270-web
  Write-Host "Built: $Output"
} finally {
  Pop-Location
}
