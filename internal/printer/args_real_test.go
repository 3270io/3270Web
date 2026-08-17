// SPDX-License-Identifier: AGPL-3.0-or-later

package printer

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Everything else in this package tests the argument vector against what the
// documentation says pr3287 accepts. This tests it against pr3287.
//
// It is worth the trouble because the option names have moved: -codepage was
// -charset before x3270 4.0, and certificate verification changed which way
// its default points. A wrong option here is not a subtle bug — pr3287 refuses
// to start at all, and it refuses at the moment somebody first tries to bind a
// printer, which is the worst time to find out.
//
// Skipped where pr3287 is not installed, which is most development machines
// and is exactly the case the feature already reports as "not available".
func TestArgsAreUnderstoodByARealPr3287(t *testing.T) {
	path, err := exec.LookPath("pr3287")
	if err != nil {
		t.Skip("pr3287 is not installed; nothing to check the options against")
	}

	// Every option this package can emit, in one vector. Association and a
	// named LU are alternatives, so the named LU runs as a second case below.
	opts := Options{
		// Port 1 on the loopback: nothing listens there, so the run ends in a
		// refused connection rather than a live printer session. A refusal is
		// the *success* case here — it means the options were all understood
		// and pr3287 got as far as dialling.
		Host:         "127.0.0.1",
		Port:         1,
		Associate:    "TRM001",
		CodePage:     "cp037",
		MPP:          132,
		CRLF:         true,
		BlankLines:   true,
		FFSkip:       true,
		FFThru:       true,
		IgnoreEOJ:    true,
		SkipCC:       true,
		EOJTimeout:   30,
		TLS:          true,
		Verify:       true,
		SpoolCommand: "true",
	}

	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"an associated printer, verifying", opts},
		{"a named LU, not verifying", func() Options {
			o := opts
			o.Associate = ""
			o.LU = "PRT001"
			o.Verify = false
			return o
		}()},
		{"the plainest possible session", Options{
			Host: "127.0.0.1", Port: 1, LU: "PRT001", SpoolCommand: "true",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, err := tc.opts.Args()
			if err != nil {
				t.Fatalf("Args() = %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			out, _ := exec.CommandContext(ctx, path, args...).CombinedOutput()

			// The exit status is not the signal — a refused connection is a
			// failure to pr3287 and a pass to us. What matters is whether it
			// understood the arguments before it tried.
			said := string(out)
			for _, complaint := range []string{"Unknown option", "unknown option", "usage:", "Usage:"} {
				if strings.Contains(said, complaint) {
					t.Fatalf("pr3287 rejected our arguments %q:\n%s", args, said)
				}
			}
		})
	}
}
