package main

import (
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

// Who a published host profile is for.
//
// An instance with one operator has no audience to decide: everything is
// theirs. Where accounts exist, an administrator publishes the host list once
// and says which of it each team may reach — which is the difference between
// a shared list of hosts and a shared list of hosts everybody has to read past
// to find their two.
//
// The rule is deliberately permissive at the bottom: a profile that names
// nobody is for everybody. Any other default would mean turning this on
// silently removed a host list somebody was already using.

// maxAudienceEntries bounds each of the three lists on a profile. They are
// written by an administrator, so this is a guard against a pasted file rather
// than against an attacker.
const maxAudienceEntries = 128

// audienceOfProfile reports whether the profile names anybody at all.
func (p ConnectionProfile) hasAudience() bool {
	return len(p.Users) > 0 || len(p.Groups) > 0 || len(p.Roles) > 0
}

// visibleTo reports whether account may use this profile.
//
// Roles are matched by name so that "everyone who administers" can be
// expressed without listing administrators, which is the list most likely to
// be out of date.
func (p ConnectionProfile) visibleTo(account users.User) bool {
	if !p.hasAudience() {
		return true
	}
	for _, name := range p.Users {
		if strings.EqualFold(strings.TrimSpace(name), account.Username) {
			return true
		}
	}
	for _, name := range p.Groups {
		if account.InGroup(name) {
			return true
		}
	}
	for _, name := range p.Roles {
		if strings.EqualFold(strings.TrimSpace(name), string(account.Role)) {
			return true
		}
	}
	return false
}

// normaliseAudience cleans the three lists the way group names are cleaned
// everywhere else, and drops roles that do not exist.
//
// An unknown role is dropped rather than refused: it can only have come from a
// typo, and keeping it would leave a profile that looks assigned and reaches
// nobody — the failure that is hardest to notice, because the person who
// cannot see the host does not know it is there.
func normaliseAudience(p *ConnectionProfile) {
	p.Users = users.NormaliseGroups(p.Users)
	p.Groups = users.NormaliseGroups(p.Groups)

	roles := users.NormaliseGroups(p.Roles)
	kept := roles[:0]
	for _, r := range roles {
		switch strings.ToLower(strings.TrimSpace(r)) {
		case string(authz.RoleUser):
			kept = append(kept, string(authz.RoleUser))
		case string(authz.RoleAdmin):
			kept = append(kept, string(authz.RoleAdmin))
		}
	}
	if len(kept) == 0 {
		p.Roles = nil
	} else {
		p.Roles = kept
	}

	if len(p.Users) > maxAudienceEntries {
		p.Users = p.Users[:maxAudienceEntries]
	}
	if len(p.Groups) > maxAudienceEntries {
		p.Groups = p.Groups[:maxAudienceEntries]
	}
}

// accountFor returns the account making this request.
//
// The principal carries the ID and the role but not the group list, which is
// on the account, so this is the one read that turns "who is asking" into
// "what may they reach".
func (app *App) accountFor(c *gin.Context) (users.User, bool) {
	principal := principalFrom(c)
	if principal.IsAnonymous() {
		return users.User{}, false
	}
	if !app.separatesUsers() {
		// One operator, no account file to consult. Given the admin role so a
		// profile assigned to administrators still reaches them, which is what
		// somebody trying the feature on a laptop would expect.
		return users.User{Username: authz.LocalUserID, Role: authz.RoleAdmin}, true
	}
	account, found, err := app.userStore().ByID(principal.UserID)
	if err != nil || !found {
		return users.User{}, false
	}
	// The role field carries the effective role, so a profile assigned to
	// administrators reaches somebody whose administration comes from a group.
	account.Role = app.effectiveRoleFor(account)
	return account, true
}

// assignedProfiles is the host list this caller may reach, in the order a menu
// should show it.
//
// This is the one function the session-selection screen and the connect page
// both read, so what an operator is offered and what they are allowed cannot
// disagree.
func (app *App) assignedProfiles(c *gin.Context) ([]ConnectionProfile, error) {
	all, err := app.visibleProfiles(c)
	if err != nil {
		return nil, err
	}
	account, ok := app.accountFor(c)
	if !ok {
		return nil, nil
	}

	out := make([]ConnectionProfile, 0, len(all))
	for _, p := range all {
		// A profile of somebody's own is theirs whatever it says: the audience
		// fields only decide who a *published* profile reaches.
		if !p.Shared || p.visibleTo(account) {
			out = append(out, p)
		}
	}
	return out, nil
}

// knownGroups lists every group in use, for the administration page to offer
// rather than making somebody remember how a name was spelled.
func (app *App) knownGroups() []string {
	if !app.separatesUsers() {
		return nil
	}
	groups, err := app.userStore().Groups()
	if err != nil {
		return nil
	}
	sort.Strings(groups)
	return groups
}
