#!/usr/bin/env python3
"""Rewrite the commit messages that name commercial terminal emulators.

Six commits in this repository's history describe the work in terms of rival
products. CLAUDE.md forbids that in anything publicly readable, and commit
messages are as public as the code. The files and the docs were cleaned long
ago; only `git log` still carries it. This rewrites those six messages and
nothing else — no tree, no author, no date, no parent structure changes.

## Why the table below is base64

The table has to contain the strings being removed. Committing it in plain text
would write those names into the tree, into this file's own diff, and into
search indexes — the exact outcome the rewrite exists to prevent. So the payload
is encoded. This is not obfuscation: `--show-table` prints all of it, and so
does `base64 -d`. It is the only way for the fix and the repository to coexist.

## Usage

    python3 scripts/rewrite-commit-messages.py --show-table
    python3 scripts/rewrite-commit-messages.py --verify <original-repo> <rewritten-repo>

and, as git-filter-repo's message callback:

    git filter-repo --refs refs/heads/main --force --message-callback '
        import importlib, sys
        sys.path.insert(0, "scripts")
        m = importlib.import_module("rewrite-commit-messages".replace("-", "_"))
        return m.rewrite(message)
    '

This file and the workflow that drives it are meant to be deleted once the
rewrite has landed.
"""

import base64
import collections
import json
import os
import subprocess
import sys

