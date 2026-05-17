# 3270Web

**The modern browser client for IBM 3270 mainframe sessions** — with AI-driven automation, chaos exploration, workflow recording, and a full REST API.

No emulator install. No thick client. Open a browser tab, connect to any TN3270 host, and work exactly like you would at a real 3270 terminal — then let the automation features do the heavy lifting.

---

## Feature Highlights

<div class="grid cards" markdown>

-   :material-robot-excited: **AI Chat Mode**

    ---

    Drive any 3270 session by typing plain English. The AI reads the current screen, proposes field fills and key presses, and waits for your approval before acting. Toggle **Auto Mode** to let it run hands-free.

    [:octicons-arrow-right-24: AI Chat Mode](ai-chat.md)

-   :material-lightning-bolt: **Chaos Exploration**

    ---

    Auto-discover every navigation path on a host. Chaos mode fills fields with generated values, presses AID keys, and records every screen transition — then exports a reusable workflow JSON ready for load testing with [3270Connect](https://github.com/3270io/3270Connect).

    [:octicons-arrow-right-24: Chaos Mode](chaos-mode.md)

-   :material-record-circle: **Workflow Recording & Playback**

    ---

    Capture terminal actions as portable JSON. Replay them with a single click, step through them one action at a time in debug mode, or hand the file to any CI pipeline.

    [:octicons-arrow-right-24: Recordings and Playback](workflow.md)

-   :material-api: **REST API**

    ---

    Connect, read screens, submit input, and control chaos runs over HTTP. Wire 3270 sessions into any automation stack — scripts, pipelines, or custom tooling.

    [:octicons-arrow-right-24: REST API](rest-api.md)

</div>

---

## Screenshots

![Connect screen](images/connect_image.png)
![Session screen](images/yorkshire_image.png)
![Logging screen](images/logging_image.png)
![Sample app](images/sampleapp1_image.png)

---

## First Session

1. Open 3270Web in your browser.
2. Enter your TN3270 host and port on the connect screen.
3. Use the terminal — keyboard shortcuts, PF keys, and field navigation all work as expected.
4. Optional: hit **Start recording** to capture the session for replay later.

[:octicons-arrow-right-24: Full setup guide](configuration.md)

---

## What's in This Guide

| Section | What you'll find |
|---|---|
| [Connect and Use 3270Web](configuration.md) | Host configuration, startup options, UI tour |
| [Recordings and Playback](workflow.md) | Record, load, play, debug, and export workflows |
| [Chaos Mode](chaos-mode.md) | Automated screen exploration and load-test export |
| [AI Chat Mode](ai-chat.md) | Conversational session control via GitHub Copilot |
| [Keyboard and Controls](keyboard-and-controls.md) | Full keyboard shortcut reference |
| [Screen Size and Model Guide](terminal-model-limits.md) | 3270 model limits and field size rules |
| [REST API](rest-api.md) | Endpoint reference for scripting and CI |
| [Feature Roadmap](feature-roadmap.md) | Planned and in-progress features |
