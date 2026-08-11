package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/macroimport"
	"github.com/jnnngs/3270Web/internal/session"
)

// Macro import. A shop with hundreds of recorded macros has hundreds of
// reasons not to move, and rewriting them by hand is the reason an evaluation
// stops. These two endpoints read a macro file and hand back the recording it
// becomes, along with a line-by-line report of what did not translate —
// see internal/macroimport for what the translator will and will not do.
//
// There are two of them because there are two jobs. The browser endpoint
// loads the result into the session, so the operator can step through it
// against the host on the next click; the API endpoint translates and returns
// without touching any session, so a script can walk a directory of three
// hundred files in one pass and get three hundred recordings and three
// hundred reports back.

// A macro file is source text somebody wrote. The recording upload limit is
// two megabytes; a quarter of that is far past any hand-written macro and
// still bounded well below what a request should carry.
const maxMacroUploadBytes = 512 * 1024

// macroTranslationResponse is what both endpoints answer with. The report is
// the part that matters: steps alone would be the silent partial conversion
// this feature exists to avoid.
type macroTranslationResponse struct {
	Name       string             `json:"name"`
	StepTotal  int                `json:"stepTotal"`
	Statements int                `json:"statements"`
	Translated int                `json:"translated"`
	Notes      []macroimport.Note `json:"notes"`
	Advisories []string           `json:"advisories,omitempty"`
	Workflow   *WorkflowConfig    `json:"workflow,omitempty"`
}

// ImportMacroHandler translates an uploaded macro file and loads the result
// as this session's recording, so Play and Debug reach it immediately.
//
// The report comes back in the response rather than in a file: the moment to
// read which twelve lines need a human is before the flow is played against a
// host, not after.
func (app *App) ImportMacroHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	playing := false
	withSessionLock(s, func() {
		playing = s.Playback != nil && s.Playback.Active
	})
	if playing {
		c.JSON(http.StatusConflict, gin.H{"error": "cannot load a recording during playback"})
		return
	}

	name, source, err := macroFileFromRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result := macroimport.Translate(source)
	host, port := workflowTargetForSession(s)
	workflow := macroWorkflow(host, port, name, result)

	if len(result.Steps) == 0 {
		// Nothing to load, but the report is the answer to "why not" and is
		// worth more than the status code on its own.
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": "no line of this file translated into a step",
			"report": macroTranslationResponse{
				Name:       recordingNameForMacro(name),
				Statements: result.Statements,
				Translated: result.Translated,
				Notes:      result.Notes,
				Advisories: result.Advisories,
			},
		})
		return
	}

	if err := macroBlocksResolve(workflow); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	raw, err := json.Marshal(workflow)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "the translated recording could not be encoded"})
		return
	}
	recordingName := recordingNameForMacro(name)
	storeLoadedWorkflow(s, recordingName, raw, len(workflow.Steps), prettyWorkflowPayload(raw))

	c.JSON(http.StatusOK, macroTranslationResponse{
		Name:       recordingName,
		StepTotal:  len(workflow.Steps),
		Statements: result.Statements,
		Translated: result.Translated,
		Notes:      result.Notes,
		Advisories: result.Advisories,
	})
}

// APITranslateMacro translates a macro file and returns the recording without
// loading it anywhere, which is what converting a library of them in one pass
// needs. It takes the file as an upload or as JSON, because a shell script
// and a program reach for different ones.
func (app *App) APITranslateMacro(c *gin.Context) {
	name, source, err := macroFileFromRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result := macroimport.Translate(source)
	// No session is in play, so the recording names no host. A caller that
	// replays it through a session gets that session's host; a caller writing
	// the file out fills the field in.
	workflow := macroWorkflow("", 0, name, result)
	if err := macroBlocksResolve(workflow); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, macroTranslationResponse{
		Name:       recordingNameForMacro(name),
		StepTotal:  len(workflow.Steps),
		Statements: result.Statements,
		Translated: result.Translated,
		Notes:      result.Notes,
		Advisories: result.Advisories,
		Workflow:   workflow,
	})
}

