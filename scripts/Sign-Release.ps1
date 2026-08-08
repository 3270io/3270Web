<#
.SYNOPSIS
    Interactive Authenticode signing for 3270Web release binaries.

.DESCRIPTION
    Run it with no arguments from VS Code and answer the prompts:

        .\scripts\Sign-Release.ps1

    It finds signtool, finds the certificate, finds the binaries, shows you
    what it is about to do, signs, timestamps and verifies.

    The certificate comes from the Windows certificate store. Certum's
    SimplySign Desktop puts a cloud certificate there by emulating a smart card
    reader, so a cloud-held key behaves exactly like a local one. A USB token or
    an imported .pfx works the same way; nothing here is Certum-specific.

    Two things about SimplySign are worth knowing, because both show up as
    confusing failures otherwise:

      * A logged-in session lasts two hours. When it lapses the certificate
        simply vanishes from the store. This script waits and lets you retry
        rather than failing, so you can log in and carry on.
      * Tick "Enable PIN cache for CSP/KSP-based applications" in SimplySign
        Desktop. Without it every file pops a PIN dialog.

.PARAMETER Path
    Files to sign. Skips the interactive file picker. Globs are accepted.

.PARAMETER Thumbprint
    Certificate to use. Skips the interactive certificate picker.

.PARAMETER TimestampUrl
    RFC 3161 timestamp authorities, tried in order. A signature without a
    timestamp stops being valid the day the certificate expires, so signing
    fails if every authority is unreachable.

.PARAMETER Yes
    Skip the final confirmation prompt.

.EXAMPLE
    .\scripts\Sign-Release.ps1

.EXAMPLE
    .\scripts\Sign-Release.ps1 -Path 3270Web.exe -Yes
#>
#Requires -Version 5.1
[CmdletBinding()]
param(
    [string[]]$Path,
    [string]$Thumbprint,
    [string[]]$TimestampUrl = @('http://time.certum.pl', 'http://timestamp.digicert.com'),
    [switch]$Yes
)

$ErrorActionPreference = 'Stop'

# ---------------------------------------------------------------- project ----

$ProjectName    = '3270Web'
$ProjectUrl     = 'https://3270Web.3270.io'
$RepoRoot       = Split-Path -Parent $PSScriptRoot
$BuildScript    = 'scripts\build-windows.ps1'
$BuildCommand   = '.\scripts\build-windows.ps1'
$CandidateGlobs = @('*.exe', 'dist\*.exe')

# ------------------------------------------------------------------- ui -------

$Sep = ([string][char]0x2500) * 68

function Write-Rule { Write-Host $Sep -ForegroundColor DarkGray }
function Write-Step {
    param([int]$Number, [int]$Total, [string]$Title)
    Write-Host ""
    Write-Host ("  [{0}/{1}]  " -f $Number, $Total) -ForegroundColor DarkGray -NoNewline
    Write-Host $Title -ForegroundColor White
    Write-Rule
}
function Write-Ok    { param([string]$m) Write-Host "  [ok]   " -ForegroundColor Green      -NoNewline; Write-Host $m }
function Write-Bad   { param([string]$m) Write-Host "  [fail] " -ForegroundColor Red        -NoNewline; Write-Host $m }
function Write-Warn2 { param([string]$m) Write-Host "  [warn] " -ForegroundColor Yellow     -NoNewline; Write-Host $m }
function Write-Info2 { param([string]$m) Write-Host "  [info] " -ForegroundColor Cyan       -NoNewline; Write-Host $m }
function Write-Dim   { param([string]$m) Write-Host $m -ForegroundColor DarkGray }

function Write-Banner {
    Write-Host ""
    Write-Host "  $ProjectName" -ForegroundColor Cyan -NoNewline
    Write-Host "  release signing" -ForegroundColor White
    Write-Dim  "  Authenticode sign, timestamp and verify Windows binaries."
    Write-Host ""
    Write-Rule
}

function Format-Size {
    param([long]$Bytes)
    if ($Bytes -ge 1MB) { return ('{0:N1} MB' -f ($Bytes / 1MB)) }
    if ($Bytes -ge 1KB) { return ('{0:N0} KB' -f ($Bytes / 1KB)) }
    return "$Bytes B"
}

