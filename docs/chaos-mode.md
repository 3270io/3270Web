# Chaos Mode

Chaos mode explores host screens by filling input fields with generated values and submitting AID keys (`Enter`, `Tab`, `PF*`, and others). It is useful for discovering navigation paths and producing reusable workflow JSON.

## Start a Chaos Run

1. Connect to a host.
2. Click **Start chaos exploration** in the toolbar.
3. Watch run activity in:
   - the toolbar (`CHAOS` indicator + stats), and
   - the Workflow Status widget (attempts, writes, transitions, and errors).

## During the Run

Chaos mode continuously:

- Reads the current screen
- Writes generated values into unprotected fields
- Sends an AID key
- Records transition and attempt metadata

You can stop the run at any time with **Stop chaos exploration**.

## Completion Status

When chaos mode ends, the UI shows completed state:

- `CHAOS COMPLETE` indicator in the toolbar
- Final run statistics (steps, transitions, unique screens/inputs)
- Completion details in the Workflow Status widget

This gives immediate confirmation that the run finished and data is ready for export.

## Download the JSON Output

After a run has data, click **Download chaos workflow JSON** in the toolbar.

- The exported file is a workflow JSON compatible with workflow load/playback.
- If a run ID is available, the filename includes it for easier future reference.

## Load and Resume Saved Runs

You can reuse previous chaos results:

1. Click **Load previous chaos run**.
2. Pick a saved run from the modal list.
3. Optional: click **Resume chaos exploration from loaded run** to continue discovery.
4. Export JSON again when done.

## Chaos Settings

Chaos behavior is configurable in **Settings -> Chaos**:

- Max steps
- Time budget
- Step delay
- Random seed
- Max field length
- Optional output file path

Use small limits first when testing new host flows, then increase limits for broader exploration.
