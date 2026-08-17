// SPDX-License-Identifier: AGPL-3.0-or-later

package copilot

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

// TestCopyWithIdleTimeoutStalls verifies that a stream which goes quiet past the
// idle window is aborted with errStreamStalled (instead of hanging) once onIdle
// unblocks the pending read.
func TestCopyWithIdleTimeoutStalls(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	var out bytes.Buffer
	start := time.Now()
	err := copyWithIdleTimeout(&out, pr, 20*time.Millisecond, func() { _ = pr.Close() })

	if !errors.Is(err, errStreamStalled) {
		t.Fatalf("err = %v, want errStreamStalled", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("stall detection took %v; expected ~idle window", elapsed)
	}
}

// TestCopyWithIdleTimeoutPassesThrough verifies that a steadily-streaming source
// (each chunk within the idle window, resetting the deadline) is copied in full
// and completes normally.
func TestCopyWithIdleTimeoutPassesThrough(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		for i := 0; i < 5; i++ {
			_, _ = pw.Write([]byte("data: chunk\n\n"))
			time.Sleep(5 * time.Millisecond)
		}
		_ = pw.Close()
	}()

	var out bytes.Buffer
	err := copyWithIdleTimeout(&out, pr, 80*time.Millisecond, func() { _ = pr.Close() })
	if err != nil {
		t.Fatalf("copyWithIdleTimeout err = %v, want nil", err)
	}
	if out.Len() == 0 {
		t.Fatalf("no data copied through")
	}
}
