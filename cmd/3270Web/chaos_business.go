// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/session"
)

// Business-understanding endpoints. These let the AI chat read discovered
// screens, attach business annotations (screen purpose + field meanings),
// catalog named business functions, and generate business-focused workflow
// JSON files. Annotations are stored in the chaos mind map so they persist
// with saved runs and travel through mind-map export/import.

const chaosScreensPreviewMaxLines = 12

// withLoadedRun runs fn against the session's loaded chaos run while holding
// the store mutex, so annotation writes cannot race concurrent status/export
// reads of the same *SavedRun.
func (s *chaosEngineStore) withLoadedRun(sessionID string, fn func(*chaos.SavedRun) error) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.loadedRuns[sessionID]
	if !ok || run == nil {
		return false, nil
	}
	return true, fn(run)
}

// sessionChaosRun resolves the session's saved-run data: the loaded run
// first, then the auto-saved run on disk (cached as the loaded run for
// subsequent calls). Shared by the export, screens, and business handlers so
// the fallback order lives in one place.
func (app *App) sessionChaosRun(s *session.Session) (*chaos.SavedRun, bool) {
	if run, ok := app.chaosEngines.getLoadedRun(s.ID); ok && run != nil {
		return run, true
	}
	if run := app.loadSessionChaosRunFromDisk(s); run != nil {
		app.chaosEngines.setLoadedRun(s.ID, run)
		return run, true
	}
	return nil, false
}

// sessionChaosMindMap resolves the freshest mind map for a session: live
// engine first, then the loaded run, then the auto-saved run on disk.
func (app *App) sessionChaosMindMap(s *session.Session) *chaos.MindMap {
	if eng, ok := app.chaosEngines.get(s.ID); ok {
		if mm := eng.MindMapSnapshot(); mm != nil {
			return mm
		}
	}
	if run, ok := app.sessionChaosRun(s); ok {
		return run.MindMap
	}
	return nil
}

// ChaosScreensListHandler handles GET /chaos/screens – returns every
// discovered screen with field metadata, learned values, key destinations and
// any business annotations, so the AI can review the application and infer
// business meaning. Pass ?include_previews=false to omit screen preview text.
func (app *App) ChaosScreensListHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	if app.chaosEngines.isRemoved(s.ID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "no chaos run data for this session"})
		return
	}
	mm := app.sessionChaosMindMap(s)
	if mm == nil || len(mm.Areas) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no screens discovered yet; run chaos exploration first"})
		return
	}
	includePreviews := c.DefaultQuery("include_previews", "true") != "false"

	screens := make([]gin.H, 0, len(mm.Areas))
	for hash, area := range mm.Areas {
		if area == nil {
			continue
		}
		entry := gin.H{
			"hash":            hash,
			"label":           area.Label,
			"visits":          area.Visits,
			"inputFieldCount": area.InputFieldCount,
		}
		if area.BusinessPurpose != "" {
			entry["businessPurpose"] = area.BusinessPurpose
		}
		if area.BusinessNotes != "" {
			entry["businessNotes"] = area.BusinessNotes
		}
		if len(area.FieldSemantics) > 0 {
			entry["fieldSemantics"] = area.FieldSemantics
		}
		if len(area.FieldMetadata) > 0 {
			entry["fieldMetadata"] = area.FieldMetadata
		}
		if len(area.KnownWorkingValues) > 0 {
			entry["knownWorkingValues"] = area.KnownWorkingValues
		}
		if len(area.KeyPresses) > 0 {
			keys := make(gin.H, len(area.KeyPresses))
			for key, kp := range area.KeyPresses {
				if kp == nil {
					continue
				}
				keys[key] = gin.H{
					"presses":      kp.Presses,
					"progressions": kp.Progressions,
					"destinations": kp.Destinations,
				}
			}
			entry["keyPresses"] = keys
		}
		if includePreviews && area.PreviewText != "" {
			entry["previewText"] = truncatePreviewLines(area.PreviewText, chaosScreensPreviewMaxLines)
		}
		screens = append(screens, entry)
	}
	sort.Slice(screens, func(i, j int) bool {
		vi, _ := screens[i]["visits"].(int)
		vj, _ := screens[j]["visits"].(int)
		if vi != vj {
			return vi > vj
		}
		hi, _ := screens[i]["hash"].(string)
		hj, _ := screens[j]["hash"].(string)
		return hi < hj
	})
	c.JSON(http.StatusOK, gin.H{
		"screenCount":       len(screens),
		"screens":           screens,
		"businessFunctions": businessFunctionNamesOf(mm),
	})
}

