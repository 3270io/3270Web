// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"

	"github.com/gin-gonic/gin"
)

// connectSessionLimitKey carries "this connect failure was the session cap"
// from the handler that saw the error to the page that reports it, the same
// way connectionDetails does. renderConnectPage takes the message as a string,
// so the kind of failure would otherwise be lost on the way.
const connectSessionLimitKey = "connectSessionLimit"

// flagSessionLimit records that a failed connect was refused for how many
// sessions are already open, so the connect page can title the panel for what
// happened and offer the sessions holding the slots.
func flagSessionLimit(c *gin.Context, err error) {
	if errors.Is(err, errSessionLimit) {
		c.Set(connectSessionLimitKey, true)
	}
}

// connectSessionLimit reports whether the failure being rendered was the cap.
func connectSessionLimit(c *gin.Context) bool {
	return c.GetBool(connectSessionLimitKey)
}
