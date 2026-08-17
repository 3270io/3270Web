// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

// Group administration: the page where teams are made, named, filled and
// pointed at mainframes.
//
// Everything here already existed in pieces, and the pieces were the problem.
// A group could only be created by typing its name into one account's
// membership field; it could only be given a host by opening each preset in
// turn and typing the name again; it could not be renamed at all, and removing
// one meant visiting every account that mentioned it. Three screens, no
// record, and a name that had to be spelled identically in all of them.
//
// So the group is the subject here rather than a field on something else. One
// row per team: who is in it, what role it grants, and which of the published
// mainframes it reaches — each editable from the group's own side. The stores
// underneath are the existing ones; what this adds is the room they are
// administered from.
//
// Deciding whether a change is allowed happens in admingroupschange.go, in
// full, before any of it is written.

const adminGroupsPath = "/admin/groups"

// adminGroupView is one group on the wire.
type adminGroupView struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Role the group grants its members, "user" when it grants nothing extra.
	Role        string   `json:"role"`
	Members     []string `json:"members"`
	MemberCount int      `json:"memberCount"`
	// Hosts names the published presets whose audience names this group, so
	// "what can this team reach" is answered by reading the row rather than by
	// opening every preset in turn.
	Hosts []string `json:"hosts"`
	// Declared marks a group with a record of its own, as against one that
	// exists only because an account, a role assignment or a host preset
	// mentions it.
	Declared bool `json:"declared"`
	// ProviderManaged marks a group whose membership arrives in a directory
	// claim and is replayed at every sign-in. Its name and its existence are
	// the provider's; everything else about it is still administered here.
	ProviderManaged bool   `json:"providerManaged,omitempty"`
	CreatedAt       string `json:"createdAt,omitempty"`
}

// adminHostView is one published preset as the host picker shows it.
type adminHostView struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Target      string `json:"target"`
	// Everyone marks a preset that names nobody and so is offered to all. It
	// is shown because assigning such a preset to one group *narrows* it, which
	// is the opposite of what "give this team a host" sounds like.
	Everyone bool `json:"everyone"`
	// SampleApp names the bundled application a preset points at, when it
	// points at one. A group's hosts are as legitimately a sample app as a
	// mainframe — an instance being evaluated or taught on has nothing else —
	// and "sampleapp:app1:3271" is the internal address, not the name anybody
	// chose it by.
	SampleApp string `json:"sampleApp,omitempty"`
}

// AdminGroupsPageHandler serves the group management page.
func (app *App) AdminGroupsPageHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "admin-groups.html", gin.H{
		"AuthEnabled": app.authMode != authz.ModeNone,
		"MaxNameLen":  users.MaxGroupNameLength,
		"MaxDescLen":  users.MaxGroupDescriptionLength,
		"Username":    usernameFrom(c),
	})
}

// AdminListGroupsHandler returns every group, with what the pickers offer.
func (app *App) AdminListGroupsHandler(c *gin.Context) {
	if app.authMode == authz.ModeNone {
		c.JSON(http.StatusOK, gin.H{"authEnabled": false, "groups": []adminGroupView{}})
		return
	}

	infos, err := app.userStore().ListGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read the group list"})
		return
	}

	profiles := app.publishedProfileList(c)
	views := app.groupViews(infos, profiles)

	accounts, err := app.userStore().List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read the account list"})
		return
	}
	usernames := make([]string, 0, len(accounts))
	var external []string
	for _, u := range accounts {
		usernames = append(usernames, u.Username)
		if u.External() {
			external = append(external, u.Username)
		}
	}
	sort.Slice(usernames, func(i, j int) bool {
		return strings.ToLower(usernames[i]) < strings.ToLower(usernames[j])
	})

	hosts := make([]adminHostView, 0, len(profiles))
	for _, p := range profiles {
		hosts = append(hosts, adminHostView{
			Name:        p.Name,
			Description: p.Description,
			Target:      p.displayTarget(),
			Everyone:    !p.hasAudience(),
			SampleApp:   sampleAppPresetLabel(p),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"authEnabled": true,
		"groups":      views,
		"usernames":   usernames,
		"hosts":       hosts,
		// Accounts the directory owns: their membership is refreshed at every
		// sign-in, so the page shows it rather than offering to change it.
		"externalUsernames":  external,
		"groupsFromProvider": app.groupsComeFromProvider(),
		"self":               usernameFrom(c),
	})
}

