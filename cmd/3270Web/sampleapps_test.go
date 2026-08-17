// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"testing"

	"github.com/jnnngs/3270Web/internal/sampleapps"
)

// The names the picker offers and the handlers that serve them live in
// different packages, and nothing but this connects the two. An application
// added to one list and not the other would otherwise fail only when somebody
// chose it from the connect page.
func TestEverySampleAppInThePickerStarts(t *testing.T) {
	for _, option := range availableSampleApps() {
		// Port 0: these tests run listeners of their own, and the sample app
		// ports are a small fixed set shared with anything else on the
		// machine.
		server, err := sampleapps.StartServerOn(option.ID, "127.0.0.1:0")
		if err != nil {
			t.Errorf("%s (%s) will not start: %v", option.Name, option.ID, err)
			continue
		}
		server.Stop()

		if !isValidHostname(option.Hostname) {
			t.Errorf("%s is not a valid hostname", option.Hostname)
		}
		if err := checkHostAllowed(option.Hostname); err != nil {
			t.Errorf("checkHostAllowed(%q) = %v", option.Hostname, err)
		}
	}

	if len(allowedSampleAppPortsList) < len(sampleAppConfigs) {
		t.Errorf("%d sample apps share %d ports, so they cannot all be running at once",
			len(sampleAppConfigs), len(allowedSampleAppPortsList))
	}
}
