# IBM 3270 Terminal Model Dimension Limits

## Overview

IBM 3270 terminals come in different models, each with specific screen dimensions. This document describes how 3270Web enforces these standard dimensions to ensure proper terminal emulation.

## Model Specifications

| Model | Rows | Columns | Description |
|-------|------|---------|-------------|
| 2     | 24   | 80      | Standard terminal (default ISPF/TSO size) |
| 3     | 32   | 80      | Extended rows |
| 4     | 43   | 80      | Large screen |
| 5     | 27   | 132     | Wide screen |

## Implementation

The dimension limits are enforced in `internal/host/screen_update.go`:

1. **Model Detection**: The terminal model is extracted from the s3270 status line (field index 5, which is the 6th field)
2. **Dimension Extraction**: Reported dimensions are extracted from the status line (field indices 6 and 7)
3. **Validation**: If reported dimensions exceed the model's standard limits, they are clamped to the maximum allowed
4. **Rendering**: The validated dimensions are used for screen rendering

## Example

For Model 2, if s3270 reports dimensions of 30x90, they will be automatically limited to 24x80:

```
Status: "U F P C(localhost) I 2 30 90 0 0 0x0 0.000"
                              ^ ^  ^
                              | |  |
                        Model | |  |
                              2 30 90 (reported)

Result: 24x80 (enforced to Model 2 limits)
```

## Configuration

The terminal model is configured via the `.env` file:

```bash
S3270_MODEL=2        # For model 2 (24x80)
S3270_MODEL=3279-2   # Alternative format for model 2
S3270_MODEL=3279-4-E # Model 4 extended (43x80)
```

Or in the XML configuration:

```xml
<s3270-options>
    <model>2</model>
</s3270-options>
```

## Testing

Test coverage is provided in `internal/host/screen_update_test.go`:

- `TestGetModelDimensions`: Verifies model-to-dimension mapping
- `TestScreenDimensionsFromStatusEnforcesLimits`: Validates dimension enforcement

## References

- [IBM 3270 Terminal Specifications](https://en.wikipedia.org/wiki/IBM_3270)
- [s3270 Documentation](http://x3270.bgp.nu/Unix/s3270-man.html)
