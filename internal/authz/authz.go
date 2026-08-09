// Package authz models who is making a request and what they are allowed to
// touch. It is deliberately free of HTTP and storage dependencies so the
// rules can be read and tested on their own.
//
// The central idea is that there is always a principal. 3270Web has one
// deployment shape today — a single operator, no login — and will grow
// another where users authenticate. Rather than branching between "no auth"
// and "auth" at every decision point, the single-operator shape is expressed
// as a principal too (see Local). Authorization logic then runs identically in
// both shapes, which means the default configuration exercises the same code
// the multi-user configuration will depend on, instead of leaving that code
// untested until someone turns it on.
package authz

import (
	"fmt"
	"strings"
)

// LocalUserID owns everything when no authentication is configured.
//
// It is a reserved name rather than an empty string so that "owned by the
// single local operator" and "owner unknown" stay distinguishable — the
// difference matters once ownership is enforced, where unknown must not be
// treated as a match.
const LocalUserID = "local"

// Role is a coarse permission tier. Two are enough: the split that matters is
// between acting on your own sessions and acting on the whole instance
// (settings, logs, restart, user administration).
type Role string

const (
	// RoleUser may drive its own sessions and nothing else.
	RoleUser Role = "user"
	// RoleAdmin may additionally change instance-wide state.
	RoleAdmin Role = "admin"
)

// Kind records how the caller proved who they are, which matters for auditing
// and for deciding what a credential may do — an API token is issued for a
// purpose and can be scoped, a browser session is not.
type Kind string

const (
	// KindWeb is a browser using a session cookie.
	KindWeb Kind = "web"
	// KindAPIToken is a bearer token on the REST or MCP surface.
	KindAPIToken Kind = "api-token"
)

// Principal is the actor behind a request.
//
// The zero value is the anonymous principal: it owns nothing, is not an admin
// and holds no scopes, so every predicate below answers no. That makes
// "authorization was never resolved" fail closed by construction rather than
// by remembering to check.
type Principal struct {
	// UserID identifies the account. Empty means anonymous.
	UserID string
	Role   Role
	Kind   Kind
	// Scopes narrows what an API token may do. Empty means unrestricted
	// within the principal's role, which is how browser sessions behave.
	Scopes []string
}

// Anonymous returns the principal for a request that carries no identity.
func Anonymous() Principal { return Principal{} }

// Local returns the principal used when AUTH_MODE is none: a single operator
// with full rights over an instance that has no other users.
func Local() Principal {
	return Principal{
		UserID: LocalUserID,
		Role:   RoleAdmin,
		Kind:   KindWeb,
	}
}

// IsAnonymous reports whether the principal has no identity at all.
func (p Principal) IsAnonymous() bool { return p.UserID == "" }

// IsAdmin reports whether the principal may change instance-wide state.
func (p Principal) IsAdmin() bool {
	return !p.IsAnonymous() && p.Role == RoleAdmin
}

// Owns reports whether the principal owns a resource labelled with ownerID.
//
// Being an admin deliberately does not grant ownership. Administering an
// instance means changing its settings, reading its logs and managing its
// users — not silently typing into somebody else's authenticated mainframe
// session. Those are different powers and conflating them would make the
// audit trail lie about who did what.
//
// An empty ownerID never matches. Unlabelled resources predate ownership, and
// treating them as "belongs to whoever asked" is the fail-open reading.
func (p Principal) Owns(ownerID string) bool {
	if p.IsAnonymous() || ownerID == "" {
		return false
	}
	return p.UserID == ownerID
}

// HasScope reports whether the principal carries the named scope. A principal
// with no scopes is unrestricted within its role, which is how browser
// sessions and the single local operator behave.
func (p Principal) HasScope(scope string) bool {
	if p.IsAnonymous() {
		return false
	}
	if len(p.Scopes) == 0 {
		return true
	}
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// String renders the principal for logs. It never includes credentials.
func (p Principal) String() string {
	if p.IsAnonymous() {
		return "anonymous"
	}
	return fmt.Sprintf("%s(%s,%s)", p.UserID, p.Role, p.Kind)
}

// Mode selects how callers are identified.
type Mode string

const (
	// ModeNone runs without authentication: every request is the single
	// local operator. This is the historical behaviour and remains the
	// default.
	ModeNone Mode = "none"
	// ModeLocal authenticates against the server's own account store.
	ModeLocal Mode = "local"
	// ModeOIDC authenticates against an external identity provider, while
	// still accepting a password for accounts that have one.
	//
	// Both, rather than either: the local account is the way back in when the
	// provider is unreachable or misconfigured, and an instance whose only
	// door depends on a service it does not run is an instance that can be
	// locked out of itself by somebody else's outage.
	ModeOIDC Mode = "oidc"
)

// ModeEnv names the environment variable that selects the mode.
const ModeEnv = "AUTH_MODE"

// ParseMode converts the configured value into a Mode.
//
// Unrecognised values are an error rather than a silent fallback. Someone who
// sets AUTH_MODE to a mode this build does not implement is asking for
// protection it cannot provide, and starting anyway would leave them believing
// the instance is guarded when it is not. Failing at startup says so plainly.
func ParseMode(value string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(ModeNone):
		return ModeNone, nil
	case string(ModeLocal):
		return ModeLocal, nil
	case string(ModeOIDC):
		return ModeOIDC, nil
	default:
		return "", fmt.Errorf("unsupported %s %q (supported: %s, %s, %s)", ModeEnv, value, ModeNone, ModeLocal, ModeOIDC)
	}
}

// Service returns the principal for a caller holding the instance-wide API
// token, which only exists where there is a single operator.
//
// It is that operator: same UserID as Local, so a session opened in the
// browser and one opened over the API belong to the same person and each can
// drive the other — which is the entire point of the API on a single-operator
// instance. Only Kind differs, so a log line still records that the request
// arrived on a token rather than in a browser.
//
// Where users are separated the shared token does not exist; see Token.
func Service() Principal {
	return Principal{
		UserID: LocalUserID,
		Role:   RoleAdmin,
		Kind:   KindAPIToken,
	}
}

// Token returns the principal for a caller holding a token issued to an
// account.
//
// It carries that account's own UserID, so a token reaches exactly what its
// owner reaches — no more, and no less. Scopes narrow it further; an empty
// list means "whatever the account itself could do".
func Token(userID string, role Role, scopes []string) Principal {
	return Principal{
		UserID: userID,
		Role:   role,
		Kind:   KindAPIToken,
		Scopes: scopes,
	}
}
