---
seo_title: "Browser TN3270 vs desktop x3270: choose your workflow"
description: >-
  Compare a self-hosted 3270Web browser terminal with desktop x3270 by
  deployment, host access and automation. Choose based on your team's real tasks.
---

# Browser TN3270 vs desktop x3270: which fits your workflow?

Choose a terminal around where it runs, how it reaches the host and the tasks
your users perform. 3270Web offers browser access through a self-hosted service;
x3270 offers a desktop terminal. **3270Web itself uses s3270 from the x3270
family** for protocol work, so this is a comparison of deployment and user
workflows, not a claim that one implements TN3270 better.

## Compare the practical trade-offs

| Decision | 3270Web browser workflow | Desktop x3270 workflow |
|---|---|---|
| Client deployment | Users open the service in a browser | Install/configure the appropriate desktop client |
| Host network route | The service connects to the TN3270 host | The workstation connects to the host |
| Operations | Maintain the shared service and its configuration | Maintain client installations and workstation configuration |
| Existing habits | Evaluate the browser keyboard, printing and screen presentation | Preserve a familiar desktop terminal setup |
| Integration | Browser embedding, JSON REST API and MCP are documented interfaces | Evaluate the x3270 family's scripting interfaces for your existing tooling |
| Recording to load testing | Export reviewed workflows to 3270Connect | Existing scripts need an explicit migration/evaluation plan |

If a small number of operators already have a working desktop setup and do
not need shared browser access, keeping that setup may be the simpler choice.
If users need access from centrally managed browser clients or a portal,
evaluate 3270Web's [shared deployment](multi-user.md) and
[embedding](embedding.md) model.

## Run a fair evaluation

Use the same test host and representative transaction in both environments.
Check terminal model, character set, field navigation, PF keys, reconnects,
printing and the errors your operators actually meet. Include accessibility
and the devices your team uses; do not infer those results from a feature list.

Inventory macros and integrations before moving clients. 3270Web can import
some BASIC-family macros into recordings, but reports unsupported statements
and cannot promise an identical conversion of every script. Read
[Recordings and Playback](workflow.md), review the conversion report and debug
the result before using it against a live host.

## Costs and boundaries

Both approaches need operational ownership. A browser service moves some
client setup into server hosting and administration; it does not eliminate
infrastructure or identity-management work. 3270Web's current release uses
AGPL-3.0-or-later, with commercial terms available; review its
[licence](licence.md) separately from x3270's own terms.

For upstream details, use the
[x3270 project documentation](https://x3270.miraheze.org/wiki/Main_Page).
For a browser evaluation with no mainframe dependency, start with
[the bundled 3270Web samples](browser-terminal.md#try-it-without-a-mainframe).