// groupViews assembles every group this instance knows about for the page.
//
// The account store's own list is only part of it. A group named by nothing
// but a published preset's audience still decides who is offered that
// mainframe, and leaving it off would make it the one kind of group that
// governs access and cannot be found, let alone maintained.
func (app *App) groupViews(infos []users.GroupInfo, profiles []ConnectionProfile) []adminGroupView {
	fromProvider := app.groupsComeFromProvider()
	views := make([]adminGroupView, 0, len(infos))
	stored := make(map[string]bool, len(infos))
	for _, info := range infos {
		stored[strings.ToLower(strings.TrimSpace(info.Name))] = true
		views = append(views, toAdminGroupView(info, profiles, fromProvider))
	}
	for _, name := range groupNamesInProfiles(profiles) {
		if stored[strings.ToLower(name)] {
			continue
		}
		views = append(views, toAdminGroupView(users.GroupInfo{
			Group: users.Group{Name: name},
			Role:  authz.RoleUser,
		}, profiles, fromProvider))
	}
	sort.Slice(views, func(i, j int) bool {
		return strings.ToLower(views[i].Name) < strings.ToLower(views[j].Name)
	})
	return views
}

func toAdminGroupView(info users.GroupInfo, profiles []ConnectionProfile, fromProvider bool) adminGroupView {
	view := adminGroupView{
		Name:        info.Name,
		Description: info.Description,
		Role:        string(info.Role),
		Members:     info.Members,
		MemberCount: len(info.Members),
		Hosts:       hostsNaming(profiles, info.Name),
		Declared:    info.Declared,
		// Only where a groups claim is actually mapped: elsewhere an account
		// that signed in through the provider still has whatever groups an
		// administrator gave it here, and they are theirs to change.
		ProviderManaged: fromProvider && len(info.ExternalMembers) > 0,
	}
	if view.Members == nil {
		view.Members = []string{}
	}
	if view.Role == "" {
		view.Role = string(authz.RoleUser)
	}
	if !info.CreatedAt.IsZero() {
		view.CreatedAt = info.CreatedAt.UTC().Format("2006-01-02 15:04")
	}
	return view
}

// hostsNaming lists the presets whose audience names this group.
func hostsNaming(profiles []ConnectionProfile, group string) []string {
	out := []string{}
	for _, p := range profiles {
		if audienceNamesGroup(p, group) {
			out = append(out, p.Name)
		}
	}
	return out
}

// publishedProfileList reads the published set, treating a read failure as
// empty: the group page is still worth serving without the host column.
//
// Only for display. Anything that *decides* something goes through
// loadPublishedProfiles, which refuses rather than guessing.
func (app *App) publishedProfileList(c *gin.Context) []ConnectionProfile {
	store, err := app.profileStoreForWrite(c, true)
	if err != nil || store == nil {
		return nil
	}
	profiles, err := store.load()
	if err != nil {
		log.Printf("admin: could not read published profiles for the groups page: %v", err)
		return nil
	}
	return profiles
}

type adminGroupRequest struct {
	// CurrentName names the group being changed when the request cannot put it
	// in the path. A group name may contain "/" — "payments/shipping" is an
	// ordinary way to write a sub-team — and no amount of encoding makes that
	// survive as a single router path parameter, so such a group could be
	// created and then never edited or deleted again. See the collection
	// routes in main.go.
	CurrentName *string `json:"currentName"`
	// Pointers so "not mentioned" and "set to empty" stay distinguishable: a
	// PATCH that only renames must not clear the membership.
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Role        *string   `json:"role"`
	Members     *[]string `json:"members"`
	Hosts       *[]string `json:"hosts"`
}

// groupTarget resolves which group a request is about: the path parameter
// where the name can be written into a URL, and "currentName" in the body
// where it cannot.
func groupTarget(c *gin.Context, req adminGroupRequest) string {
	if name := strings.TrimSpace(c.Param("name")); name != "" {
		return name
	}
	if req.CurrentName != nil {
		return strings.TrimSpace(*req.CurrentName)
	}
	return ""
}

// AdminCreateGroupHandler declares a group, and applies whatever the create
// dialog filled in alongside the name.
func (app *App) AdminCreateGroupHandler(c *gin.Context) {
	if err := app.requireAuthEnabled(c); err != nil {
		return
	}

	var req adminGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// The dialog creates and fills in one go, so the same request may carry a
	// role, a membership and a host list. All of it is checked together, and
	// against the same rules the edit path uses, so the two cannot drift.
	change, _, aerr := app.preflightGroupChange(c, "", req, true)
	if aerr != nil {
		respondAdminError(c, aerr)
		return
	}

	notes, aerr := app.applyGroupChange(c, change)
	if aerr != nil {
		respondAdminError(c, aerr)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"group": app.groupViewFor(c, change.final), "notes": notes})
}