func truncatePreviewLines(preview string, maxLines int) string {
	lines := strings.Split(preview, "\n")
	if len(lines) <= maxLines {
		return preview
	}
	return strings.Join(lines[:maxLines], "\n") + "\n…"
}

func businessFunctionNamesOf(mm *chaos.MindMap) []string {
	fns := chaos.BusinessFunctionsOf(mm)
	names := make([]string, 0, len(fns))
	for _, fn := range fns {
		names = append(names, fn.Name)
	}
	return names
}

// overviewMaxScreens bounds how many screens the business overview lists so a
// large discovered application cannot blow past the model's context window.
const overviewMaxScreens = 60

// overviewMaxNavPerScreen bounds the navigation edges reported per screen.
const overviewMaxNavPerScreen = 6

// ChaosBusinessOverviewHandler handles GET /chaos/business/overview – returns a
// synthesized business model of the whole application: coverage stats, every
// discovered screen with its business purpose / key fields / navigation, the
// cataloged business functions, and the explicit gaps in current understanding
// so the AI knows what to investigate next.
func (app *App) ChaosBusinessOverviewHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	if app.chaosEngines.isRemoved(s.ID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "no chaos run data for this session"})
		return
	}
	mm := app.sessionChaosMindMap(s)
	if mm == nil || len(mm.Areas) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no screens discovered yet; run chaos exploration first"})
		return
	}
	c.JSON(http.StatusOK, buildBusinessAppOverview(mm))
}

