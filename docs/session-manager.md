---
description: >-
  What an account meets when an administrator has assigned it host profiles
  — a session manager to pick from, rather than a connect form nobody can
  fill in.
---

# The session manager

On an instance where an administrator has assigned host profiles, the connect
form is the wrong first thing to show. The person signing in does not need to
be asked for a hostname nobody told them, and on a shared instance they may not
be entitled to type one.

So what an account meets depends on what it was given:

| Hosts assigned | What happens |
|---|---|
| None | The connect form, exactly as before |
| One | Connected to it, no question asked |
| Several | A session manager — a real 3270 selection screen |

The middle case is the one that matters most. Somebody whose account reaches
one mainframe should sign in and be on it.

---

## The selection screen

```
 3270.io                                                        SESSION MANAGER
 ------------------------------------------------------------------------------
  SEL SYSTEM           DESCRIPTION
 ------------------------------------------------------------------------------
    1 CICSPROD         Production CICS region  (mvs1.example:992)
    2 CICSTEST         Test CICS region  (mvs2.example:23)
    3 TSO              TSO/ISPF logon  (mvs1.example:23)
    4 IMSDEV           IMS development  (mvs3.example:23)

 4 systems
 SELECTION ==> ______

 ENTER Connect   PF3 Sign off   PF12 Clear
```

**It is a real TN3270 application, not a web page that looks like one.** The
terminal negotiates with it, so the operator information area, the cursor,
tabbing, Enter and the PF keys are the terminal's own — because they are. An
operator's muscle memory applies to it exactly as it does to a mainframe's own
session manager.

Type the number of a system and press **Enter**. The system's name works too,
for people who know it.

Choosing does not open a second session: the one you are in is re-pointed at
the mainframe you chose. Same tab, same session, and the host's first screen
replaces the menu.

### Keys

| Key | Does |
|---|---|
| `Enter` | Connect to the selected system |
| `PF7` / `PF8` | Back and forward a page, when there is more than one |
| `PF3` | Sign off — ends the session and returns to the connect form |
| `PF12` | Clear the selection field |

Paging keys are only offered when there is more than one page. A list longer
than the screen pages rather than being cut off: the numbers are global, so
system 21 is 21 on whichever page it is found.

---

## Deciding who gets which mainframes

Host profiles are published once by an administrator; the audience decides who
each one is for.

The administration area manages these directly: **Admin → Session screen**
lists every published preset with the audience it carries, and adds, edits and
removes them in place. The same thing can be done from the connect page — open
**Profiles** as an administrator, tick **Share with everyone** — and both
routes write the same store, so a preset made in one room is editable in the
other. Either way, *Who this host is for* takes three lists:

| Field | Meaning |
|---|---|
| Groups | Anybody in one of these teams |
| Users | Named accounts |
| Roles | Everybody holding the role — `user` or `admin` |

The three are an **or**, not an and: matching any one is enough. That is the
shape these lists come in — "the payments team, plus Dave who covers for them".

**Leave all three empty and the host is for everybody.** That is what every
profile in an existing deployment is, so turning this on never quietly takes a
host list away from the people using it.

The audience is a restriction and not a display filter. Both paths that connect
by name resolve profiles through the same check, so naming a host you were not
given gets the same answer as naming one that does not exist.

### Groups

Groups are teams, kept on the account. On their own they say nothing about
permission — they decide which mainframes an account is offered — and exist so
access can be decided for a team rather than a person at a time.

Set them on the **Accounts** page. A group exists because somebody is in it, so
typing a new name creates one; the field offers the names already in use so the
same team is not spelled two ways.

A group *can* carry a role, when an administrator assigns one under
**Accounts → Group roles**: everyone in the group then holds that role on top
of their own, for as long as they are in it. See
[roles from groups](authentication.md#roles-from-groups).

Under [`AUTH_MODE=oidc`](authentication.md#single-sign-on-oidc) with
`OIDC_GROUPS_CLAIM` set, these are the **directory's own groups**, refreshed at
every sign-in. The team somebody is on is already recorded somewhere, and it is
not here. The Accounts page will not let those be edited, since the next
sign-in would overwrite the change.

---

## Branding it

An operator meets this screen before anything of their own organisation's, so
it carries the instance's name. **Admin → Session screen** sets it, with a
live preview of the real screen beside the fields.

| Field | |
|---|---|
| Title | The name in the top-left. Defaults to `3270.io` |
| Banner | Optional artwork under the title bar |
| Footer | An optional line above the key legend — an operations contact, a classification marking |

The banner is **empty by default, deliberately**. Block-letter artwork was the
first thing built here and it was the wrong thing: it ate a third of a 24-row
screen, it read as decoration rather than as a system identifying itself, and
it set an example a site would follow — paste in a five-line logo and the menu
has room for four hosts. A session manager announces itself in one line, and
the room saved is the host list.

If you do want artwork, it is bounded: up to six lines of 78 characters, and a
taller banner costs list entries rather than overwriting the command line.

Everything is held to printable ASCII. The screen is built into a 3270 data
stream and drawn through a code page, so a control character is not a rendering
problem but a malformed stream, and anything outside the code page is a
question mark at best. **What was changed is reported back** rather than
silently dropped — a banner that lost a character should not first be noticed
by somebody who cannot edit it.

---

## Related

- [User accounts and sign-in](authentication.md) — roles, and single sign-on
- [Running a shared instance](multi-user.md) — what one account can see of
  another's
