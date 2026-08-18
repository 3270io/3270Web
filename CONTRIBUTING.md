# Contributing to 3270Web

Contributions are welcome — bug reports, host-compatibility findings, docs
fixes and code alike. This page covers the licence terms your contribution
comes under, and the mechanics of getting a change in.

## Licence terms for contributions

3270Web is licensed under AGPL-3.0-or-later, and is also offered under
commercial terms to users who cannot take the AGPL (see
[`LICENSING.md`](LICENSING.md)). For that dual offering to stay possible, the
project needs slightly more from a contributor than the licence alone gives.

By submitting a contribution — a pull request, a patch, a suggested diff in an
issue — you agree that:

1. You license your contribution to the project and to everyone else under
   **AGPL-3.0-or-later**, the same terms as the rest of 3270Web; and
2. You grant the copyright holder a perpetual, worldwide, non-exclusive,
   royalty-free and irrevocable right to **license your contribution under
   other terms as well**, including commercial terms and the MIT Licence used
   by [3270Connect](https://github.com/3270io/3270Connect); and
3. You **keep your copyright**. This is a licence grant, not an assignment.
   Nothing here takes ownership of your work away from you, and you remain
   free to use your own contribution however you like, elsewhere.

Point 2 is what makes the commercial offering and the shared packages work.
Without it the project could not license a contributed line to a commercial
user, and could not copy a shared authentication fix back into 3270Connect.

If you are contributing work owned by your employer, make sure whoever can
agree to the above on their behalf has done so before you open the pull
request.

## Sign your commits (DCO)

Every commit needs a Developer Certificate of Origin sign-off, which records
that you have the right to submit the work. Add it with `-s`:

```bash
git commit -s -m "Fix the thing"
```

That appends the line git checks for:

```
Signed-off-by: Your Name <your.email@example.com>
```

The full DCO text is at <https://developercertificate.org>. Signing off
certifies the DCO **and** the terms in the section above.

## Before you open a pull request

```bash
go test ./...                # the canonical check; keep it green
go vet ./...
gofmt -l .                   # should print nothing
```

For front-end changes, start the server and check the change in a browser
before marking it done — the terminal UI has behaviour that tests do not
cover.

## Changing how a screen is read or drawn

Anything touching the 3270 data stream — the decoder in `internal/host`, the
field model, the renderer, or the styles the renderer names — belongs in the
**terminal conformance suite** rather than in a test built from a captured
transcript.

The difference matters. A captured `ReadBuffer` response answers "does this
parse", and nothing more: the capture was written by whoever wrote the test,
so it agrees with them by construction. A terminal-fidelity bug is exactly a
disagreement between what the terminal says and what this code believes it
says, and that kind of bug survives that kind of test indefinitely. Blink was
compared against a value the attribute cannot hold for as long as there were
tests for blink.

The suite is in `internal/host`:

| File | What it holds |
|---|---|
| `tn3270host_test.go` | A scripted TN3270 host and a 3270 data-stream builder |
| `conformance_test.go` | Field attributes, extended attributes, characters |
| `conformance_inbound_test.go` | What the terminal sends *back* — AID bytes, modified fields, the cursor |
| `conformance_screen_test.go` | Screen sizes, code pages, unformatted screens |
| `conformance_edges_test.go` | Where the buffer wraps and where fields meet |

A test writes a real data stream, a real `s3270` reads it, and the assertion
is on the decoded `Screen` — or, for the inbound direction, on the bytes the
host received:

```go
screen := newScreen(80).
    at(0, 0).fieldExtended(faProtected, 0x42, AttrColRed, 0x41, AttrEhBlink).text("URGENT").
    at(1, 0).field(0).cursor().
    bytes()

host := startConformanceHost(t, screen)
term := connectTerminal(t, host, "-model", "2")
```

`go test ./internal/host/` runs it. A platform with no `s3270` to run skips
rather than fails; point `S3270_TEST_BINARY` at one to be sure it is running.

There is one thing the suite cannot see: a style the renderer names and the
stylesheet has never heard of, which renders as nothing at all with no error
anywhere. `internal/render/style_coverage_test.go` covers that gap by
rendering every attribute the decoder can produce and checking the stylesheet
defines each class it emits. Add the class *and* the rule, and it stays quiet.

New Go files need the licence header the rest of the tree carries:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
```

## Third-party code

Do not paste in code you did not write unless its licence is compatible with
AGPL-3.0-or-later **and** you can license it under the terms above — which,
for someone else's code, you usually cannot. If a change needs a third-party
component, add it as a dependency and record it in
[`THIRD-PARTY-LICENSES.md`](THIRD-PARTY-LICENSES.md) rather than vendoring the
source in unmarked. Copyleft dependencies stricter than the AGPL, and
GPLv2-only code, cannot be taken at all.

## Reporting security issues

Please do not open a public issue for a security problem. Contact the
maintainer through <https://3270.io> and give us a chance to fix it before it
is described in public.
