<#
.SYNOPSIS
    Authenticode-signs Windows binaries with the project's code signing certificate.

.DESCRIPTION
    Wraps signtool.exe so that local signing and CI signing are the same code
    path. The certificate is read from the Windows certificate store, which is
    where Certum's SimplySign Desktop publishes a cloud certificate: it emulates
    a smart card reader, so a cloud-held key looks exactly like a local one to
    signtool. Nothing here is Certum-specific — a USB token or an imported .pfx
    presents the same way.

    Prerequisites when signing against Certum's cloud certificate:
      1. SimplySign Desktop is installed and logged in (the session lasts two
         hours, after which it needs a fresh token from the SimplySign mobile
         app).
      2. "Enable PIN cache for CSP/KSP-based applications" is ticked in
         SimplySign Desktop, otherwise every single file prompts for a PIN and
         an unattended run blocks forever on an invisible dialog.

.PARAMETER Path
    One or more files to sign. Globs are accepted.

.PARAMETER Thumbprint
    SHA-1 thumbprint of the signing certificate. Optional: when omitted the
    script picks the sole code-signing certificate in the store and fails if
    there is more or less than one, which is safer than silently signing with
    whichever certificate happened to sort first.

.PARAMETER TimestampUrl
    RFC 3161 timestamp authority. Timestamping is what keeps a signature valid
    after the certificate expires, so it is not optional here — a failure to
    timestamp fails the run.

.EXAMPLE
    ./scripts/sign-windows.ps1 -Path 3270Web.exe
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string[]]$Path,

    [string]$Thumbprint = $env:WINDOWS_SIGNING_THUMBPRINT,

    [string]$TimestampUrl = 'http://time.certum.pl',

    [string]$Description = '3270Web',

    [string]$DescriptionUrl = 'https://3270Web.3270.io',

    [int]$TimestampRetries = 5
)

$ErrorActionPreference = 'Stop'

function Find-SignTool {
    $onPath = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($onPath) { return $onPath.Source }

    # The Windows SDK installs a signtool per SDK version and per architecture.
    # Sorting by LastWriteTime rather than by the version string avoids the
    # classic "10.0.19041 sorts above 10.0.22621" lexicographic trap.
    $roots = @(
        "${env:ProgramFiles(x86)}\Windows Kits\10\bin",
        "$env:ProgramFiles\Windows Kits\10\bin"
    ) | Where-Object { $_ -and (Test-Path $_) }

    foreach ($root in $roots) {
        $candidate = Get-ChildItem -Path $root -Filter signtool.exe -Recurse -ErrorAction SilentlyContinue |
            Where-Object { $_.FullName -match '\\(x64|x86)\\' } |
            Sort-Object LastWriteTime -Descending |
            Select-Object -First 1
        if ($candidate) { return $candidate.FullName }
    }

    throw "signtool.exe not found. Install the Windows SDK 'Signing Tools for Desktop Apps' component."
}

function Resolve-SigningCertificate {
    param([string]$Thumbprint)

    # CurrentUser is where SimplySign Desktop surfaces the cloud certificate;
    # LocalMachine is checked too so an imported machine-wide cert also works.
    $stores = @('Cert:\CurrentUser\My', 'Cert:\LocalMachine\My') |
        Where-Object { Test-Path $_ }

    $all = @(foreach ($store in $stores) {
        Get-ChildItem -Path $store -CodeSigningCert -ErrorAction SilentlyContinue
    })

    if ($Thumbprint) {
        $clean = ($Thumbprint -replace '[^0-9A-Fa-f]', '').ToUpperInvariant()
        $match = $all | Where-Object { $_.Thumbprint -eq $clean } | Select-Object -First 1
        if (-not $match) {
            throw "No code signing certificate with thumbprint $clean is present. Is the SimplySign Desktop session logged in?"
        }
        return $match
    }

    $valid = @($all | Where-Object { $_.NotAfter -gt (Get-Date) })
    if ($valid.Count -eq 0) {
        throw 'No unexpired code signing certificate found. Log in to SimplySign Desktop, or set WINDOWS_SIGNING_THUMBPRINT.'
    }
    if ($valid.Count -gt 1) {
        $list = ($valid | ForEach-Object { "  $($_.Thumbprint)  $($_.Subject)" }) -join "`n"
        throw "Multiple code signing certificates found; set WINDOWS_SIGNING_THUMBPRINT to choose one:`n$list"
    }
    return $valid[0]
}

$signtool = Find-SignTool
Write-Host "signtool:    $signtool"

$cert = Resolve-SigningCertificate -Thumbprint $Thumbprint
Write-Host "certificate: $($cert.Subject)"
Write-Host "thumbprint:  $($cert.Thumbprint)"
Write-Host "expires:     $($cert.NotAfter.ToString('yyyy-MM-dd'))"

$daysLeft = ($cert.NotAfter - (Get-Date)).Days
if ($daysLeft -lt 30) {
    # An expired certificate fails the build with an opaque signtool error, so
    # say it plainly while there is still time to renew. Certum's open source
    # certificates are issued for a year at a time.
    Write-Warning "Signing certificate expires in $daysLeft day(s)."
}

$files = @(foreach ($p in $Path) {
    $resolved = Resolve-Path -Path $p -ErrorAction SilentlyContinue
    if (-not $resolved) { throw "File not found: $p" }
    $resolved.Path
})

foreach ($file in $files) {
    Write-Host ""
    Write-Host "Signing $file"

    $signed = $false
    for ($attempt = 1; $attempt -le $TimestampRetries; $attempt++) {
        $output = & $signtool sign `
            /sha1 $cert.Thumbprint `
            /fd sha256 `
            /tr $TimestampUrl `
            /td sha256 `
            /d $Description `
            /du $DescriptionUrl `
            $file 2>&1

        if ($LASTEXITCODE -eq 0) {
            $signed = $true
            break
        }

        # Public timestamp authorities rate-limit and go down; that is by far
        # the most common cause of a failed signing run and it is transient, so
        # it is worth backing off rather than failing the release.
        Write-Warning "signtool attempt $attempt/$TimestampRetries failed:`n$output"
        if ($attempt -lt $TimestampRetries) {
            Start-Sleep -Seconds ([Math]::Pow(2, $attempt))
        }
    }

    if (-not $signed) {
        throw "Failed to sign $file after $TimestampRetries attempt(s)."
    }

    # Signing can succeed while producing a signature that does not chain to a
    # trusted root, which only shows up on a user's machine as a SmartScreen
    # warning. /pa applies the Authenticode policy, so this is the same check
    # Windows will make.
    & $signtool verify /pa /v $file
    if ($LASTEXITCODE -ne 0) {
        throw "Signature verification failed for $file."
    }
}

Write-Host ""
Write-Host "Signed and verified $($files.Count) file(s)."
