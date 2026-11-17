// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
)

// A host that hangs up is not a server error.
//
// s3270 stays running when its host goes away: the subprocess and its pipe are
// fine, it answers every request, and what it answers is a refusal. So the
// first the web page knew of a dead host was a 500 on the next key, then a
// full-page "Something went wrong" — a crash report for what is an ordinary
// event, on a page that offered nothing to do about it.
//
// The pieces below let every request that meets the drop say so in one voice:
// a 409 with a code the page already has recovery for (session-expiry.js), and
// for a request that wants a page rather than JSON, a page that names what
// happened.

// errHostDisconnected marks a failure whose cause is the host having hung up.
var errHostDisconnected = errors.New("the connection to the host was lost")

// hostLostCode is the machine-readable code the browser's reconnect handling
// keys on. It travels with HTTP 409, the status the REST API already uses for
// "the session is not connected to a host".
const hostLostCode = "host_disconnected"

// hostLostMessage is what is said to the person, in the JSON error and in the
// toast the page raises from it.
const hostLostMessage = "The connection to the host was lost. What you typed has not been sent."

// lostHostOr returns err unchanged unless it means the host is gone, in which
// case it is wrapped so that errors.Is(err, errHostDisconnected) holds.
//
// A host that is not running at all counts too, whatever it said on the way
// out: there is nothing left to send a key to.
func lostHostOr(h host.Host, err error) error {
	if err == nil {
		return nil
	}
	if host.IsDisconnected(err) || (h != nil && !h.IsConnected()) {
		return fmt.Errorf("%w: %v", errHostDisconnected, err)
	}
	return err
}

// respondHostLostJSON answers a script-driven request that met a dead host.
func respondHostLostJSON(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{"error": hostLostMessage, "code": hostLostCode})
}

// renderHostLostPage answers a page request that met a dead host.
func renderHostLostPage(c *gin.Context, cause error) {
	detail := ""
	if cause != nil {
		detail = cause.Error()
	}
	c.HTML(http.StatusConflict, "error.html", gin.H{
		"Title":   "The connection to the host was lost",
		"Message": "The host closed the connection, so the key you pressed was not sent. Return to the terminal to reconnect, or choose a connection.",
		"Error":   detail,
	})
}

// markOIADisconnected overwrites the Operator Information Area fields of a
// page rendered from the last screen a dead host sent. That screen's status
// line still says "connected" — it is the last thing the host said — so the
// page would otherwise open on "Ready for input" over a connection that is gone.
func markOIADisconnected(oia gin.H) gin.H {
	oia["oiaOnline"] = "–"
	oia["oiaConnected"] = false
	oia["oiaIndicator"] = "X DISCONNECTED"
	oia["oiaExplanation"] = "Connection to the host was lost"
	return oia
}
