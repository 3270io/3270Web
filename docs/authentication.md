# User Accounts and Sign-In

By default 3270Web has no sign-in. It assumes one operator on a machine they
control, which is right for the desktop build and for `go run` on a laptop.

Setting `AUTH_MODE=local` turns on accounts: a sign-in page, per-user
passwords, and sessions that expire. Turn it on whenever more than one person
can reach the port.

!!! warning "Sign-in is not a substitute for keeping the port private"
    An account boundary decides *who* may use 3270Web. It does not encrypt
    anything, and it is one of several things a shared instance needs. Read
    [Running a shared instance](multi-user.md) before putting one on a network
    you do not control.

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
      - "127.0.0.1:3270:3270"
    environment:
      - AUTH_MODE=local
```

Three values are accepted: `none` (the default), `local`, and `oidc` for
[single sign-on](#single-sign-on-oidc). Any other value stops startup with an
error rather than quietly running without authentication — a setting that looks
like protection but is not would be worse than none at all.

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

The code goes to the server's log file and to its standard error, so under
Docker `docker compose logs 3270Web` shows it. Case, spaces and dashes are all
ignored, so it can be typed however it was copied.

!!! warning "Put the data directory on a volume first"
    A container keeps its accounts in the image layer unless told otherwise, so
    the next deploy would delete the administrator you are about to create and
    reopen first-run setup. See
    [Keeping the state](multi-user.md#keeping-the-state).

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

## The administration area

Administrators get an **Admin** button in the web interface header, which
opens the administration area at `/admin`. Its front page is an overview of
the instance: how many accounts exist, who holds a live login, the terminal
sessions currently running, and refusals recorded in the last 24 hours,
followed by the most recent audit entries. The numbers refresh themselves
every half minute while the page is open.

Every page in the area carries the same navigation — **Overview**,
**Accounts**, **Groups**, **Session screen**, **Audit trail** and **Logs** —
so nothing administrative is more than one click from anything else. The
Session screen page manages what the
[session-selection screen](session-manager.md) offers and to whom: the
published host presets, each with the users, groups or roles it is for, and
the branding at the top of the screen. Groups sits between the two because it
is what joins them — an account goes into a team on one side, and a team is
offered a mainframe on the other.

### Live terminal sessions

The overview lists every terminal session the instance is running: whose it
is, which mainframe it is connected to, and when it was last touched. Each
row offers **Disconnect**, which drops the mainframe connection and frees the
session's slot under the caps — useful for a session somebody left connected
to a production region, or for reclaiming capacity when the instance is at
its limit. A session running an unattended chaos exploration or workflow
playback is marked **Automation running**, and the confirmation says the run
will be interrupted.

Disconnecting ends the terminal only. The owner stays signed in, and their
browser learns the terminal has gone the next time it asks. What an
administrator cannot do, deliberately, is see the session's screen or type
into it — administering the instance is not the same power as sitting at
somebody's authenticated mainframe terminal. Every administrative disconnect
is written to the audit trail: who did it, whose session, and the host.

## Managing accounts

The **Accounts** page, at `/admin/users`, is where you add accounts, change
roles, reset passwords, disable and re-enable people, and delete accounts.
The list can be filtered by name or group, or narrowed to administrators,
disabled accounts, accounts that still owe a password change, or accounts
that arrive by single sign-on.

A few actions are deliberately unavailable, because each would strand you on a
page you could no longer use, with no way back except the CLI:

- Removing your own administrator role
- Disabling or deleting your own account
- Demoting, disabling or deleting the last enabled administrator

Disabling an account, resetting its password or deleting it signs that person
out everywhere immediately. A password an administrator sets is temporary: its
owner is asked to choose their own the next time they sign in.

The **Add account** and **Reset password** dialogs both offer **Generate**,
which invents a 20-character password from the browser's cryptographic random
source and reveals it so you can read it out or hand it over — **Copy** puts it
on the clipboard. It is worth using: the alternative is a password you thought
of, which in practice is the same one for every account you create. The
generated alphabet leaves out `O`/`0` and `I`/`l`/`1`, because a temporary
password exists to be transcribed by somebody else.

Nothing shows the password again after the account is saved. If it is lost
before it reaches its owner, reset it — that is what **Reset password** is for.

**Changing a role also takes effect immediately, in whatever browser that
person already has open** — it does not wait for them to sign in again, and it
does not sign them out. Demotion is the direction that matters: a demoted
administrator who kept the role until their session expired could restore it
from the Accounts page they were still standing on.

## Managing groups

A group is a team. It decides which mainframes its members are offered, and it
can carry a role, so an instance is administered by "whoever is on the ops
rota" rather than by a list of names that goes out of date.

The **Groups** page, at `/admin/groups`, is where a group is made and
maintained. One row per team, and one dialog that does the whole job:

| Field | What it does |
|---|---|
| **Name** | What the group is called. No commas — a comma separates one group from the next everywhere a list of them is typed |
| **Description** | An optional note, shown beside the name in the table |
| **Role granted** | The role every member holds on top of their own; see [Roles from groups](#roles-from-groups) |
| **Members** | The accounts in the group, ticked from the account list |
| **Hosts offered** | The published host presets the group's members are offered on the [session manager](session-manager.md) |

**A group may be empty.** That is deliberate: an instance is usually set up
teams-first — the host list is written before the people arrive — and a group
that only existed while somebody was in it could not be prepared in advance.
Create the group, assign it its hosts, and everyone added later inherits them
with nothing further to configure.

**Hosts are assigned from either side, and it is one fact.** Ticking a preset
here adds the group to that preset's audience, which is the same list the
Session screen page edits from the preset's side. Untick it and the group
loses the host. One caution the page repeats: a preset that ends up naming
nobody at all is offered to *everyone* — that is the long-standing rule that
stops switching audiences on from taking a host list away from an instance
already using one — so the page says so when a change leaves a preset in that
state.

**Renaming a group carries everything with it**: its members, the role it
grants and the presets it reaches, including the group's name inside each
preset's audience. Renaming is what to do when the same team ends up spelled
two ways; the old name stops existing rather than lingering as an empty group
beside the new one.

**Deleting a group** removes it from every account in it, drops the role it
granted, and strips its name from every preset that offered it. The accounts
and the presets themselves are untouched — only the group is.

Groups that predate this page are listed alongside declared ones, marked
**in use**: those are names that exist only because an account carries one or
a role is assigned to one, including the groups an identity provider sends.
They can be described, renamed, filled and deleted like any other.

Where [single sign-on](#single-sign-on-oidc) maps a groups claim, membership
of a directory-owned account belongs to the directory: those accounts appear
in the member list marked **single sign-on** and cannot be ticked here,
because the next sign-in would overwrite the change. Change them in the
directory.

Creating, changing and deleting a group is written to the audit trail as
`group.created`, `group.updated` and `group.deleted`.

### Roles from groups

A role can be assigned to a group, on the **Groups** page or under
**Accounts → Group roles**. Everyone in the group then holds that role *on top
of* whatever their account holds in its own right — an account's effective role
is the stronger of the two.
Inheritance is additive only: being in a group never takes a role away, so
adding somebody to a team cannot quietly demote them.

The Accounts page shows the result honestly. Somebody who is an administrator
because of a group wears the same Administrator badge, with *via ‹group›*
under it, and the administrator count on the page and the overview counts
them. Assigning, revoking, joining and leaving all take effect immediately in
sessions that are already open, exactly as a direct role change does.

The self-lockout rules extend to cover inheritance:

- You cannot clear a group's role assignment if your own administrator role
  depends on it, you cannot remove your own account from such a group, and you
  cannot delete the group — all three are self-demotion wearing different
  clothes.
- The assignment that keeps the instance's *only* enabled administrator
  cannot be removed, just as that administrator cannot be demoted, disabled
  or deleted. The guard counts inherited roles, so a deployment may
  legitimately demote its last direct administrator while a group keeps the
  instance administrable.

Under [single sign-on](#single-sign-on-oidc) with a groups claim mapped, the
groups arrive from the directory at each sign-in — so a role assigned here to
a directory group follows the directory's own membership, which is the point
of managing teams centrally. (`OIDC_ADMIN_GROUPS` still works and is checked
independently; group roles are this instance's own mapping, visible and
editable on the page.)

Every change is written to the audit trail as `group.role_changed`, with the
group and the role it now grants.

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

One difference from the web interface: a running server does not see the file
change. Disabling an account from the console stops its API tokens at once —
the owner is looked up on every call — but a browser already signed in is
ended by a periodic sweep instead, so allow up to ten minutes. Disabling from
the Accounts page ends those logins on the spot.

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

## Single sign-on (OIDC)

`AUTH_MODE=oidc` signs people in through an OpenID Connect identity provider —
the directory an organisation already runs — instead of asking them to invent
another password. Accounts appear here the first time somebody signs in;
nobody has to be added in advance.

**Local accounts keep working.** This is not an either/or, and the reason
matters: an instance whose only door depends on a service it does not run can
be locked out of itself by somebody else's outage or a mistyped setting.
First-run setup still asks for a local administrator, and that account is the
way back in. Everybody else uses the provider.

### Configuring it

```bash
AUTH_MODE=oidc
OIDC_ISSUER=https://login.example.com/realms/staff
OIDC_CLIENT_ID=3270web
OIDC_CLIENT_SECRET=…
OIDC_REDIRECT_URL=https://3270web.example.com/auth/sso/callback
```

Register `OIDC_REDIRECT_URL` with the provider as an allowed redirect URI. It
must be the address a browser actually reaches, and it must end in
`/auth/sso/callback`.

Everything else — the authorization and token endpoints, the signing keys — is
read from the provider's discovery document, so there is nothing else to copy
across. The issuer must be `https`, the one exception being a loopback address
so a provider running on the same machine can be tried without a certificate.

3270Web asks for the authorization code flow with PKCE. There is no
implicit or hybrid flow: the browser only ever carries a one-time code, and
the token exchange happens server to server.

### Roles from the directory

| Variable | Meaning |
|---|---|
| `OIDC_GROUPS_CLAIM` | Which claim carries group membership (default `groups`) |
| `OIDC_ADMIN_GROUPS` | Members of these groups get the `admin` role |
| `OIDC_ALLOWED_GROUPS` | If set, only members of these groups may sign in at all |
| `OIDC_USERNAME_CLAIM` | Which claim to take a display name from |
| `OIDC_END_SESSION` | Also end the provider's session when signing out here |

Both group lists are comma-separated and matched without regard to case. The
claim may be an array of strings or one space-separated string; either is read.

`OIDC_ADMIN_GROUPS` is re-applied on **every** sign-in, in both directions —
somebody removed from the group is an ordinary user the next time they sign in.
That is the point of managing roles centrally.

Leave it unset and the provider says nothing about roles: everybody arrives as
a `user`, and an administrator promotes people on the Accounts page. Those
promotions survive later sign-ins.

`OIDC_ALLOWED_GROUPS` answers a different question — not what somebody may do
here, but whether they belong here at all. One directory usually serves many
services, and everybody in it being able to open a mainframe terminal is rarely
what was meant. Somebody outside every listed group is refused, and no account
is created for them.

### What an account looks like

An account is found by the provider's **issuer and subject**, never by name. A
directory renames people, and the subject is the one claim a provider promises
not to recycle. So a rename follows through to the username here and changes
nothing else — the same account, the same saved work, the same audit history.

The username is a display name derived from a claim. Characters the account
store does not accept become dashes, so `alice@corp.example` appears as
`alice-corp.example`.

Two things an administrator can still do, and one they cannot:

- **Disabling works**, and outranks the provider. An account disabled here
  stays out however happily the directory goes on authenticating it.
- **Roles work**, unless `OIDC_ADMIN_GROUPS` is set, in which case the
  directory is the authority.
- **Passwords do not.** An account that signs in through the provider has no
  local password and cannot be given one. A second door the provider knows
  nothing about is one it cannot close when it closes the person's access.

A name a local account already holds is refused rather than linked. Otherwise
anybody who can be named in the directory could take over the break-glass
administrator by being called `root`.

### If the provider is unreachable

The sign-in page says so and offers the password form. Discovery is deferred
until somebody presses the button, so a provider that is down does not stop
3270Web from starting — which is exactly when the local administrator is
needed.

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
pinning on plain HTTP and disables it behind TLS — see
[Running without TLS](multi-user.md#running-without-tls) for why. Set it to
`false` if people reach the instance through a NAT or VPN whose address
changes mid-session, and `true` to enforce it regardless.

---

## Next

Accounts decide who may sign in. An instance more than one person can reach
needs more than that: what each account can see, what an automated client may
do, which hosts the terminal may be pointed at, and what is recorded.

**→ [Running a shared instance](multi-user.md)**
