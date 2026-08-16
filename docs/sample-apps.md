---
seo_title: "Bundled sample TN3270 applications for trying 3270Web"
description: >-
  3270Web can start TN3270 sample applications on loopback — a pet store and
  two games — so you can try the terminal, chaos mode and AI Chat with no
  mainframe.
---
# Bundled Sample Applications

3270Web ships with TN3270 applications it can start for you, so you can try
the terminal, chaos mode, workflow recording and the AI features without a
mainframe to point at.

The line-up in under a minute — the connect-page picker, the Pet Store,
and Snake on a second tab:

![type:video](videos/howto-sample-apps.webm)

A sample app is a listener this process opens on your behalf, on loopback
only. On a shared instance the headless API will not start one unless
`ALLOW_SAMPLE_APPS` is on; see [Configuration](configuration.md).

## Starting One

=== "From the browser"

    On the connect page, click **Start sample app**, pick an application and a
    port, then **Start sample app**.

=== "From the selection screen"

    Every bundled app is already a host preset, waiting on **Admin → Session
    screen** marked *Not offered*. Click **Offer** on one and it joins the
    selection screen operators meet at sign-in; give it an audience instead
    and only those people are offered it. See the [session
    manager](session-manager.md#sample-apps-as-hosts).

=== "From the session picker"

    **Profiles** on the connect page, and **+ New session** in the tab bar,
    both open a picker that lists every bundled sample app under the
    connection profiles. Choosing one opens a session against it — this is
    also how you open a second and third session without a mainframe.

    The same list appears as **Bundled sample app** at the top of the profile
    editor, where choosing one fills in the host and port for a profile you
    are saving.

=== "By hostname"

    Type the sample's target into the hostname box on the connect page:

    ```text
    sampleapp:petstore
    sampleapp:petstore:3271
    ```

    The port is optional and defaults to 3271. Allowed ports are 3271 to 3278
    — one per bundled sample, so they can all run side by side.

=== "From the API or an MCP client"

    ```bash
    curl -X POST http://localhost:3270/api/v1/sessions \
      -H 'Content-Type: application/json' \
      -d '{"host":"sampleapp:petstore:3271"}'
    ```

`mock` and `demo` are accepted as shorthand for the default sample.

## As a Host for Something Else

Everything above starts a listener inside this process, on loopback. That is
right for the terminal and no use to anything else: a colleague's laptop, an
automation run in another container, a CI job that wants a 3270 host that
behaves like one and does not need booking.

`3270Web sampleapp` serves the same applications as an ordinary TN3270 host,
in the foreground, until you stop it.

```bash
3270Web sampleapp                                   # the pet store on 3271
3270Web sampleapp --app petstore:3271 --app app1:3272
3270Web sampleapp --app wordle,snake,pong           # ports assigned from 3271
3270Web sampleapp --list                            # what there is to serve
```

| Option | Default | Purpose |
|---|---|---|
| `--app <id[:port]>` | `petstore` | Application to serve. Repeatable, and accepts a comma-separated list. Ports not given are assigned from 3271 upwards. |
| `--bind <address>` | `0.0.0.0` | Interface to listen on. `127.0.0.1` keeps it local. |
| `--list` | | Print the available applications and exit. |

Unlike every other listener in 3270Web, this one binds every interface by
default — there is no other reason to run it. That is safe here and nowhere
else in the product: these are demonstration screens with no data behind them
and nothing to sign in to. Nothing about them is a mainframe.

!!! tip "The lab"
    `docker-compose.lab.yml` in this repository runs three containers on one
    network: these applications as a TN3270 host, the terminal, and
    [3270Connect](https://3270connect.3270.io)'s operations console. Drive an
    application by hand in the terminal, then replay it a hundred times from
    the console, against the same screens.

    ```bash
    docker compose -f docker-compose.lab.yml up -d
    ```

    ```
    http://localhost:3270    the terminal. Connect to "sampleapps", port 3271
    http://localhost:9200    the console
    ```

## What Is Bundled

| Target | Application |
|---|---|
| `sampleapp:petstore` | **Pet Store** — retail counter and back office. The default. |
| `sampleapp:app1` | Name entry and field validation. |
| `sampleapp:app2` | RSS newsreader, showing dynamically built screens. |
| `sampleapp:app3` | A matrix of every 3270 field attribute, colour and highlight. |
| `sampleapp:wordle` | **Word Guess** — six goes at a five letter word. |
| `sampleapp:tictactoe` | **Noughts and Crosses** — against a machine that can be made unbeatable. |
| `sampleapp:snake` | **Snake** — eat, grow, and do not run into anything. |
| `sampleapp:pong` | **Bat and Ball** — first to five points. |

---

## The Pet Store

`sampleapp:petstore` is a complete retail system for a fictional pet shop:
a counter that sells livestock, feed, accessories and medicines, and the
back office that configures, maintains and audits it. It exists because
most of what 3270Web does only becomes visible against an application with
somewhere to go — a screen graph with branches, guarded doors and records
that change when you act on them.

Everything is held in memory, per connection. Disconnect and reconnect and
you get a fresh, identical store, so nothing you do while exploring can
spoil the next person's demonstration.

### Signing On

The first screen is `PET010`. Sign on with:

- **ADMIN / ADMIN** for the full back office, or
- **any user id and any password** for a counter clerk.

The password is not checked — this is a demonstration, not a security
control. The user id decides the role: ids that appear in user maintenance
(`PET720`) take that record's role, and everything else is a clerk. Clerks
can read every screen but cannot change configuration, users or jobs, which
is what makes the authorisation messages worth looking at.

### Getting Around

Every screen carries a **command line**. Type a command, press ++enter++,
and you go there from wherever you are:

| Command | Goes to |
|---|---|
| `MENU` | Main menu |
| `CUST`, `NEWCUST` | Customer list, create an account |
| `STOCK`, `CHECK` | Stock catalogue, items below reorder level |
| `POS`, `ORDER` | Order entry, order enquiry |
| `INV`, `PAY`, `PAYHIST` | Invoices, payment entry, payments received |
| `ADMIN`, `CONFIG`, `USERS`, `JOBS`, `AUDIT` | Back office |
| `RPT`, `SALES`, `VALUE`, `BALANCE` | Reports |
| `INFO`, `SIZE`, `HELP`, `OFF` | Terminal information, screen size, help, sign off |

Name a record to go straight to it — `CUST C0007`, `STOCK LIV-0001`,
`INV I00003`, `ORDER O00005`, `PAY I00003`.

`HELP` (or ++f1++) lists the lot. On a list screen, type `S` beside a line
to open it; ++f7++ and ++f8++ page; ++f12++ returns to the main menu.

### The Screens

| ID | Screen |
|---|---|
| `PET010` | Sign on |
| `PET100` | Main menu |
| `PET200` `PET210` `PET220` | Customer enquiry, maintenance, new account |
| `PET300` `PET310` `PET320` | Stock catalogue, item and adjustment, stock check and reorder |
| `PET400` `PET410` `PET420` | Order entry, order enquiry, order detail |
| `PET500` `PET510` | Invoice enquiry, invoice detail |
| `PET600` `PET610` | Payment entry, payment history |
| `PET700` | Back office menu |
| `PET710` `PET720` `PET730` `PET740` | Configuration, users and security, housekeeping and batch, audit log |
| `PET800` `PET810` `PET820` `PET830` | Reports menu, sales summary, stock valuation, customer balances |
| `PET900` `PET910` | Terminal and model information, help |

Each screen prints its identifier in the top-left corner. That is what
screen discovery, chaos mind-maps and an AI assistant use to tell one screen
from another, and it is why the pet store is a good target for all three.

### Things That Actually Happen

The selling chain has consequences, and each step checks the one before it:

1. **Order entry** (`PET400`) prices a basket against live stock and warns
   when a line asks for more than is on the shelf.
2. **Raising an invoice** (++f5++ on `PET420`) is where stock actually
   leaves the shelves — and where the application refuses if it is not
   there.
3. **Posting a payment** (`PET600`) clears the invoice and the customer's
   balance. An overpayment is refused; a part payment leaves the invoice
   `PART`.

The back office is equally real. Saving configuration on `PET710` changes
the VAT rate that new invoices use. Running `CUSTPURG` on `PET730` really
does delete closed accounts. Every one of these writes to the audit log on
`PET740`.

### Terminal Models

The pet store notices what size terminal it was given, which makes it a
convenient way to see model handling at work. See the
[Screen Size and Model Guide](terminal-model-limits.md) for how to choose
one.

- The bottom line of every screen reports the negotiated model, the buffer
  in use and the terminal type the client identified itself with.
- Lists show as many rows as the screen has: 13 on a model 2, 21 on a
  model 3, 32 on a model 4.
- A wide terminal (model 5, 132 columns) gets extra columns — telephone and
  email on the customer list, supplier and bin on the catalogue — and a
  trading position panel beside the main menu.
- ++f9++ redraws the current screen on the other buffer, so you can switch
  between the default 24x80 and the terminal's alternate size and watch the
  layout follow. `PET900` explains what was negotiated; a model 2 says so
  rather than pretending.

### Trying the Rest of 3270Web Against It

- **[Chaos Mode](chaos-mode.md)** — connect, then start a chaos run. The
  branching menus, line-action columns, validated forms and guarded back
  office give the explorer a real graph to map, and the exported workflow
  JSON is a usable recording of it.
- **[AI Chat](ai-chat.md)** and the **[MCP Server](mcp.md)** — the command
  line and the named records are what make plain-language instructions work:
  *"open customer C0007"*, *"which stock lines are below their reorder
  level?"*, *"take a card payment for the balance of invoice I00003"*.
- **[Recordings and Playback](workflow.md)** — record a sale from order
  entry through to payment and replay it.
- **[Host Compatibility Profiler](host-profiler.md)** — the sample runs a
  real terminal against a real TN3270 server, so the profiler has genuine
  answers to report.

---

## The Games

Four small games, for when you want to *use* the terminal rather than read
about it. They are also the samples that lean hardest on the parts of the
datastream the others touch lightly: a separately coloured field per
character cell, extended highlighting, and a screen that changes completely
on every transmission.

A 3270 terminal is a block mode device. The host writes a screen, the
keyboard locks, and nothing comes back until you press an AID key — there is
no key-at-a-time input to read and no way to redraw on a timer. So none of
these is the real time game of the same name, and the two that would need a
clock are turned into what the device can actually do: **one tick of the
world per transmission**.

All four draw on the default 24x80 buffer whatever your terminal negotiated,
so a board looks the same everywhere. Every screen prints its identifier in
the top-left corner, the same as the pet store, so chaos mode and an AI
assistant can tell one from another.

### Word Guess — `sampleapp:wordle`

`WRD010`. Six goes at a five letter word. Type a guess on the entry line and
press ++enter++; each letter comes back green (right letter, right place),
yellow (right letter, wrong place) or blue (not in the word), and the
alphabet along the bottom carries the best thing known about every letter so
far. A letter of the answer can only be claimed once, so a guess with two of
a letter against an answer with one marks only one of them.

++f5++ starts another word, ++f6++ shows the answer, ++f1++ explains the
rules. A guess is exactly one transmission, which makes this the game worth
pointing a workflow recording or the MCP server at.

### Noughts and Crosses — `sampleapp:tictactoe`

`TTT010`. The squares are numbered 1 to 9 and a free square shows its own
number; type one and press ++enter++. You are X, and the machine replies as O
in the same transmission. Whoever opens alternates from game to game.

++f6++ cycles the level: `EASY` plays at random, `FAIR` searches for most of
its moves and blunders the rest, and `PERFECT` searches every move and cannot
be beaten — a draw is the best result available to you, which is the point.

### Snake — `sampleapp:snake`

`SNK010`. Eat the food, grow by three cells each time, and do not run into
the walls or into yourself. ++f7++ ++f8++ ++f10++ ++f11++ turn and take one
tick; ++f6++ switches the walls between solid and open.

The entry line is what makes it playable over a block mode link: it takes a
run of moves and plays them in order, so one ++enter++ can be a whole
manoeuvre.

```text
RRDD     right, right, down, down
3R2D     the same thing, counted
.        carry on in the direction you are already going
```

++enter++ on an empty line is a single tick.

### Bat and Ball — `sampleapp:pong`

`PNG010`. Your bat is on the left, the machine's on the right, first to five
points. ++f7++ and ++f8++ move your bat and take a tick with them, and the
entry line takes a run of moves (`UUD`, `3U`, `..` to hold still) the same way
the snake does. Which half of the bat the ball strikes decides which way it
leaves.

++f6++ cycles the level. The easy levels only move the machine's bat while
the ball is coming towards it, and make it hesitate; the hard one tracks the
ball wherever it is.
