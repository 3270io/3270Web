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

// sessionChaosMindMap resolves the freshest mind map for a session: live
// engine first, then the loaded run, then the auto-saved run on disk.
func (app *App) sessionChaosMindMap(s *session.Session) *chaos.MindMap {
	if eng, ok := app.chaosEngines.get(s.ID); ok {
		if st := eng.Status(); st.MindMap != nil {
			return st.MindMap
		}
	}
	if run, ok := app.chaosEngines.getLoadedRun(s.ID); ok && run != nil {
		return run.MindMap
	}
	if run := app.loadSessionChaosRunFromDisk(s); run != nil {
		app.chaosEngines.setLoadedRun(s.ID, run)
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
	} else if found, runErr := app.chaosEngines.withLoadedRun(s.ID, func(run *chaos.SavedRun) error {
		var genErr error
		data, genErr = chaos.GenerateBusinessWorkflowFromSavedRun(run, req.Name, req.Parameters, targetHost, targetPort)
		return genErr
	}); found {
		err = runErr
	} else if run := app.loadSessionChaosRunFromDisk(s); run != nil {
		app.chaosEngines.setLoadedRun(s.ID, run)
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
// engine when one exists (the engine mutex serializes against the run loop),
// otherwise to the loaded run under the store mutex, loading the auto-saved
// run from disk if needed. Saved-run writes are re-persisted immediately so
// annotations survive restarts.
func (app *App) applyChaosBusinessWrite(sessionID string, onEngine func(*chaos.Engine) error, onRun func(*chaos.SavedRun) error) error {
	if eng, ok := app.chaosEngines.get(sessionID); ok {
		return onEngine(eng)
	}
	writeAndSave := func(run *chaos.SavedRun) error {
		if err := onRun(run); err != nil {
			return err
		}
		if app.chaosRunsDir != "" {
			if err := chaos.SaveRun(app.chaosRunsDir, run); err != nil {
				return err
			}
		}
		return nil
	}
	if found, err := app.chaosEngines.withLoadedRun(sessionID, writeAndSave); found {
		return err
	}
	if s, ok := app.SessionManager.GetSession(sessionID); ok {
		if run := app.loadSessionChaosRunFromDisk(s); run != nil {
			app.chaosEngines.setLoadedRun(sessionID, run)
			if found, err := app.chaosEngines.withLoadedRun(sessionID, writeAndSave); found {
				return err
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