function Get-CommonName {
    param([string]$Subject)
    if ($Subject -match 'CN=(?<cn>[^,]+)') { return $Matches['cn'].Trim() }
    return $Subject
}

function Read-Line {
    <#
        Read-Host returns $null once stdin reaches EOF, so calling .Trim() on it
        blows up with a null-reference error the moment this script is piped or
        run without a console. Returns $null for EOF and a trimmed string
        otherwise; callers treat $null as "abort".
    #>
    $line = $null
    try { $line = Read-Host } catch { return $null }
    if ($null -eq $line) { return $null }
    return $line.Trim()
}

function Read-Choice {
    <#
        Prompts until the answer is one of $Valid. Returns the lowercased key,
        or $null if input ended. An empty answer selects $Default, so pressing
        Enter always does the safe thing.
    #>
    param(
        [string]$Prompt,
        [string[]]$Valid,
        [string]$Default
    )
    while ($true) {
        Write-Host ""
        Write-Host "  $Prompt " -ForegroundColor White -NoNewline
        Write-Host "[$($Valid -join '/')]" -ForegroundColor DarkGray -NoNewline
        Write-Host " (default: $Default): " -ForegroundColor DarkGray -NoNewline

        $answer = Read-Line
        if ($null -eq $answer) { Write-Host ""; return $null }

        $answer = $answer.ToLowerInvariant()
        if (-not $answer) { return $Default.ToLowerInvariant() }
        if ($Valid -contains $answer) { return $answer }
        Write-Warn2 "Please answer one of: $($Valid -join ', ')"
    }
}

# -------------------------------------------------------------- discovery ----

function Find-SignTool {
    $onPath = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($onPath) { return $onPath.Source }

    # The Windows SDK keeps one signtool per SDK version per architecture.
    # Sorting by LastWriteTime rather than by the version string sidesteps the
    # classic "10.0.19041 sorts above 10.0.22621" lexicographic trap.
    $roots = @(
        "${env:ProgramFiles(x86)}\Windows Kits\10\bin",
        "$env:ProgramFiles\Windows Kits\10\bin"
    ) | Where-Object { $_ -and (Test-Path $_) }

    foreach ($root in $roots) {
        $found = Get-ChildItem -Path $root -Filter signtool.exe -Recurse -ErrorAction SilentlyContinue |
            Where-Object { $_.FullName -match '\\x64\\' } |
            Sort-Object LastWriteTime -Descending |
            Select-Object -First 1
        if ($found) { return $found.FullName }
    }
    return $null
}

function Get-SigningCertificates {
    # CurrentUser is where SimplySign Desktop publishes the cloud certificate.
    # LocalMachine is read too so a machine-wide import also works; it is not
    # readable without elevation, hence the silent continue.
    $stores = @('Cert:\CurrentUser\My', 'Cert:\LocalMachine\My') |
        Where-Object { Test-Path $_ }

    $certs = @(foreach ($store in $stores) {
        Get-ChildItem -Path $store -CodeSigningCert -ErrorAction SilentlyContinue
    })

    return @($certs |
        Where-Object { $_.NotAfter -gt (Get-Date) -and $_.HasPrivateKey } |
        Sort-Object NotAfter -Descending)
}

function Get-SignatureInfo {
    <#
        Describes a file's existing signature as a short status plus a colour,
        so the file table can show at a glance what still needs signing.
    #>
    param([string]$File)

    $sig = Get-AuthenticodeSignature -FilePath $File -ErrorAction SilentlyContinue
    if (-not $sig -or $sig.Status -eq 'NotSigned') {
        return [pscustomobject]@{ Text = 'unsigned'; Colour = 'Yellow'; Signed = $false }
    }
    if ($sig.Status -eq 'Valid') {
        $who = if ($sig.SignerCertificate) { Get-CommonName $sig.SignerCertificate.Subject } else { 'unknown' }
        return [pscustomobject]@{ Text = "signed: $who"; Colour = 'Green'; Signed = $true }
    }
    return [pscustomobject]@{ Text = "signed ($($sig.Status))"; Colour = 'Red'; Signed = $true }
}

# ------------------------------------------------------------------ steps ----