_PAYLOAD = (
    "eyJyZXBsYWNlbWVudHMiOiBbWyJBbHNvIHJlY29yZHMgcGFyaXR5IGdhcHMgYWdhaW5zdCBRdWlj"
    "azMyNzAgYW5kIFBDT01NIChjb25jdXJyZW50XG5zZXNzaW9ucywgSU5EJEZJTEUsIGtleWJvYXJk"
    "IHJlbWFwcGluZywgaG90c3BvdHMsIHBlci1zZXNzaW9uIFRMUy9MVSksIiwgIkFsc28gcmVjb3Jk"
    "cyB0aGUgY2FwYWJpbGl0eSBnYXBzIGFnYWluc3Qgd2hhdCBhbiBlbnRlcnByaXNlIDMyNzBcbnRl"
    "cm1pbmFsIGlzIGdlbmVyYWxseSBleHBlY3RlZCB0byBoYXZlIChjb25jdXJyZW50IHNlc3Npb25z"
    "LCBJTkQkRklMRSxcbmtleWJvYXJkIHJlbWFwcGluZywgaG90c3BvdHMsIHBlci1zZXNzaW9uIFRM"
    "Uy9MVSksIl0sIFsic28gaXQgZmlsbHMgdGhlIGRpc3BsYXkgcmF0aGVyIHRoYW4gYSB3aW5kb3cg"
    "4oCUIHdoaWNoIGlzIG1vcmUgdGhhblxuUXVpY2szMjcwIG9yIFBDT01NIGNhbiBkbywgc2luY2Ug"
    "dGhleSBhcmUgYm91bmRlZCBieSB0aGVpciBvd24gd2luZG93LlxuQWx0K0VudGVyIGlzIGRlbGli"
    "ZXJhdGVseSB0aGUgc2hvcnRjdXQgYm90aCBvZiB0aG9zZSBoYXZlIHVzZWQgZm9yIGZ1bGxcbnNj"
    "cmVlbiBmb3IgZGVjYWRlcy4iLCAic28gaXQgZmlsbHMgdGhlIGRpc3BsYXkgcmF0aGVyIHRoYW4g"
    "YSB3aW5kb3cg4oCUIHdoaWNoIGEgZGVza3RvcCBlbXVsYXRvclxuY2Fubm90IGRvLCBzaW5jZSBp"
    "dCBpcyBib3VuZGVkIGJ5IGl0cyBvd24gd2luZG93LiBBbHQrRW50ZXIgaXNcbmRlbGliZXJhdGVs"
    "eSB0aGUgc2hvcnRjdXQgb3BlcmF0b3JzIGhhdmUgdXNlZCBmb3IgZnVsbCBzY3JlZW4gZm9yXG5k"
    "ZWNhZGVzLiJdLCBbIlRoZSBzaW5nbGUgbW9zdC1jaXRlZCBnYXAgYWdhaW5zdCBRdWljazMyNzAs"
    "IFBDT01NIGFuZCBSdW1iYSsuIEdldHRpbmcgYVxuZGF0YXNldCBvZmYgdGhlIGhvc3QgaW50byBh"
    "IHNwcmVhZHNoZWV0LCBvciBhIGZpbGUgdXAgdG8gaXQsIG90aGVyd2lzZVxubWVhbnQgbGVhdmlu"
    "ZyAzMjcwV2ViIGVudGlyZWx5LiIsICJUaGUgbW9zdC1jaXRlZCBtaXNzaW5nIGNhcGFiaWxpdHks"
    "IGFuZCBvbmUgYW4gZW50ZXJwcmlzZSAzMjcwIHRlcm1pbmFsXG5pcyBnZW5lcmFsbHkgZXhwZWN0"
    "ZWQgdG8gaGF2ZS4gR2V0dGluZyBhIGRhdGFzZXQgb2ZmIHRoZSBob3N0IGludG8gYVxuc3ByZWFk"
    "c2hlZXQsIG9yIGEgZmlsZSB1cCB0byBpdCwgb3RoZXJ3aXNlIG1lYW50IGxlYXZpbmcgMzI3MFdl"
    "YlxuZW50aXJlbHkuIl0sIFsiQWRkIGEga2V5Ym9hcmQgcmVtYXBwaW5nIFVJIHdpdGggUENPTU0g"
    "a2V5bWFwIGltcG9ydCIsICJBZGQgYSBrZXlib2FyZCByZW1hcHBpbmcgVUkgd2l0aCBrZXltYXAg"
    "ZmlsZSBpbXBvcnQiXSwgWyJvcGVyYXRvcnMgY2FycnkgdHdlbnR5IHllYXJzIG9mIG11c2NsZSBt"
    "ZW1vcnkgZnJvbSBQQ09NTSBvciBRdWljazMyNzAgaGFkXG50byByZXRyYWluIHRoZW0uIEV2ZXJ5"
    "IGRlc2t0b3AgZW11bGF0b3Igc2hpcHMgYSByZW1hcHBpbmcgZGlhbG9nO1xuUXVpY2szMjcwIGFk"
    "ZGl0aW9uYWxseSBpbXBvcnRzIFBDT01NIGtleWJvYXJkIGZpbGVzLCB3aGljaCBpcyB3aGF0IHR1"
    "cm5zIGFcbm1pZ3JhdGlvbiBpbnRvIGEgbW9ybmluZyByYXRoZXIgdGhhbiBhIHByb2plY3QuIFRo"
    "aXMgaXMgdGhlIGxhc3Qgb3BlbiIsICJvcGVyYXRvcnMgY2FycnkgdHdlbnR5IHllYXJzIG9mIG11"
    "c2NsZSBtZW1vcnkgZnJvbSBhbm90aGVyIHRlcm1pbmFsXG5lbXVsYXRvciBoYWQgdG8gcmV0cmFp"
    "biB0aGVtLiBSZW1hcHBpbmcgaXMgc3RhbmRhcmQgaW4gdGhpcyBjYXRlZ29yeSwgYW5kXG5iZWlu"
    "ZyBhYmxlIHRvIHJlYWQgYW4gZXhpc3Rpbmcga2V5Ym9hcmQgZmlsZSBpcyB3aGF0IHR1cm5zIGEg"
    "bWlncmF0aW9uXG5pbnRvIGEgbW9ybmluZyByYXRoZXIgdGhhbiBhIHByb2plY3QuIFRoaXMgaXMg"
    "dGhlIGxhc3Qgb3BlbiJdLCBbIi0gVGhlIFBDT01NIC5LTVAgZGlhbGVjdCB2YXJpZXMgYmV0d2Vl"
    "biB2ZXJzaW9ucywgc28gdGhlIGltcG9ydGVyIGlzIiwgIi0gVGhlIC5LTVAgZGlhbGVjdCB2YXJp"
    "ZXMgYmV0d2VlbiB2ZXJzaW9ucywgc28gdGhlIGltcG9ydGVyIGlzIl0sIFsiLSBcIkltcG9ydCBQ"
    "Q09NTVwiIGJlY29tZXMgXCJJbXBvcnQga2V5bWFwIGZpbGVcIi4gVGhlIGZpbGUtcGlja2VyIGZp"
    "bHRlciBpc1xuICB1bmNoYW5nZWQsIHNvIGEgLktNUCBmaWxlIGlzIHN0aWxsIHdoYXQgaXQgb2Zm"
    "ZXJzIHRvIG9wZW4uXG4tIGltcG9ydFBjb21tIC0+IGltcG9ydEtleW1hcEZpbGUsIFBDT01NX0ZV"
    "TkNUSU9OUyAtPlxuICBLRVlNQVBfRklMRV9GVU5DVElPTlMsIHBjb21tS2V5VG9TaWduYXR1cmUg"
    "LT4ga2V5bWFwRmlsZUtleVRvU2lnbmF0dXJlLFxuICBwY29tbUZ1bmN0aW9uVG9BY3Rpb24gLT4g"
    "a2V5bWFwRmlsZUZ1bmN0aW9uVG9BY3Rpb24sIGFuZCB0aGVcbiAgZGF0YS1hdHRyaWJ1dGVzIHRo"
    "YXQgZHJpdmUgdGhlIHR3byBmaWxlIGlucHV0cy4iLCAiLSBUaGUgaW1wb3J0IGJ1dHRvbiBub3cg"
    "c2F5cyBcIkltcG9ydCBrZXltYXAgZmlsZVwiLiBUaGUgZmlsZS1waWNrZXIgZmlsdGVyXG4gIGlz"
    "IHVuY2hhbmdlZCwgc28gYSAuS01QIGZpbGUgaXMgc3RpbGwgd2hhdCBpdCBvZmZlcnMgdG8gb3Bl"
    "bi5cbi0gVGhlIHZlbmRvci1uYW1lZCBpZGVudGlmaWVycyBiZWNvbWUgZm9ybWF0LW5hbWVkIG9u"
    "ZXMg4oCUXG4gIGltcG9ydEtleW1hcEZpbGUsIEtFWU1BUF9GSUxFX0ZVTkNUSU9OUywga2V5bWFw"
    "RmlsZUtleVRvU2lnbmF0dXJlIGFuZFxuICBrZXltYXBGaWxlRnVuY3Rpb25Ub0FjdGlvbiDigJQg"
    "YWxvbmcgd2l0aCB0aGUgZGF0YS1hdHRyaWJ1dGVzIHRoYXQgZHJpdmVcbiAgdGhlIHR3byBmaWxl"
    "IGlucHV0cy4iXV0sICJ0b2tlbnMiOiBbInF1aWNrMzI3MCIsICJwY29tbSIsICJydW1iYSIsICJh"
    "dHRhY2htYXRlIiwgInBlcnNvbmFsIGNvbW11bmljYXRpb25zIl19"
)


