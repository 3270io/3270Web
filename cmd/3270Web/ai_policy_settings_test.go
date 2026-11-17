// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"testing"

	"github.com/jnnngs/3270Web/internal/aipolicy"
)

// The AI policy switches are the administrator's. If the settings API could
// write them, a signed-in user could permit themselves an endpoint on the
// server's own network — which is the thing the switches exist to prevent.
func TestAIPolicySwitchesCannotBeWrittenThroughTheSettingsAPI(t *testing.T) {
	allowed := settingsWritableKeys()
	for _, key := range []string{
		aipolicy.EnvDisabled,
		aipolicy.EnvAllowedProviders,
		aipolicy.EnvAllowedEndpointHosts,
		aipolicy.EnvAllowPrivateEndpoints,
	} {
		if settingsKeyWritable(key, allowed) {
			t.Errorf("%s can be written through the settings API", key)
		}
		if settingsKeyWritable(settingsExtraOptionsPrefix+key, allowed) {
			t.Errorf("%s can be written through the settings API by way of the options prefix", key)
		}
	}
}
