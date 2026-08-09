package authz

import (
	"strings"
	"testing"
)

// The zero Principal is what a request carries when authorization was never
// resolved. Every predicate must answer no, so a missing middleware or a
// forgotten branch denies rather than admits.
func TestAnonymousFailsClosed(t *testing.T) {
	p := Anonymous()

	if !p.IsAnonymous() {
		t.Error("Anonymous().IsAnonymous() = false")
	}
	if p.IsAdmin() {
		t.Error("anonymous is admin")
	}
	if p.Owns(LocalUserID) {
		t.Error("anonymous owns a labelled resource")
	}
	if p.Owns("") {
		t.Error("anonymous owns an unlabelled resource")
	}
	if p.HasScope("anything") {
		t.Error("anonymous holds a scope")
	}
	if got := p.String(); got != "anonymous" {
		t.Errorf("String() = %q, want %q", got, "anonymous")
	}

	// The zero value must behave exactly like Anonymous(), since a Principal
	// reaches a predicate as a zero value whenever resolution was skipped.
	var zero Principal
	if !zero.IsAnonymous() || zero.IsAdmin() || zero.Owns(LocalUserID) || zero.HasScope("x") {
		t.Error("zero value does not behave like Anonymous()")
	}
}

func TestLocalPrincipal(t *testing.T) {
	p := Local()

	if p.IsAnonymous() {
		t.Error("Local() is anonymous")
	}
	if !p.IsAdmin() {
		t.Error("Local() is not admin")
	}
	if !p.Owns(LocalUserID) {
		t.Error("Local() does not own its own sessions")
	}
	if !p.HasScope("anything") {
		t.Error("Local() should be unrestricted")
	}
}

// Ownership is not a consequence of being an admin. Administering the
// instance and driving another user's authenticated terminal are different
// powers, and merging them would make the audit trail misattribute actions.
func TestAdminDoesNotInheritOwnership(t *testing.T) {
	admin := Principal{UserID: "alice", Role: RoleAdmin, Kind: KindWeb}

	if !admin.IsAdmin() {
		t.Fatal("expected admin")
	}
	if admin.Owns("bob") {
		t.Error("admin owns another user's resource")
	}
	if !admin.Owns("alice") {
		t.Error("admin does not own its own resource")
	}
}

func TestOwns(t *testing.T) {
	tests := []struct {
		name    string
		user    string
		ownerID string
		want    bool
	}{
		{"match", "alice", "alice", true},
		{"mismatch", "alice", "bob", false},
		{"unlabelled resource", "alice", "", false},
		{"anonymous principal", "", "alice", false},
		{"both empty", "", "", false},
		{"case sensitive", "alice", "Alice", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Principal{UserID: tt.user, Role: RoleUser, Kind: KindWeb}
			if got := p.Owns(tt.ownerID); got != tt.want {
				t.Errorf("Principal{%q}.Owns(%q) = %v, want %v", tt.user, tt.ownerID, got, tt.want)
			}
		})
	}
}

func TestHasScope(t *testing.T) {
	unrestricted := Principal{UserID: "alice", Role: RoleUser, Kind: KindWeb}
	if !unrestricted.HasScope("sessions:write") {
		t.Error("no scopes should mean unrestricted within the role")
	}

	scoped := Principal{
		UserID: "alice",
		Role:   RoleUser,
		Kind:   KindAPIToken,
		Scopes: []string{"sessions:read"},
	}
	if !scoped.HasScope("sessions:read") {
		t.Error("granted scope reported missing")
	}
	if scoped.HasScope("sessions:write") {
		t.Error("ungranted scope reported present")
	}
}

func TestIsAdminRequiresIdentity(t *testing.T) {
	// A role without a user is not a principal; it must not confer anything.
	rogue := Principal{Role: RoleAdmin}
	if rogue.IsAdmin() {
		t.Error("a role with no UserID granted admin")
	}
}

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"":         ModeNone,
		"none":     ModeNone,
		"NONE":     ModeNone,
		"  none  ": ModeNone,
		"local":    ModeLocal,
		"LOCAL":    ModeLocal,
		"  local ": ModeLocal,
	}
	for in, want := range cases {
		got, err := ParseMode(in)
		if err != nil {
			t.Errorf("ParseMode(%q) returned %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", in, got, want)
		}
	}
}

// A mode this build cannot implement must stop startup. Accepting it would
// leave an operator believing the instance is authenticated when it is not.
func TestParseModeRejectsUnsupported(t *testing.T) {
	for _, in := range []string{"proxy", "true", "off", "ldap", "yes", "saml"} {
		if got, err := ParseMode(in); err == nil {
			t.Errorf("ParseMode(%q) = %q, want an error", in, got)
		}
	}
}

func TestParseModeErrorNamesTheVariable(t *testing.T) {
	_, err := ParseMode("nonsense")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The message is what an operator sees at startup; it has to say which
	// setting is wrong and what is accepted.
	for _, want := range []string{ModeEnv, "nonsense", string(ModeNone)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
