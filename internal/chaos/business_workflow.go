// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// GenerateBusinessWorkflow resolves a cataloged business function plus
// user-supplied parameter values into a playback-compatible workflow JSON
// document. The workflow carries Name/Description/BusinessFunction/Parameters
// metadata so it is self-describing; ChaosDiscovery is intentionally omitted
// to keep business workflows lean.
func GenerateBusinessWorkflow(mm *MindMap, name string, paramValues map[string]string, hostName string, port int, header *WorkflowHeader) ([]byte, error) {
	if mm == nil || len(mm.BusinessFunctions) == 0 {
		return nil, fmt.Errorf("no business functions cataloged; annotate screens and save functions first")
	}
	fn := mm.BusinessFunctions[normalizeBusinessFunctionName(name)]
	if fn == nil {
		return nil, fmt.Errorf("business function %q not found; available: %s", name, strings.Join(businessFunctionNames(mm), ", "))
	}

	resolved := make(map[string]string, len(paramValues))
	for key, value := range paramValues {
		if err := validateBusinessParameterValue(value); err != nil {
			return nil, fmt.Errorf("parameter %q: %w", key, err)
		}
		resolved[strings.TrimSpace(key)] = value
	}
	var usedParams []session.WorkflowParameter
	for _, p := range fn.Parameters {
		value, ok := resolved[p.Name]
		if !ok || strings.TrimSpace(value) == "" {
			if p.Required {
				return nil, fmt.Errorf("required parameter %q is missing", p.Name)
			}
			continue
		}
		param := session.WorkflowParameter{
			Name:        p.Name,
			Description: p.Description,
			Value:       value,
		}
		if row, col, length, ok := parseMindMapFieldKey(p.FieldKey); ok {
			param.Row = row
			param.Column = col
			param.Length = length
		}
		// Values headed for sensitive fields (password-style hidden fields or
		// fields the AI marked sensitive) are not echoed in the metadata. The
		// FillString step still carries the value — playback needs it — so
		// the document as a whole must still be handled as sensitive.
		if isSensitiveParameter(mm, fn, p) {
			param.Value = ""
			param.Sensitive = true
		}
		usedParams = append(usedParams, param)
	}

	steps := []session.WorkflowStep{{Type: "Connect"}}
	fnSteps := fn.Steps
	if len(fnSteps) == 0 {
		// Function recorded only an entry screen: synthesize a single step that
		// fills its parameters there and presses Enter.
		fnSteps = []BusinessFunctionStep{{ScreenHash: fn.EntryScreenHash, AIDKey: "Enter"}}
		for _, p := range fn.Parameters {
			if strings.TrimSpace(p.FieldKey) == "" {
				continue
			}
			fnSteps[0].Inputs = append(fnSteps[0].Inputs, BusinessFunctionInput{FieldKey: p.FieldKey, Parameter: p.Name})
		}
	}

	currentHash := ""
	for i, step := range fnSteps {
		stepHash := strings.TrimSpace(step.ScreenHash)
		if stepHash == "" {
			return nil, fmt.Errorf("step %d: screenHash is required", i+1)
		}
		// Bridge navigation gaps between the previous step's landing screen and
		// this step's screen using the learned key-press graph.
		if currentHash != "" && currentHash != stepHash {
			edges, ok := mm.findKeyPath(currentHash, stepHash)
			if !ok {
				return nil, fmt.Errorf("step %d: no known key path from screen %s to %s", i+1, currentHash, stepHash)
			}
			for _, edge := range edges {
				steps = append(steps, session.WorkflowStep{Type: aidKeyToStepType(edge.AIDKey)})
			}
		}
		for _, input := range step.Inputs {
			row, col, length, ok := parseMindMapFieldKey(input.FieldKey)
			if !ok {
				return nil, fmt.Errorf("step %d: invalid fieldKey %q", i+1, input.FieldKey)
			}
			text := input.Value
			if ref := strings.TrimSpace(input.Parameter); ref != "" {
				value, ok := resolved[ref]
				if !ok || strings.TrimSpace(value) == "" {
					return nil, fmt.Errorf("step %d: no value supplied for parameter %q", i+1, ref)
				}
				text = value
			}
			if err := validateBusinessParameterValue(text); err != nil {
				return nil, fmt.Errorf("step %d field %s: %w", i+1, input.FieldKey, err)
			}
			if text == "" {
				continue
			}
			steps = append(steps, session.WorkflowStep{
				Type:        "FillString",
				Coordinates: &session.WorkflowCoordinates{Row: row, Column: col, Length: length},
				Text:        text,
			})
		}
		aidKey := step.AIDKey
		if strings.TrimSpace(aidKey) == "" {
			aidKey = "Enter"
		}
		steps = append(steps, session.WorkflowStep{Type: aidKeyToStepType(aidKey)})
		currentHash = strings.TrimSpace(step.ExpectHash)
		if currentHash != "" {
			if check, ok := checkStepForArea(mm.Areas[currentHash]); ok {
				steps = append(steps, check)
			}
		}
	}
	steps = append(steps, session.WorkflowStep{Type: "Disconnect"})

	export := exportedWorkflow{
		Host:             hostName,
		Port:             port,
		Name:             fn.Name,
		Description:      fn.Description,
		BusinessFunction: fn.Name,
		Parameters:       usedParams,
		Steps:            steps,
	}
	if header != nil {
		export.EveryStepDelay = cloneWorkflowDelayRange(header.EveryStepDelay)
		export.OutputFilePath = header.OutputFilePath
		export.RampUpBatchSize = header.RampUpBatchSize
		export.RampUpDelay = header.RampUpDelay
		export.EndOfTaskDelay = cloneWorkflowDelayRange(header.EndOfTaskDelay)
	}
	return json.MarshalIndent(export, "", "  ")
}