def _table():
    data = json.loads(base64.b64decode("".join(_PAYLOAD.split())))
    pairs = [(old.encode(), new.encode()) for old, new in data["replacements"]]
    tokens = [t.encode() for t in data["tokens"]]
    return pairs, tokens


def rewrite(message):
    """Apply the replacements to one commit message.

    Exact-string, not regex. A replacement either applies verbatim or it does
    not apply at all, so a near-miss surfaces as a leftover in the verification
    grep rather than as a silently mangled sentence.
    """
    if isinstance(message, str):
        message = message.encode()
    pairs, _ = _table()
    for old, new in pairs:
        message = message.replace(old, new)
    return message


def _git(repo, *args):
    return subprocess.run(
        ["git", "-C", repo, *args], capture_output=True, text=True, check=True,
    ).stdout


def _fields(repo, sha):
    """Tree, parents, and author/committer identity for one commit."""
    fmt = "%T%n%P%n%an%n%ae%n%aI%n%cn%n%ce%n%cI"
    out = _git(repo, "show", "-s", "--format=" + fmt, sha).splitlines()
    return out[0], out[1].split(), tuple(out[2:8])


def _message(repo, sha):
    return subprocess.run(
        ["git", "-C", repo, "log", "-1", "--format=%B", sha],
        capture_output=True, check=True,
    ).stdout


def _load_map(rewritten):
    path = os.path.join(rewritten, ".git", "filter-repo", "commit-map")
    mapping = {}
    with open(path) as fh:
        for line in fh.read().splitlines()[1:]:
            old, new = line.split()
            mapping[old] = new
    return mapping


