# Release signing

How `3270Web.exe` gets an Authenticode signature, and what has to be true
for `.github/workflows/release.yml` to do it automatically.

## Why this is not just a secret and an action

Certum's **Code Signing in the Cloud** stores the private key in Certum's HSM,
which is what the CA/Browser Forum baseline requirements have demanded since
2023. The "cloud" refers to where the key lives, not to an interface you can
call.

There is no signing API. The only client is **SimplySign Desktop**, a GUI
application that emulates a smart card reader so the cloud certificate appears
in the Windows certificate store; `signtool.exe` then uses it exactly as it
would a USB token. Authenticating that emulated reader needs a one-time code
from the SimplySign mobile app, and the resulting session lasts **two hours**.

Three consequences follow, and they are the reason this file exists:

- **GitHub-hosted runners cannot sign.** `windows-latest` is a fresh VM per job
  with no logged-in SimplySign session, and no secret can substitute for one.
- **The runner cannot be a Windows service.** A service runs in its own logon
  session and will not see the certificate that SimplySign published into the
  interactive desktop's store. It must run interactively.
- **Something has to re-authenticate every two hours**, or before each release.

This is not specific to this project. Certum's own resellers tell buyers to
verify the two-hour window suits their deployment before purchasing, and other
open source projects on the same "Open Source Developer" product have hit the
same wall. Certum has said CI/CD support is planned, with no date.

## Current state: signing is off

`release.yml` runs on a `v*` tag and produces a **draft** release with unsigned
binaries and a `SHA256SUMS.txt`. To finish a release today:

1. Download `3270Web.exe` from the draft release.
2. Sign it on a machine with a SimplySign session:
   ```powershell
   ./scripts/sign-windows.ps1 -Path 3270Web.exe
   ```
3. Re-upload it, regenerate `SHA256SUMS.txt` (`sha256sum ... > SHA256SUMS.txt`),
   and publish the draft.

That is the same manual step as before; everything around it is now automated.

## Turning signing on

### 1. Prepare the Windows machine

Use the machine that already signs releases.

- Install SimplySign Desktop and confirm `signtool` can sign by hand.
- In SimplySign Desktop, enable **"PIN cache for CSP/KSP-based applications."**
  Without it every file prompts for a PIN, and an unattended job blocks
  forever on a dialog nobody can see.
- Disable screen lock and sleep for the signing account. Locking the desktop
  can tear down the emulated card session.

### 2. Register it as a self-hosted runner

Under **Settings → Actions → Runners → New self-hosted runner**, then:

- Add the labels `windows` and `signing`. `release.yml` targets
  `[self-hosted, windows, signing]`, so it will not land on the existing Linux
  self-hosted runner that `docker-publish.yml` uses.
- **Run it interactively** — `run.cmd` from the logged-in signing account, not
  `svc.cmd install`. Auto-login plus a logon-triggered scheduled task makes
  this survive a reboot.

A self-hosted runner executes whatever a workflow tells it to, so treat that
machine as holding a release key: keep it off the public internet where
possible, and note that `pull_request` runs never reach it — `release.yml`
triggers only on tags and manual dispatch.

### 3. Set the repository variables

Under **Settings → Secrets and variables → Actions → Variables**:

| Variable | Value | Purpose |
|---|---|---|
| `WINDOWS_SIGNING` | `enabled` | Turns the `sign` job on |
| `WINDOWS_SIGNING_THUMBPRINT` | SHA-1 thumbprint | Optional; needed only if the store holds more than one code signing certificate |

Neither is a secret — a thumbprint is public information, present in every
signed binary. There is deliberately no secret here, because there is no
key material to give GitHub.

Find the thumbprint with:

```powershell
Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert |
  Format-List Subject, Thumbprint, NotAfter
```

## Keeping the session alive

Two hours is the whole problem. Pick one:

**Log in before releasing (recommended).** Releases are tag-driven and
infrequent. Cut the tag, unlock SimplySign from your phone, and the job signs
well inside the window. Zero extra machinery, and the certificate stays behind
a second factor.

**Script the login.** The QR code from SimplySign enrolment is a standard
`otpauth://` URI, so the code can be generated on the machine and typed into
SimplySign Desktop by a scheduled task. This gets to genuinely unattended
signing.

Be clear-eyed about the trade: storing the TOTP seed on the build machine puts
both factors in one place, so anyone who compromises that machine can sign as
you for the life of the certificate. That may well be an acceptable trade for
an open source certificate on a machine you control — but it should be a
decision, not a side effect.

## If the two-hour window stops being workable

The constraint is Certum's, not GitHub's, and it does not apply to signing
services built for pipelines — Azure Trusted Signing, SSL.com eSigner,
DigiCert KeyLocker, or SignPath (which has a free tier for open source) all
expose a CLI or REST interface a hosted runner can call with a secret.

Moving would mean a different certificate, but `scripts/sign-windows.ps1` is
the only place that knows how signing works, and the `sign` job is the only
place that knows where it runs.
