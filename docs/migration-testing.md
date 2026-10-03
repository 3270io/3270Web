---
seo_title: "Compare 3270 screen flows before a mainframe migration"
description: >-
  Compare recorded 3270 screen maps across a baseline and candidate host,
  investigate field and navigation changes, then replay representative business flows.
---

# Compare 3270 screen flows before a mainframe migration

3270Web can compare exported screen maps from two environments to identify
field and navigation differences. Use it as one input to a migration review,
then replay representative transactions and verify their business outcomes.
A matching screen map is not proof that data, timing or every application
path is equivalent.

## Keep the comparison reproducible

Choose an approved test account and a known initial state on each host.
Match terminal model, screen size, code page and the permissions of the
accounts. Record the application build, configuration and test-data version
with each export. A different account audience can change reachable screens
even when the application code is unchanged.

Start with a small journey: sign in, navigate to one enquiry, inspect the
result and return. A [bundled sample](sample-apps.md) is a useful place to
learn the controls before exploring a business application.

## Capture baseline and candidate maps

1. Connect to the baseline test host and use
   [Chaos Mode](chaos-mode.md) to explore the approved paths.
2. Export the mind map and label it with the host/build/test-data revision.
3. Connect to the candidate host, restore the same initial state and repeat
   the exploration with comparable settings.
4. Keep both exports and the discovery reports together. Record excluded or
   unexplored paths explicitly; absence from an export is not necessarily
   absence from the application.

See [Chaos Mind-Map Compare](chaos-compare.md) for export commands and the
`baseline`/`candidate` request format. The comparison operates on the two
supplied JSON documents; it does not silently run a new exploration.

## Interpret deltas as questions to investigate

| Report result | Follow-up check |
|---|---|
| Only in baseline | Was the screen removed, unreachable with this account, or simply not explored? |
| Only in candidate | Is this an intended new path or an unexpected transition? |
| Field added/removed/modified | Does an input moved or resized here affect the recorded workflow? |
| Transition removed/redirected | Does the same PF/Enter action still reach the expected business step? |
| No deltas | Was meaningful coverage achieved, and are business results also equivalent? |

For example, a removed PF3 transition is a reason to replay that navigation
step manually and in a recorded workflow. A new field is a reason to inspect
its position and semantics, not to immediately rewrite every test. The
[comparison reference](chaos-compare.md#response-format) describes how areas
are matched and the exact counters returned.

## Replay the business result

Debug the [recorded workflow](workflow.md) on each host. Check the expected
fields and final outcome, and compare approved screen snapshots where useful.
Account for legitimate differences such as generated IDs or timestamps rather
than treating every changed character as a defect.

Use [Host Compatibility Profiler](host-profiler.md) to compare negotiated
terminal capabilities separately. When functional checks pass, run an
approved workload through
[3270Connect load testing](https://3270connect.3270.io/load-testing/) to
investigate timing and concurrency under a defined load profile.

## Keep an evidence record

Retain the two maps, discovery settings, inspected deltas, recorded workflow,
assertion results and the known coverage gaps. Restrict access to exports and
screens that contain business data. Repeat the same review after a fix; use
the resulting evidence to support the migration decision alongside data,
security and operational testing.
