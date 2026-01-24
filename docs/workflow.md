# Workflow Configuration

Workflows allow automating interactions with 3270 hosts. They are JSON files that define a sequence of steps to be executed against a host.

## Structure

The root object contains configuration for the session and a list of steps.

```json
{
  "Host": "hostname:port",
  "Steps": [ ... ]
}
```

### Top-Level Fields

| Field | Type | Description |
|-------|------|-------------|
| `Host` | String | Target host (e.g., `localhost:3270` or `sampleapp:app1`). Optional; if omitted, the current session host is used. |
| `Port` | Integer | Target port. Optional. |
| `Steps` | Array | List of [Step](#step-objects) objects. Required. |
| `EveryStepDelay` | Object | Default delay between steps. See [Delay Object](#delay-object). |
| `OutputFilePath` | String | (Internal) Path used during recording. |
| `RampUpBatchSize` | Integer | (Internal) Batch size for load testing. |
| `RampUpDelay` | Float | (Internal) Delay for ramp-up. |
| `EndOfTaskDelay` | Object | Delay after the workflow completes. See [Delay Object](#delay-object). |

### Step Objects

Each step represents an action.

| Field | Type | Description |
|-------|------|-------------|
| `Type` | String | The type of action (see [Step Types](#step-types)). Required. |
| `Coordinates` | Object | Target coordinates for `FillString`. See [Coordinates Object](#coordinates-object). |
| `Text` | String | Text to type for `FillString`. |
| `StepDelay` | Object | Delay after this step. Overrides `EveryStepDelay`. See [Delay Object](#delay-object). |

### Coordinates Object

Used by `FillString` to specify where to type.

| Field | Type | Description |
|-------|------|-------------|
| `Row` | Integer | Row number (1-based). |
| `Column` | Integer | Column number (1-based). |

### Delay Object

Defines a random delay duration range.

| Field | Type | Description |
|-------|------|-------------|
| `Min` | Float | Minimum delay in seconds. |
| `Max` | Float | Maximum delay in seconds. |

## Step Types

| Type | Description |
|------|-------------|
| `Connect` | Recorded at start. Ignored during playback (session connects automatically). |
| `Disconnect` | Recorded at end. Ignored during playback to keep the session alive. |
| `FillString` | Types text into a field at the specified `Coordinates`. |
| `PressEnter` | Sends the Enter key. |
| `PressTab` | Sends the Tab key. |
| `PressPF<n>` | Sends a PF key (e.g., `PressPF1`, `PressPF12`). |
| `PressPA<n>` | Sends a PA key (e.g., `PressPA1`). |
| `PressClear` | Sends the Clear key. |
| `PressReset` | Sends the Reset key. |
| `PressEraseInput` | Sends the Erase Input key. |
| `PressEraseEOF` | Sends the Erase EOF key. |
| `PressHome` | Sends the Home key. |
| `PressUp`, `PressDown`, `PressLeft`, `PressRight` | Sends cursor movement keys. |

## Example

```json
{
  "Host": "sampleapp:app1",
  "Port": 3270,
  "EveryStepDelay": {
    "Min": 0.5,
    "Max": 1.0
  },
  "Steps": [
    {
      "Type": "Connect"
    },
    {
      "Type": "FillString",
      "Coordinates": {
        "Row": 5,
        "Column": 21
      },
      "Text": "User"
    },
    {
      "Type": "PressEnter"
    },
    {
      "Type": "Disconnect"
    }
  ]
}
```