// AdminUpdateGroupHandler renames a group, describes it, sets the role it
// grants, replaces its membership, or changes the hosts it reaches.
//
// Serves both the path route and the collection route; see groupTarget.
func (app *App) AdminUpdateGroupHandler(c *gin.Context) {
	if err := app.requireAuthEnabled(c); err != nil {
		return
	}

	var req adminGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	change, _, aerr := app.preflightGroupChange(c, groupTarget(c, req), req, false)
	if aerr != nil {
		respondAdminError(c, aerr)
		return
	}

	notes, aerr := app.applyGroupChange(c, change)
	if aerr != nil {
		respondAdminError(c, aerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"group": app.groupViewFor(c, change.final), "notes": notes})
}

// applyGroupChange writes a change that preflight has already found to be
// applicable in full.
//
// The order is not arbitrary. The record — the name — moves last of the
// account-store writes, because it is the one every other store and every
// preset audience keys off: a failure before it leaves the group findable
// under the name the administrator typed, rather than half-renamed. The
// presets follow the record, in one atomic write each.
//
// Only what actually landed is audited.
func (app *App) applyGroupChange(c *gin.Context, change *groupChange) ([]string, *adminError) {
	var notes []string

	if change.create || change.adopt {
		// The description goes in with the record rather than as an update
		// behind it, so making a group is one audited event and not two.
		description := ""
		if change.description != nil {
			description = *change.description
			change.description = nil
		}
		if _, err := app.userStore().CreateGroup(change.current, description); err != nil {
			if errors.Is(err, users.ErrGroupExists) {
				return nil, conflict("a group with that name already exists")
			}
			return nil, badRequest("%s", humanPasswordError(err))
		}
		if change.create {
			log.Printf("auth: %s created group %q", adminActor(c), change.current)
			app.auditRequest(c, audit.EventGroupCreated, audit.Success, change.current, nil)
		} else {
			// The group was already deciding who reached a host; it simply had
			// no record. Giving it one is what makes it maintainable.
			log.Printf("auth: %s declared group %q, until now named only by a host preset",
				adminActor(c), change.current)
			app.auditRequest(c, audit.EventGroupCreated, audit.Success, change.current,
				map[string]string{"adopted": "named only by a host preset"})
		}
	}

	if change.role != nil {
		if err := app.userStore().SetGroupRole(change.current, *change.role); err != nil {
			return notes, app.undoAdoption(c, change, serverError("%s", humanPasswordError(err)))
		}
		log.Printf("auth: %s set group %q role to %s", adminActor(c), change.current, *change.role)
		app.auditRequest(c, audit.EventGroupRoleSet, audit.Success, change.current,
			map[string]string{"role": string(*change.role)})
		app.pushEffectiveRoles()
	}

	if change.members != nil {
		lockExternal := app.groupsComeFromProvider()
		skipped, err := app.userStore().SetGroupMembers(change.current, *change.members, lockExternal)
		if err != nil {
			return notes, app.undoAdoption(c, change, serverError("%s", humanPasswordError(err)))
		}
		if len(skipped) > 0 {
			notes = append(notes, "Left alone: "+strings.Join(skipped, ", ")+
				" — their groups come from the identity provider and are refreshed at every sign-in.")
		}
		log.Printf("auth: %s set the membership of group %q (%d account(s))",
			adminActor(c), change.current, len(*change.members))
		app.auditRequest(c, audit.EventGroupUpdated, audit.Success, change.current,
			map[string]string{"change": "members", "members": strings.Join(users.NormaliseGroups(*change.members), " ")})
		// Membership can carry a role, so the people already signed in are
		// given the recomputed one rather than the one they logged in with.
		app.pushEffectiveRoles()
	}

	if change.rename || change.description != nil {
		var newName *string
		if change.rename {
			newName = &change.final
		}
		updated, err := app.userStore().UpdateGroup(
			change.current, newName, change.description, app.groupsComeFromProvider())
		if err != nil {
			return notes, app.undoAdoption(c, change, app.groupStoreError(err))
		}
		if change.rename {
			// The audience of a published preset holds the name as text, and
			// the store cannot reach it. Following the rename through here is
			// what stops a renamed group from silently losing its hosts.
			if err := app.renameGroupInProfiles(c, change.current, updated.Name); err != nil {
				return notes, app.undoRename(c, change, updated.Name, err)
			}
			log.Printf("auth: %s renamed group %q to %q", adminActor(c), change.current, updated.Name)
			app.auditRequest(c, audit.EventGroupUpdated, audit.Success, updated.Name,
				map[string]string{"change": "renamed", "from": change.current})
		} else if change.description != nil {
			app.auditRequest(c, audit.EventGroupUpdated, audit.Success, updated.Name,
				map[string]string{"change": "description"})
		}
		change.final = updated.Name
	}

	if change.hosts != nil {
		changed, err := app.setGroupHosts(c, change.final, *change.hosts)
		if err != nil {
			return notes, serverError("could not save the host presets: %v", err)
		}
		if changed {
			log.Printf("admin: %s set the host list of group %q", adminActor(c), change.final)
			app.auditRequest(c, audit.EventGroupUpdated, audit.Success, change.final,
				map[string]string{"change": "hosts", "hosts": strings.Join(*change.hosts, " ")})
		}
	}

	return notes, nil
}

