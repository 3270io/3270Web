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
	// exists only because an account or a role assignment mentions it.
	Declared  bool   `json:"declared"`
	CreatedAt string `json:"createdAt,omitempty"`
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
	views := make([]adminGroupView, 0, len(infos))
	for _, info := range infos {
		views = append(views, toAdminGroupView(info, profiles))
	}

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
		"groupsFromProvider": app.ssoEnabled() && app.sso.GroupsClaim != "",
		"self":               usernameFrom(c),
	})
}

func toAdminGroupView(info users.GroupInfo, profiles []ConnectionProfile) adminGroupView {
	view := adminGroupView{
		Name:        info.Name,
		Description: info.Description,
		Role:        string(info.Role),
		Members:     info.Members,
		MemberCount: len(info.Members),
		Hosts:       hostsNaming(profiles, info.Name),
		Declared:    info.Declared,
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
		for _, g := range p.Groups {
			if strings.EqualFold(strings.TrimSpace(g), strings.TrimSpace(group)) {
				out = append(out, p.Name)
				break
			}
		}
	}
	return out
}

// publishedProfileList reads the published set, treating a read failure as
// empty: the group page is still worth serving without the host column.
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
	// Pointers so "not mentioned" and "set to empty" stay distinguishable: a
	// PATCH that only renames must not clear the membership.
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Role        *string   `json:"role"`
	Members     *[]string `json:"members"`
	Hosts       *[]string `json:"hosts"`
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
	if req.Name == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a group name is required"})
		return
	}
	name := strings.TrimSpace(*req.Name)
	description := ""
	if req.Description != nil {
		description = strings.TrimSpace(*req.Description)
	}

	group, err := app.userStore().CreateGroup(name, description)
	if err != nil {
		if errors.Is(err, users.ErrGroupExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "a group with that name already exists"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": humanPasswordError(err)})
		return
	}

	log.Printf("auth: %s created group %q", adminActor(c), group.Name)
	app.auditRequest(c, audit.EventGroupCreated, audit.Success, group.Name, nil)

	// The dialog creates and fills in one go, so the same request may carry a
	// role, a membership and a host list. Each is applied by the same code the
	// edit path uses, so the two cannot drift apart.
	notes, err := app.applyGroupChanges(c, group.Name, req)
	if err != nil {
		// The group exists; only part of what was asked for landed. Say so
		// rather than reporting a failure that would send somebody looking for
		// a group that is plainly there.
		c.JSON(http.StatusOK, gin.H{
			"group":   app.groupViewFor(c, group.Name),
			"warning": err.Error(),
		})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"group": app.groupViewFor(c, group.Name), "notes": notes})
}

// AdminUpdateGroupHandler renames a group, describes it, sets the role it
// grants, replaces its membership, or changes the hosts it reaches.
func (app *App) AdminUpdateGroupHandler(c *gin.Context) {
	if err := app.requireAuthEnabled(c); err != nil {
		return
	}

	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing group name"})
		return
	}

	var req adminGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if _, err := app.userStore().Group(name); err != nil {
		if errors.Is(err, users.ErrGroupNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no such group"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read the group"})
		return
	}

	// A rename or a description is the group's own record, so it happens first
	// and everything after it addresses the new name.
	if req.Name != nil || req.Description != nil {
		updated, err := app.userStore().UpdateGroup(name, req.Name, req.Description)
		if err != nil {
			if errors.Is(err, users.ErrGroupExists) {
				c.JSON(http.StatusConflict, gin.H{"error": "a group with that name already exists"})
				return
			}
			if errors.Is(err, users.ErrGroupNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "no such group"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": humanPasswordError(err)})
			return
		}
		if updated.Name != name {
			// The audience of a published preset holds the name as text, and
			// the store cannot reach it. Following the rename through here is
			// what stops a renamed group from silently losing its hosts.
			app.renameGroupInProfiles(c, name, updated.Name)
			log.Printf("auth: %s renamed group %q to %q", adminActor(c), name, updated.Name)
			app.auditRequest(c, audit.EventGroupUpdated, audit.Success, updated.Name,
				map[string]string{"change": "renamed", "from": name})
		} else if req.Description != nil {
			app.auditRequest(c, audit.EventGroupUpdated, audit.Success, updated.Name,
				map[string]string{"change": "description"})
		}
		name = updated.Name
	}

	notes, err := app.applyGroupChanges(c, name, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"group": app.groupViewFor(c, name), "notes": notes})
}

