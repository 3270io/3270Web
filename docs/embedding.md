---
description: >-
  Put 3270Web inside another application: an iframe in a portal page, or a
  single-page app that talks to the API and never shows the terminal at all.
---

# Embedding 3270Web

3270Web can be put inside another application: an iframe in a portal page, or
the screen drawn by a single-page app that talks to the API and never shows
the terminal at all. Both are off by default and both are turned on by the
same setting.

This page is the whole of what an integrator needs. If a frame stays blank or
a call is refused, the [Troubleshooting](#troubleshooting) section names the
symptom rather than the header.

## Why it is off by default

A browser stops one origin's page from framing, reading or posting to another
on purpose, and 3270Web takes all three of those defences. Embedding means
relaxing them for named origins:

| What blocks the frame | What embedding changes |
| --- | --- |
| `X-Frame-Options: SAMEORIGIN` and `frame-ancestors 'self'` | `frame-ancestors` names the allowed origins instead |
| The session cookie is `SameSite=Lax`, so it is not sent inside a cross-site frame | It becomes `SameSite=None; Secure` |
| A cross-origin call to `/api/v1` has no CORS permission | The named origins get one |

Relaxing them for *everybody* would be a downgrade for every deployment that
never embeds anything. So the origins are named, one by one, and nothing is
inferred from the request.

## Turning it on

Set `EMBED_ORIGINS` to the origins that may embed the terminal. Comma or
whitespace separated; each entry is a scheme and a host, with a port if it is
not the default:

```bash
EMBED_ORIGINS="https://portal.example.com,https://intranet.example.com:8443"
```

There is no wildcard. `*`, `https://*.example.com` and a bare
`portal.example.com` are all refused, and a refused entry is dropped rather
than treated as permission for anything — a typo cannot widen the list.

Read back what the server made of it:

```bash
curl https://3270web.example.com/embed/config
```

```json
{
  "enabled": true,
  "origins": ["https://portal.example.com"],
  "secure_request": true,
  "frame_ancestors": "frame-ancestors 'self' https://portal.example.com;"
}
```

An entry the server could not use is reported under `ignored`, with the
reason.

### HTTPS is required

A framed terminal is a cross-site context, so the session cookie has to be
`SameSite=None`, and browsers only accept that on a `Secure` cookie. Over
plain HTTP there is no combination that works: the cookie is either not sent
or discarded, and the frame shows the connect page forever.

Serve 3270Web over TLS, or behind a proxy that terminates TLS. Behind a proxy
there is a second setting: the hop into this process is plain HTTP, so the
server cannot see the TLS by itself and reads `X-Forwarded-Proto` instead —
but only when `TRUST_PROXY_HEADERS=true` says a proxy is really in front of
it. That gate is not optional paranoia: the header is set by whoever sends the
request, so believing it unconditionally would let any client assert that its
own connection was secure.

```bash
EMBED_ORIGINS="https://portal.example.com"
TRUST_PROXY_HEADERS=true      # only if a proxy terminates TLS for you
```

`/embed/config` reports `secure_request: false` and a `warning` when this is
the thing that is missing.

## In an iframe

```html
<iframe
  src="https://3270web.example.com/screen?embed=1"
  title="Mainframe terminal"
  style="width: 100%; height: 640px; border: 0"
></iframe>
```

`?embed=1` renders the terminal without the menu bar, the session tab bar or
the animated background — the page around the frame has its own.
The screen, its operator information area and all the keyboard handling are
untouched: a terminal that cannot be typed into is not a smaller terminal.

The choice is remembered in a cookie for the rest of the session, because
`/screen` is reached by a redirect from the connect form and a query parameter
does not survive a redirect. `?embed=0` clears it.

`?embed=1` is presentation only. It permits nothing — what may frame the
server is decided by `EMBED_ORIGINS` and enforced by the browser — so it does
not matter that anyone can add it to a URL.

### Showing the menu bar

Add the `embedded-toolbar` class to the frame's `<body>` to keep the menu bar.
The simplest way is a second query parameter your page appends and a small
stylesheet of your own; the default is the screen and nothing else, because
the common case is a portal that has its own controls.

## Driving the frame from the page around it

The frame and the page are different origins, so the page cannot reach into
the document. `postMessage` is the channel, and 3270Web answers a small set of
named commands on it.

Every inbound message must come from an origin named in `EMBED_ORIGINS`. The
same list decides who may frame the terminal and who may drive it.

```js
const frame = document.querySelector("iframe");
const TERMINAL = "https://3270web.example.com";

let nextId = 1;
const pending = new Map();

window.addEventListener("message", (event) => {
  if (event.origin !== TERMINAL) return;
  const msg = event.data;
  if (!msg || msg.source !== "3270web") return;

  if (msg.type === "ready") {
    console.log("terminal ready, commands:", msg.commands);
  }
  if (msg.type === "screen") {
    console.log("the host repainted the screen");
  }
  if (msg.type === "result") {
    const settle = pending.get(msg.id);
    if (settle) {
      pending.delete(msg.id);
      msg.ok ? settle.resolve(msg.data) : settle.reject(new Error(msg.error));
    }
  }
});

function send(type, extra = {}) {
  const id = nextId++;
  frame.contentWindow.postMessage({ type, id, ...extra }, TERMINAL);
  return new Promise((resolve, reject) => pending.set(id, { resolve, reject }));
}

// Read the screen, type into a field, and press Enter.
const screen = await send("getScreen");
await send("writeField", { row: 5, col: 20, text: "SMITH" });
await send("submit", { aid: "Enter" });
```

### Commands

| Command | Arguments | What it does |
| --- | --- | --- |
| `getScreen` | — | The screen as JSON: text, fields, cursor, keyboard state |
| `sendKey` | `key` | One AID or navigation key: `Enter`, `PF3`, `Tab`, `Clear` |
| `writeField` | `row`, `col`, `text` | Write into the field containing that cell |
| `submit` | `aid` | Submit what was written, with `Enter` by default |
| `moveCursor` | `row`, `col` | Move the cursor without sending anything to the host |
| `waitForUnlock` | — | Wait for the host to finish and the keyboard to unlock |
| `ping` | — | A liveness check that touches no host state |

Rows and columns are 0-indexed, matching what `getScreen` reports.

### Events

| Event | When |
| --- | --- |
| `ready` | The frame has loaded and is listening. Carries the session id and the command list. |
| `screen` | The host repainted the screen without being asked — the screen a caller polling after its own commands would never see. |
| `result` | The answer to one command, carrying the `id` that command was sent with. |

Nothing here is a new capability: each command calls the same endpoint the
menus call, so a page around the frame can do what the person looking at
the frame could already do, and nothing beyond it.

## Without an iframe: calling the API directly

A single-page app that draws the screen itself does not need a frame. It needs
[the REST API](rest-api.md), which the same `EMBED_ORIGINS` list opens to
cross-origin callers.

```js
const res = await fetch("https://3270web.example.com/api/v1/sessions", {
  headers: { Authorization: `Bearer ${token}` },
});
```

Two things to know about this path:

- **It is bearer-authenticated, and never uses the session cookie.** Credentials
  are not allowed on these cross-origin requests, so a browser with a 3270Web
  session already open cannot have it borrowed by a page that merely happens to
  be on the allowlist. Every call carries `Authorization: Bearer <API_TOKEN>`
  of its own.
- **`API_TOKEN` must be set**, or the entire `/api/v1` surface answers 503. See
  [REST API](rest-api.md).

Putting a token that drives a mainframe into a browser is a decision, not a
detail. The usual answer is that the SPA's own backend holds the token and
proxies, in which case CORS is not involved at all and `EMBED_ORIGINS` is only
needed for the iframe.

## Troubleshooting

**The frame is blank and the console says the page refused to connect.**
The framing origin is not in `EMBED_ORIGINS`. Check `/embed/config` — it lists
the origins the server accepted and, under `ignored`, the entries it could not
use and why. An origin has to match on scheme, host *and* port.

**The frame shows the connect page and never keeps a session.**
The session cookie is not reaching the frame — a cross-site cookie needs
`SameSite=None`, which browsers only accept on a `Secure` cookie. Check
`secure_request` in `/embed/config`. If it is `false` while you are serving
over HTTPS, the missing piece is `TRUST_PROXY_HEADERS=true`: the server cannot
see TLS that a proxy terminated. If you are genuinely on plain HTTP, no
configuration makes this work.

**`postMessage` commands are ignored.**
The frame only listens to origins in `EMBED_ORIGINS`. Post to the terminal's
exact origin — not `*` — and check that the page's own origin is on the list.

**A cross-origin API call fails at the preflight.**
Preflights are answered for allowlisted origins only, and are refused with 403
for everything else. The `Origin` the browser sends must match a list entry
exactly.

**A cross-origin API call returns 401 after a successful preflight.**
That is the intended split: CORS decides whether the browser may make the call
and the token decides whether the server will answer it. Send
`Authorization: Bearer <API_TOKEN>`.

## Related

- [REST API](rest-api.md) — the full endpoint reference
- [Guided Business Tasks](business-tasks.md) — running a whole flow from a form
  rather than driving the screen keystroke by keystroke, which is often what an
  embedding page actually wants
- [Configuration](configuration.md) — the other environment variables
