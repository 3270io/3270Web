// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import "testing"

// A well-formed s3270 status line, as documented in s3270_helpers.go:
// keyboard formatting protection connection mode model rows cols row col wid time
func statusLine(keyboard, connection string) string {
	return keyboard + " F P " + connection + " I 4 24 80 5 12 0x0 0.010"
}

func TestOIAIndicator(t *testing.T) {
	tests := []struct {
		name          string
		keyboard      string
		wantIndicator string
	}{
		{"unlocked keyboard is ready", "U", OIAReady},
		{"locked keyboard shows system wait", "L", OIASystemWait},
		{"error state shows operator error", "E", OIAOperatorErr},
		{"unknown state inhibits rather than claiming ready", "Z", OIASystemWait},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Screen{Status: statusLine(tt.keyboard, "C(mainframe)")}
			got, explanation := s.OIAIndicator()
			if got != tt.wantIndicator {
				t.Errorf("OIAIndicator() = %q, want %q", got, tt.wantIndicator)
			}
			if explanation == "" {
				t.Error("OIAIndicator() returned no explanation; screen readers and tooltips rely on it")
			}
		})
	}
}

func TestOIAIndicatorWithoutStatusLine(t *testing.T) {
	s := &Screen{}
	got, _ := s.OIAIndicator()
	if got != OIAReady {
		t.Errorf("OIAIndicator() with no status = %q, want %q", got, OIAReady)
	}
}

func TestStatusConnected(t *testing.T) {
	tests := []struct {
		name          string
		connection    string
		wantConnected bool
		wantPeer      string
	}{
		{"connected reports the peer", "C(mainframe.example.com)", true, "mainframe.example.com"},
		{"not connected", "N", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Screen{Status: statusLine("U", tt.connection)}
			connected, peer, ok := s.StatusConnected()
			if !ok {
				t.Fatal("StatusConnected() reported no usable status line")
			}
			if connected != tt.wantConnected {
				t.Errorf("connected = %v, want %v", connected, tt.wantConnected)
			}
			if peer != tt.wantPeer {
				t.Errorf("peer = %q, want %q", peer, tt.wantPeer)
			}
		})
	}
}

// A missing status line must be distinguishable from a reported
// disconnection: the client triggers reconnection on the latter, and doing
// so on the former would fight with a session that is merely still starting.
func TestStatusConnectedWithoutStatusLine(t *testing.T) {
	s := &Screen{}
	connected, _, ok := s.StatusConnected()
	if ok {
		t.Error("StatusConnected() claimed a usable status line where there is none")
	}
	if connected {
		t.Error("StatusConnected() reported connected with no status line")
	}
}
