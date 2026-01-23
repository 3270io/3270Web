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
    <model>3</model>
    <!-- Additional command-line arguments for s3270 -->
    <additional>-trace</additional>
</s3270-options>
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
