---
seo_title: "The 3270Web session manager and assigned host profiles"
description: >-
  What an account meets once an administrator has assigned it host profiles
  — a session manager to pick from, instead of a connect form nobody can
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
replaces the menu on the same keystroke — the "connecting" frame in between is
the menu telling you what it is doing, not a screen waiting for another key.

To reach a second mainframe at the same time, use **+ New session** in the tab
bar. Where the selection screen is what you sign in to, it opens another one —
so the second tab starts on the same list, and you choose from it the same way.
Both tabs read *Session manager* until each has been pointed at a system, after
which each takes that system's name.

### Keys

| Key | Does |
|---|---|
| `Enter` | Connect to the selected system |
| `PF7` / `PF8` | Back and forward a page, when there is more than one |
| `PF3` | Sign off — ends the session and returns to the connect form, without redrawing the menu |
| `PF12` | Clear the selection field |

Paging keys are only offered when there is more than one page. A list longer
than the screen pages rather than being cut off: the numbers are global, so
system 21 is 21 on whichever page it is found.

### Columns

`SYSTEM` is as wide as the longest profile name on the menu, so a list of
descriptive names — "Pet Store - Retail & Back Office" rather than `CICSPRD1` —
is readable without opening anything. Short names keep the tight layout above.
The column stops growing where `DESCRIPTION` would be squeezed below a usable
width; a name longer than the room left is shortened with a trailing `>`.

---

## Deciding who gets which mainframes

Host profiles are published once by an administrator; the audience decides who
each one is for.

The administration area manages these directly: **Admin → Session screen**
lists every published preset with the audience it carries, and adds, edits and
removes them in place. The same thing can be done from the connect page — open
**Profiles** as an administrator, tick **Share with everyone** — and from
**Admin → Groups**, which sets the same audience from the team's side. Every
route writes the same store, so a preset made in one room is editable in the
others. Either way, *Who this host is for* takes three lists:

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

A preset can also be on the presets page and nowhere else. Untick **Offer this
preset** and it drops off the selection screen, off the connect page and out of
the *Profiles* picker, for everybody including you — the row stays, marked
**Not offered**, with an **Offer** button on it. That is a different state from
an empty audience, which means *everyone*: it is for a host that exists but has
not been handed out yet, which is how the [bundled sample
apps](#sample-apps-as-hosts) arrive. Naming any audience offers the preset to
them, so a preset assigned from the Groups page is never left reaching nobody.

The audience is a restriction and not a display filter. Both paths that connect
by name resolve profiles through the same check, so naming a host you were not
given gets the same answer as naming one that does not exist.

### Sample apps as hosts

**Every bundled sample app is already a preset.** They are added the first time
an administrator opens **Admin → Session screen**, each marked *Not offered* —
so nothing about the instance changes until you say so, and there is no form to
fill in when you do.

Offering one is a single click on **Offer**, which makes it available to
everyone; giving it an audience instead — here or from **Admin → Groups** —
offers it to just those people. From then on it behaves like any other preset:
it appears on the selection screen, carries an audience, and can be assigned to
a group.

That is what makes the whole feature usable before there is a mainframe to
reach. An instance being evaluated, or one being used to teach on, can offer
the sample apps, put the trainees in a group, and give the group those hosts;
everybody who signs in meets a real selection screen with real systems on it.

Remove any you do not want and they stay removed — a later start does not put
them back. The **Add sample app** list above the table is how one comes back
if you change your mind; it is hidden while every app is already on the list,
which is the ordinary state. The preset dialog's **Bundled sample app** list
points a preset you are writing yourself at one of them, filling in the host
and port.

Each sample app has its own port (3271 upwards), so offering more
than one does not leave two presets fighting over a single listener. The port
is fixed to that range: the sample apps are TN3270 servers 3270Web starts
itself, and 3270 is what the web interface listens on.

### Groups

Groups are teams. On their own they say nothing about permission — they decide
which mainframes an account is offered — and exist so access can be decided for
a team rather than a person at a time.

**Admin → Groups** is where they are made and maintained: name a group, tick
the accounts in it, tick the host presets it should reach, and — where a team
administers this instance — give it a role. A group may be empty, so the teams
can be prepared before the accounts arrive; anyone added later inherits the
hosts the group already has. See
[managing groups](authentication.md#managing-groups).

That page and this one edit the same fact from opposite sides. Ticking a preset
on a group adds the group to that preset's *Groups* audience; naming a group in
the audience here puts the preset on that group's host list. There is one copy
of it, so the two cannot disagree.

Membership is also editable one account at a time, on the **Accounts** page —
useful when somebody joins a team and the team is not what you came to change.

A group *can* carry a role: everyone in it then holds that role on top of their
own, for as long as they are in it. See
[roles from groups](authentication.md#roles-from-groups).

Under [`AUTH_MODE=oidc`](authentication.md#single-sign-on-oidc) with
`OIDC_GROUPS_CLAIM` set, membership comes from the **directory's own groups**,
refreshed at every sign-in. The team somebody is on is already recorded
somewhere, and it is not here. Those accounts cannot have their membership
edited on either page, since the next sign-in would overwrite the change —
though the group itself can still be given hosts and a role here.

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

## Moving a set-up to another instance

Presets are usually built on a test instance and then have to appear on the
production one. Retyping forty of them is where two environments start
disagreeing about which mainframe a name points at, so **Admin → Session
screen → Library** moves them as a file instead — together with the
[Guided Business Tasks](business-tasks.md) recorded on the instance, because a
task drives an application on a particular mainframe and a catalogue that
arrives without its host list is a menu of operations none of which run.

Download the library, take the file to the other instance, choose it there.
Choosing it reports what it would change, entry by entry, and writes nothing;
the import happens when you press **Import**. Whether an existing name is left
alone or overwritten is a checkbox, and either way the report says which
happened to each entry.

A file this build cannot store in full is not stored at all. One malformed
preset refuses the whole library, with that preset named — the alternative is
storing the good half and leaving somebody to work out where it stopped, which
is the state a library exists to avoid.

What a library will not carry:

- **Audiences that name individual accounts.** Those accounts exist only on
  the instance the file came from, and a file handed to another site should
  not carry a staff list. Groups and roles survive. A preset whose only
  audience was named accounts arrives **not offered**, so a lost restriction
  cannot quietly become "everyone".
- **Nothing secret**, because a preset holds none: a host, a port, TLS
  settings, an LU and a terminal model. Sign-on credentials are typed by the
  person signing on.
- **Tasks an installed extension contributed.** Install the extension at the
  far end; copying its tasks would freeze a duplicate that stops changing when
  the extension is updated.

Each of these is written into the file's own `notes`, so the person opening it
at the other end can see why an audience is narrower than it was. The same
thing is on the API as `GET` and `POST /api/v1/library` — see
[REST API](rest-api.md#get-apiv1library-and-post-apiv1library) — which is the
form to use from a deployment pipeline.

---

## Related

- [User accounts and sign-in](authentication.md) — roles, and single sign-on
- [Running a shared instance](multi-user.md) — what one account can see of
  another's
