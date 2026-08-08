package main

import (
	"errors"
	"fmt"
	"os"

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
