// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/task"
)

// A library is only worth having if what one deployment writes another can
// read, so most of these tests run a real router at each end and move the
// bytes between them rather than calling the two halves in one process.

func libraryTaskNamed(name string) task.Task {
	return task.Task{
		Name:        name,
		Description: "Retrieves the current cleared balance.",
		Parameters: []task.Parameter{{
			Name: "account_number", Label: "Account number", Required: true,
		}},
		Steps: []task.Step{{
			Description: "Enter the account number",
			Expect:      []task.Expect{{Row: 1, Column: 1, Text: "ACCOUNT ENQUIRY"}},
			Inputs:      []task.Input{{Row: 5, Column: 20, Parameter: "account_number"}},
			AIDKey:      "Enter",
		}},
		Outputs: []task.Output{{
			Name: "cleared_balance", Label: "Cleared balance",
			Row: 8, Column: 20, Length: 12,
		}},
	}
}

func libraryProfileNamed(name string) ConnectionProfile {
	return ConnectionProfile{Name: name, Host: "mainframe.example.test", Port: 992, TLS: true}
}

// newLibraryTestSite builds an instance with accounts, and returns a token for
// an administrator of it.
func newLibraryTestSite(t *testing.T) (*App, *gin.Engine, string) {
	t.Helper()
	app, r := newAuthTestApp(t, "local")
	admin := addUser(t, app, "root", authz.RoleAdmin, false)
	return app, r, issueToken(t, app, admin.ID, "library", nil)
}

func exportLibraryVia(t *testing.T, r *gin.Engine, token, query string) (string, libraryDocument) {
	t.Helper()
	w := apiGet(r, "/api/v1/library"+query, token)
	if w.Code != http.StatusOK {
		t.Fatalf("export = %d (%s)", w.Code, w.Body.String())
	}
	var doc libraryDocument
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("export is not a library document: %v", err)
	}
	return w.Body.String(), doc
}

