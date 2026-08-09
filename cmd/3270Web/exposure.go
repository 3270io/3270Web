package main

import (
	"log"
	"net"
	"strings"

	"github.com/jnnngs/3270Web/internal/authz"
)

// warnIfUnauthenticatedAndExposed says so, loudly, when this instance is
// listening somewhere other people can reach it and has no sign-in.
//
// The two settings are individually reasonable and only dangerous together.
// AUTH_MODE=none is the historical default and the right one for a laptop:
// there is one person, and asking them to invent a password to reach their own
// terminal would be theatre. Listening on every interface is what a container
// has to do for a published port to work at all — a loopback-only listener
// inside a container is unreachable however the ports are mapped.
//
// Put together they are an unauthenticated administrator: with no
// authentication every request resolves to authz.Local(), which holds the
// admin role, so whoever reaches the port can open terminal sessions, read the
// settings and the log, and restart the service. Nothing about the running
// system looks different, which is why this is worth a paragraph in the log
// rather than a line.
//
// It warns rather than refuses. An operator who has put this behind an
// authenticating proxy, or on a network where that is genuinely the intent,
// has made a decision this process cannot see, and refusing to start would
// break their deployment on an upgrade to make a point. The shipped Docker
// Compose file turns authentication on instead, which is where the default
// that mattered actually lives.
func (app *App) warnIfUnauthenticatedAndExposed(addr string) {
	if app.authMode != authz.ModeNone || !isExposedListenAddr(addr) {
		return
	}
	log.Printf("SECURITY: 3270Web is listening on %s with %s unset, so it is "+
		"reachable from the network with no sign-in.", addr, authz.ModeEnv)
	log.Printf("SECURITY: every request then arrives as the local operator, " +
		"which carries the administrator role — terminal sessions, settings, " +
		"the audit log and restart, without a password.")
	log.Printf("SECURITY: set %s=local (the first start prints a setup code "+
		"for creating the first administrator), or bind the listener to "+
		"127.0.0.1 with WEBUI_BIND. See https://3270Web.3270.io/multi-user/", authz.ModeEnv)
}

// isExposedListenAddr reports whether a listen address can be reached from
// somewhere other than this machine.
//
// The unspecified address ("0.0.0.0", "::", and the empty host net.Listen
// accepts) is every interface, which is the case that matters. A specific
// non-loopback address is reachable on that interface. Loopback is not.
func isExposedListenAddr(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		host = strings.TrimSpace(addr)
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A name rather than an address. "localhost" is the one worth
		// recognising; anything else this process cannot classify is assumed
		// reachable, because a warning nobody needed is cheaper than a silence
		// somebody did.
		return !strings.EqualFold(host, "localhost")
	}
	return !ip.IsLoopback()
}
