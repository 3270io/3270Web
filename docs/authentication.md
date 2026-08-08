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

Starting with `AUTH_MODE=local` and no accounts creates one and prints a
one-time password to the log:

```
auth: created the first admin account "admin"
auth: one-time password: l0DcFoiufFprA0k8QFUB1HOL
auth: this is shown once and must be changed at first sign-in
```

Under Docker, read it with `docker compose logs 3270Web`.

The password is generated per instance, never a fixed default, and shown once.
The account can do nothing except change its own password until it does, so a
password sitting in a log file does not stay usable.

## Managing accounts

Account management is a console command. It edits the same file the server
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

## What sign-in does not yet separate

Accounts currently control **who may use the instance**. Work to separate what
different users can see from one another is ongoing:

- Terminal sessions are labelled with their owner, but that label is not yet
  enforced on every route.
- Chaos runs, saved tasks, connection profiles and themes are still shared
  across everyone on the instance.
- The `/api/v1` token remains a single instance-wide credential rather than a
  per-user one.

Until that lands, treat every account on an instance as able to see the
instance's stored data. For genuine separation between people who should not
see each other's work, run one container and one volume per user.