// macroFileFromRequest reads the macro from a multipart upload or from a JSON
// body carrying the same two things a file has: a name and its text.
func macroFileFromRequest(c *gin.Context) (string, string, error) {
	if file, err := c.FormFile("macro"); err == nil {
		reader, err := file.Open()
		if err != nil {
			return "", "", errors.New("the uploaded macro file could not be read")
		}
		defer reader.Close()
		if file.Size > maxMacroUploadBytes {
			return "", "", fmt.Errorf("macro file exceeds %d bytes", maxMacroUploadBytes)
		}
		payload, err := io.ReadAll(io.LimitReader(reader, maxMacroUploadBytes+1))
		if err != nil {
			return "", "", errors.New("the uploaded macro file could not be read")
		}
		if len(payload) > maxMacroUploadBytes {
			return "", "", fmt.Errorf("macro file exceeds %d bytes", maxMacroUploadBytes)
		}
		if len(payload) == 0 {
			return "", "", errors.New("the macro file is empty")
		}
		return file.Filename, string(payload), nil
	} else if !errors.Is(err, http.ErrMissingFile) && c.ContentType() == "multipart/form-data" {
		return "", "", errors.New("a macro file is required")
	}

	var body struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return "", "", errors.New("send the macro as a file upload or as JSON with name and content")
	}
	if strings.TrimSpace(body.Content) == "" {
		return "", "", errors.New("the macro file is empty")
	}
	if len(body.Content) > maxMacroUploadBytes {
		return "", "", fmt.Errorf("macro file exceeds %d bytes", maxMacroUploadBytes)
	}
	return body.Name, body.Content, nil
}

// macroWorkflow wraps translated steps in the recording the rest of the app
// reads. The delays live on the steps that a wait in the macro asked for, so
// there is deliberately no every-step delay: inventing one would slow a flow
// the macro ran at full speed.
func macroWorkflow(host string, port int, sourceName string, result macroimport.Result) *WorkflowConfig {
	steps := result.Steps
	if steps == nil {
		steps = []session.WorkflowStep{}
	}
	return &WorkflowConfig{
		Host:        host,
		Port:        port,
		Name:        recordingNameForMacro(sourceName),
		Description: macroDescription(sourceName, result),
		Steps:       steps,
	}
}

// macroBlocksResolve checks that the branches and loops a translation
// produced close the way playback needs them to.
//
// It is the one thing about a translated recording nobody downstream can
// recover from: a step that did not translate is reported and a step that
// fails is logged, but a recording whose If has no EndIf is refused as a
// whole when it is played, and the operator is left with a file, a report
// that said the branch converted, and nothing that runs. The translator
// closes every block it opens; this is the assertion that says so at the
// point where the file would otherwise be handed over.
func macroBlocksResolve(workflow *WorkflowConfig) error {
	if workflow == nil || len(workflow.Steps) == 0 {
		return nil
	}
	if _, err := compileWorkflowSteps(workflow.Steps); err != nil {
		return fmt.Errorf("the translated recording's branches do not resolve, so it was not loaded: %v", err)
	}
	return nil
}

func macroDescription(sourceName string, result macroimport.Result) string {
	base := macroBaseName(sourceName)
	if base == "" {
		base = "a macro file"
	}
	if len(result.Notes) == 0 {
		return fmt.Sprintf("Imported from %s.", base)
	}
	lines := "lines"
	if len(result.Notes) == 1 {
		lines = "line"
	}
	// The count travels with the file. Somebody opening this recording in six
	// months should not have to guess whether it was a whole translation.
	return fmt.Sprintf("Imported from %s. %d %s of the macro did not translate and were left out.", base, len(result.Notes), lines)
}

// macroBaseName reduces whatever the client called the file to a bare name.
// A browser sends the name the operator picked, and a client sends whatever
// it likes; neither is allowed to introduce a path.
func macroBaseName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	name = strings.TrimSpace(strings.Trim(name, "."))
	if name == "" || name == "/" {
		return ""
	}
	// Keep it to characters a filename and a piece of UI text can both carry.
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_', r == ' ':
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// recordingNameForMacro names the recording after the macro it came from, so
// a directory of converted files still says which is which.
func recordingNameForMacro(sourceName string) string {
	base := macroBaseName(sourceName)
	if base == "" {
		return "imported-macro.json"
	}
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if base == "" {
		return "imported-macro.json"
	}
	return base + ".json"
}

// workflowTargetForSession answers with the host this session is pointed at,
// which is the one a translated recording will be replayed against.
func workflowTargetForSession(s *session.Session) (string, int) {
	var host string
	var port int
	withSessionLock(s, func() {
		host = s.TargetHost
		port = s.TargetPort
	})
	if port == 0 {
		port = 3270
	}
	return host, port
}
