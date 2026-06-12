package chaos

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// BusinessFieldSemantic describes the business meaning of a single input
// field on a screen (e.g. "Customer number"). Field keys use the same
// R<row>C<col>L<len> format as MindMapFieldMetadata.
type BusinessFieldSemantic struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Example     string `json:"example,omitempty"`
	Sensitive   bool   `json:"sensitive,omitempty"`
}

// BusinessParameter is a user-supplied input to a business function (e.g.
// "account_number"). It maps onto a concrete field on a discovered screen.
type BusinessParameter struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ScreenHash  string `json:"screenHash,omitempty"`
	FieldKey    string `json:"fieldKey,omitempty"`
	Example     string `json:"example,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// BusinessFunctionInput fills one field during a business function step.
// Exactly one of Value (a literal such as a menu choice) or Parameter (the
// name of a BusinessParameter resolved at generation time) should be set.
type BusinessFunctionInput struct {
	FieldKey  string `json:"fieldKey"`
	Value     string `json:"value,omitempty"`
	Parameter string `json:"parameter,omitempty"`
}

// BusinessFunctionStep is one screen interaction in a business function:
// fill zero or more fields on the screen identified by ScreenHash, then
// press AIDKey. ExpectHash optionally names the screen the application
// should land on, used to emit CheckValue guards in generated workflows.
type BusinessFunctionStep struct {
	ScreenHash string                  `json:"screenHash"`
	Inputs     []BusinessFunctionInput `json:"inputs,omitempty"`
	AIDKey     string                  `json:"aidKey"`
	ExpectHash string                  `json:"expectHash,omitempty"`
}

// BusinessFunction is a named, parameterized business operation discovered
// in the 3270 application (e.g. "Create customer", "Account inquiry").
// Functions live inside the MindMap so they persist with saved runs and
// travel through mind-map export/import.
type BusinessFunction struct {
	Name            string                 `json:"name"`
	Description     string                 `json:"description,omitempty"`
	EntryScreenHash string                 `json:"entryScreenHash,omitempty"`
	Steps           []BusinessFunctionStep `json:"steps,omitempty"`
	Parameters      []BusinessParameter    `json:"parameters,omitempty"`
	UpdatedAt       time.Time              `json:"updatedAt,omitempty"`
}

// normalizeBusinessFunctionName produces the catalog key for a function name.
func normalizeBusinessFunctionName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// parseMindMapFieldKey is the inverse of mindMapFieldKey.
func parseMindMapFieldKey(key string) (row, col, length int, ok bool) {
	n, err := fmt.Sscanf(strings.TrimSpace(key), "R%dC%dL%d", &row, &col, &length)
	if err != nil || n != 3 || row <= 0 || col <= 0 || length <= 0 {
		return 0, 0, 0, false
	}
	return row, col, length, true
}

// annotateMindMapArea merges a business annotation into the area for hash,
// creating the area if needed. Non-empty purpose/notes overwrite; field
// semantics are merged per field key. Callers must hold whatever lock guards
// the mind map (engine mutex or store mutex).
func annotateMindMapArea(mm *MindMap, hash, purpose, notes string, semantics map[string]BusinessFieldSemantic) error {
	if mm == nil {
		return fmt.Errorf("mind map is nil")
	}
	area := mm.ensureArea(strings.TrimSpace(hash))
	if area == nil {
		return fmt.Errorf("screen hash is required")
	}
	if v := strings.TrimSpace(purpose); v != "" {
		area.BusinessPurpose = v
	}
	if v := strings.TrimSpace(notes); v != "" {
		area.BusinessNotes = v
	}
	for key, sem := range semantics {
		key = strings.TrimSpace(key)
		if key == "" || strings.TrimSpace(sem.Name) == "" {
			continue
		}
		if area.FieldSemantics == nil {
			area.FieldSemantics = make(map[string]BusinessFieldSemantic)
		}
		area.FieldSemantics[key] = sem
	}
	return nil
}

// upsertMindMapBusinessFunction validates and stores a business function in
// the mind map catalog, keyed by normalized name. Callers must hold whatever
// lock guards the mind map.
func upsertMindMapBusinessFunction(mm *MindMap, fn BusinessFunction) error {
	if mm == nil {
		return fmt.Errorf("mind map is nil")
	}
	fn.Name = strings.TrimSpace(fn.Name)
	key := normalizeBusinessFunctionName(fn.Name)
	if key == "" {
		return fmt.Errorf("business function name is required")
	}
	if len(fn.Steps) == 0 && strings.TrimSpace(fn.EntryScreenHash) == "" {
		return fmt.Errorf("business function needs steps or an entryScreenHash")
	}
	paramNames := make(map[string]bool, len(fn.Parameters))
	for _, p := range fn.Parameters {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return fmt.Errorf("business function parameter name is required")
		}
		if key := strings.TrimSpace(p.FieldKey); key != "" {
			if _, _, _, ok := parseMindMapFieldKey(key); !ok {
				return fmt.Errorf("parameter %q: invalid fieldKey %q (expected R<row>C<col>L<len>)", name, p.FieldKey)
			}
		}
		paramNames[name] = true
	}
	for i, step := range fn.Steps {
		if strings.TrimSpace(step.ScreenHash) == "" {
			return fmt.Errorf("step %d: screenHash is required", i+1)
		}
		if strings.TrimSpace(step.AIDKey) == "" {
			fn.Steps[i].AIDKey = "Enter"
		}
		for _, input := range step.Inputs {
			if _, _, _, ok := parseMindMapFieldKey(input.FieldKey); !ok {
				return fmt.Errorf("step %d: invalid fieldKey %q (expected R<row>C<col>L<len>)", i+1, input.FieldKey)
			}
			if ref := strings.TrimSpace(input.Parameter); ref != "" && !paramNames[ref] {
				return fmt.Errorf("step %d: input references undefined parameter %q", i+1, ref)
			}
		}
	}
	fn.UpdatedAt = time.Now().UTC()
	if mm.BusinessFunctions == nil {
		mm.BusinessFunctions = make(map[string]*BusinessFunction)
	}
	fnCopy := fn
	mm.BusinessFunctions[key] = &fnCopy
	return nil
}

// sortedBusinessFunctions returns the catalog as a name-sorted slice of copies.
func sortedBusinessFunctions(mm *MindMap) []BusinessFunction {
	if mm == nil || len(mm.BusinessFunctions) == 0 {
		return nil
	}
	out := make([]BusinessFunction, 0, len(mm.BusinessFunctions))
	for _, fn := range mm.BusinessFunctions {
		if fn == nil {
			continue
		}
		out = append(out, cloneBusinessFunction(fn))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cloneBusinessFunction(fn *BusinessFunction) BusinessFunction {
	out := *fn
	if len(fn.Steps) > 0 {
		out.Steps = make([]BusinessFunctionStep, len(fn.Steps))
		for i, step := range fn.Steps {
			stepCopy := step
			stepCopy.Inputs = append([]BusinessFunctionInput(nil), step.Inputs...)
			out.Steps[i] = stepCopy
		}
	}
	out.Parameters = append([]BusinessParameter(nil), fn.Parameters...)
	return out
}

// AnnotateSavedRun merges a business annotation into a loaded run's mind map.
// Callers must serialize access to the run (e.g. the chaos engine store lock).
func AnnotateSavedRun(run *SavedRun, hash, purpose, notes string, semantics map[string]BusinessFieldSemantic) error {
	if run == nil {
		return fmt.Errorf("no chaos run loaded")
	}
	if run.MindMap == nil {
		run.MindMap = newMindMap()
	}
	return annotateMindMapArea(run.MindMap, hash, purpose, notes, semantics)
}

// UpsertSavedRunBusinessFunction adds or replaces a business function in a
// loaded run's catalog. Callers must serialize access to the run.
func UpsertSavedRunBusinessFunction(run *SavedRun, fn BusinessFunction) error {
	if run == nil {
		return fmt.Errorf("no chaos run loaded")
	}
	if run.MindMap == nil {
		run.MindMap = newMindMap()
	}
	return upsertMindMapBusinessFunction(run.MindMap, fn)
}

// BusinessFunctionsOf returns the catalog of a mind map as a name-sorted
// slice of copies.
func BusinessFunctionsOf(mm *MindMap) []BusinessFunction {
	return sortedBusinessFunctions(mm)
}

// AnnotateArea records a business purpose and field semantics for a screen.
// Safe to call while the engine is running.
func (e *Engine) AnnotateArea(hash, purpose, notes string, semantics map[string]BusinessFieldSemantic) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.mindMap == nil {
		e.mindMap = newMindMap()
	}
	return annotateMindMapArea(e.mindMap, hash, purpose, notes, semantics)
}

// UpsertBusinessFunction adds or replaces a named business function in the
// engine's catalog. Safe to call while the engine is running.
func (e *Engine) UpsertBusinessFunction(fn BusinessFunction) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.mindMap == nil {
		e.mindMap = newMindMap()
	}
	return upsertMindMapBusinessFunction(e.mindMap, fn)
}

// BusinessFunctionsSnapshot returns a copy of the engine's business function
// catalog, sorted by name. Safe to call while the engine is running.
func (e *Engine) BusinessFunctionsSnapshot() []BusinessFunction {
	e.mu.Lock()
	defer e.mu.Unlock()
	return sortedBusinessFunctions(e.mindMap)
}
