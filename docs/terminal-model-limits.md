---
seo_title: "3270 screen sizes and terminal models 2, 3, 4 and 5"
description: >-
  3270 screen sizes model by model — rows, columns and where each one is
  typically used — plus how to choose between them, including for AS/400 and
  IBM i hosts.
---

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

## AS/400 and IBM i Hosts

These hosts drive their terminals with 5250, not 3270, and 3270Web does not
speak 5250 — neither does the `s3270` it runs. What makes them reachable is a
front end on the host that translates between the two protocols. It works, and
it comes with two conditions worth knowing before the first connection rather
than after it.

**Use model 2.** An AS/400 or IBM i host does not natively understand any 3270
model other than model 2, at 24x80. This is not the usual "unless the
application wants otherwise" advice that applies elsewhere on this page: any
other model is simply the wrong answer against these hosts.

```dotenv
S3270_MODEL=2
```

**Expect 5250 function keys to arrive as PF-key sequences.** The front end
expresses 5250-specific operations through 3270 keys, so a key the host
documentation names once may be two keystrokes here. 5250 Clear is PF3, and
5250 F3 is PA1 followed by PF3. Recordings store the 3270 keys actually sent —
that F3 is saved as `PressPA1` then `PressPF3` — so a session captured against
one of these hosts replays without any 5250 knowledge on 3270Web's part.

Both conditions are properties of the host's front end rather than of 3270Web,
and they are documented upstream by the x3270 project whose `s3270` 3270Web
drives: [5250 support](https://x3270.miraheze.org/wiki/5250_support). Native
5250 sits on the [feature roadmap](feature-roadmap.md#category-parity).

## Practical Guidance

- When recordings fail at `FillString` coordinates, confirm the same model is active.
- Keep the same model across environments (dev/test/prod) for reliable playback.
- If text alignment looks wrong, check both model and code page settings.
- Against an AS/400 or IBM i host, use model 2 — see above.