// labelForHash returns a short human label for a screen hash, falling back to a
// truncated hash when the area has no label.
func labelForHash(mm *chaos.MindMap, hash string) string {
	if mm != nil {
		if area, ok := mm.Areas[hash]; ok && area != nil && area.Label != "" {
			return area.Label
		}
	}
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

// areaHasProgression reports whether any AID key on the area has caused a
// screen transition (i.e. the screen is not a dead end).
func areaHasProgression(area *chaos.MindMapArea) bool {
	if area == nil {
		return false
	}
	for _, kp := range area.KeyPresses {
		if kp != nil && kp.Progressions > 0 {
			return true
		}
	}
	return false
}

// buildBusinessAppOverview synthesizes the mind map into the business-overview
// payload. Pure function of the mind map so it is straightforward to test.
func buildBusinessAppOverview(mm *chaos.MindMap) gin.H {
	// Stable screen ordering: most-visited first, then hash for determinism.
	type areaEntry struct {
		hash string
		area *chaos.MindMapArea
	}
	areas := make([]areaEntry, 0, len(mm.Areas))
	for hash, area := range mm.Areas {
		if area == nil {
			continue
		}
		areas = append(areas, areaEntry{hash: hash, area: area})
	}
	sort.Slice(areas, func(i, j int) bool {
		if areas[i].area.Visits != areas[j].area.Visits {
			return areas[i].area.Visits > areas[j].area.Visits
		}
		return areas[i].hash < areas[j].hash
	})

	var (
		annotated       int
		withTransitions int
		totalInputs     int
		unannotated     []gin.H
		noWorkingValues []gin.H
		deadEnds        []gin.H
	)

	screens := make([]gin.H, 0, len(areas))
	for _, ae := range areas {
		area := ae.area
		totalInputs += area.InputFieldCount
		if area.BusinessPurpose != "" {
			annotated++
		}
		hasProg := areaHasProgression(area)
		if hasProg {
			withTransitions++
		}

		label := area.Label
		if label == "" {
			label = labelForHash(mm, ae.hash)
		}

		// Track gaps (independent of the per-screen list cap below).
		if area.BusinessPurpose == "" {
			unannotated = append(unannotated, gin.H{"hash": ae.hash, "label": label})
		}
		if area.InputFieldCount > 0 && len(area.KnownWorkingValues) == 0 {
			noWorkingValues = append(noWorkingValues, gin.H{
				"hash": ae.hash, "label": label, "inputFieldCount": area.InputFieldCount,
			})
		}
		if !hasProg {
			deadEnds = append(deadEnds, gin.H{"hash": ae.hash, "label": label})
		}

		if len(screens) >= overviewMaxScreens {
			continue
		}

		entry := gin.H{
			"hash":            ae.hash,
			"label":           label,
			"visits":          area.Visits,
			"inputFieldCount": area.InputFieldCount,
		}
		if area.BusinessPurpose != "" {
			entry["businessPurpose"] = area.BusinessPurpose
		}
		if area.BusinessNotes != "" {
			entry["businessNotes"] = area.BusinessNotes
		}
		if keyFields := overviewKeyFields(area); len(keyFields) > 0 {
			entry["keyFields"] = keyFields
		}
		if nav := overviewNavigation(mm, area); len(nav) > 0 {
			entry["navigation"] = nav
		}
		screens = append(screens, entry)
	}

	// Business functions + the examples gap.
	fns := chaos.BusinessFunctionsOf(mm)
	funcList := make([]gin.H, 0, len(fns))
	var funcsMissingExamples []string
	for _, fn := range fns {
		params := make([]gin.H, 0, len(fn.Parameters))
		missing := false
		for _, p := range fn.Parameters {
			if p.Required && p.Example == "" {
				missing = true
			}
			params = append(params, gin.H{
				"name":     p.Name,
				"example":  p.Example,
				"required": p.Required,
			})
		}
		if missing {
			funcsMissingExamples = append(funcsMissingExamples, fn.Name)
		}
		funcList = append(funcList, gin.H{
			"name":        fn.Name,
			"description": fn.Description,
			"parameters":  params,
		})
	}

	return gin.H{
		"coverage": gin.H{
			"screensDiscovered":      len(areas),
			"screensAnnotated":       annotated,
			"screensWithTransitions": withTransitions,
			"totalInputFields":       totalInputs,
			"businessFunctions":      len(funcList),
		},
		"screens":           screens,
		"screensTruncated":  len(areas) > len(screens),
		"businessFunctions": funcList,
		"gaps": gin.H{
			"unannotatedScreens":       unannotated,
			"screensWithoutValues":     noWorkingValues,
			"deadEndScreens":           deadEnds,
			"functionsMissingExamples": funcsMissingExamples,
		},
	}
}

// overviewKeyFields merges field metadata with any business semantics into a
// compact per-field list for the overview.
func overviewKeyFields(area *chaos.MindMapArea) []gin.H {
	if area == nil || len(area.FieldMetadata) == 0 {
		return nil
	}
	keys := make([]string, 0, len(area.FieldMetadata))
	for k := range area.FieldMetadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]gin.H, 0, len(keys))
	for _, k := range keys {
		meta := area.FieldMetadata[k]
		f := gin.H{
			"key":     k,
			"numeric": meta.Numeric,
			"hidden":  meta.Hidden,
		}
		if sem, ok := area.FieldSemantics[k]; ok {
			f["name"] = sem.Name
			if sem.Example != "" {
				f["example"] = sem.Example
			}
			if sem.Sensitive {
				f["sensitive"] = true
			}
		} else if vals, ok := area.KnownWorkingValues[k]; ok && len(vals) > 0 && !meta.Hidden {
			// No business name yet, but we know a value that worked.
			f["exampleWorkingValue"] = vals[len(vals)-1]
		}
		out = append(out, f)
	}
	return out
}

// overviewNavigation lists where each AID key on the screen leads, most
// productive first, bounded by overviewMaxNavPerScreen.
func overviewNavigation(mm *chaos.MindMap, area *chaos.MindMapArea) []gin.H {
	if area == nil || len(area.KeyPresses) == 0 {
		return nil
	}
	type navEdge struct {
		key    string
		toHash string
		count  int
	}
	edges := make([]navEdge, 0)
	for key, kp := range area.KeyPresses {
		if kp == nil || kp.Progressions == 0 {
			continue
		}
		for toHash, count := range kp.Destinations {
			edges = append(edges, navEdge{key: key, toHash: toHash, count: count})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].count != edges[j].count {
			return edges[i].count > edges[j].count
		}
		if edges[i].key != edges[j].key {
			return edges[i].key < edges[j].key
		}
		return edges[i].toHash < edges[j].toHash
	})
	if len(edges) > overviewMaxNavPerScreen {
		edges = edges[:overviewMaxNavPerScreen]
	}
	out := make([]gin.H, 0, len(edges))
	for _, e := range edges {
		out = append(out, gin.H{
			"key":     e.key,
			"toLabel": labelForHash(mm, e.toHash),
			"toHash":  e.toHash,
			"count":   e.count,
		})
	}
	return out
}

// chaosScreenAnnotateRequest is the JSON body for POST /chaos/screens/annotate.
type chaosScreenAnnotateRequest struct {
	ScreenHash      string                                 `json:"screen_hash"`
	BusinessPurpose string                                 `json:"business_purpose"`
	Notes           string                                 `json:"notes"`
	FieldSemantics  map[string]chaos.BusinessFieldSemantic `json:"field_semantics"`
}

