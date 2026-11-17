// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import "testing"

// A host that drops the session is redialled by the terminal without a word,
// and the press that caused it comes back looking like a transition. The
// first-screen test cannot see it when the reconnect lands somewhere other
// than sign-on; the connect count can.
func TestAKeyThatDropsTheSessionIsBlockedNotLearned(t *testing.T) {
	app := newFakeApp()
	app.dropOn = "menu:PF(2)"
	cfg := fakeAppConfig(120)
	cfg.AIDKeyWeights = map[string]int{"Enter": 40, "PF(1)": 10, "PF(2)": 40, "PF(12)": 10}
	e, attempts := runFakeApp(t, app, cfg)

	drops := 0
	for _, p := range app.pressList() {
		if p == "menu:PF(2)" {
			drops++
		}
	}
	if drops == 0 {
		t.Skip("the run never pressed the dropping key; nothing to check")
	}
	if drops > 1 {
		t.Errorf("the key that dropped the session was pressed %d times on that screen", drops)
	}

	sawEnded := false
	for _, a := range attempts {
		if a.AIDKey == "PF(2)" && a.SessionEnded {
			sawEnded = true
			if a.Outcome != OutcomeSessionEnded {
				t.Errorf("outcome = %q, want %q", a.Outcome, OutcomeSessionEnded)
			}
		}
	}
	if !sawEnded {
		t.Fatal("no attempt was marked as ending the session")
	}
	menu := areaByLabelPrefix(t, e.Status().MindMap, "APP100")
	if !containsString(menu.AutoBlockedKeys, "PF(2)") {
		t.Errorf("menu AutoBlockedKeys = %v, want PF(2)", menu.AutoBlockedKeys)
	}
	if kp := menu.KeyPresses["PF(2)"]; kp != nil && kp.Progressions > 0 {
		t.Errorf("a session drop was counted as %d progression(s)", kp.Progressions)
	}
}