function Step-Environment {
    Write-Step 1 5 'Environment'

    Write-Ok ("PowerShell {0} on {1}" -f $PSVersionTable.PSVersion, [System.Environment]::OSVersion.VersionString)

    if (-not $IsWindowsHost) {
        Write-Bad 'Authenticode signing needs Windows.'
        Write-Dim  '         signtool.exe and the Windows certificate store do not exist here.'
        return $null
    }

    $signtool = Find-SignTool
    if (-not $signtool) {
        Write-Bad 'signtool.exe not found.'
        Write-Host ""
        Write-Dim  '         Install the Windows SDK and tick "Signing Tools for Desktop Apps":'
        Write-Dim  '           winget install --id Microsoft.WindowsSDK'
        return $null
    }
    Write-Ok "signtool: $signtool"

    # Informational only. SimplySign is one of several ways the certificate can
    # get into the store, so its absence is not an error.
    $simplySign = Get-Process -Name 'SimplySignDesktop', 'SimplySign*' -ErrorAction SilentlyContinue
    if ($simplySign) {
        Write-Ok 'SimplySign Desktop is running.'
    } else {
        Write-Info2 'SimplySign Desktop does not appear to be running.'
        Write-Dim   '         Fine if the certificate comes from a token or an imported .pfx.'
    }

    return $signtool
}

function Step-Certificate {
    param([string]$WantThumbprint, [switch]$Auto)

    Write-Step 2 5 'Certificate'

    # Capped, and skipped entirely under -Yes. Read-Host returns empty on a
    # closed stdin, which Read-Choice turns into the default answer, so an
    # uncapped "retry?" loop spins forever the moment this is run without a
    # terminal attached.
    $attemptsLeft = 10

    while ($true) {
        $certs = Get-SigningCertificates

        if ($WantThumbprint) {
            $clean = ($WantThumbprint -replace '[^0-9A-Fa-f]', '').ToUpperInvariant()
            $match = $certs | Where-Object { $_.Thumbprint -eq $clean } | Select-Object -First 1
            if ($match) { return $match }
            Write-Bad "No usable certificate with thumbprint $clean."
        }
        elseif ($certs.Count -eq 1) {
            Show-Certificate $certs[0]
            return $certs[0]
        }
        elseif ($certs.Count -gt 1) {
            Write-Info2 "$($certs.Count) code signing certificates available:"
            Write-Host ""
            for ($i = 0; $i -lt $certs.Count; $i++) {
                Write-Host ("    {0}. " -f ($i + 1)) -ForegroundColor White -NoNewline
                Write-Host (Get-CommonName $certs[$i].Subject) -ForegroundColor Cyan
                Write-Dim  ("       expires {0}   {1}" -f $certs[$i].NotAfter.ToString('yyyy-MM-dd'), $certs[$i].Thumbprint)
            }
            while ($true) {
                Write-Host ""
                Write-Host "  Which certificate? " -ForegroundColor White -NoNewline
                Write-Host "[1-$($certs.Count)]: " -ForegroundColor DarkGray -NoNewline
                $pick = Read-Line
                if ($null -eq $pick) { return $null }
                if ($pick -match '^\d+$' -and [int]$pick -ge 1 -and [int]$pick -le $certs.Count) {
                    $chosen = $certs[[int]$pick - 1]
                    Write-Host ""
                    Show-Certificate $chosen
                    return $chosen
                }
                Write-Warn2 "Enter a number between 1 and $($certs.Count)."
            }
        }
        else {
            Write-Bad 'No usable code signing certificate in the store.'
            Write-Host ""
            Write-Dim  '         If you use SimplySign, the session has probably lapsed:'
            Write-Dim  '         it lasts two hours. Open SimplySign Desktop and log in with'
            Write-Dim  '         a fresh code from the mobile app, then retry.'
        }

        if ($Auto) { return $null }

        $attemptsLeft--
        if ($attemptsLeft -le 0) {
            Write-Host ""
            Write-Bad 'Giving up after repeated attempts.'
            return $null
        }

        # The lapsed-session case is the common one and it is trivially
        # recoverable, so offer a retry instead of making them start over.
        $again = Read-Choice -Prompt 'Retry the certificate search?' -Valid @('r', 'q') -Default 'r'
        if ($null -eq $again -or $again -eq 'q') { return $null }
        $WantThumbprint = $null
        Write-Host ""
    }
}

