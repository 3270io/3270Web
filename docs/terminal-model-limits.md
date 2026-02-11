# Screen Size and Model Guide

3270 screen size depends on the terminal model in use.

## Model Sizes

| Model | Rows | Columns | Typical use |
|---|---:|---:|---|
| 2 | 24 | 80 | Standard 3270 screens |
| 3 | 32 | 80 | Extra rows |
| 4 | 43 | 80 | Large-screen workflows |
| 5 | 27 | 132 | Wide-screen workflows |

## Why This Matters

Your model affects:

- How much of a screen is visible
- Cursor coordinates used in recordings
- Compatibility with host applications that expect a specific size

If a host app expects 24x80, use Model 2 unless instructed otherwise.

## Choosing a Model

Set your model in configuration or `.env`.

Example:

```dotenv
S3270_MODEL=2
```

Alternative values are also accepted (for example `3279-4-E`).

## Practical Guidance

- When recordings fail at `FillString` coordinates, confirm the same model is active.
- Keep the same model across environments (dev/test/prod) for reliable playback.
- If text alignment looks wrong, check both model and code page settings.