// ChaosScreenAnnotateHandler handles POST /chaos/screens/annotate – records a
// business purpose and per-field semantics for a discovered screen.
func (app *App) ChaosScreenAnnotateHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var req chaosScreenAnnotateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.ScreenHash) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "screen_hash is required"})
		return
	}
	if err := app.applyChaosBusinessWrite(s.ID, func(eng *chaos.Engine) error {
		return eng.AnnotateArea(req.ScreenHash, req.BusinessPurpose, req.Notes, req.FieldSemantics)
	}, func(run *chaos.SavedRun) error {
		return chaos.AnnotateSavedRun(run, req.ScreenHash, req.BusinessPurpose, req.Notes, req.FieldSemantics)
	}); err != nil {
		respondChaosBusinessWriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "screenHash": strings.TrimSpace(req.ScreenHash)})
}

// chaosBusinessFunctionRequest is the JSON body for POST /chaos/business/functions.
type chaosBusinessFunctionRequest struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	EntryScreenHash string `json:"entry_screen_hash"`
	Steps           []struct {
		ScreenHash string `json:"screen_hash"`
		Inputs     []struct {
			FieldKey  string `json:"field_key"`
			Value     string `json:"value"`
			Parameter string `json:"parameter"`
		} `json:"inputs"`
		AIDKey     string `json:"aid_key"`
		ExpectHash string `json:"expect_hash"`
	} `json:"steps"`
	Parameters []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		ScreenHash  string `json:"screen_hash"`
		FieldKey    string `json:"field_key"`
		Example     string `json:"example"`
		Required    bool   `json:"required"`
	} `json:"parameters"`
}

func (r chaosBusinessFunctionRequest) toBusinessFunction() chaos.BusinessFunction {
	fn := chaos.BusinessFunction{
		Name:            r.Name,
		Description:     r.Description,
		EntryScreenHash: r.EntryScreenHash,
	}
	for _, step := range r.Steps {
		out := chaos.BusinessFunctionStep{
			ScreenHash: step.ScreenHash,
			AIDKey:     step.AIDKey,
			ExpectHash: step.ExpectHash,
		}
		for _, input := range step.Inputs {
			out.Inputs = append(out.Inputs, chaos.BusinessFunctionInput{
				FieldKey:  input.FieldKey,
				Value:     input.Value,
				Parameter: input.Parameter,
			})
		}
		fn.Steps = append(fn.Steps, out)
	}
	for _, p := range r.Parameters {
		fn.Parameters = append(fn.Parameters, chaos.BusinessParameter{
			Name:        p.Name,
			Description: p.Description,
			ScreenHash:  p.ScreenHash,
			FieldKey:    p.FieldKey,
			Example:     p.Example,
			Required:    p.Required,
		})
	}
	return fn
}

// ChaosBusinessFunctionSaveHandler handles POST /chaos/business/functions –
// adds or replaces a named business function in the catalog.
func (app *App) ChaosBusinessFunctionSaveHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var req chaosBusinessFunctionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: " + err.Error()})
		return
	}
	fn := req.toBusinessFunction()
	if err := app.applyChaosBusinessWrite(s.ID, func(eng *chaos.Engine) error {
		return eng.UpsertBusinessFunction(fn)
	}, func(run *chaos.SavedRun) error {
		return chaos.UpsertSavedRunBusinessFunction(run, fn)
	}); err != nil {
		respondChaosBusinessWriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "name": strings.TrimSpace(fn.Name)})
}

// ChaosBusinessFunctionsListHandler handles GET /chaos/business/functions.
func (app *App) ChaosBusinessFunctionsListHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	mm := app.sessionChaosMindMap(s)
	fns := chaos.BusinessFunctionsOf(mm)
	if fns == nil {
		fns = []chaos.BusinessFunction{}
	}
	c.JSON(http.StatusOK, gin.H{"functions": fns})
}

// chaosBusinessGenerateRequest is the JSON body for POST /chaos/business/generate-workflow.
type chaosBusinessGenerateRequest struct {
	Name       string            `json:"name"`
	Parameters map[string]string `json:"parameters"`
	Host       string            `json:"host"`
	Port       int               `json:"port"`
}

