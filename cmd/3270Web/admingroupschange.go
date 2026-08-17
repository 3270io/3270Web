// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

// Deciding whether a change to a group is allowed, before any of it happens.
//
// The group page edits four separate things behind one Save: the group's own
// record, the role it grants, who is in it, and which published presets it
// reaches. They live in two stores and, in the case of the presets, in a file
// this package writes as a whole. The first version applied them in order and
// checked each as it went, which meant a request could rename a group, then
// discover that the demotion it also carried would lock the administrator out
// of the page, and answer "no" having already done half of it. What the
// administrator saw was a refusal; what the instance had was the rename.
//
// So validation is separated from mutation. Everything that can be decided by
// looking — the name, the collisions, the role, the membership, the hosts, and
// whether any preset would be left offered to everyone — is decided first,
// against one read of each store. Only then is anything written, and the
// writes that span several presets go through connectionProfileStore.mutate so
// the whole file moves at once or not at all.

// adminError is a refusal with the status code that fits it, so the mapping
// from "what was wrong" to "what the browser is told" is made once at the
// point the reason is known rather than repeated at every return.
//
//	400 the request cannot be honoured as written, including a change that
//	    would lock the caller out of administration
//	404 there is no such group
//	409 a name collision, a group the identity provider owns, or a preset
//	    that would silently become everybody's
//	500 a store would not answer
type adminError struct {
	status int
	msg    string
}

func (e *adminError) Error() string { return e.msg }

func badRequest(format string, args ...any) *adminError {
	return &adminError{status: http.StatusBadRequest, msg: fmt.Sprintf(format, args...)}
}

func notFound(format string, args ...any) *adminError {
	return &adminError{status: http.StatusNotFound, msg: fmt.Sprintf(format, args...)}
}

func conflict(format string, args ...any) *adminError {
	return &adminError{status: http.StatusConflict, msg: fmt.Sprintf(format, args...)}
}

func serverError(format string, args ...any) *adminError {
	return &adminError{status: http.StatusInternalServerError, msg: fmt.Sprintf(format, args...)}
}

// respondAdminError answers the request with the refusal.
func respondAdminError(c *gin.Context, err *adminError) {
	c.JSON(err.status, gin.H{"error": err.msg})
}

// groupChange is a combined request that has been read, checked against both
// stores, and found to be applicable in full.
//
// Nothing here is a request any more: the name is resolved to the spelling the
// instance holds, the role is a role rather than a string, and every list has
// been matched against what exists. Applying it can still fail on a store that
// will not answer, but not on anything that could have been known first.
type groupChange struct {
	// current is the group as it is named now, in the spelling the instance
	// holds rather than the one the request happened to use.
	current string
	// final is the name it carries afterwards, equal to current unless renamed.
	final  string
	rename bool
	// adopt marks a group that exists only because a published preset's
	// audience names it. Editing one declares it, which is how a group that
	// controls a host but has no record anywhere becomes maintainable.
	adopt bool
	// create marks a group that does not exist yet at all.
	create      bool
	description *string
	role        *authz.Role
	members     *[]string
	hosts       *[]string
}

// groupsComeFromProvider reports whether this deployment takes group
// membership from an identity provider, which is what makes a group the
// directory feeds the directory's to name.
func (app *App) groupsComeFromProvider() bool {
	return app.ssoEnabled() && app.sso.GroupsClaim != ""
}

/* ---------------------------------------------------------------- */
/* Groups that exist only in a preset's audience                     */
/* ---------------------------------------------------------------- */

// groupNamesInProfiles collects the group names published audiences carry.
//
// These are group names stored outside the account store — a preset's audience
// holds them as text — so nothing in internal/users knows they exist. A name
// that only appears here still decides who is offered a mainframe, which makes
// leaving it off the groups page the one way to have a group that governs
// access and cannot be found.
func groupNamesInProfiles(profiles []ConnectionProfile) []string {
	seen := make(map[string]string, len(profiles))
	order := make([]string, 0, len(profiles))
	for _, p := range profiles {
		for _, g := range p.Groups {
			name := strings.TrimSpace(g)
			key := strings.ToLower(name)
			if key == "" || seen[key] != "" {
				continue
			}
			seen[key] = name
			order = append(order, key)
		}
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, seen[key])
	}
	return out
}

