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

You can also seed chaos mode directly from a loaded recording:

1. Load a recording in the recording section.
2. Click **Load recording into chaos** in the chaos toolbar section.
3. Start or resume chaos exploration.

When chaos output is saved, its filename is kept separate from the loaded recording filename to avoid overwriting the recording JSON.

## Chaos Hints

Chaos Hints let you guide generated input values during exploration.

1. Click **Edit chaos hints** in the chaos toolbar.
2. Add hint rows with:
   - `Transaction` values (for example, known transaction codes), and/or
   - `Known data` values (comma or newline separated).
3. Optional: click **Load from recording** to import hint candidates from a previous recording JSON.
4. Click **Save hints** to persist them.
5. Use **Load saved** to reload persisted hints into the modal.

Notes:

- Hints are saved to `chaos-hints.json`.
- Saved hints are automatically applied when starting or resuming chaos if request-level hints are not explicitly supplied.
- Transaction hints are preferred for early field writes, while known data values are reused across fields when they fit field constraints.

## Chaos Settings

Chaos behavior is configurable in **Settings -> Chaos**:

- Max steps
- Time budget
- Step delay
- Random seed
- Max field length
- Optional output file path
- Exclude no-progress events (default on)

Use small limits first when testing new host flows, then increase limits for broader exploration.
