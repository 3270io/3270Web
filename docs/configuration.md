# Connect and Use 3270Web

This page explains connection setup and the full Settings modal from a user perspective.

## Connect to a Host

You can connect to:

- `hostname:port` (example: `mainframe.example.com:23`)
- `IPv4:port` (example: `10.0.0.5:23`)
- `IPv6:port` (example: `[::1]:23`)
- `sampleapp:<id>` for bundled sample targets

If autoconnect is enabled, 3270Web will connect automatically on startup.

## Open Settings

1. Click the Settings icon in the toolbar.
2. Use tabs to switch sections.
3. Edit values.
4. Click **Save settings**.
5. If prompted, restart 3270Web to apply startup-level changes.

You can also use:

- **Refresh** to reload current values
- **Reset to defaults** inside each settings section
- **Maximize** for easier editing of long values

## Toolbar Callouts

![Toolbar screenshot](images/toolbar-real.png){: .doc-medal }
{: .doc-medal-wrap }

1. Disconnect
2. Logs
3. Settings
4. Start recording
5. View recording (when a recording is loaded)

## Settings Modal Callouts

![Settings modal screenshot](images/settings-modal-real.png){: .doc-medal }
{: .doc-medal-wrap }

1. Refresh values
2. Maximize/restore modal
3. Close settings
4. Section tabs
5. Active section content
6. Reset section to defaults
7. Save settings

## Settings Sections (Full)

### Connectivity

Controls how 3270Web reaches the host.

Includes:

- Port, connect timeout
- IPv4/IPv6 preference
- Proxy settings
- callback/script port and socket options

Use this section when you need alternate network routing or script protocol listeners.

### TLS/Security

Controls TLS certificate and protocol behavior.

Includes:

- Certificate verification toggle
- Min/max TLS protocol
- Client certificate and key files
- CA file/directory/chain
- Accepted hostname and client certificate name

Use this when connecting to secured hosts with custom certificate requirements.

### Emulation

Controls terminal identity and data representation.

Includes:

- Terminal model (`3278`/`3279` variants)
- Host code page
- Terminal/device/user identity options
- NVT mode and oversize behavior

This section directly affects screen size, field positions, and recording reliability.

### Automation/Startup

Controls startup and command automation behavior.

Includes:

- Exec command override
- Login macro
- HTTPD binding
- Minimum required s3270 version

Use this for scripted startup flows and custom launch behavior.

### Diagnostics

Controls trace and diagnostic output.

Includes:

- Trace enable/disable
- Trace file and max size
- Help/version/unit-test environment flags

Use this section when troubleshooting host interaction issues.

### App

Controls application-level UI features.

Includes:

- `Allow log access`
- `Use keypad` (show virtual keypad by default)
- **Terminal Font** dropdown — pick from the three bundled IBM 3270-style fonts (Regular, Semi-Condensed, Condensed). See [Terminal Fonts](terminal-fonts.md) for usage notes.

Use this section to control log visibility and default keyboard UI behavior.

### Chaos

Controls chaos exploration defaults.

Includes:

- `CHAOS_MAX_STEPS`
- `CHAOS_TIME_BUDGET_SEC`
- `CHAOS_STEP_DELAY_SEC`
- `CHAOS_SEED`
- `CHAOS_MAX_FIELD_LENGTH`
- `CHAOS_LEARNED_INPUT_REUSE_BIAS` (default `1.0`) — weight applied to known-good input values when generating new field writes.
- `CHAOS_LEARNED_KEY_REUSE_BIAS` (default `1.0`) — how often the engine retries AID keys that have previously caused a transition versus exploring untried keys.
- `CHAOS_EXPORT_SUCCESS_BALANCE` (default `1.0`) — when exporting the chaos workflow JSON, balances steps drawn from successful transitions against exploratory steps.
- `CHAOS_OUTPUT_FILE`
- `CHAOS_EXCLUDE_NO_PROGRESS_EVENTS`

Use this section to tune how aggressively chaos mode explores screens and where optional output should be written. See [Chaos Mode](chaos-mode.md) for a deeper walkthrough of the bias settings.

## UI Conveniences

A few quality-of-life behaviours worth knowing about:

- **Collapsible toolbar sections** — the Recording and Chaos toolbar
  groups collapse with a chevron and remember their state, so a busy
  toolbar can be trimmed to just the controls you care about.
- **Required hostname input** — the connect form marks the hostname
  field as required and announces missing-value errors to assistive
  tech, so it is harder to accidentally submit an empty form.
- **Saved host deletion confirmation** — removing a saved host profile
  pops a confirmation modal; the destructive action cannot fire on a
  single mis-click.
- **Destructive disconnect styling** — the Disconnect toolbar button is
  visually tagged as destructive (warning tint at rest) so it is
  distinct from the safer navigation buttons next to it.
- **Toast notifications** — short-lived theme-aware toasts surface the
  result of background actions (save, export, error) without taking
  focus.

## Log Access

If log access is enabled in settings, you can open the Logs modal from the toolbar and:

- Turn verbose logging on/off
- Refresh logs
- Copy/download logs
- Clear logs

## Best Practices

- Keep one known-good model/code page profile per host environment.
- Apply TLS changes carefully and verify certificate paths.
- Prefer debug playback for new recordings before running full play mode.
