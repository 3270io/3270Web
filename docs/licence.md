---
seo_title: "Licence: what the AGPL means for a 3270Web deployment"
description: >-
  3270Web is AGPL-3.0-or-later. Self-hosting it unmodified asks nothing of you;
  the obligation starts only if you modify it and serve the modified version to
  other people. Commercial terms are available.
---

# Licence

3270Web is free software, released under the **GNU Affero General Public
License, version 3 or later** (AGPL-3.0-or-later). The full text ships with the
source in [`LICENSE`](https://github.com/3270io/3270Web/blob/main/LICENSE).

The AGPL has a reputation for being the one to read carefully, so this page
says plainly what it does and does not ask of a 3270Web deployment.

## Running 3270Web asks nothing of you

If you deploy 3270Web as published — from a release binary, the container
image, or a `go build` of an unmodified checkout — you have **no obligations
under this licence at all**, whatever you use it for.

That covers, explicitly:

- **Commercial use.** A bank, an insurer, a government department, a managed
  service provider. There is no seat count, no user cap, no fee, no
  registration, no phoning home.
- **Internal deployment at any scale.** One operator on a laptop or ten
  thousand users behind a corporate load balancer.
- **Production hosts.** Connect it to whatever TN3270 systems you like.
- **CI and automation.** Drive it from pipelines, schedulers, or your own
  tooling.

You may also redistribute it unchanged — hand a colleague the container image,
mirror the release into an internal registry — provided you pass on the licence
and the source with it.

## What is *not* a modification

The obligation attaches to modifying **3270Web's own source**. None of the
following is a modification, and none of it puts any requirement on your code:

| You do this | Your obligation |
|---|---|
| Set `3270Web-config.xml`, environment variables, themes, keymaps, connection profiles | None. Configuration is not modification. |
| Call the [REST API](rest-api.md) or the [MCP server](mcp.md) from your own application | None. Your application is a separate work, under whatever licence you choose. |
| [Embed the terminal](embedding.md) in your portal with an `<iframe>` | None. The surrounding page is yours. |
| Run [workflows](workflow.md), [chaos exploration](chaos-mode.md) or the [host profiler](host-profiler.md) | None. Recordings, mind maps, reports and compatibility profiles are your data. |
| Drive host applications through the terminal | None. Nothing you do *through* a terminal is touched by the terminal's licence. |
| Write a skill or an extension that talks to 3270Web over its published interfaces | None, as long as it is a separate program rather than a patch to this one. |

## What the AGPL actually asks

Section 13 is the clause the AGPL adds to the ordinary GPL, and it has one
trigger:

> If you **modify** 3270Web, and you run that modified version as a service
> **other people use over a network**, then those users must be offered the
> corresponding source of your modified version, under this same licence.

Both halves have to be true. Modify it and keep it to yourself — a patched
build only you use — and nothing is triggered. Serve it unmodified to a
hundred thousand users and nothing is triggered. Patch it *and* put the patched
version in front of other people, and the source of that patched version has to
be available to them.

The running application makes that offer for you: the **About** dialog carries
the licence and a link to the source, which is where a network user is meant to
find it. If you modify 3270Web, point that link at *your* source.

"Other people" means people other than you or your organisation. Running a
modified build inside your own company, for your own staff, is internal use —
the FSF's long-standing reading is that this is not conveying to the public.

## Why this licence

3270Web is a network application, and the AGPL is the licence written for
network applications. The bargain it strikes is narrow: improvements to a
terminal that is served to users stay available to the people it is served to.
It costs an ordinary self-hosting deployment nothing, and it prevents the one
outcome that would hollow the project out — someone taking it, improving it,
and serving it back as a closed hosted product.

[3270Connect](https://3270connect.3270.io) — the automation and load-testing
toolkit alongside it — stays under the **MIT Licence**, deliberately. It is a
toolkit whose packages are meant to be imported into other people's code, and
copyleft would defeat that. Different job, different licence.

## Commercial licensing

If your organisation cannot take the AGPL — a blanket policy against it, or a
product that embeds 3270Web and cannot publish its own source — 3270Web is also
available under commercial terms. The copyright sits in one place, so this can
be agreed directly.

Open an issue on [GitHub](https://github.com/3270io/3270Web/issues) or get in
touch through [3270.io](https://3270.io).

## Where the licence changes

3270Web was previously released under the MIT Licence. **0.5.0 is the first
AGPL release; everything up to and including v0.4 is MIT.**

| Version | Licence |
|---|---|
| v0.4 and earlier | MIT |
| 0.5.0 onward | AGPL-3.0-or-later, or commercial terms |

Every version published under MIT stays under it, permanently — a licence
already granted cannot be withdrawn. If you took 3270Web at v0.4 or earlier,
that code remains yours to use on that basis, and you are under no obligation
to move to a later one. The change binds later versions only.

## Third-party components

The AGPL covers 3270Web's own source and nothing else. **s3270**, which carries
every screen it renders, is an independent program run as a separate process,
and it stays BSD 3-Clause licensed — see [Acknowledgements](acknowledgements.md).
The fonts, the vendored JavaScript and the Go dependencies likewise keep their
own permissive terms.

All of it is recorded in
[`THIRD-PARTY-LICENSES.md`](https://github.com/3270io/3270Web/blob/main/THIRD-PARTY-LICENSES.md),
which also says where to obtain the source of the s3270 each distribution
ships.

!!! note "Not legal advice"

    This page describes the maintainer's intent and reading in plain terms so
    that a deployment decision does not need a lawyer. Where it and the
    [licence text](https://www.gnu.org/licenses/agpl-3.0.html) differ, the
    licence text governs.