// mergeGroupNames folds extra names into a list, case-insensitively, with the
// list's own spelling winning.
//
// The account store's spelling is the one somebody chose on purpose; a preset
// audience holds whatever was typed into it. Both are the same group, and the
// page must not show it twice.
func mergeGroupNames(canonical []string, extra ...[]string) []string {
	out := make([]string, 0, len(canonical))
	seen := make(map[string]bool, len(canonical))
	for _, name := range canonical {
		name = strings.TrimSpace(name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		out = append(out, name)
	}
	for _, list := range extra {
		for _, name := range list {
			name = strings.TrimSpace(name)
			if name == "" || seen[strings.ToLower(name)] {
				continue
			}
			seen[strings.ToLower(name)] = true
			out = append(out, name)
		}
	}
	return out
}

// profileOnlyGroupName finds the spelling a preset audience uses for a name
// the account store has never heard of.
func profileOnlyGroupName(profiles []ConnectionProfile, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	for _, p := range profiles {
		for _, g := range p.Groups {
			if strings.EqualFold(strings.TrimSpace(g), name) {
				return strings.TrimSpace(g), true
			}
		}
	}
	return "", false
}

// audienceNamesGroup reports whether this preset's audience names the group.
func audienceNamesGroup(p ConnectionProfile, group string) bool {
	group = strings.TrimSpace(group)
	for _, g := range p.Groups {
		if strings.EqualFold(strings.TrimSpace(g), group) {
			return true
		}
	}
	return false
}

// audienceSurvivesWithout reports whether the preset would still name somebody
// once this group is taken off it.
//
// Roles count as well as users and other groups: a preset for administrators
// names an audience just as surely as one that lists them, and treating it as
// unrestricted would be the same mistake in a different field.
func audienceSurvivesWithout(p ConnectionProfile, group string) bool {
	if len(p.Users) > 0 || len(p.Roles) > 0 {
		return true
	}
	for _, g := range p.Groups {
		if !strings.EqualFold(strings.TrimSpace(g), strings.TrimSpace(group)) {
			return true
		}
	}
	return false
}

// presetsLeftNamingNobody projects an audience change without saving any of it
// and names the presets that would end up naming nobody at all.
//
// A published preset that names nobody is offered to everybody. That rule is
// right — it is what stops turning audiences on from taking a host list away
// from a deployment that never had one — but it makes removal the dangerous
// direction. Unticking a group's last host, or deleting the group a preset
// names, reads as "take this away from them" and lands as "give this to
// everyone", and nothing on the screen says so.
//
// Which presets an administrator meant to open up is not a thing this page can
// guess, so it does not: the change is refused, and choosing Everyone stays an
// explicit act on the preset itself.
//
// keeps reports whether the group remains on a given preset after the change.
func presetsLeftNamingNobody(profiles []ConnectionProfile, group string, keeps func(ConnectionProfile) bool) []string {
	var out []string
	for _, p := range profiles {
		if !audienceNamesGroup(p, group) {
			continue
		}
		if keeps != nil && keeps(p) {
			continue
		}
		if audienceSurvivesWithout(p, group) {
			continue
		}
		out = append(out, p.Name)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// exposureRefusal is the wording an administrator gets when a removal would
// hand a restricted preset to everyone.
func exposureRefusal(presets []string) *adminError {
	subject := "host preset"
	if len(presets) > 1 {
		subject = "host presets"
	}
	return conflict("that would leave the %s %s naming nobody, which offers %s to everyone. "+
		"Nothing has been changed. If that is what you want, open %s on the Session screen page "+
		"and choose Everyone explicitly; otherwise give %s another group, user or role first.",
		subject, quotedList(presets), pluralThem(presets), pluralIt(presets), pluralIt(presets))
}

func quotedList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, "\""+n+"\"")
	}
	return strings.Join(quoted, ", ")
}

func pluralThem(names []string) string {
	if len(names) > 1 {
		return "them"
	}
	return "it"
}

