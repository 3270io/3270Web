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

**One thing is still shared that should not be.** The `/api/v1` token is a
single instance-wide credential rather than a per-user one. A client holding
it can list and drive **any** session, including one opened in somebody's
browser. That is what the token has always been; it is called out here because
everything else is separated now, and the token is the remaining exception.

For genuine separation between people who should not see each other's work at
all, run one container and one volume per user.
