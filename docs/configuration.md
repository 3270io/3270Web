# Configuration Reference

3270Web uses an XML configuration file located at `webapp/WEB-INF/3270Web-config.xml`.

If this file is missing, the application uses default values.

## Structure

The root element is `<config>`.

### Execution Path

Specifies the location of the `s3270` binary.

```xml
<exec-path>/usr/bin</exec-path>
```

### s3270 Options

Configures the underlying 3270 emulator process.

```xml
<s3270-options>
    <!-- Character set (default: bracket) -->
    <charset>bracket</charset>
    <!-- IBM Model number, e.g., 2, 3, 4, 5 (default: 3) -->
    <!-- Note: This may be overridden by S3270_MODEL in the .env file -->
    <!-- See terminal-model-limits.md for screen dimension details -->
    <model>3</model>
    <!-- Additional command-line arguments for s3270 -->
    <additional>-trace</additional>
</s3270-options>
```

### .env Overrides

At startup, 3270Web loads a `.env` file (created with defaults if missing) alongside the executable. Each variable maps to a specific `s3270` command-line option using the `S3270_<OPTION_NAME>` convention. Values in `.env` are applied after the XML config and do not overwrite existing environment variables set by the shell.

Example (overrides TLS and tracing):

```dotenv
S3270_NO_VERIFY_CERT=true
S3270_TRACE=true
S3270_TRACE_FILE=/tmp/s3270.trace
```

All supported options, defaults, and descriptions are listed in the generated `.env` file at the repo root. Update those values to change the arguments passed to `s3270`.

> **Note:** The generated `.env` file typically sets `S3270_MODEL=3279-4-E` by default. If this value is present, it will override the `<model>` setting in `3270Web-config.xml`. See [Terminal Model Dimensions](terminal-model-limits.md) for details on how screen sizes are enforced for each model.

#### Argument Parsing

For options that accept multiple arguments (like `S3270_SET`, `S3270_XRM`, or `S3270_EXEC_COMMAND`), values are parsed using shell-like quoting rules:

1.  **Splitting**: The value is split by spaces into individual arguments.
2.  **Quoting**: Use single (`'`) or double (`"`) quotes within the value to treat strings with spaces as a single argument.
3.  **Escaping**: Use backslash (`\`) to escape characters (e.g., `\"` for a literal quote).

**Examples:**

To pass `-set title "My Session"` (where "My Session" is a single argument):

```dotenv
# Option 1: Unquoted value in .env, using quotes for the argument
S3270_SET=title "My Session"

# Option 2: Quoted value in .env, using single quotes for the argument
S3270_SET="title 'My Session'"

# Option 3: Quoted value in .env, escaping inner double quotes
S3270_SET="title \"My Session\""
```

### Target Host

Sets the default host to connect to.

```xml
<!-- autoconnect="true" automatically connects on startup -->
<target-host autoconnect="true">localhost:3270</target-host>
```

### Fonts

Defines available fonts for the terminal display. The `default` attribute specifies which font is selected by default.

```xml
<fonts default="Terminus">
    <font name="Terminus" description="Terminus Font" />
    <font name="Courier" description="Courier New" />
</fonts>
```

### Color Schemes

Defines visual themes. Attributes control colors for different field types using two-letter codes:

*   **P**: Protected (static text)
*   **U**: Unprotected (input fields)
*   **N**: Normal intensity
*   **I**: Intensified (bright/bold)
*   **H**: Hidden (no display)

Combined with **Fg** (Foreground) and **Bg** (Background).

Example: `pnfg` = **P**rotected **N**ormal **F**ore**g**round.

```xml
<colorschemes default="Green">
    <scheme name="Green"
            pnfg="green" pnbg="black"
            pifg="lime" pibg="black"
            phfg="black" phbg="black"
            unfg="lime" unbg="black"
            uifg="white" uibg="black"
            uhfg="black" uhbg="black" />
</colorschemes>
```

## Security Configuration

### Log Access

By default, access to the application logs via the web interface is disabled for security reasons.

To enable the `/logs` endpoints (view, toggle, clear, download), you must set the `ALLOW_LOG_ACCESS` environment variable:

```bash
ALLOW_LOG_ACCESS=true
```

When enabled, logs are accessible at `/logs` and `/logs/download`.

## Complete Example

```xml
<?xml version="1.0" encoding="UTF-8"?>
<config>
    <exec-path>/usr/local/bin</exec-path>

    <s3270-options>
        <model>4</model>
        <charset>bracket</charset>
    </s3270-options>

    <target-host autoconnect="false">mainframe.example.com:23</target-host>

    <fonts default="Monospace">
        <font name="Monospace" description="Standard Monospace" />
    </fonts>

    <colorschemes default="Classic">
        <scheme name="Classic"
                pnfg="#00ff00" pnbg="#000000"
                pifg="#ffffff" pibg="#000000"
                phfg="#000000" phbg="#000000"
                unfg="#00ff00" unbg="#000000"
                uifg="#ffffff" uibg="#000000"
                uhfg="#000000" uhbg="#000000" />
    </colorschemes>
</config>
```