func pluralIt(names []string) string {
	if len(names) > 1 {
		return "each of them"
	}
	return "it"
}

/* ---------------------------------------------------------------- */
/* Preflight                                                         */
/* ---------------------------------------------------------------- */

// preflightGroupChange reads a combined request and answers whether all of it
// can be applied, without applying any of it.
//
// target is the group being changed, ignored when creating. Everything it
// needs is read once here: the group, the account list, the role assignments
// and the published presets.
func (app *App) preflightGroupChange(
	c *gin.Context, target string, req adminGroupRequest, creating bool,
) (*groupChange, []ConnectionProfile, *adminError) {
	profiles, loadErr := app.loadPublishedProfiles(c)
	if loadErr != nil {
		return nil, nil, loadErr
	}

	change := &groupChange{create: creating}
	var info users.GroupInfo

	if creating {
		if req.Name == nil {
			return nil, nil, badRequest("a group name is required")
		}
		name := strings.TrimSpace(*req.Name)
		if err := users.ValidateGroupName(name); err != nil {
			return nil, nil, badRequest("%s", humanPasswordError(err))
		}
		if taken, err := app.groupNameTaken(name, profiles); err != nil {
			return nil, nil, err
		} else if taken {
			return nil, nil, conflict("a group with that name already exists")
		}
		change.current, change.final = name, name
	} else {
		resolved, profileOnly, err := app.resolveGroupTarget(target, profiles)
		if err != nil {
			return nil, nil, err
		}
		info = resolved
		change.current, change.final = info.Name, info.Name
		change.adopt = profileOnly
		if profileOnly {
			// The name has only ever been a string in a preset's audience, so
			// it has never been through the rules a declared name must pass.
			// Said here rather than as a store failure half way through.
			if err := users.ValidateGroupName(info.Name); err != nil {
				return nil, nil, badRequest(
					"%q cannot be managed as a group: %s. Change the audience on the Session screen page instead.",
					info.Name, humanPasswordError(err))
			}
		}
	}

	// A rename, including one that only changes capitals: the membership and
	// the audiences carry the spelling, so they have to follow it.
	if !creating && req.Name != nil {
		candidate := strings.TrimSpace(*req.Name)
		if err := users.ValidateGroupName(candidate); err != nil {
			return nil, nil, badRequest("%s", humanPasswordError(err))
		}
		if !strings.EqualFold(candidate, change.current) {
			if taken, err := app.groupNameTaken(candidate, profiles); err != nil {
				return nil, nil, err
			} else if taken {
				return nil, nil, conflict("a group with that name already exists")
			}
		}
		change.final = candidate
		change.rename = candidate != change.current
	}

	// A group the directory feeds is the directory's to name: the claim is
	// replayed at every sign-in, so a local rename produces two groups where
	// there was one. Enforced in the store as well; refused here so the
	// administrator is told before anything else in the request is applied.
	if change.rename && app.groupsComeFromProvider() && len(info.ExternalMembers) > 0 {
		return nil, nil, conflict(
			"%q takes its membership from the identity provider, so its name is set there. "+
				"Rename or remove it at the provider; its description, the role it grants and "+
				"the hosts it reaches can still be changed here.", change.current)
	}

	if req.Description != nil {
		description := strings.TrimSpace(*req.Description)
		if len(description) > users.MaxGroupDescriptionLength {
			return nil, nil, badRequest("a group description is longer than %d characters",
				users.MaxGroupDescriptionLength)
		}
		change.description = &description
	}

	if req.Role != nil {
		role := authz.RoleUser
		if strings.EqualFold(strings.TrimSpace(*req.Role), string(authz.RoleAdmin)) {
			role = authz.RoleAdmin
		}
		// Clearing the assignment your own administrator role rests on would
		// lock you out of the page you are standing on. Refused here exactly as
		// it is on the accounts page, and for the same reason.
		if role != authz.RoleAdmin && app.selfAdminDependsOnGroup(c, change.current) {
			return nil, nil, badRequest(
				"your own administrator role comes from this group; another administrator has to change it")
		}
		change.role = &role
	}

	if req.Members != nil {
		wanted := *req.Members
		// Leaving a group can take a role away, so removing yourself from one
		// that grants you administration is self-demotion by another route.
		if app.selfAdminDependsOnGroup(c, change.current) && !containsFold(wanted, usernameFrom(c)) {
			return nil, nil, badRequest(
				"your administrator role comes from this group; you cannot remove yourself from it")
		}
		if err := app.checkMembersExist(wanted); err != nil {
			return nil, nil, err
		}
		change.members = &wanted
	}

	// Both of the guards above are about the caller. This one is about the
	// instance: an administrator may not leave it with nobody able to
	// administer it, whichever of them does it.
	if err := app.checkAdministratorSurvives(change); err != nil {
		return nil, nil, err
	}

	if req.Hosts != nil {
		wanted, err := hostSelection(*req.Hosts, profiles)
		if err != nil {
			return nil, nil, err
		}
		exposed := presetsLeftNamingNobody(profiles, change.current, func(p ConnectionProfile) bool {
			return wanted[strings.ToLower(strings.TrimSpace(p.Name))]
		})
		if len(exposed) > 0 {
			return nil, nil, exposureRefusal(exposed)
		}
		hosts := *req.Hosts
		change.hosts = &hosts
	}

	return change, profiles, nil
}

