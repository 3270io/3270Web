param(
  [string]$Output = "3270Web",
  [string]$Goos = "linux",
  [string]$Goarch = (go env GOARCH)
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$repo = Split-Path -Parent $root

Push-Location $repo
try {
  $env:GOOS = $Goos
  $env:GOARCH = $Goarch
  $env:CGO_ENABLED = "0"

  Write-Host "Building Linux executable (GOOS=$env:GOOS GOARCH=$env:GOARCH)..."
  go build -trimpath -ldflags "-s -w" -o $Output ./cmd/3270Web
  if ($LASTEXITCODE -ne 0) {
    throw "go build failed with exit code $LASTEXITCODE"
  }
  Write-Host "Built: $Output"
} finally {
  Pop-Location
}