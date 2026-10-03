// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"net/url"
	"os"
	"strings"
)

// The operator may link their helpdesk; never accept executable URL schemes.
func accessHelpURL() string {
	raw := strings.TrimSpace(os.Getenv("AUTH_HELP_URL"))
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return ""
	}
	if u.Scheme == "https" && u.Host != "" {
		return raw
	}
	if u.Scheme == "mailto" && u.Opaque != "" {
		return raw
	}
	return ""
}
