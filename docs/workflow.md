# Recordings and Playback

3270Web can capture terminal actions as JSON recordings and run them later.

## Recording and Playback Callouts

![Recording and playback screenshot](images/workflow-controls-real.png){: .doc-medal }
{: .doc-medal-wrap }

1. Start recording
2. Play recording
3. Debug recording
4. View recording JSON
5. Remove loaded recording
6. Workflow status indicator/widget

## Record a Session

1. Connect to the target host.
2. Click **Start recording**.
3. Perform your terminal actions.
4. Click **Stop recording**.
5. Optional: click **Download** to save the generated JSON file.

## Load a Recording

1. Click **Load recording**.
2. Choose a `.json` file.
3. Confirm the filename appears as loaded in the toolbar.
4. Click **View recording** to inspect the full JSON.

## Play a Recording

1. Load a recording.
2. Click **Play recording**.
3. Watch playback status in the toolbar and Workflow Status widget.
4. Use **Pause/Resume** in play mode if needed.
5. Click **Stop playback** to end execution.

## Debug a Recording (Step-by-step)

1. Load a recording.
2. Click **Debug recording**.
3. Use **Step** to execute one action at a time.
4. Watch current step number/type in the status indicators.
5. Click **Stop playback** when done.

Debug mode is recommended for new or edited recordings.

## Remove a Loaded Recording

Click **Remove recording** to clear the currently loaded file from the session.

## Workflow Status Widget

The Workflow Status panel shows:

- Current step and total steps
- Current action type
- Delay range and applied delay (when present)
- Recent playback events

You can:

- Open it from the status indicator
- Minimize/maximize it
- Enable or disable tracking

## Recording JSON Basics

A recording includes a `Steps` array of actions.

Example:

```json
{
  "Host": "sampleapp:app1",
  "Steps": [
    { "Type": "Connect" },
    {
      "Type": "FillString",
      "Coordinates": { "Row": 5, "Column": 21 },
      "Text": "User"
    },
    { "Type": "PressEnter" },
    { "Type": "Disconnect" }
  ]
}
```

Common action types:

- `FillString`
- `PressEnter`
- `PressTab`
- `PressPF<n>` (for example `PressPF3`)

## Troubleshooting Playback

- Confirm host and port are correct.
- Confirm terminal model matches the one used when recording.
- Confirm screen layout and field coordinates still match host screens.
- Add delays for timing-sensitive screens.
- Use Debug mode to find the first failing step.