def verify(original, rewritten):
    """Refuse the push unless the rewrite changed messages and nothing else.

    Driven by git-filter-repo's commit-map rather than by zipping the two
    histories positionally, because the commit COUNT legitimately drops here and
    a positional comparison cannot tell a safe collapse from a dropped commit.

    Why it drops: main carries a duplicated subgraph — the residue of an earlier
    attempt at this same rewrite being *merged into* main instead of replacing
    it. Those duplicate commits differ from their twins only in the messages
    this script rewrites, so once both sides say the same thing they become
    byte-identical and git stores one object instead of two. Eleven pairs
    collapse that way. Nothing is lost, and the check below proves it: every
    collapse must be between commits that already had the same tree, the same
    author and committer, and the same parents after remapping.

    Checking that the tip trees match is not enough, and that is not a
    hypothetical: an earlier attempt with `git filter-branch` over a commit
    range produced an identical tip tree while silently dropping eleven
    *different* commits, because it pruned merge parents outside the range.
    """
    failures = []
    mapping = _load_map(rewritten)
    pairs, tokens = _table()

    dropped = [o for o, n in mapping.items() if set(n) == {"0"}]
    print("originals in the commit map  : %d" % len(mapping))
    print("originals mapped to nothing  : %d" % len(dropped))
    for sha in dropped:
        failures.append("%s was dropped entirely" % sha)

    changed = []
    for old, new in mapping.items():
        if set(new) == {"0"}:
            continue
        to, po, io = _fields(original, old)
        tn, pn, inew = _fields(rewritten, new)
        if to != tn:
            failures.append("%s -> %s: tree changed" % (old, new))
        if io != inew:
            failures.append("%s -> %s: author/committer changed" % (old, new))
        if len(po) != len(pn):
            failures.append(
                "%s -> %s: parent count changed (%d -> %d); a merge was linearised"
                % (old, new, len(po), len(pn))
            )
        mo, mn = _message(original, old), _message(rewritten, new)
        if mo != mn:
            changed.append(old)
            if rewrite(mo) != mn:
                failures.append(
                    "%s -> %s: message is not what the table produces" % (old, new)
                )

    # Every snapshot that existed must still exist.
    ta = set(_git(original, "rev-list", "--format=%T", "--no-commit-header",
                  "main").split())
    tb = set(_git(rewritten, "rev-list", "--format=%T", "--no-commit-header",
                  "main").split())
    print("distinct trees               : %d original, %d rewritten"
          % (len(ta), len(tb)))
    if ta != tb:
        failures.append(
            "the set of trees in history changed (%d -> %d): a snapshot was "
            "lost or invented" % (len(ta), len(tb))
        )

    # Where two originals became one commit, they must have been duplicates
    # already — same tree, same identity, same parents once remapped.
    collapsed = collections.defaultdict(list)
    for old, new in mapping.items():
        if set(new) != {"0"}:
            collapsed[new].append(old)
    groups = {n: o for n, o in collapsed.items() if len(o) > 1}
    print("collapsed groups             : %d" % len(groups))
    for new, olds in groups.items():
        trees = {_fields(original, o)[0] for o in olds}
        ids = {_fields(original, o)[2] for o in olds}
        remapped = {tuple(mapping[p] for p in _fields(original, o)[1]) for o in olds}
        if len(trees) != 1 or len(ids) != 1 or len(remapped) != 1:
            failures.append(
                "%s: collapsed commits that were not already duplicates (%s)"
                % (new, ", ".join(olds))
            )

    # The set that changed must be exactly the set that needed to.
    should = [o for o in mapping if any(t in _message(original, o).lower()
                                        for t in tokens)]
    print("messages carrying a name     : %d" % len(should))
    print("messages rewritten           : %d" % len(changed))
    if sorted(should) != sorted(changed):
        failures.append(
            "rewrote %d messages but %d needed it" % (len(changed), len(should))
        )

    # And nothing may remain.
    left = [n for n in set(mapping.values())
            if set(n) != {"0"}
            and any(t in _message(rewritten, n).lower() for t in tokens)]
    print("messages still carrying one  : %d" % len(left))
    for sha in left:
        failures.append("%s: still names a vendor after the rewrite" % sha)

    _report(failures)
    return 1 if failures else 0


def _report(failures):
    if failures:
        print("\nVERIFICATION FAILED")
        for f in failures:
            print("  - " + f)
        print("\nNothing will be pushed.")
    else:
        print("\nVERIFICATION PASSED: messages only.")


def main(argv):
    if len(argv) == 2 and argv[1] == "--show-table":
        pairs, tokens = _table()
        for i, (old, new) in enumerate(pairs, 1):
            print("--- replacement %d ---" % i)
            print("OLD:\n" + old.decode())
            print("NEW:\n" + new.decode())
        print("--- tokens the verification greps for ---")
        print(", ".join(t.decode() for t in tokens))
        return 0
    if len(argv) == 4 and argv[1] == "--verify":
        return verify(argv[2], argv[3])
    print(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
