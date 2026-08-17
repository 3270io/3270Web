// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/jnnngs/3270Web/internal/printer"
)

// The spool helper: `3270Web print-spool -dir <dir>`.
//
// pr3287 hands each finished print job to a command with the job's bytes on
// standard input. The default is `lpr`, which is the right answer for a
// terminal sitting next to a printer and the wrong one for a terminal served
// to a browser tab: there is no printer on the server, and the person who
// wants the output is somewhere else.
//
// So the command is this program, invoked again in a mode that does one thing:
// read the job, write it into the session's spool directory under a name that
// records when it arrived. Running ourselves rather than a shell one-liner is
// what makes the feature work the same way on every platform without shipping
// a script, depending on `mktemp` being installed, or building a filename in a
// shell — the last of which is the one that would eventually be a bug worth
// a CVE number.
//
// Nothing here is reachable over HTTP. It is a subcommand, chosen before the
// server starts, with the spool directory named by the server that launched it.

// runPrintSpool is the subcommand's body. It returns the process exit code;
// pr3287 reports a non-zero one, which is how a job that could not be stored
// becomes visible rather than silently vanishing.
func runPrintSpool(args []string, stdin io.Reader, stderr io.Writer) int {
	fs := flag.NewFlagSet("print-spool", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "directory to write the print job into")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dir == "" {
		fmt.Fprintln(stderr, "print-spool: -dir is required")
		return 2
	}

	job, err := printer.Spool{Dir: *dir}.Receive(stdin, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "print-spool: %v\n", err)
		// A job that was stored and then failed to prune is still a job the
		// operator has. Saying so and exiting cleanly is more honest than
		// reporting a print failure that did not happen.
		if job.Name == "" {
			return 1
		}
	}
	if job.Truncated {
		fmt.Fprintf(stderr, "print-spool: job %s exceeded %d bytes and was truncated\n",
			job.Name, printer.MaxJobBytes)
	}
	return 0
}
