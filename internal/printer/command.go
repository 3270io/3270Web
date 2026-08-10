package printer

import (
	"fmt"
	"runtime"
	"strings"
)

// pr3287 delivers a job by handing it to a *shell* command, not to an argument
// vector: the string given to -command is passed to `sh -c` (or to the command
// interpreter on Windows). So the one place in this feature where quoting has
// to be right is here.
//
// What is quoted is never caller input. The executable is this program's own
// path and the directory is the one the server chose for the session, so the
// realistic failure is a space in "C:\Program Files\3270Web" rather than an
// attempt to inject a second command. It is still built rather than
// interpolated, and the characters that could end the quoting are refused
// outright, because "the input is trusted" is a claim that has to keep being
// true as the code around it changes.

// SpoolSubcommand is the argument that puts this program into spool-helper
// mode: read one print job on standard input, write it into the directory
// named by -dir. It exists so that a browser-delivered terminal has somewhere
// to put a print job without a shell script, a temp-file convention, or a
// dependency on what happens to be installed beside it.
const SpoolSubcommand = "print-spool"

// SpoolCommandFor builds the -command string pr3287 runs for each job.
func SpoolCommandFor(exePath, spoolDir string) (string, error) {
	return spoolCommand(exePath, spoolDir, runtime.GOOS == "windows")
}

func spoolCommand(exePath, spoolDir string, windows bool) (string, error) {
	exePath = strings.TrimSpace(exePath)
	spoolDir = strings.TrimSpace(spoolDir)
	if exePath == "" {
		return "", fmt.Errorf("the program's own path is not known, so print jobs have nowhere to go")
	}
	if spoolDir == "" {
		return "", fmt.Errorf("no spool directory was given")
	}
	quoted := make([]string, 0, 2)
	for _, part := range []string{exePath, spoolDir} {
		q, err := shellQuote(part, windows)
		if err != nil {
			return "", err
		}
		quoted = append(quoted, q)
	}
	return fmt.Sprintf("%s %s -dir %s", quoted[0], SpoolSubcommand, quoted[1]), nil
}

// shellQuote wraps one path so the shell sees it as a single word.
func shellQuote(value string, windows bool) (string, error) {
	// Rejected on both platforms: a newline ends the command whatever the
	// quoting, and a NUL cannot survive the trip at all.
	if strings.ContainsAny(value, "\n\r\x00") {
		return "", fmt.Errorf("path %q contains characters that cannot be used in a print command", value)
	}
	if windows {
		// cmd.exe has no escape inside double quotes, and expands %VAR%
		// even within them, so both characters are refused rather than
		// quoted around.
		if strings.ContainsAny(value, `"%`) {
			return "", fmt.Errorf("path %q contains characters that cannot be used in a print command", value)
		}
		return `"` + value + `"`, nil
	}
	// A single-quoted string in sh ends only at the next single quote, so
	// the one character to handle is that quote itself.
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'", nil
}
