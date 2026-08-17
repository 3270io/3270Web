# Licensing

3270Web's own source is licensed under the **GNU Affero General Public
License, version 3 or later** (AGPL-3.0-or-later). The full text is in
[`LICENSE`](LICENSE).

This page explains what that means in practice, what it does *not* cover,
and how to get different terms if the AGPL does not suit you.

## What it means for you

**Running it, unmodified, is unconditional.** Self-host 3270Web for your own
users — inside a bank, on a laptop, in CI, behind a corporate proxy — and the
AGPL asks nothing of you. Use it commercially, connect it to production hosts,
put it in front of as many people as you like. There is no user count, no
seat, no registration and no fee.

**The obligation only starts if you modify it and let others use it over a
network.** If you change 3270Web's source and run that changed version as a
service other people interact with, section 13 says those users must be
offered the corresponding source of your modified version, under the same
licence. That is the whole of the copyleft bargain: improvements to a
network-served terminal stay available to the people it is served to.

**What is *not* a modification:**

- Configuring 3270Web — `3270Web-config.xml`, environment variables, themes,
  keymaps, connection profiles.
- Calling its REST API, its MCP server, or embedding the terminal in your own
  page with an `<iframe>`. Your application on the other end of the API is
  your own work, under whatever licence you choose.
- The screens, data or host applications you drive with it. Nothing you do
  *through* the terminal is touched by this licence.
- Workflow recordings, chaos reports and compatibility profiles it produces.
  Its output is yours.

## Commercial licensing

If the AGPL does not fit — a policy that prohibits AGPL software, a product
that embeds 3270Web and cannot publish its own source, an OEM arrangement —
3270Web is also available under commercial terms. The copyright is held in
one place, so this can be agreed directly.

Open an issue at <https://github.com/3270io/3270Web/issues> or contact the
maintainer through <https://3270.io>.

## Where the licence changes

**0.5.0 is the first AGPL release. Everything up to and including v0.4 is
MIT.**

| Version | Licence |
|---|---|
| v0.4 and earlier | MIT |
| 0.5.0 onward | AGPL-3.0-or-later, or commercial terms |

The MIT releases stay MIT, permanently. A licence already granted cannot be
withdrawn: if you obtained 3270Web at v0.4 or earlier, those terms continue to
apply to that code, and you may keep using and forking it on that basis. This
change binds later versions only.

## Third-party components are unaffected

3270Web does not implement TN3270 itself — **s3270**, from the x3270 family,
carries every screen it renders, and runs as a separate process invoked over
its scripting interface. s3270 is BSD 3-Clause licensed and stays that way; it
is an independent program aggregated with 3270Web, not part of it, and
relicensing 3270Web's own code does not and cannot relicense it.

The BSD 3-Clause terms permit exactly this combination — a permissive licence
may be brought into a copyleft work, and the obligations it does impose
(reproducing the copyright notice, the conditions and the disclaimer for
binary redistribution) are met in
[`THIRD-PARTY-LICENSES.md`](THIRD-PARTY-LICENSES.md), which also records where
each distribution obtains its s3270 and where to get its source.

The same holds for the other bundled components — the 3270 fonts, the vendored
JavaScript, the Go module dependencies. Each keeps its own terms. All are
permissive (MIT, ISC, BSD or Apache-2.0) and all are compatible with the AGPL.

## The boundary with 3270Connect

[3270Connect](https://github.com/3270io/3270Connect) is **MIT licensed and
stays MIT.** It is an automation toolkit whose packages are meant to be
imported by other people's code, and copyleft would defeat that.

The two repositories deliberately share several packages —
`internal/{authz,authsession,users,apitoken,audit,oidc,reqsec,agent,profiler}`
— kept in step by hand rather than by a module dependency. That sharing now
crosses a licence boundary, so it has one rule:

> Code may move from 3270Connect (MIT) into 3270Web (AGPL). It may **not**
> move from 3270Web into 3270Connect unless the copyright holder owns it or
> it was contributed under the terms in [`CONTRIBUTING.md`](CONTRIBUTING.md).

MIT is compatible with the AGPL in one direction only. Today every line in
those packages is the maintainer's own, so both copies are lawful — the
copyright holder may license their own work under any number of licences at
once. The rule exists so that an outside contribution to an AGPL-only file
does not get copied into an MIT repository later, when the provenance is no
longer obvious. If in doubt, write the code in 3270Connect first and copy it
across in the permitted direction.

## SPDX

Source files carry `SPDX-License-Identifier: AGPL-3.0-or-later`. The
repository-level identifier is the same. Automated licence scanners should
read the SPDX headers and `LICENSE`; `THIRD-PARTY-LICENSES.md` is the
authority for anything 3270Web ships but did not write.
