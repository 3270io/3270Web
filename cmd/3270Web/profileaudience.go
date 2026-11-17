// SPDX-License-Identifier: AGPL-3.0-or-later

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

// offered reports whether this preset is on anybody's list at all.
//
// Naming an audience offers it to them, whatever the flag says. That is what
// keeps the two from contradicting each other: a preset assigned to a group
// from the groups page — which sets the audience and knows nothing about this
// flag — would otherwise look assigned and reach nobody, the failure nobody
// notices because the person who cannot see the host does not know it exists.
func (p ConnectionProfile) offered() bool {
	return !p.NotOffered || p.hasAudience()
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

// settleOffered stops the flag and the audience from disagreeing on the way
// in, so what the presets page shows back is what was actually saved.
//
// offered() already treats a named audience as an offer; this only makes the
// stored record say the same thing.
func settleOffered(p *ConnectionProfile) {
	if p.hasAudience() {
		p.NotOffered = false
	}
}

// offeredOnly drops the presets that are not on anybody's list yet.
func offeredOnly(profiles []ConnectionProfile) []ConnectionProfile {
	out := make([]ConnectionProfile, 0, len(profiles))
	for _, p := range profiles {
		if p.offered() {
			out = append(out, p)
		}
	}
	return out
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

// assignedProfiles is the host list this caller may connect to by name, in the
// order a menu should show it: the published hosts their account reaches, and
// the connections they saved for themselves.
//
// This is the one function the session-selection screen and the connect page
// both read, so what an operator is offered and what they are allowed cannot
// disagree. It is not the list an administrator gave them — see hostsAssigned,
// which is the one to ask what the first screen should be.
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

// hostsAssigned is what an administrator has given this caller: the published
// hosts their account reaches, and nothing they saved for themselves.
//
// It is the list the first screen is decided by — none is the connect form, one
// is a connection made for them, several is the session manager — because what
// an instance has chosen to hand somebody is a different fact from what they
// happen to have saved. Asking assignedProfiles instead meant saving a
// connection changed what the account met at sign-in: one saved host was dialled
// without being asked for, and two put a menu where the connect form had been.
//
// A saved connection that shares a name with a published host replaces it in
// the lists the account picks from, so that its own settings win. It is still
// the host they were given, and counts as it; the entry returned is the one
// that wins, so a connection made for them uses those settings too.
//
// Under a single operator there is no published list and nobody to assign one,
// so nothing is assigned. A configured target host with auto-connect is how an
// instance like that says where to go.
func (app *App) hostsAssigned(c *gin.Context) ([]ConnectionProfile, error) {
	published := app.publishedProfiles(c)
	if published == nil {
		return nil, nil
	}
	account, ok := app.accountFor(c)
	if !ok {
		return nil, nil
	}
	shared, err := published.load()
	if err != nil {
		return nil, err
	}
	given := make(map[string]bool, len(shared))
	for _, p := range offeredOnly(shared) {
		if p.visibleTo(account) {
			given[profileNameKey(p.Name)] = true
		}
	}
	if len(given) == 0 {
		return nil, nil
	}

	reachable, err := app.assignedProfiles(c)
	if err != nil {
		return nil, err
	}
	out := make([]ConnectionProfile, 0, len(given))
	for _, p := range reachable {
		if given[profileNameKey(p.Name)] {
			out = append(out, p)
		}
	}
	return out, nil
}

// knownGroups lists every group in use, for the administration page to offer
// rather than making somebody remember how a name was spelled.
//
// The account store holds most of them. The rest are names a published
// preset's audience carries and nothing else does — perfectly real groups,
// since they decide who is offered that mainframe, but invisible to a store
// that has never been told about them. Leaving them out of the pickers is how
// a second spelling of a group that already governs a host gets typed in.
func (app *App) knownGroups(c *gin.Context) []string {
	if !app.separatesUsers() {
		return nil
	}
	groups, err := app.userStore().Groups()
	if err != nil {
		return nil
	}
	sort.Strings(groups)
	// The store's spelling wins where both have one: it is the one somebody
	// chose rather than the one that happened to be typed into an audience.
	merged := mergeGroupNames(groups, groupNamesInProfiles(app.publishedProfileList(c)))
	sort.Strings(merged)
	return merged
}
