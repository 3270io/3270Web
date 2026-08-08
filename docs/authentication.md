# User Accounts and Sign-In

By default 3270Web has no sign-in. It assumes one operator on a machine they
control, which is right for the desktop build and for `go run` on a laptop.

Setting `AUTH_MODE=local` turns on accounts: a sign-in page, per-user
passwords, and sessions that expire. Turn it on whenever more than one person
can reach the port.

!!! warning "Sign-in is not a substitute for keeping the port private"
    An account boundary decides *who* may use 3270Web. It does not encrypt
    anything. Read [Running without TLS](#running-without-tls) before putting
    an instance on a network you do not control.

---

## Turning it on

```bash
AUTH_MODE=local
```

Or in `docker-compose.yml`:

```yaml
services:
  3270Web:
    image: ghcr.io/3270io/3270web:latest
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      - AUTH_MODE=local
```

Only `none` (the default) and `local` are accepted. Any other value stops
startup with an error rather than quietly running without authentication — a
setting that looks like protection but is not would be worse than none at all.

## First start

Start with `AUTH_MODE=local` and no accounts, and 3270Web waits in setup mode:
every page redirects to a one-time setup screen where you create the first
administrator.

To stop the first person who reaches the port from claiming the instance, the
form asks for a **setup code** printed in the server log:

```
auth: no accounts yet — open the web interface to create the first administrator
auth: setup code: EJWQ-RUYN-7XL3-PT3O
auth: the code is required once, and stops working as soon as the account exists
```

Under Docker, read it with `docker compose logs 3270Web`. Case, spaces and
dashes are all ignored, so it can be typed however it was copied.

Open 3270Web in a browser, enter the code, and choose your own username and
password. You are signed in immediately, and setup closes for good — the page
redirects to the sign-in form from then on, and the code stops working.

!!! note "Nothing is created behind your back"
    3270Web never invents an account or a default password. Until you complete
    setup there are no credentials to leak, and the administrator's password is
    one you chose rather than one printed in a log.

If you would rather not use the web form, create the account with the CLI
before starting the server. Setup does not arm when an account already exists:

```bash
3270Web user add root --admin
```

## Managing accounts

Administrators get an **Accounts** page in the web interface, reachable from
the button beside their name in the header, or directly at `/admin/users`. From
there you can add accounts, change roles, reset passwords, disable and re-enable
people, and delete accounts.

A few actions are deliberately unavailable, because each would strand you on a
page you could no longer use, with no way back except the CLI:

- Removing your own administrator role
- Disabling or deleting your own account
- Demoting, disabling or deleting the last enabled administrator

Disabling an account, resetting its password or deleting it signs that person
out everywhere immediately. A password an administrator sets is temporary: its
owner is asked to choose their own the next time they sign in.

### From the command line

Account management is also a console command. It edits the same file the server
reads, so it works whether or not the server is running — a new account can
sign in immediately, without a restart.

```bash
3270Web user add alice              # create a regular account
3270Web user add root --admin       # create an administrator
3270Web user list
3270Web user passwd alice
3270Web user disable alice
3270Web user enable alice
```

Passwords are prompted for on a terminal, or read from stdin when piped:

```bash
printf '%s\n' "$NEW_PASSWORD" | 3270Web user passwd alice
```

They are never taken as a command-line argument, where they would be visible
to every other process on the machine and recorded in shell history.

Inside a container:

```bash
docker compose exec 3270Web /app/3270Web user add alice
```

### Roles

| Role | May |
|---|---|
| `user` | Sign in and drive their own terminal sessions |
| `admin` | The same, plus instance-wide administration |

New accounts are `user` unless `--admin` is given. The command refuses to
disable the last enabled administrator, since that leaves an instance nobody
can administer.

### Passwords

At least 12 characters. There are no composition rules — required digits and
symbols shrink the search space more than they enlarge it, while a length
floor does not. Passwords are stored as Argon2id hashes; the plaintext is
never written anywhere.

Changing a password signs out that account's other sessions, which is usually
the point of changing one.

## Session lifetime

| Variable | Default | Meaning |
|---|---|---|
| `AUTH_SESSION_IDLE` | `30m` | Sign out after this long with no activity |
| `AUTH_SESSION_MAX` | `12h` | Sign out this long after signing in, however active |
| `AUTH_BIND_SESSION_IP` | `auto` | Pin a session to the address that created it |
| `MAX_SESSIONS_PER_USER` | `6` | Concurrent terminal sessions one account may hold |
| `MAX_TOTAL_SESSIONS` | `64` | Concurrent terminal sessions across the instance |

Sessions live in memory, so restarting the server signs everyone out.

`AUTH_BIND_SESSION_IP` accepts `auto`, `true` or `false`. `auto` enables
pinning on plain HTTP and disables it behind TLS — see below for why. Set it
to `false` if people reach the instance through a NAT or VPN whose address
changes mid-session, and `true` to enforce it regardless.

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

**API tokens belong to accounts too** — see below. A token reaches exactly
what its owner reaches and nothing else, so an automated client is no way
around any of the above.

For genuine separation between people who should not see each other's work at
all, run one container and one volume per user.

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
out. Signing in has [its own throttle](#session-lifetime), which is a
different problem — that one is about guessing, not about cost.