// GenerateBusinessWorkflow resolves a business function against the engine's
// live mind map. Safe to call while the engine is running.
func (e *Engine) GenerateBusinessWorkflow(name string, paramValues map[string]string, hostName string, port int) ([]byte, error) {
	e.mu.Lock()
	mm := e.mindMap.clone()
	header := e.workflowHeader.clone()
	e.mu.Unlock()
	if hostName == "" {
		hostName = e.cfg.ExportHost
	}
	if port == 0 {
		port = e.cfg.ExportPort
	}
	return GenerateBusinessWorkflow(mm, name, paramValues, hostName, port, header)
}

// GenerateBusinessWorkflowFromSavedRun resolves a business function against a
// saved run's mind map and workflow header.
func GenerateBusinessWorkflowFromSavedRun(run *SavedRun, name string, paramValues map[string]string, hostName string, port int) ([]byte, error) {
	if run == nil {
		return nil, fmt.Errorf("no chaos run loaded")
	}
	return GenerateBusinessWorkflow(run.MindMap, name, paramValues, hostName, port, run.WorkflowHeader)
}

// isSensitiveParameter reports whether a parameter's value lands in a field
// that is hidden (3270 non-display, password-style) or that the AI marked
// sensitive in the screen's field semantics — either via the parameter's own
// screen/field mapping or via any function step input that references it.
func isSensitiveParameter(mm *MindMap, fn *BusinessFunction, p BusinessParameter) bool {
	if fieldIsSensitive(mm, p.ScreenHash, p.FieldKey) {
		return true
	}
	for _, step := range fn.Steps {
		for _, input := range step.Inputs {
			if strings.TrimSpace(input.Parameter) == strings.TrimSpace(p.Name) &&
				fieldIsSensitive(mm, step.ScreenHash, input.FieldKey) {
				return true
			}
		}
	}
	return false
}

func fieldIsSensitive(mm *MindMap, screenHash, fieldKey string) bool {
	if mm == nil {
		return false
	}
	area := mm.Areas[strings.TrimSpace(screenHash)]
	if area == nil {
		return false
	}
	key := strings.TrimSpace(fieldKey)
	if sem, ok := area.FieldSemantics[key]; ok && sem.Sensitive {
		return true
	}
	if meta, ok := area.FieldMetadata[key]; ok && meta.Hidden {
		return true
	}
	return false
}

// validateBusinessParameterValue applies the same field-text rule as the
// screen write endpoints: CR/LF/TAB would be interpreted by the s3270
// protocol layer rather than typed into the field.
func validateBusinessParameterValue(value string) error {
	if host.ContainsForbiddenFieldText(value) {
		return fmt.Errorf("value must not contain CR/LF/TAB")
	}
	return nil
}

func businessFunctionNames(mm *MindMap) []string {
	names := make([]string, 0, len(mm.BusinessFunctions))
	for _, fn := range mm.BusinessFunctions {
		if fn != nil {
			names = append(names, fn.Name)
		}
	}
	sort.Strings(names)
	return names
}

// keyPathEdge is one hop in a navigation path: press AIDKey on screen From
// to reach screen To.
type keyPathEdge struct {
	From   string
	AIDKey string
	To     string
}

// findKeyPath runs a breadth-first search over the learned key-press graph
// (area.KeyPresses[key].Destinations) and returns the shortest key-only
// navigation path between two screens. Edges are explored in deterministic
// order (sorted keys/destinations) so generation is reproducible.
func (m *MindMap) findKeyPath(fromHash, toHash string) ([]keyPathEdge, bool) {
	if m == nil || len(m.Areas) == 0 {
		return nil, false
	}
	fromHash = strings.TrimSpace(fromHash)
	toHash = strings.TrimSpace(toHash)
	if fromHash == "" || toHash == "" {
		return nil, false
	}
	if fromHash == toHash {
		return nil, true
	}
	type visit struct {
		hash string
		via  keyPathEdge
		prev int
	}
	queue := []visit{{hash: fromHash, prev: -1}}
	seen := map[string]bool{fromHash: true}
	for i := 0; i < len(queue); i++ {
		area := m.Areas[queue[i].hash]
		if area == nil {
			continue
		}
		keys := make([]string, 0, len(area.KeyPresses))
		for key, kp := range area.KeyPresses {
			if kp != nil && len(kp.Destinations) > 0 {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			dests := make([]string, 0, len(area.KeyPresses[key].Destinations))
			for dest := range area.KeyPresses[key].Destinations {
				dests = append(dests, dest)
			}
			sort.Strings(dests)
			for _, dest := range dests {
				if seen[dest] {
					continue
				}
				seen[dest] = true
				queue = append(queue, visit{
					hash: dest,
					via:  keyPathEdge{From: queue[i].hash, AIDKey: key, To: dest},
					prev: i,
				})
				if dest == toHash {
					var edges []keyPathEdge
					for idx := len(queue) - 1; idx > 0; idx = queue[idx].prev {
						edges = append(edges, queue[idx].via)
					}
					for l, r := 0, len(edges)-1; l < r; l, r = l+1, r-1 {
						edges[l], edges[r] = edges[r], edges[l]
					}
					return edges, true
				}
			}
		}
	}
	return nil, false
}
