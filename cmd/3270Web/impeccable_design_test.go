// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"strings"
	"testing"
)

// Design-judgement fixes from IMPECCABLE_AUDIT.md. These pin decisions that
// are easy to undo silently.

// Finding 3 follow-up: promoting the dialog titles to h2 left the nested
// confirmation headings at h4, which skipped a level from the h2 above them.
func TestConnectConfirmHeadingsDoNotSkipALevel(t *testing.T) {
	src := templateSource(t, "connect.html")
	for _, id := range []string{"settings-restart-title", "settings-discard-title"} {
		if !strings.Contains(src, `<h3 id="`+id+`"`) {
			t.Errorf("connect.html: %s is not an h3 under the settings h2 (finding 3)", id)
		}
		if strings.Contains(src, `<h4 id="`+id+`"`) {
			t.Errorf("connect.html: %s is still an h4, skipping a heading level (finding 3)", id)
		}
	}
}
