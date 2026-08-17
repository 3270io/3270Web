// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/egress"
	"github.com/jnnngs/3270Web/internal/hostpolicy"
)

// errHostNotAllowed marks a refusal by policy rather than a failure to
// connect. The distinction reaches the caller as a status code: a dial that
// failed is worth retrying and answers 502, while a host this instance will
// never dial is 403 and retrying it is pointless.
var errHostNotAllowed = errors.New("host not allowed")

// allowedHostsEnv names the deployment-wide allowlist.
const allowedHostsEnv = "ALLOWED_HOSTS"

// checkHostAllowed reports whether this deployment may dial hostname.
//
// isValidHostname already refuses malformed targets and the addresses no
// server should dial on a caller's behalf — loopback, link-local, the
// unspecified address. This is the question after that one: a valid, routable
// host is still not necessarily one this instance should reach.
//
// It matters because a 3270 terminal is a client for arbitrary TCP. On a
// shared instance, anybody who can open a session can point it at anything the
// container can reach, which makes the terminal a route into the network it
// sits in. An allowlist is what turns "reachable from the container" into
// "reachable on purpose".
//
// Unset means unrestricted, which is the historical behaviour and the right
// default for a laptop or a lab.
func checkHostAllowed(hostname string) error {
	patterns := os.Getenv(allowedHostsEnv)
	if patterns == "" {
		return nil
	}
	// The bundled sample apps are this process talking to itself on loopback,
	// and they are already behind ALLOW_SAMPLE_APPS. Requiring them in the
	// allowlist as well would mean an operator who fenced their instance to
	// the production LPAR could no longer run the demo, for no gain: the
	// listener is one 3270Web started, not somewhere on the network.
	if isSampleAppHostname(hostname) {
		return nil
	}
	if hostpolicy.Allowed(patterns, hostname) {
		return nil
	}
	// The message names the setting rather than listing what is permitted:
	// the list itself describes the network's shape, and a caller who cannot
	// already read the configuration does not need it.
	return refuse(errHostNotAllowed, fmt.Sprintf(
		"this 3270Web instance is not permitted to connect to %q (see %s)",
		hostpolicy.Hostname(hostname), allowedHostsEnv))
}

// requestContext is the request's context, or a background one for the
// callers that have no request — which in practice means tests.
func requestContext(c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	return c.Request.Context()
}

// checkHostResolves refuses a target whose name resolves somewhere no server
// should dial on a caller's behalf.
//
// isValidHostname already refuses those addresses, but only when they are
// typed as literals — that is all a syntax check can see. "localhost" is not
// an IP literal and neither is a name in a zone the caller controls, so both
// walk straight past it and s3270 resolves them for real a moment later. The
// terminal is a client for arbitrary TCP; pointed at loopback it reaches
// whatever else is listening on this machine, and pointed at 169.254.169.254
// it reaches the instance-metadata service and the credentials in it.
//
// This asks the question the literal check cannot: what does the name actually
// come back as. Every answer is tested, because a name that resolves to one
// routable address and one forbidden one can hand out either.
//
// A lookup that fails is not a refusal. s3270 is about to try the same name
// and produce its own, more accurate message about why it could not connect,
// and turning "DNS is briefly unavailable" into "this host is forbidden" would
// be both wrong and confusing. The gap that leaves — a name this process
// cannot resolve but s3270 can — is logged rather than guessed at.
func checkHostResolves(ctx context.Context, hostname string) error {
	// The bundled sample apps are this process talking to itself on loopback.
	// They never went through the literal check either; see isValidHostname,
	// which short-circuits the "sampleapp:" pseudo-hostname before it.
	if isSampleAppHostname(hostname) {
		return nil
	}
	host := hostpolicy.Hostname(hostname)
	if host == "" {
		return nil
	}
	// Loopback is refused here and permitted for AI providers, which is not an
	// inconsistency: a model on this machine is a thing an operator asks for by
	// name, and a mainframe on this machine is not a thing that exists. The
	// literal check in isValidHostname draws the same line.
	policy := egress.Default()
	policy.DenyLoopback = true
	if err := policy.CheckHost(ctx, host); err != nil {
		var blocked *egress.ErrBlocked
		if errors.As(err, &blocked) {
			return refuse(errHostNotAllowed, fmt.Sprintf(
				"this 3270Web instance will not connect to %q: %s", host, blocked.Reason))
		}
		log.Printf("connect: could not check where %q resolves: %v", host, err)
	}
	return nil
}
