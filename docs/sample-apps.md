# Bundled Sample Applications

3270Web ships with TN3270 applications it can start for you, so you can try
the terminal, chaos mode, workflow recording and the AI features without a
mainframe to point at.

A sample app is a listener this process opens on your behalf, on loopback
only. On a shared instance the headless API will not start one unless
`ALLOW_SAMPLE_APPS` is on; see [Configuration](configuration.md).

## Starting One

=== "From the browser"

    On the connect page, click **Start sample app**, pick an application and a
    port, then **Start sample app**.

=== "By hostname"

    Type the sample's target into the hostname box on the connect page:

    ```text
    sampleapp:petstore
    sampleapp:petstore:3271
    ```

    The port is optional and defaults to 3271. Allowed ports are 3271 to 3275,
    so several samples can run side by side.

=== "From the API or an MCP client"

    ```bash
    curl -X POST http://localhost:3270/api/v1/sessions \
      -H 'Content-Type: application/json' \
      -d '{"host":"sampleapp:petstore:3271"}'
    ```

`mock` and `demo` are accepted as shorthand for the default sample.

## What Is Bundled

| Target | Application |
|---|---|
| `sampleapp:petstore` | **Pet Store** — retail counter and back office. The default. |
| `sampleapp:app1` | Name entry and field validation. |
| `sampleapp:app2` | RSS newsreader, showing dynamically built screens. |
| `sampleapp:app3` | A matrix of every 3270 field attribute, colour and highlight. |

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
