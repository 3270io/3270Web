# AI Chat Mode

AI Chat mode is a side panel that lets you drive a 3270 session through conversation. You type instructions in plain language; the AI reads the current screen, fills fields, presses keys, and runs chaos exploration — all with your approval before each action.

## Open the Panel

1. Connect to a host.
2. Click the **Open Copilot chat** button in the toolbar (the chat bubble icon).
3. The side panel slides open on the right.

## Sign In

The first time you open the panel you need to authenticate with GitHub:

1. Click **Sign in with GitHub**.
2. Copy the one-time code shown.
3. Visit the verification URL, enter the code, and approve the access request in your browser.
4. Return to 3270Web — the panel polls automatically and logs you in once you approve.

To sign out at any time, click the **Sign out** button in the panel header.

### GitHub Enterprise

If your organisation uses GitHub Enterprise, click **Set Enterprise URL** and enter your GitHub Enterprise instance URL before signing in.

## Send a Message

Type any instruction in the input box and press **Send** (or ++enter++).

Examples:

- *"Read the current screen and tell me what options are available."*
- *"Navigate to the account inquiry menu and look up account 12345."*
- *"Run a chaos exploration and give me a summary when it finishes."*

The AI reads the current screen before acting, then proposes one tool call at a time.

## Tool Approval

Each AI action requires explicit approval:

- The panel shows a **Run** button before executing any tool call.
- Click **Run** to approve it, or ignore it to skip.
- This prevents unintended writes or key presses.

To let the AI proceed without pausing, enable **Auto Mode** (toggle in the panel header). In Auto Mode the panel runs tool calls automatically without waiting for you to click **Run** each time.

## Available Actions

The AI can perform the following actions on your 3270 session:

| Action | Description |
|--------|-------------|
| Read screen | Returns the current screen as ASCII text with a full field map (row, col, value, protection flags) |
| Send key | Sends any AID key: `Enter`, `PF1`–`PF24`, `PA1`–`PA3`, `Tab`, `BackTab`, `Clear`, `Reset`, `Home`, arrow keys, and more |
| Write field | Writes text into an unprotected field at a given row and column |
| Submit screen | Writes modified fields then presses `Enter` |

## Chaos Integration

The AI can run and monitor chaos exploration directly from the chat panel:

| Command | What it does |
|---------|-------------|
| Start chaos | Begins automated exploration with configurable step/time limits |
| Stop / Resume chaos | Stops a running run or resumes a loaded one |
| Chaos status | Returns current progress (steps, transitions, unique screens) |
| Chaos report | Generates a Markdown discovery report with an ASCII screen graph, per-screen stats, and suggested next experiments |
| Save screen hints | Saves known transaction codes, data values, and key assignments for a specific screen |
| Export workflow | Exports the learned paths as 3270Connect-compatible JSON |

The default system prompt uses a five-phase workflow: read the screen → review existing hints → ask you to choose run mode → start exploration → export results. You can override this by writing your own instructions.

See [Chaos Mode](chaos-mode.md) for how chaos exploration works independently of the AI panel.

## Model Selection

Use the **Model** dropdown in the panel to switch between available Copilot models at any time. The default is Claude Opus 4.

## Clear the Conversation

Click **Clear** in the panel header to remove all messages from the current chat session. A confirmation dialog appears before the history is deleted.

## Keyboard Shortcut

There is no default keyboard shortcut for the panel. All other 3270 key bindings remain active while the panel is open; see [Keyboard and Controls](keyboard-and-controls.md).