// resolveGroupTarget finds the group a request is about.
//
// A name the account store holds is that group. A name only a preset audience
// carries is also a group — it decides who is offered that host — and is
// returned with the flag that says editing it will declare it.
func (app *App) resolveGroupTarget(
	target string, profiles []ConnectionProfile,
) (users.GroupInfo, bool, *adminError) {
	target = strings.TrimSpace(target)
	if target == "" {
		return users.GroupInfo{}, false, badRequest(
			"name the group to change, either in the path or as \"currentName\" in the request")
	}
	info, err := app.userStore().Group(target)
	switch {
	case err == nil:
		return info, false, nil
	case errors.Is(err, users.ErrGroupNotFound):
		name, found := profileOnlyGroupName(profiles, target)
		if !found {
			return users.GroupInfo{}, false, notFound("no such group")
		}
		return users.GroupInfo{
			Group: users.Group{Name: name},
			Role:  authz.RoleUser,
		}, true, nil
	default:
		return users.GroupInfo{}, false, serverError("could not read the group")
	}
}

// groupNameTaken reports whether anything already answers to this name: a
// declared record, a role assignment, an account's membership, or a published
// preset's audience.
//
// The last one matters as much as the rest. A preset audience naming
// "shipping" puts shipping on this page, so creating or renaming to it would
// produce a second row for a group that is plainly there — and a rename onto
// it would quietly merge two audiences.
func (app *App) groupNameTaken(name string, profiles []ConnectionProfile) (bool, *adminError) {
	if _, err := app.userStore().Group(name); err == nil {
		return true, nil
	} else if !errors.Is(err, users.ErrGroupNotFound) {
		return false, serverError("could not read the group list")
	}
	_, inProfiles := profileOnlyGroupName(profiles, name)
	return inProfiles, nil
}

// checkMembersExist refuses a membership naming an account that is not there,
// rather than storing it and leaving a group that looks fuller than it is.
func (app *App) checkMembersExist(wanted []string) *adminError {
	if len(wanted) == 0 {
		return nil
	}
	accounts, err := app.userStore().List()
	if err != nil {
		return serverError("could not read the account list")
	}
	for _, name := range wanted {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		found := false
		for _, u := range accounts {
			if strings.EqualFold(u.Username, name) {
				found = true
				break
			}
		}
		if !found {
			return badRequest("there is no account named %q", name)
		}
	}
	return nil
}