// undoAdoption takes back a record this request declared, when a later write
// in the same request failed.
//
// Only the adoption: a group that existed before the request is left as the
// request found it, which it is, because nothing before this point has changed
// it. A group the request created is left alone for the reason the create
// handler always had — it is plainly there, and reporting it as absent would
// send somebody looking for a group they can see.
func (app *App) undoAdoption(c *gin.Context, change *groupChange, cause *adminError) *adminError {
	if change.create {
		return &adminError{status: cause.status, msg: cause.msg +
			" The group " + change.current + " was created; the rest of the request was not applied."}
	}
	if !change.adopt {
		return cause
	}
	if err := app.userStore().DeleteGroup(change.current, false); err != nil {
		log.Printf("admin: %q was declared and then could not be undone after %v: %v",
			change.current, cause, err)
		return serverError("%s The group %q has been left declared; nothing else was changed.",
			cause.msg, change.current)
	}
	return cause
}

// undoRename puts a rename back when the presets could not follow it.
//
// A group renamed in the account store and not in the audiences is a group
// that has silently lost its hosts, which is the failure this whole page
// exists to stop. So it goes back, and if it cannot, the response says exactly
// what state the instance is in rather than reporting a clean failure.
func (app *App) undoRename(c *gin.Context, change *groupChange, renamedTo string, cause error) *adminError {
	back := change.current
	if _, err := app.userStore().UpdateGroup(renamedTo, &back, nil, false); err != nil {
		log.Printf("admin: %q could not be renamed in the host presets (%v) and could not be renamed back: %v",
			change.current, cause, err)
		return serverError("the host presets could not be updated (%v) and the group could not be "+
			"renamed back, so it is now called %q while the presets still name %q. "+
			"Rename it back on this page to repair it.", cause, renamedTo, change.current)
	}
	log.Printf("admin: renaming %q in the host presets failed (%v); the group has been renamed back",
		change.current, cause)
	return app.undoAdoption(c, change,
		serverError("the host presets could not be updated (%v), so nothing has been changed.", cause))
}

// groupStoreError maps what the account store refuses to the answer that fits.
func (app *App) groupStoreError(err error) *adminError {
	switch {
	case errors.Is(err, users.ErrGroupExists):
		return conflict("a group with that name already exists")
	case errors.Is(err, users.ErrGroupNotFound):
		return notFound("no such group")
	case errors.Is(err, users.ErrProviderManagedGroup):
		return conflict("that group takes its membership from the identity provider, so its name " +
			"is set there. Rename or remove it at the provider; its description, the role it " +
			"grants and the hosts it reaches can still be changed here.")
	default:
		return badRequest("%s", humanPasswordError(err))
	}
}

