---
seo_title: "Choose a browser-based TN3270 terminal with 3270Web"
description: >-
  Evaluate 3270Web for browser-based IBM 3270 access: self-hosting, shared
  accounts, recordings and optional AI. Try a bundled sample before a live host.
---

# Choose a browser-based TN3270 terminal for your team

3270Web is a self-hosted IBM 3270 terminal accessed through a browser. It suits
developers, testers and operators who want terminal access without deploying
a desktop emulator to every client, plus recording, APIs and optional AI
assistance. A server still has to run 3270Web and reach your TN3270 host; the
browser alone does not connect directly to the mainframe.

## Check the deployment fit

| Question | What to evaluate |
|---|---|
| Where does it run? | A server or workstation using a supported native binary, Docker or Compose |
| Who can reach the host? | The 3270Web service needs network access to the TN3270 endpoint |
| How do users sign in? | Configure accounts and the host profiles each audience can use |
| What does the host require? | Confirm TLS, terminal model, code page, LU routing and keyboard behaviour |
| Do you need automation? | Record JSON flows, use the REST API or connect an MCP client |
| Do you need AI? | Optional; choose your own provider or local model and its data policy |

Use [Installation](installation.md), [Shared Instances](multi-user.md) and
[Terminal Models](terminal-model-limits.md) to check requirements. Test your
actual application before selecting a client; a compatible protocol does not
guarantee identical behaviour for every screen or workflow.

## Try it without a mainframe

1. Follow [Install and Run](installation.md) for your chosen method.
2. Open the address shown by the installation instructions.
3. On the connect page, choose **Start sample app**, or use
   `sampleapp:petstore` as the hostname.
4. Navigate a short flow, enter fields and check the keyboard/PF keys you use.
5. [Record it](workflow.md), download the JSON, then replay it in debug mode
   and verify that it lands on the same result.

The [bundled samples](sample-apps.md) make the first evaluation independent of
host access and production data. To measure concurrency later, pass a reviewed
workflow to [3270Connect](https://3270connect.3270.io/load-testing/).

## Understand costs and licensing

The current open-source release uses **AGPL-3.0-or-later**; commercial terms
are also available. Read the [licence guide](licence.md) for version history
and deployment considerations. The open-source software has no acquisition
fee; your hosting, operations and optional AI provider are separate costs.
AI is not required for ordinary terminal use or recording.

3270Web runs **s3270**, part of the x3270 family, as a separate process. It
adds a browser interface and associated workflows rather than replacing that
protocol implementation. See [Acknowledgements](acknowledgements.md).

## Choose the next step

- [Compare browser and desktop workflows](browser-vs-desktop.md) before a migration.
- [Connect your test host](configuration.md) and check a representative transaction.
- [Set up shared access](multi-user.md) before offering the instance to a team.
- [Embed the terminal](embedding.md) when it belongs inside an existing portal.
- [Configure AI providers](ai-providers.md) only if your workflow needs them.