// applyGroupChanges applies the parts of a request that are the same whether
// the group was just created or already existed: the role it grants, who is in
// it, and which mainframes it reaches.
//
// Returns notes — things that happened which the administrator should know
// about but which are not failures, such as a preset that now names nobody and
// so is offered to everyone.
func (app *App) applyGroupChanges(c *gin.Context, name string, req adminGroupRequest) ([]string, error) {
	var notes []string

	if req.Role != nil {
		role := authz.RoleUser
		if strings.EqualFold(*req.Role, string(authz.RoleAdmin)) {
			role = authz.RoleAdmin
		}
		// Clearing the assignment your own administrator role rests on would
		// lock you out of the page you are standing on. Refused here exactly as
		// it is on the accounts page, and for the same reason.
		if role != authz.RoleAdmin && app.selfAdminDependsOnGroup(c, name) {
			return notes, errors.New("your own administrator role comes from this group; another administrator has to change it")
		}
		if err := app.userStore().SetGroupRole(name, role); err != nil {
			return notes, errors.New(humanPasswordError(err))
		}
		log.Printf("auth: %s set group %q role to %s", adminActor(c), name, role)
		app.auditRequest(c, audit.EventGroupRoleSet, audit.Success, name,
			map[string]string{"role": string(role)})
		app.pushEffectiveRoles()
	}

	if req.Members != nil {
		wanted := *req.Members
		// Leaving a group can take a role away, so removing yourself from one
		// that grants you administration is self-demotion by another route.
		if app.selfAdminDependsOnGroup(c, name) && !containsFold(wanted, usernameFrom(c)) {
			return notes, errors.New("your administrator role comes from this group; you cannot remove yourself from it")
		}
		lockExternal := app.ssoEnabled() && app.sso.GroupsClaim != ""
		skipped, err := app.userStore().SetGroupMembers(name, wanted, lockExternal)
		if err != nil {
			if errors.Is(err, users.ErrGroupNotFound) {
				return notes, errors.New("no such group")
			}
			return notes, errors.New(humanPasswordError(err))
		}
		if len(skipped) > 0 {
			notes = append(notes, "Left alone: "+strings.Join(skipped, ", ")+
				" — their groups come from the identity provider and are refreshed at every sign-in.")
		}
		log.Printf("auth: %s set the membership of group %q (%d account(s))", adminActor(c), name, len(wanted))
		app.auditRequest(c, audit.EventGroupUpdated, audit.Success, name,
			map[string]string{"change": "members", "members": strings.Join(users.NormaliseGroups(wanted), " ")})
		// Membership can carry a role, so the people already signed in are
		// given the recomputed one rather than the one they logged in with.
		app.pushEffectiveRoles()
	}

	if req.Hosts != nil {
		opened, everyone, err := app.setGroupHosts(c, name, *req.Hosts)
		if err != nil {
			return notes, err
		}
		for _, host := range everyone {
			notes = append(notes, "\""+host+"\" now names nobody, so it is offered to everyone. "+
				"Name a group, user or role on it to narrow it again.")
		}
		if opened {
			log.Printf("admin: %s set the host list of group %q", adminActor(c), name)
			app.auditRequest(c, audit.EventGroupUpdated, audit.Success, name,
				map[string]string{"change": "hosts", "hosts": strings.Join(*req.Hosts, " ")})
		}
	}

	return notes, nil
}

// AdminDeleteGroupHandler removes a group: its record, the role it granted,
// every membership of it, and its name from every published preset.
func (app *App) AdminDeleteGroupHandler(c *gin.Context) {
	if err := app.requireAuthEnabled(c); err != nil {
		return
	}

	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing group name"})
		return
	}
	if app.selfAdminDependsOnGroup(c, name) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "your own administrator role comes from this group; another administrator has to remove it",
		})
		return
	}

	if err := app.userStore().DeleteGroup(name); err != nil {
		if errors.Is(err, users.ErrGroupNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no such group"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": humanPasswordError(err)})
		return
	}

	// A preset still naming a deleted group would be offered to nobody, which
	// looks exactly like a preset that is broken. Strip the name, and say so
	// where dropping it leaves a preset naming nobody at all.
	everyone := app.removeGroupFromProfiles(c, name)

	log.Printf("auth: %s deleted group %q", adminActor(c), name)
	app.auditRequest(c, audit.EventGroupDeleted, audit.Success, name, nil)
	app.pushEffectiveRoles()

	var notes []string
	for _, host := range everyone {
		notes = append(notes, "\""+host+"\" named only that group, so it is now offered to everyone.")
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true, "notes": notes})
}