// AdminDeleteGroupHandler removes a group: its record, the role it granted,
// every membership of it, and its name from every published preset.
//
// Serves both the path route and the collection route; see groupTarget.
func (app *App) AdminDeleteGroupHandler(c *gin.Context) {
	if err := app.requireAuthEnabled(c); err != nil {
		return
	}

	// The path route carries no body at all, so binding is only attempted
	// where the name has to come from one.
	var req adminGroupRequest
	if strings.TrimSpace(c.Param("name")) == "" {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "name the group to delete as \"currentName\" in the request",
			})
			return
		}
	}

	profiles, aerr := app.loadPublishedProfiles(c)
	if aerr != nil {
		respondAdminError(c, aerr)
		return
	}
	info, profileOnly, aerr := app.resolveGroupTarget(groupTarget(c, req), profiles)
	if aerr != nil {
		respondAdminError(c, aerr)
		return
	}
	name := info.Name

	if app.selfAdminDependsOnGroup(c, name) {
		respondAdminError(c, badRequest(
			"your own administrator role comes from this group; another administrator has to remove it"))
		return
	}
	if app.groupsComeFromProvider() && len(info.ExternalMembers) > 0 {
		respondAdminError(c, conflict(
			"%q takes its membership from the identity provider, which would recreate it at the next "+
				"sign-in. Remove it at the provider instead; until then its description, the role it "+
				"grants and the hosts it reaches can still be changed here.", name))
		return
	}
	// Deleting takes the role assignment and every membership with it, so it
	// is the same question the accounts page asks before a demotion.
	empty := []string{}
	roleUser := authz.RoleUser
	if aerr := app.checkAdministratorSurvives(&groupChange{
		current: name, role: &roleUser, members: &empty,
	}); aerr != nil {
		respondAdminError(c, aerr)
		return
	}
	// A preset naming only this group would be left naming nobody, which is
	// how a deletion that reads as "take this away" lands as "give this to
	// everyone". Refused whole rather than applied and reported.
	if exposed := presetsLeftNamingNobody(profiles, name, nil); len(exposed) > 0 {
		respondAdminError(c, exposureRefusal(exposed))
		return
	}

	// A group named only by a preset audience has no record to delete; taking
	// the name out of the presets is the whole of it.
	if !profileOnly {
		if err := app.userStore().DeleteGroup(name, app.groupsComeFromProvider()); err != nil {
			respondAdminError(c, app.groupStoreError(err))
			return
		}
	}
	// A preset still naming a deleted group would be offered to nobody, which
	// looks exactly like a preset that is broken.
	if err := app.removeGroupFromProfiles(c, name); err != nil {
		respondAdminError(c, serverError(
			"the group was deleted but the host presets still name it: %v. "+
				"Open each preset on the Session screen page to clear it.", err))
		return
	}

	log.Printf("auth: %s deleted group %q", adminActor(c), name)
	app.auditRequest(c, audit.EventGroupDeleted, audit.Success, name, nil)
	app.pushEffectiveRoles()

	c.JSON(http.StatusOK, gin.H{"deleted": true, "notes": []string{}})
}

// groupViewFor re-reads one group for the response, so what the page redraws
// is what was actually stored rather than what was asked for.
func (app *App) groupViewFor(c *gin.Context, name string) adminGroupView {
	profiles := app.publishedProfileList(c)
	info, err := app.userStore().Group(name)
	if err != nil {
		if canonical, found := profileOnlyGroupName(profiles, name); found {
			return toAdminGroupView(users.GroupInfo{
				Group: users.Group{Name: canonical},
				Role:  authz.RoleUser,
			}, profiles, app.groupsComeFromProvider())
		}
		return adminGroupView{Name: name, Role: string(authz.RoleUser), Members: []string{}, Hosts: []string{}}
	}
	return toAdminGroupView(info, profiles, app.groupsComeFromProvider())
}

// selfAdminDependsOnGroup reports whether the caller administers this instance
// only because of the named group — through its role assignment, and their
// membership of it.
//
// Read straight from the store rather than through accountFor, which reports
// the effective role: it is precisely the difference between the account's own
// role and the effective one that this question is about.
func (app *App) selfAdminDependsOnGroup(c *gin.Context, group string) bool {
	account, found, err := app.userStore().ByID(principalFrom(c).UserID)
	if err != nil || !found || account.Role == authz.RoleAdmin {
		return false
	}
	current := app.groupRolesOrNone()
	if users.EffectiveRole(account, current) != authz.RoleAdmin {
		return false
	}
	next := make(map[string]authz.Role, len(current))
	for g, r := range current {
		if !strings.EqualFold(strings.TrimSpace(g), strings.TrimSpace(group)) {
			next[g] = r
		}
	}
	return users.EffectiveRole(account, next) != authz.RoleAdmin
}

