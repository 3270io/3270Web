# Running a shared instance

Everything here is about the same situation: 3270Web on a port more than one
person can reach. On a laptop almost none of it applies — the controls below
are off until you turn them on, and the two that are always on (data
separation and the audit trail) have nothing to do on an instance with one
user.

Start with [accounts](authentication.md) — without them there is only one
identity, and nothing below has anybody to attribute anything to. Then work
down this page: what people are separated *from*, what the connection itself
exposes, what an automated client may do, which hosts the terminal may be
pointed at, and what is written down.

| Control | Default | What it decides |
|---|---|---|
| [`AUTH_MODE`](authentication.md) | `none` | Whether there are accounts at all |
| [Session and data separation](#what-is-separated-and-what-is-not) | on with accounts | What one account can see of another's |
| [`TRUST_PROXY_HEADERS`](#running-without-tls) | off | Whether cookies are marked `Secure` behind a TLS proxy |
| [API tokens](#api-tokens) | — | What an automated client may reach |
| [`ALLOWED_HOSTS`](#limiting-what-an-instance-can-reach) | unset | Which mainframes the terminal may be pointed at |
| [`RATE_LIMIT_*`](#rate-limits) | see below | How fast one caller may use what the instance pays for |
| [The audit trail](#the-audit-trail) | always on | What is recorded, and who may read it |
| [`DATA_DIR`](#keeping-the-state) | beside the program | Where accounts and everyone's work are kept |

---

## Keeping the state

Accounts, API tokens, the audit trail and everyone's saved work are files. By
default they sit beside the program, which is right for a desktop install and
wrong for a container: the image is replaced on every deploy.

**In Docker, keep them in a folder on the host.** The published image sets
`DATA_DIR=/data`, and the compose file bind-mounts a folder beside itself:

```yaml
services:
  3270Web:
    image: ghcr.io/3270io/3270web:latest
    environment:
      - AUTH_MODE=local
    volumes:
      - ./data:/data
```

A host folder rather than a named volume, so the accounts and the audit trail
can be backed up, inspected and copied with ordinary tools instead of through
`docker volume`.

Create it and hand it to the user the server runs as, before the first start:

```bash
mkdir -p ./data && sudo chown -R 10001:10001 ./data
```

A bind mount keeps the host's ownership — unlike a named volume, Docker does
not adjust it — and the server runs unprivileged as uid 10001. Skip this and
the container **refuses to start**, naming the directory and the `chown`. That
is deliberate: the alternative is falling back to the image layer, where
everything works until the deploy that silently deletes every account.

Without a data folder of some kind, `docker compose pull && docker compose up
-d` — the ordinary way to take an upgrade — takes every account with it. The
instance comes back with no accounts, which means it comes back in **first-run
setup**, waiting for whoever reaches it first to claim it. The audit trail of
what happened before the upgrade goes at the same time, and issued API tokens
stop working.

`DATA_DIR` names the directory; the program's own directory keeps the binary
and the web assets, which is why the folder is mounted somewhere else rather
than over the top of them. The account and token CLIs read the same setting, so
`docker compose exec 3270Web /app/3270Web user add alice` edits the accounts the
running server is using.

### Backing it up

It is a directory of small files, so a copy is a backup:

```bash
tar czf 3270web-$(date +%F).tar.gz -C ./data .
```

It holds password hashes and token hashes — no plaintext of either — plus the
audit trail and everyone's saved work. Treat it as you would any file naming
your users and the hosts they reach.

---

## What is separated, and what is not

**Terminal sessions are private to the account that opened them.** Holding a
session's ID is no longer enough to use it: every route resolves the session
through one ownership check, and a session belonging to somebody else is
reported as not found rather than refused, so the difference cannot be used to
discover which IDs are real. This includes administrators — administering the
instance means managing settings, logs and accounts, not typing into another
person's authenticated terminal.

Each account is also capped at `MAX_SESSIONS_PER_USER` concurrent sessions
(default 6), with `MAX_TOTAL_SESSIONS` (default 64) bounding the instance.
Every session is an `s3270` subprocess, so these are process limits rather
than tidiness.

Instance-wide administration — settings, logs, restart and account management
— requires the `admin` role.

**Chaos runs, hints and saved tasks are private too.** A saved run holds
captured screens and the field values that produced them — a record of a real
application's contents — so each account keeps its own. Under
`AUTH_MODE=local` these live in `users/<account-id>/` beneath the data
directory, with published files in `shared/`. A single-operator instance keeps
the flat layout it always had.

**Connection profiles and themes are shared, deliberately.** Everyone connects
to the same mainframes, so a host list only has to be entered once. An
administrator publishes a profile or theme with the **Share with everyone**
option; everybody sees it, and only an administrator can change or remove it.

Saving without that option gives you your own copy. If your copy has the same
name as a published one, yours is what you see — the same way overriding a
setting works — and everybody else keeps the published version.

**Saved tasks are private**, like chaos runs. A task is a recorded procedure
somebody is working on rather than infrastructure the team shares.

When authentication is switched on for an existing instance, the migration
follows the same reasoning: connection profiles and themes become the
published set so nobody loses the host list they were using, while chaos runs,
hints and saved tasks go to the first administrator.

**API tokens belong to accounts too** — see [API tokens](#api-tokens) below.
A token reaches exactly what its owner reaches and nothing else, so an
automated client is no way around any of the above.

For genuine separation between people who should not see each other's work at
all, run one container and one volume per user.

---

## Running without TLS

3270Web does not terminate TLS itself. On a private network many deployments
run it over plain HTTP, which is workable but has consequences worth stating
plainly.

**Anyone who can observe traffic on the network segment can read the password
as it is submitted, and can copy the session cookie afterwards.** No setting
changes that. The sign-in page says so when the connection is not encrypted.

Two controls reduce what a copied cookie is worth:

- **Address pinning** (`AUTH_BIND_SESSION_IP`, on by default without TLS)
  refuses a session presented from a different address, so a cookie captured
  passively cannot simply be replayed from the attacker's own machine.
- **Short lifetimes** bound how long a captured cookie stays valid.

Neither makes plain HTTP equivalent to TLS. An attacker positioned on the path
— rather than merely listening — can still work around both.

If you can put a TLS-terminating reverse proxy in front, do; it is the single
largest improvement available. Then set:

```bash
TRUST_PROXY_HEADERS=true
```

so 3270Web reads `X-Forwarded-Proto` and `X-Forwarded-For`, marks its cookies
`Secure`, and sees real client addresses instead of the proxy's. Only enable
it behind a proxy you control: those headers are set by whoever sends the
request, so a directly-reachable instance would let any client assert its own
connection is secure and choose its own apparent address.

---

## API tokens

Automated clients — CI jobs, RPA bots, AI clients over
[MCP](mcp.md) — authenticate with a Bearer token rather than a password.

Which token depends on whether the instance has accounts:

| | Credential | Reaches |
|---|---|---|
| Single operator (`AUTH_MODE=none`) | the `API_TOKEN` environment variable | everything, because there is one person |
| Accounts (`AUTH_MODE=local`) | a token issued to an account | exactly what that account reaches |

With accounts on, `API_TOKEN` is **refused at startup**. One credential held
by every client would reach every account's sessions, which is the thing the
mode was turned on to prevent; starting anyway would leave you believing users
were separated while one environment variable said otherwise.

### Issuing one

```bash
3270Web token add alice "ci pipeline"
3270Web token add alice scraper --read-only
3270Web token add alice deploy --expires 720h
3270Web token list
3270Web token list alice
3270Web token revoke 3f1c8a24b90de7c5
3270Web token revoke-all alice
```

Inside a container:

```bash
docker compose exec 3270Web /app/3270Web token add alice "ci pipeline"
```

The token is printed once, when it is issued:

```
issued 3f1c8a24b90de7c5 for alice (read+write)

  3270w_3f1c8a24b90de7c5_kzq4…

This is the only time the token is shown.
```

Only a hash is stored, so a copy of the token file yields nothing usable — and
a lost token is replaced rather than recovered. The `3270w_` prefix is there so
a leaked credential is recognisable to a secret scanner.

### Scopes

`--read-only` issues a token that can read but not change anything: it may
fetch screens, list sessions and read catalogues, but not type into a field,
press a key, or open or close a session. Anything that changes state is
refused with `403`.

Scope follows the HTTP method — `GET`, `HEAD` and `OPTIONS` are reads,
everything else is a write. [MCP over HTTP](mcp.md) is a `POST` for every tool
call, including read-only ones, so an MCP client needs a full token.

### Lifetime

A token works until it is revoked, until `--expires` passes, or until its
account is disabled or deleted — the owner is looked up on every call, so
disabling somebody stops their automated clients at the same moment it stops
them signing in. Re-enabling the account brings its tokens back rather than
making everything be reissued.

Refusals are deliberately identical whether a token is unknown, revoked or
expired. Saying which would confirm that a presented token is real.

### With MCP

An AI client that launches `3270Web mcp` itself cannot sign in, so on an
instance with accounts it needs a token and an explicit URL:

```bash
3270Web mcp --url http://127.0.0.1:8080 --token "$MY_TOKEN"
```

Tool calls then act as that account: `list_sessions` shows its sessions, and
`use_session` reaches its sessions only.

---

## Limiting what an instance can reach

A 3270 terminal is a client for arbitrary TCP. Whoever can open a session can
point it at anything the server can reach, which on a hosted instance means
the terminal is a route into the network it sits in.

`ALLOWED_HOSTS` fences that:

```bash
ALLOWED_HOSTS=*.mainframe.corp.example,10.20.30.*
```

Comma-separated shell globs, matched against the host part so the port does
not have to be written out. It applies on **every** path — the connect form,
the tab bar, the REST API, workflow playback, and MCP — because a fence with
one gate open is not a fence. A refusal is recorded in the
[audit trail](#the-audit-trail).

Unset means unrestricted. That is the historical behaviour and the right
default for a laptop or a lab; an allowlist you must configure before the
product works at all is one people switch off.

Two things are deliberately outside it. Hostname *validity* is a separate,
always-on check that refuses loopback, link-local and the unspecified address
— those are never dialled on a caller's behalf, allowlist or not. And the
bundled sample apps are exempt: they are this process talking to itself, and
they already have their own switch in `ALLOW_SAMPLE_APPS`.

`MCP_ALLOWED_HOSTS` still exists and is now a **narrower** fence for AI
clients specifically, on top of `ALLOWED_HOSTS`. A deployment can be willing
to reach its whole estate from a browser while letting a model near only the
test LPAR.

---

## Rate limits

A handful of routes cost the instance something rather than the caller:
opening a session starts a subprocess, chaos exploration presses keys at a
mainframe unattended, a transfer moves a file, AI chat spends an upstream
quota.

| Variable | Default | Applies to |
|---|---|---|
| `RATE_LIMIT_CONNECT` | 20/min | Opening a session, on every path |
| `RATE_LIMIT_CHAOS` | 10/min | Starting or resuming chaos exploration |
| `RATE_LIMIT_TRANSFER` | 20/min | IND$FILE send and receive |
| `RATE_LIMIT_AI` | 60/min | The AI chat endpoint |

Counted per account — per address where there are no accounts — so one busy
person cannot throttle everybody, and nobody gets a fresh allowance by opening
another tab. `0` turns a limit off.

The defaults are generous on purpose. These exist to stop a runaway loop or a
deliberate flood, not to pace ordinary work: a limit that honest use runs into
is a limit somebody removes entirely.

Everything else is unlimited. Reading a screen is cheap and constant, and a
general request limiter would be the change that gets the whole idea thrown
out. Signing in has [its own throttle](authentication.md#session-lifetime),
which is a different problem — that one is about guessing, not about cost.

---

## The audit trail

Sign-ins, sessions and changes to the instance are recorded in a file of their
own, separate from the debug log. An administrator reads it at
**/admin/audit**, or downloads it whole.

The debug log is for diagnosing the server: verbose, written by whatever code
happens to be running, and its wording changes whenever a message reads badly.
That does not suit the question an audit answers — *who opened a session
against that host, and when* — which has to survive being asked months later.

### What is recorded

| Event | Recorded with |
|---|---|
| Sign-in succeeded, failed, or throttled | account, address, and why it failed |
| Sign-out, password changed | account, address |
| First administrator created, or a wrong setup code | address |
| Account created, changed, deleted | who did it, and to whom |
| API token issued, revoked, or refused | token id, account, scopes |
| Session opened, or refused | account, target host, and why it was refused |
| File transfer | direction and the host-side dataset name |
| Settings changed | which keys — never their values |
| Server restarted, log access changed | who did it |

A refused sign-in is recorded **with the real reason** — a disabled account is
distinguished from a wrong password — even though the reply to the browser
never says which. The person at the keyboard must not learn which usernames
exist; the administrator reading the trail is entitled to know.

### What is never recorded

Passwords, tokens, screen contents, and the values typed into fields. Settings
appear as the keys that changed, because one of them holds a keyfile password.
The file is read by every administrator and may be shipped elsewhere, so
anything written to it is disclosed for as long as it exists.

Successful API calls are not recorded either — only refused ones. A line per
request would turn the trail into an access log and bury everything else in it.

### The file

One JSON object per line, appended, mode `0600`, at `audit.log` beside the
account store (`AUDIT_LOG_PATH` moves it). It can be read with the tools you
already have:

```bash
jq -r 'select(.event == "session.opened") | [.time, .actor.username, .target] | @tsv' audit.log
```

It rolls over at 8 MiB, keeping one previous generation — enough that a
restart or a busy afternoon does not lose the morning. **A deployment that
needs real retention should ship the lines somewhere else**; two files on the
same disk are a bound on size, not an archive.

There is no switch to turn it off. A trail somebody can disable before acting
is not a trail — which is also why the file is admin-readable but the
`ALLOW_LOG_ACCESS` toggle that gates the *debug* log does not apply to it, and
why turning that toggle on is itself recorded here.

Writing is best-effort: if the disk fills, the event is lost and the failure
goes to the debug log, but the request still succeeds. An audit that can refuse
a sign-in because a disk filled up is a denial of service dressed as a
safeguard.

---

## Checking it works

The Go tests cover each control on this page. What they cannot cover is
whether the parts compose in a browser against a real server, so there is a
script that walks the whole thing:

```bash
AUTH_MODE=local ALLOW_SAMPLE_APPS=1 go run ./cmd/3270Web    # another terminal
node scripts/check-multi-user.mjs --code EJWQ-RUYN-7XL3-PT3O
```

Run it against a **fresh data directory** — it starts from an instance with no
accounts. It completes first-run setup, creates a second account, signs in as
that account and is made to choose a new password, opens a terminal, confirms
the account is refused administration and cannot use the other's session, and
checks the trail recorded all of it. It names whichever step fails and leaves
screenshots behind.