func importLibraryVia(t *testing.T, r *gin.Engine, token, query, body string) (int, libraryReport) {
	t.Helper()
	w := apiCall(r, http.MethodPost, "/api/v1/library"+query, token, body)
	var answer struct {
		Error  string        `json:"error"`
		Report libraryReport `json:"report"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil {
		t.Fatalf("import answered %d with %q, which is not JSON: %v", w.Code, w.Body.String(), err)
	}
	return w.Code, answer.Report
}

func seedLibrary(t *testing.T, app *App, r *gin.Engine, token string) {
	t.Helper()
	doc := libraryDocument{
		FormatVersion: libraryFormatVersion,
		Tasks:         []task.Task{libraryTaskNamed("Account balance enquiry")},
		Profiles:      []ConnectionProfile{libraryProfileNamed("Production TSO")},
	}
	body, _ := json.Marshal(doc)
	if code, report := importLibraryVia(t, r, token, "", string(body)); code != http.StatusOK || report.Added != 2 {
		t.Fatalf("seed import = %d, added %d", code, report.Added)
	}
}

// The property the whole feature rests on: what one instance hands out is what
// the next one takes, with no conversion between them.
func TestLibraryMovesBetweenDeployments(t *testing.T) {
	source, sourceRouter, sourceToken := newLibraryTestSite(t)
	seedLibrary(t, source, sourceRouter, sourceToken)

	body, doc := exportLibraryVia(t, sourceRouter, sourceToken, "?download=1")
	if len(doc.Tasks) != 1 || len(doc.Profiles) != 1 {
		t.Fatalf("export carried %d task(s) and %d preset(s)", len(doc.Tasks), len(doc.Profiles))
	}
	if doc.FormatVersion != libraryFormatVersion {
		t.Errorf("formatVersion = %d, want %d", doc.FormatVersion, libraryFormatVersion)
	}

	_, target, targetToken := newLibraryTestSite(t)
	code, report := importLibraryVia(t, target, targetToken, "", body)
	if code != http.StatusOK {
		t.Fatalf("import = %d, report %+v", code, report)
	}
	if report.Added != 2 || report.Rejected != 0 {
		t.Fatalf("import added %d, rejected %d: %+v", report.Added, report.Rejected, report.Entries)
	}

	_, arrived := exportLibraryVia(t, target, targetToken, "")
	if len(arrived.Tasks) != 1 || arrived.Tasks[0].Name != "Account balance enquiry" {
		t.Errorf("tasks at the far end: %+v", arrived.Tasks)
	}
	if len(arrived.Profiles) != 1 || arrived.Profiles[0].Host != "mainframe.example.test" {
		t.Errorf("presets at the far end: %+v", arrived.Profiles)
	}
	if !arrived.Profiles[0].TLS {
		t.Error("TLS did not survive the trip, so the preset connects in the clear")
	}
}

// A downloaded file is offered as one.
func TestLibraryDownloadIsAnAttachment(t *testing.T) {
	_, r, token := newLibraryTestSite(t)
	w := apiGet(r, "/api/v1/library?download=1", token)
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") {
		t.Errorf("Content-Disposition = %q", got)
	}
	if w := apiGet(r, "/api/v1/library", token); w.Header().Get("Content-Disposition") != "" {
		t.Error("an ordinary export offered itself as a download")
	}
}

func TestLibraryIncludeNarrowsTheDocument(t *testing.T) {
	app, r, token := newLibraryTestSite(t)
	seedLibrary(t, app, r, token)

	if _, doc := exportLibraryVia(t, r, token, "?include=tasks"); len(doc.Profiles) != 0 || len(doc.Tasks) != 1 {
		t.Errorf("include=tasks gave %d task(s) and %d preset(s)", len(doc.Tasks), len(doc.Profiles))
	}
	if _, doc := exportLibraryVia(t, r, token, "?include=profiles"); len(doc.Tasks) != 0 || len(doc.Profiles) != 1 {
		t.Errorf("include=profiles gave %d task(s) and %d preset(s)", len(doc.Tasks), len(doc.Profiles))
	}
	if w := apiGet(r, "/api/v1/library?include=everything", token); w.Code != http.StatusBadRequest {
		t.Errorf("include=everything = %d, want 400", w.Code)
	}
}

// An existing name is left alone unless replacing was asked for, and either
// way the report says which it was.
func TestLibraryImportConflictPolicy(t *testing.T) {
	app, r, token := newLibraryTestSite(t)
	seedLibrary(t, app, r, token)

	changed := libraryProfileNamed("Production TSO")
	changed.Host = "somewhere.else.test"
	doc := libraryDocument{FormatVersion: libraryFormatVersion, Profiles: []ConnectionProfile{changed}}
	body, _ := json.Marshal(doc)

	code, report := importLibraryVia(t, r, token, "", string(body))
	if code != http.StatusOK || report.Skipped != 1 || report.Replaced != 0 {
		t.Fatalf("default policy: %d, skipped %d, replaced %d", code, report.Skipped, report.Replaced)
	}
	if _, arrived := exportLibraryVia(t, r, token, "?include=profiles"); arrived.Profiles[0].Host != "mainframe.example.test" {
		t.Error("a skipped preset was overwritten anyway")
	}

	code, report = importLibraryVia(t, r, token, "?onConflict=replace", string(body))
	if code != http.StatusOK || report.Replaced != 1 {
		t.Fatalf("replace policy: %d, replaced %d", code, report.Replaced)
	}
	if _, arrived := exportLibraryVia(t, r, token, "?include=profiles"); arrived.Profiles[0].Host != "somewhere.else.test" {
		t.Error("replace did not replace")
	}

	if w := apiCall(r, http.MethodPost, "/api/v1/library?onConflict=merge", token, string(body)); w.Code != http.StatusBadRequest {
		t.Errorf("an invented conflict policy = %d, want 400", w.Code)
	}
}

// The dry run is the whole reason an administrator can point this at a
// production instance: it answers with what would happen and touches nothing.
func TestLibraryDryRunWritesNothing(t *testing.T) {
	_, r, token := newLibraryTestSite(t)
	doc := libraryDocument{
		FormatVersion: libraryFormatVersion,
		Tasks:         []task.Task{libraryTaskNamed("Account balance enquiry")},
		Profiles:      []ConnectionProfile{libraryProfileNamed("Production TSO")},
	}
	body, _ := json.Marshal(doc)

	code, report := importLibraryVia(t, r, token, "?dryRun=true", string(body))
	if code != http.StatusOK {
		t.Fatalf("dry run = %d", code)
	}
	if !report.DryRun || report.Added != 2 {
		t.Fatalf("dry run reported %+v", report)
	}

	_, after := exportLibraryVia(t, r, token, "")
	if len(after.Tasks) != 0 || len(after.Profiles) != 0 {
		t.Errorf("a dry run stored %d task(s) and %d preset(s)", len(after.Tasks), len(after.Profiles))
	}
}

// One entry this build refuses refuses the file. Storing the good half and
// stopping is the state that leaves two deployments quietly different, which
// is the thing a library exists to prevent.
func TestLibraryRefusesTheWholeFileWhenAnEntryIsBad(t *testing.T) {
	_, r, token := newLibraryTestSite(t)

	bad := libraryTaskNamed("Broken")
	bad.Steps = nil // a task needs at least one step
	doc := libraryDocument{
		FormatVersion: libraryFormatVersion,
		Tasks:         []task.Task{libraryTaskNamed("Account balance enquiry"), bad},
		Profiles:      []ConnectionProfile{libraryProfileNamed("Production TSO")},
	}
	body, _ := json.Marshal(doc)

	code, report := importLibraryVia(t, r, token, "", string(body))
	if code != http.StatusBadRequest {
		t.Fatalf("import = %d, want 400", code)
	}
	if report.Rejected != 1 {
		t.Fatalf("rejected %d entries: %+v", report.Rejected, report.Entries)
	}
	var named bool
	for _, e := range report.Entries {
		if e.Name == "Broken" && e.Action == libraryRejected && e.Reason != "" {
			named = true
		}
	}
	if !named {
		t.Errorf("the report does not say which entry was refused, or why: %+v", report.Entries)
	}

	_, after := exportLibraryVia(t, r, token, "")
	if len(after.Tasks) != 0 || len(after.Profiles) != 0 {
		t.Errorf("a refused import stored %d task(s) and %d preset(s)", len(after.Tasks), len(after.Profiles))
	}
}

func TestLibraryRefusesADocumentThatNamesSomethingTwice(t *testing.T) {
	_, r, token := newLibraryTestSite(t)
	doc := libraryDocument{
		FormatVersion: libraryFormatVersion,
		Profiles: []ConnectionProfile{
			libraryProfileNamed("Production TSO"),
			{Name: "production tso", Host: "other.example.test", Port: 23},
		},
	}
	body, _ := json.Marshal(doc)
	if code, report := importLibraryVia(t, r, token, "", string(body)); code != http.StatusBadRequest || report.Rejected != 1 {
		t.Fatalf("two presets under one name: %d, rejected %d", code, report.Rejected)
	}
}

func TestLibraryFromANewerBuildIsRefusedByName(t *testing.T) {
	_, r, token := newLibraryTestSite(t)
	body, _ := json.Marshal(libraryDocument{
		FormatVersion: libraryFormatVersion + 1,
		Profiles:      []ConnectionProfile{libraryProfileNamed("Production TSO")},
	})
	w := apiCall(r, http.MethodPost, "/api/v1/library", token, string(body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("= %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "newer build") {
		t.Errorf("the refusal does not say why: %s", w.Body.String())
	}
}

func TestLibraryRefusesAnEmptyDocument(t *testing.T) {
	_, r, token := newLibraryTestSite(t)
	body, _ := json.Marshal(libraryDocument{FormatVersion: libraryFormatVersion})
	if w := apiCall(r, http.MethodPost, "/api/v1/library", token, string(body)); w.Code != http.StatusBadRequest {
		t.Errorf("an empty library = %d, want 400", w.Code)
	}
}

// A preset offered to named accounts must not arrive at another deployment
// either naming them or, worse, offered to everybody because the names were
// dropped.
func TestLibraryDoesNotCarryNamedAccountsOrWidenTheirAudience(t *testing.T) {
	app, r, token := newLibraryTestSite(t)

	restricted := libraryProfileNamed("Payments only")
	restricted.Users = []string{"j.okafor", "a.silva"}
	withGroup := libraryProfileNamed("Payments and ops")
	withGroup.Users = []string{"j.okafor"}
	withGroup.Groups = []string{"operations"}

	store, scope := app.libraryProfileTarget(adminContext(t))
	if scope != libraryScopePublished {
		t.Fatalf("an administrator's library covers %q, want the published list", scope)
	}
	for _, p := range []ConnectionProfile{restricted, withGroup} {
		if _, err := store.upsert(p); err != nil {
			t.Fatalf("seed preset: %v", err)
		}
	}

	body, doc := exportLibraryVia(t, r, token, "?include=profiles")
	if strings.Contains(body, "okafor") || strings.Contains(body, "silva") {
		t.Error("the exported file names accounts that exist only on this deployment")
	}
	byName := map[string]ConnectionProfile{}
	for _, p := range doc.Profiles {
		byName[p.Name] = p
	}
	if p := byName["Payments only"]; !p.NotOffered {
		t.Error("a preset whose only audience was named accounts arrived offered to everyone")
	}
	if p := byName["Payments and ops"]; p.NotOffered || len(p.Groups) != 1 || p.Groups[0] != "operations" {
		t.Errorf("the group audience did not survive: %+v", p)
	}
	if len(doc.Notes) == 0 {
		t.Error("nothing in the file says why an audience is narrower than it was")
	}
}

// adminContext builds a gin context carrying an administrator principal, for
// the few checks that are about which store a request resolves to rather than
// about what a handler answers.
func adminContext(t *testing.T) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/library", nil)
	c.Set(principalContextKey, authz.Principal{
		UserID: "0000000000000000000000000000ffff",
		Role:   authz.RoleAdmin,
		Kind:   authz.KindAPIToken,
	})
	return c
}

// Publishing to everyone is an administrator's call, so an ordinary account's
// library is their own list — not a refusal, and not a way into the published
// one.
func TestLibraryImportByANonAdminStaysOutOfThePublishedList(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	admin := addUser(t, app, "root", authz.RoleAdmin, false)
	user := addUser(t, app, "alice", authz.RoleUser, false)
	adminToken := issueToken(t, app, admin.ID, "admin", nil)
	userToken := issueToken(t, app, user.ID, "alice", nil)

	body, _ := json.Marshal(libraryDocument{
		FormatVersion: libraryFormatVersion,
		Profiles:      []ConnectionProfile{libraryProfileNamed("Alice's sandbox")},
	})
	code, report := importLibraryVia(t, r, userToken, "", string(body))
	if code != http.StatusOK || report.Added != 1 {
		t.Fatalf("a user's import = %d, added %d", code, report.Added)
	}
	if report.ProfileScope != libraryScopeOwn {
		t.Errorf("profileScope = %q, want %q", report.ProfileScope, libraryScopeOwn)
	}

	if _, published := exportLibraryVia(t, r, adminToken, "?include=profiles"); len(published.Profiles) != 0 {
		t.Errorf("a user's import reached the published host list: %+v", published.Profiles)
	}
	if _, own := exportLibraryVia(t, r, userToken, "?include=profiles"); len(own.Profiles) != 1 {
		t.Errorf("the user's own list holds %d preset(s)", len(own.Profiles))
	}
}

// Tasks are one account's own, and a library must not become a way to read
// somebody else's catalogue.
func TestLibraryTasksStayWithTheAccountThatImportedThem(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	bob := addUser(t, app, "bob", authz.RoleUser, false)
	aliceToken := issueToken(t, app, alice.ID, "alice", nil)
	bobToken := issueToken(t, app, bob.ID, "bob", nil)

	body, _ := json.Marshal(libraryDocument{
		FormatVersion: libraryFormatVersion,
		Tasks:         []task.Task{libraryTaskNamed("Account balance enquiry")},
	})
	if code, _ := importLibraryVia(t, r, aliceToken, "", string(body)); code != http.StatusOK {
		t.Fatalf("alice's import = %d", code)
	}
	if _, bobs := exportLibraryVia(t, r, bobToken, "?include=tasks"); len(bobs.Tasks) != 0 {
		t.Errorf("bob's library holds alice's tasks: %+v", bobs.Tasks)
	}
}

// A file with no formatVersion is read as this version rather than refused:
// it can only have been hand-written, and refusing a document that is
// otherwise exactly right teaches nobody anything.
func TestLibraryWithoutAFormatVersionIsRead(t *testing.T) {
	_, r, token := newLibraryTestSite(t)
	body := `{"profiles":[{"name":"Hand written","host":"mainframe.example.test","port":23}]}`
	if code, report := importLibraryVia(t, r, token, "", body); code != http.StatusOK || report.Added != 1 {
		t.Fatalf("= %d, added %d", code, report.Added)
	}
}

func TestLibraryIncludeParsing(t *testing.T) {
	for _, tc := range []struct {
		in                     string
		wantTasks, wantProfile bool
		wantErr                bool
	}{
		{in: "", wantTasks: true, wantProfile: true},
		{in: "tasks", wantTasks: true},
		{in: "profiles", wantProfile: true},
		{in: "presets", wantProfile: true},
		{in: "tasks,profiles", wantTasks: true, wantProfile: true},
		{in: " TASKS , profiles ", wantTasks: true, wantProfile: true},
		{in: "themes", wantErr: true},
		{in: ",", wantErr: true},
	} {
		tasks, profiles, err := parseLibraryInclude(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("include=%q was accepted", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("include=%q: %v", tc.in, err)
			continue
		}
		if tasks != tc.wantTasks || profiles != tc.wantProfile {
			t.Errorf("include=%q gave tasks=%v profiles=%v", tc.in, tasks, profiles)
		}
	}
}