// setGroupHosts makes the group's host list exactly the named presets.
//
// This is the audience field on each preset, written from the group's side.
// The same fact seen from the other room: a preset lists the groups it is for,
// and a group lists the presets it reaches, and there is one copy of it.
//
// One write for the whole file, so a group assigned to four presets cannot end
// up on two of them because the third save failed.
func (app *App) setGroupHosts(c *gin.Context, group string, hosts []string) (changed bool, err error) {
	store, err := app.profileStoreForWrite(c, true)
	if err != nil {
		return false, err
	}

	wanted := make(map[string]bool, len(hosts))
	for _, name := range hosts {
		if name = strings.TrimSpace(name); name != "" {
			wanted[strings.ToLower(name)] = true
		}
	}

	var touched []ConnectionProfile
	_, err = store.mutate(func(profiles []ConnectionProfile) ([]ConnectionProfile, bool, error) {
		touched = nil
		anyChange := false
		for i := range profiles {
			has := audienceNamesGroup(profiles[i], group)
			want := wanted[strings.ToLower(strings.TrimSpace(profiles[i].Name))]
			if has == want {
				continue
			}
			if want {
				profiles[i].Groups = users.NormaliseGroups(
					append(append([]string{}, profiles[i].Groups...), group))
			} else {
				profiles[i].Groups = groupsWithout(profiles[i].Groups, group)
				// Preflight has already refused this, so reaching it means the
				// published set changed underneath the request. Refusing at the
				// write is what makes the rule the file's rather than the
				// page's: nothing is saved, and no preset silently opens up.
				if !profiles[i].hasAudience() {
					return nil, false, fmt.Errorf(
						"%q would be left naming nobody, so it would be offered to everyone", profiles[i].Name)
				}
			}
			normaliseAudience(&profiles[i])
			profiles[i].Shared = false
			touched = append(touched, profiles[i])
			anyChange = true
		}
		return profiles, anyChange, nil
	})
	if err != nil {
		return false, err
	}
	for _, p := range touched {
		app.auditRequest(c, audit.EventProfilePublished, audit.Success, p.Name,
			map[string]string{"host": p.displayTarget(), "audience": audienceSummary(p)})
	}
	return len(touched) > 0, nil
}

// renameGroupInProfiles follows a rename into every published preset that
// names the old spelling, in one write.
func (app *App) renameGroupInProfiles(c *gin.Context, from, to string) error {
	store, err := app.profileStoreForWrite(c, true)
	if err != nil {
		return err
	}
	var touched []ConnectionProfile
	_, err = store.mutate(func(profiles []ConnectionProfile) ([]ConnectionProfile, bool, error) {
		touched = nil
		anyChange := false
		for i := range profiles {
			renamed := false
			for j, g := range profiles[i].Groups {
				if strings.EqualFold(strings.TrimSpace(g), strings.TrimSpace(from)) {
					profiles[i].Groups[j] = to
					renamed = true
				}
			}
			if !renamed {
				continue
			}
			normaliseAudience(&profiles[i])
			profiles[i].Shared = false
			touched = append(touched, profiles[i])
			anyChange = true
		}
		return profiles, anyChange, nil
	})
	if err != nil {
		return err
	}
	for _, p := range touched {
		app.auditRequest(c, audit.EventProfilePublished, audit.Success, p.Name,
			map[string]string{"host": p.displayTarget(), "audience": audienceSummary(p)})
	}
	return nil
}

// removeGroupFromProfiles strips a deleted group's name from every preset, in
// one write.
func (app *App) removeGroupFromProfiles(c *gin.Context, name string) error {
	store, err := app.profileStoreForWrite(c, true)
	if err != nil {
		return err
	}
	var touched []ConnectionProfile
	_, err = store.mutate(func(profiles []ConnectionProfile) ([]ConnectionProfile, bool, error) {
		touched = nil
		anyChange := false
		for i := range profiles {
			if !audienceNamesGroup(profiles[i], name) {
				continue
			}
			profiles[i].Groups = groupsWithout(profiles[i].Groups, name)
			// The same guard as setGroupHosts, and for the same reason: this is
			// the write, so this is where the rule has to hold.
			if !profiles[i].hasAudience() {
				return nil, false, fmt.Errorf(
					"%q would be left naming nobody, so it would be offered to everyone", profiles[i].Name)
			}
			normaliseAudience(&profiles[i])
			profiles[i].Shared = false
			touched = append(touched, profiles[i])
			anyChange = true
		}
		return profiles, anyChange, nil
	})
	if err != nil {
		return err
	}
	for _, p := range touched {
		app.auditRequest(c, audit.EventProfilePublished, audit.Success, p.Name,
			map[string]string{"host": p.displayTarget(), "audience": audienceSummary(p)})
	}
	return nil
}

// containsFold reports whether the list holds the name, ignoring case and
// surrounding space.
func containsFold(list []string, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), name) {
			return true
		}
	}
	return false
}