function Show-Certificate {
    param($Cert)

    $daysLeft = [int]($Cert.NotAfter - (Get-Date)).TotalDays
    $colour = if ($daysLeft -lt 14) { 'Red' } elseif ($daysLeft -lt 45) { 'Yellow' } else { 'Green' }

    Write-Ok ("Using: {0}" -f (Get-CommonName $Cert.Subject))
    Write-Dim ("         issuer     {0}" -f (Get-CommonName $Cert.Issuer))
    Write-Dim ("         thumbprint {0}" -f $Cert.Thumbprint)
    Write-Host ("         expires    {0}  " -f $Cert.NotAfter.ToString('yyyy-MM-dd')) -ForegroundColor DarkGray -NoNewline
    Write-Host ("({0} days left)" -f $daysLeft) -ForegroundColor $colour

    if ($daysLeft -lt 45) {
        Write-Warn2 'Renew soon. Signatures already timestamped stay valid after expiry.'
    }
}

function Step-Files {
    param([string[]]$Explicit, [switch]$Auto)

    Write-Step 3 5 'Files'

    if ($Explicit) {
        $files = @()
        foreach ($p in $Explicit) {
            $resolved = @(Resolve-Path -Path $p -ErrorAction SilentlyContinue)
            if (-not $resolved) { Write-Bad "Not found: $p"; return $null }
            $files += $resolved.Path
        }
        foreach ($f in $files) { Write-Ok $f }
        return $files
    }

    $found = @()
    foreach ($glob in $CandidateGlobs) {
        $found += @(Get-ChildItem -Path (Join-Path $RepoRoot $glob) -File -ErrorAction SilentlyContinue)
    }
    $found = @($found | Sort-Object FullName -Unique)

    if ($found.Count -eq 0) {
        Write-Warn2 "No binaries found under $RepoRoot."
        Write-Dim   ("         Looked for: {0}" -f ($CandidateGlobs -join ', '))

        # Kicking off a build is too big a side effect to infer from -Yes,
        # which only means "don't ask me to confirm the signing".
        if ($Auto) { return $null }

        $build = Read-Choice -Prompt "Build them now with $BuildCommand ?" -Valid @('y', 'n') -Default 'y'
        if ($null -eq $build -or $build -eq 'n') { return $null }

        Write-Host ""
        Write-Info2 "Running $BuildCommand ..."
        # A child process, not dot-sourcing: the build script sets its own
        # error preferences and calls exit, neither of which should be able to
        # take this session down mid-run. Reuse whichever host is running us so
        # the build sees the same PowerShell edition you invoked.
        $host_exe = (Get-Process -Id $PID).Path
        Push-Location $RepoRoot
        try { & $host_exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $RepoRoot $BuildScript) }
        finally { Pop-Location }
        if ($LASTEXITCODE -ne 0) { Write-Bad 'Build failed.'; return $null }

        foreach ($glob in $CandidateGlobs) {
            $found += @(Get-ChildItem -Path (Join-Path $RepoRoot $glob) -File -ErrorAction SilentlyContinue)
        }
        $found = @($found | Sort-Object FullName -Unique)
        if ($found.Count -eq 0) { Write-Bad 'Still nothing to sign.'; return $null }
        Write-Host ""
    }

    Write-Info2 "$($found.Count) candidate file(s):"
    Write-Host ""
    $status = @{}
    for ($i = 0; $i -lt $found.Count; $i++) {
        $f = $found[$i]
        $info = Get-SignatureInfo $f.FullName
        $status[$f.FullName] = $info

        Write-Host ("    {0}. " -f ($i + 1)) -ForegroundColor White -NoNewline
        Write-Host ("{0,-28}" -f $f.Name) -ForegroundColor Cyan -NoNewline
        Write-Host ("{0,10}  " -f (Format-Size $f.Length)) -ForegroundColor DarkGray -NoNewline
        Write-Host ("{0,-17}  " -f $f.LastWriteTime.ToString('yyyy-MM-dd HH:mm')) -ForegroundColor DarkGray -NoNewline
        Write-Host $info.Text -ForegroundColor $info.Colour
    }

    # Default to the files that still need signing; re-signing an already-signed
    # binary is legitimate but is rarely what you meant to do by pressing Enter.
    $unsignedIdx = @()
    for ($i = 0; $i -lt $found.Count; $i++) {
        if (-not $status[$found[$i].FullName].Signed) { $unsignedIdx += ($i + 1) }
    }
    $defaultText = if ($unsignedIdx.Count -gt 0) { $unsignedIdx -join ',' } else { 'all' }

    while ($true) {
        Write-Host ""
        Write-Dim  "  Select files: numbers (1,3), a range (1-3), 'all', or 'q' to quit."
        Write-Host "  Which files? " -ForegroundColor White -NoNewline
        Write-Host "(default: $defaultText): " -ForegroundColor DarkGray -NoNewline
        $sel = Read-Line
        if ($null -eq $sel) { Write-Host ""; return $null }
        if (-not $sel) { $sel = $defaultText }
        if ($sel -eq 'q') { return $null }

        if ($sel -eq 'all') { return @($found | ForEach-Object { $_.FullName }) }

        $idx = New-Object System.Collections.Generic.List[int]
        $bad = $false
        foreach ($part in ($sel -split ',')) {
            $part = $part.Trim()
            if ($part -match '^(\d+)-(\d+)$') {
                foreach ($n in [int]$Matches[1]..[int]$Matches[2]) { $idx.Add($n) }
            } elseif ($part -match '^\d+$') {
                $idx.Add([int]$part)
            } else {
                $bad = $true; break
            }
        }
        if (-not $bad) {
            $picked = @($idx | Sort-Object -Unique | Where-Object { $_ -ge 1 -and $_ -le $found.Count })
            if ($picked.Count -gt 0) {
                return @($picked | ForEach-Object { $found[$_ - 1].FullName })
            }
        }
        Write-Warn2 "Didn't understand that. Try 1,2 or 1-3 or all."
    }
}