// ChaosBusinessGenerateWorkflowHandler handles POST /chaos/business/generate-workflow –
// resolves a cataloged business function and parameter values into a
// playback-compatible workflow JSON document.
func (app *App) ChaosBusinessGenerateWorkflowHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var req chaosBusinessGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	targetHost := strings.TrimSpace(req.Host)
	targetPort := req.Port
	withSessionLock(s, func() {
		if targetHost == "" {
			targetHost = s.TargetHost
		}
		if targetPort == 0 {
			targetPort = s.TargetPort
		}
	})

	var data []byte
	var err error
	if eng, ok := app.chaosEngines.get(s.ID); ok {
		data, err = eng.GenerateBusinessWorkflow(req.Name, req.Parameters, targetHost, targetPort)
	} else if run, ok := app.sessionChaosRun(s); ok {
		data, err = chaos.GenerateBusinessWorkflowFromSavedRun(run, req.Name, req.Parameters, targetHost, targetPort)
	} else {
		c.JSON(http.StatusNotFound, gin.H{"error": "no chaos run data for this session"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Validate the generated workflow parses with the standard loader before
	// handing it to the user.
	if _, parseErr := parseWorkflowPayload(data); parseErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "generated workflow failed validation: " + parseErr.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

// applyChaosBusinessWrite routes a business-knowledge write to the live
// engine when one exists, otherwise to the loaded run, loading the
// auto-saved run from disk if needed. Both paths run under the store mutex
// (withEngine / withLoadedRun), so a write can never fall into the gap where
// completeEngine snapshots and unregisters a finishing engine. Engine writes
// persist with the completion snapshot (same durability as the discovery
// data itself); saved-run writes are re-persisted to disk immediately, with
// the file write kept outside the store lock.
func (app *App) applyChaosBusinessWrite(sessionID string, onEngine func(*chaos.Engine) error, onRun func(*chaos.SavedRun) error) error {
	if found, err := app.chaosEngines.withEngine(sessionID, onEngine); found {
		return err
	}
	var encoded []byte
	var runID string
	// The run belongs to whoever owns the session it came from, so that is
	// where it is written back — the request's principal would be the same
	// person here, but the session is the thing the run is attached to.
	runsDir := ""
	if s, ok := app.SessionManager.PeekSession(sessionID); ok && s != nil {
		runsDir = app.chaosRunsDirForSession(s.OwnerID)
	}
	mutateAndEncode := func(run *chaos.SavedRun) error {
		if err := onRun(run); err != nil {
			return err
		}
		if runsDir != "" {
			data, err := chaos.EncodeRun(run)
			if err != nil {
				return err
			}
			encoded = data
			runID = run.ID
		}
		return nil
	}
	persist := func() error {
		if encoded == nil {
			return nil
		}
		return chaos.WriteRunFile(runsDir, runID, encoded)
	}
	if found, err := app.chaosEngines.withLoadedRun(sessionID, mutateAndEncode); found {
		if err != nil {
			return err
		}
		return persist()
	}
	if s, ok := app.SessionManager.GetSession(sessionID); ok {
		if run := app.loadSessionChaosRunFromDisk(s); run != nil {
			app.chaosEngines.setLoadedRun(sessionID, run)
			if found, err := app.chaosEngines.withLoadedRun(sessionID, mutateAndEncode); found {
				if err != nil {
					return err
				}
				return persist()
			}
		}
	}
	return errNoChaosRunData
}

var errNoChaosRunData = &chaosBusinessNoDataError{}

type chaosBusinessNoDataError struct{}

func (*chaosBusinessNoDataError) Error() string {
	return "no chaos run data for this session; run chaos exploration or load a saved run first"
}

func respondChaosBusinessWriteError(c *gin.Context, err error) {
	if _, ok := err.(*chaosBusinessNoDataError); ok {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

// ChaosBusinessToTaskHandler converts a discovered business function into a
// business task draft, or every function at once when no name is given.
//
// This is the join between exploration and use: a chaos run maps an
// application, a business function names one path through it, and a task makes
// that path something a person can run from a form. The draft is returned for
// review rather than saved — it has no outputs, because which part of the
// final screen is the answer is a judgement a chaos run cannot make, and its
// notes list every other assumption.
func (app *App) ChaosBusinessToTaskHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	mm := app.sessionChaosMindMap(s)
	name := strings.TrimSpace(c.Query("name"))

	if name == "" {
		drafts, err := chaos.BusinessFunctionsToTaskDrafts(mm)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"drafts": drafts})
		return
	}

	draft, err := chaos.BusinessFunctionToTask(mm, name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, draft)
}