// checkAdministratorSurvives projects the role and membership parts of the
// change onto the account list and refuses one that would leave the instance
// with nobody able to administer it.
//
// The store refuses this too, on each write. Projecting it here is what keeps
// the refusal from arriving after the rename in the same request has already
// been saved.
func (app *App) checkAdministratorSurvives(change *groupChange) *adminError {
	if change.role == nil && change.members == nil {
		return nil
	}
	accounts, err := app.userStore().List()
	if err != nil {
		return serverError("could not read the account list")
	}
	assigned, err := app.userStore().GroupRoles()
	if err != nil {
		return serverError("could not read the group roles")
	}
	if countAdministrators(accounts, assigned) == 0 {
		// Nothing to protect; refusing here would only make an instance that
		// already has no administrator impossible to give one.
		return nil
	}

	next := make(map[string]authz.Role, len(assigned)+1)
	for g, r := range assigned {
		if !strings.EqualFold(strings.TrimSpace(g), change.current) {
			next[g] = r
		}
	}
	if change.role != nil {
		if *change.role == authz.RoleAdmin {
			next[change.current] = authz.RoleAdmin
		}
	} else {
		// Not mentioned in the request, so whatever the group grants now is
		// what it will still grant afterwards.
		for g, r := range assigned {
			if strings.EqualFold(strings.TrimSpace(g), change.current) {
				next[change.current] = r
			}
		}
	}

	projected := make([]users.User, len(accounts))
	copy(projected, accounts)
	if change.members != nil {
		lockExternal := app.groupsComeFromProvider()
		for i := range projected {
			want := containsFold(*change.members, projected[i].Username)
			// An account the directory owns keeps whatever the directory gave
			// it, because that is what the write will do as well.
			if lockExternal && projected[i].External() {
				want = projected[i].InGroup(change.current)
			}
			if want {
				projected[i].Groups = users.NormaliseGroups(
					append(append([]string{}, projected[i].Groups...), change.current))
			} else {
				projected[i].Groups = groupsWithout(projected[i].Groups, change.current)
			}
		}
	}

	if countAdministrators(projected, next) == 0 {
		return badRequest("that would leave the instance with no enabled administrator, " +
			"which can only be undone by editing the account file by hand")
	}
	return nil
}

// countAdministrators counts the enabled accounts that would administer the
// instance, counting the roles groups grant.
func countAdministrators(accounts []users.User, groupRoles map[string]authz.Role) int {
	n := 0
	for _, u := range accounts {
		if !u.Disabled && users.EffectiveRole(u, groupRoles) == authz.RoleAdmin {
			n++
		}
	}
	return n
}

// groupsWithout returns the membership list with one group taken out of it.
func groupsWithout(list []string, name string) []string {
	out := make([]string, 0, len(list))
	for _, g := range list {
		if strings.EqualFold(strings.TrimSpace(g), strings.TrimSpace(name)) {
			continue
		}
		out = append(out, g)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// hostSelection turns the requested host list into a lookup, refusing a name
// that is not a published preset.
//
// Silently dropping one would make "the host I ticked is not on the group"
// something an administrator has to discover by reading the row back.
func hostSelection(hosts []string, profiles []ConnectionProfile) (map[string]bool, *adminError) {
	wanted := make(map[string]bool, len(hosts))
	for _, name := range hosts {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		known := false
		for _, p := range profiles {
			if strings.EqualFold(strings.TrimSpace(p.Name), name) {
				known = true
				break
			}
		}
		if !known {
			return nil, badRequest("there is no published host preset named %q", name)
		}
		wanted[strings.ToLower(name)] = true
	}
	return wanted, nil
}

// loadPublishedProfiles reads the published set for a decision that depends on
// it, treating a read failure as a refusal rather than as an empty list.
//
// publishedProfileList degrades to nil so the page can still be drawn without
// its host column. That is right for display and wrong here: "no preset names
// this group" and "the presets could not be read" are the same value, and
// acting on the first when it was really the second is how a group deletion
// would sail past the check that stops a preset becoming everybody's.
func (app *App) loadPublishedProfiles(c *gin.Context) ([]ConnectionProfile, *adminError) {
	store, err := app.profileStoreForWrite(c, true)
	if err != nil {
		return nil, &adminError{status: http.StatusForbidden, msg: err.Error()}
	}
	profiles, err := store.load()
	if err != nil {
		return nil, serverError("could not read the host presets: %v", err)
	}
	return profiles, nil
}