function Step-Confirm {
    param($Cert, [string[]]$Files, [switch]$Auto)

    Write-Step 4 5 'Confirm'

    Write-Host "  Certificate " -ForegroundColor DarkGray -NoNewline
    Write-Host (Get-CommonName $Cert.Subject) -ForegroundColor Cyan
    Write-Host "  Timestamp   " -ForegroundColor DarkGray -NoNewline
    Write-Host $TimestampUrl[0] -ForegroundColor Cyan
    Write-Host "  Digest      " -ForegroundColor DarkGray -NoNewline
    Write-Host "SHA-256" -ForegroundColor Cyan
    Write-Host "  Files       " -ForegroundColor DarkGray -NoNewline
    Write-Host "$($Files.Count)" -ForegroundColor Cyan
    foreach ($f in $Files) { Write-Dim ("                {0}" -f $f) }

    if ($Auto) { return $true }
    return (Read-Choice -Prompt 'Sign these files?' -Valid @('y', 'n') -Default 'y') -eq 'y'
}

function Invoke-Sign {
    param([string]$SignTool, $Cert, [string]$File)

    # Every timestamp authority is tried before giving up: a signature without a
    # timestamp dies with the certificate, so an unreachable TSA is a hard
    # failure rather than something to shrug off. Two attempts each, because
    # these services rate-limit and briefly fall over more than anything else in
    # the chain.
    $lastDetail = 'signing failed'

    for ($t = 0; $t -lt $TimestampUrl.Count; $t++) {
        $tsa = $TimestampUrl[$t]

        for ($attempt = 1; $attempt -le 2; $attempt++) {
            $out = & $SignTool sign `
                /sha1 $Cert.Thumbprint `
                /fd sha256 `
                /tr $tsa `
                /td sha256 `
                /d $ProjectName `
                /du $ProjectUrl `
                $File 2>&1

            if ($LASTEXITCODE -eq 0) {
                return [pscustomobject]@{ Ok = $true; Timestamp = $tsa; Detail = $null }
            }

            $lastDetail = (@($out) | Where-Object { $_ -match '\S' } | Select-Object -Last 1) -as [string]

            if ($attempt -lt 2) {
                Write-Host "retry " -ForegroundColor DarkYellow -NoNewline
                Start-Sleep -Seconds 3
            }
            elseif ($t -lt $TimestampUrl.Count - 1) {
                Write-Host "next-tsa " -ForegroundColor DarkYellow -NoNewline
            }
        }
    }

    return [pscustomobject]@{ Ok = $false; Timestamp = $null; Detail = $lastDetail }
}

function Step-Sign {
    param([string]$SignTool, $Cert, [string[]]$Files)

    Write-Step 5 5 'Signing'

    $results = @()
    foreach ($file in $Files) {
        $name = Split-Path $file -Leaf
        Write-Host ("  {0,-30} " -f $name) -ForegroundColor White -NoNewline

        $r = Invoke-Sign -SignTool $SignTool -Cert $Cert -File $file
        if (-not $r.Ok) {
            Write-Host "FAILED" -ForegroundColor Red
            $results += [pscustomobject]@{ File = $name; Ok = $false; Detail = $r.Detail }
            continue
        }

        # signtool can emit a signature that does not chain to a trusted root;
        # that only surfaces later as a SmartScreen warning on a user's machine.
        # /pa applies the Authenticode policy, which is the check Windows makes.
        $null = & $SignTool verify /pa /q $file 2>&1
        if ($LASTEXITCODE -ne 0) {
            Write-Host "signed, VERIFY FAILED" -ForegroundColor Red
            $results += [pscustomobject]@{ File = $name; Ok = $false; Detail = 'signature did not verify under the Authenticode policy' }
            continue
        }

        Write-Host "signed" -ForegroundColor Green -NoNewline
        Write-Dim ("  ({0})" -f $r.Timestamp)
        $results += [pscustomobject]@{ File = $name; Ok = $true; Detail = $r.Timestamp }
    }
    return $results
}

# ------------------------------------------------------------------- main ----

# $IsWindows exists in PowerShell 6+. In Windows PowerShell 5.1 it is undefined,
# and that edition only ever runs on Windows.
$IsWindowsHost = if ($null -eq (Get-Variable -Name IsWindows -ErrorAction SilentlyContinue)) { $true } else { $IsWindows }

$started = Get-Date
Write-Banner

$signtool = Step-Environment
if (-not $signtool) { Write-Host ""; exit 1 }

$cert = Step-Certificate -WantThumbprint $Thumbprint -Auto:$Yes
if (-not $cert) { Write-Host ""; Write-Dim '  Nothing signed.'; Write-Host ""; exit 1 }

$files = Step-Files -Explicit $Path -Auto:$Yes
if (-not $files) { Write-Host ""; Write-Dim '  Nothing signed.'; Write-Host ""; exit 1 }

if (-not (Step-Confirm -Cert $cert -Files $files -Auto:$Yes)) {
    Write-Host ""; Write-Dim '  Cancelled. Nothing signed.'; Write-Host ""; exit 130
}

$results = Step-Sign -SignTool $signtool -Cert $cert -Files $files

# ---------------------------------------------------------------- summary ----

$ok     = @($results | Where-Object { $_.Ok })
$failed = @($results | Where-Object { -not $_.Ok })

Write-Host ""
Write-Rule
Write-Host ""
if ($failed.Count -eq 0) {
    Write-Host "  Signed $($ok.Count) file(s)" -ForegroundColor Green -NoNewline
    Write-Dim (" in {0:N1}s." -f ((Get-Date) - $started).TotalSeconds)
    Write-Host ""
    Write-Dim  "  Attach them to the GitHub release, then publish it."
    Write-Host ""
    exit 0
}

Write-Host "  $($failed.Count) of $($results.Count) file(s) failed:" -ForegroundColor Red
foreach ($f in $failed) { Write-Dim ("    {0} - {1}" -f $f.File, $f.Detail) }
Write-Host ""

# Point at the cause that actually occurred. Guessing "your session lapsed"
# after a timestamp outage sends you to re-authenticate something that was
# never the problem.
$detailText = ($failed | ForEach-Object { $_.Detail }) -join ' '
if ($detailText -match 'timestamp') {
    Write-Dim '  Every timestamp server was unreachable. Check the network and try again;'
    Write-Dim '  these services do go down briefly. Pass -TimestampUrl to use another one.'
} elseif ($detailText -match 'private key|keyset|cannot be found|0x8009|smart card') {
    Write-Dim '  That reads like the key became unavailable mid-run, which is what a'
    Write-Dim '  lapsed SimplySign session looks like. Log in again and re-run.'
} else {
    Write-Dim '  If you use SimplySign, check the session is still live, then re-run.'
}
Write-Host ""
exit 1