// groupViewFor re-reads one group for the response, so what the page redraws
// is what was actually stored rather than what was asked for.
func (app *App) groupViewFor(c *gin.Context, name string) adminGroupView {
	info, err := app.userStore().Group(name)
	if err != nil {
		return adminGroupView{Name: name, Role: string(authz.RoleUser), Members: []string{}, Hosts: []string{}}
	}
	return toAdminGroupView(info, app.publishedProfileList(c))
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
// Returns whether anything changed, and the presets that ended up naming
// nobody — those are offered to everyone, which is a change worth saying out
// loud rather than leaving to be discovered.
func (app *App) setGroupHosts(c *gin.Context, group string, hosts []string) (changed bool, everyone []string, err error) {
	store, storeErr := app.profileStoreForWrite(c, true)
	if storeErr != nil {
		return false, nil, errors.New(storeErr.Error())
	}
	profiles, loadErr := store.load()
	if loadErr != nil {
		return false, nil, fmt.Errorf("could not read the host presets: %w", loadErr)
	}

	wanted := make(map[string]bool, len(hosts))
	for _, name := range hosts {
		if name = strings.TrimSpace(name); name != "" {
			wanted[strings.ToLower(name)] = true
		}
	}

	for _, p := range profiles {
		has := false
		for _, g := range p.Groups {
			if strings.EqualFold(strings.TrimSpace(g), strings.TrimSpace(group)) {
				has = true
				break
			}
		}
		want := wanted[strings.ToLower(strings.TrimSpace(p.Name))]
		if has == want {
			continue
		}
		if want {
			p.Groups = users.NormaliseGroups(append(append([]string{}, p.Groups...), group))
		} else {
			kept := make([]string, 0, len(p.Groups))
			for _, g := range p.Groups {
				if strings.EqualFold(strings.TrimSpace(g), strings.TrimSpace(group)) {
					continue
				}
				kept = append(kept, g)
			}
			p.Groups = kept
			if !p.hasAudience() {
				everyone = append(everyone, p.Name)
			}
		}
		normaliseAudience(&p)
		p.Shared = false
		if _, err := store.upsert(p); err != nil {
			return changed, everyone, fmt.Errorf("could not save the preset %q: %w", p.Name, err)
		}
		changed = true
		app.auditRequest(c, audit.EventProfilePublished, audit.Success, p.Name,
			map[string]string{"host": p.displayTarget(), "audience": audienceSummary(p)})
	}
	return changed, everyone, nil
}

// renameGroupInProfiles follows a rename into every published preset that
// names the old spelling.
func (app *App) renameGroupInProfiles(c *gin.Context, from, to string) {
	store, err := app.profileStoreForWrite(c, true)
	if err != nil {
		return
	}
	profiles, err := store.load()
	if err != nil {
		log.Printf("admin: could not re-read presets to rename group %q: %v", from, err)
		return
	}
	for _, p := range profiles {
		renamed := false
		for i, g := range p.Groups {
			if strings.EqualFold(strings.TrimSpace(g), strings.TrimSpace(from)) {
				p.Groups[i] = to
				renamed = true
			}
		}
		if !renamed {
			continue
		}
		normaliseAudience(&p)
		p.Shared = false
		if _, err := store.upsert(p); err != nil {
			log.Printf("admin: could not rename group %q in preset %q: %v", from, p.Name, err)
		}
	}
}

// removeGroupFromProfiles strips a deleted group's name from every preset,
// returning the ones left naming nobody.
func (app *App) removeGroupFromProfiles(c *gin.Context, name string) []string {
	store, err := app.profileStoreForWrite(c, true)
	if err != nil {
		return nil
	}
	profiles, err := store.load()
	if err != nil {
		log.Printf("admin: could not re-read presets to drop group %q: %v", name, err)
		return nil
	}
	var everyone []string
	for _, p := range profiles {
		kept := make([]string, 0, len(p.Groups))
		dropped := false
		for _, g := range p.Groups {
			if strings.EqualFold(strings.TrimSpace(g), strings.TrimSpace(name)) {
				dropped = true
				continue
			}
			kept = append(kept, g)
		}
		if !dropped {
			continue
		}
		p.Groups = kept
		if !p.hasAudience() {
			everyone = append(everyone, p.Name)
		}
		normaliseAudience(&p)
		p.Shared = false
		if _, err := store.upsert(p); err != nil {
			log.Printf("admin: could not drop group %q from preset %q: %v", name, p.Name, err)
		}
	}
	return everyone
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
