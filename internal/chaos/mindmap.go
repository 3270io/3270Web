package chaos

import (
	"fmt"
	"strings"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

const maxKnownValuesPerField = 12

// MindMap captures a lightweight graph of discovered application areas.
// Areas are keyed by screen hash (or a synthetic ID when seeded from a recording).
type MindMap struct {
	Areas map[string]*MindMapArea `json:"areas,omitempty"`
}

// MindMapArea represents one discovered application area.
type MindMapArea struct {
	Hash               string                          `json:"hash"`
	Label              string                          `json:"label,omitempty"`
	Visits             int                             `json:"visits"`
	FirstSeen          time.Time                       `json:"firstSeen,omitempty"`
	LastSeen           time.Time                       `json:"lastSeen,omitempty"`
	FieldCount         int                             `json:"fieldCount"`
	InputFieldCount    int                             `json:"inputFieldCount"`
	NumericFieldCount  int                             `json:"numericFieldCount"`
	HiddenFieldCount   int                             `json:"hiddenFieldCount"`
	FieldMetadata      map[string]MindMapFieldMetadata `json:"fieldMetadata,omitempty"`
	KnownWorkingValues map[string][]string             `json:"knownWorkingValues,omitempty"`
	KeyPresses         map[string]*MindMapKeyPress     `json:"keyPresses,omitempty"`
}

// MindMapFieldMetadata describes one input field in an area.
type MindMapFieldMetadata struct {
	Row       int  `json:"row"`
	Column    int  `json:"column"`
	Length    int  `json:"length"`
	Numeric   bool `json:"numeric"`
	Hidden    bool `json:"hidden"`
	MultiLine bool `json:"multiLine"`
}

// MindMapKeyPress captures how a key is used from an area.
type MindMapKeyPress struct {
	Presses      int            `json:"presses"`
	Progressions int            `json:"progressions"`
	Destinations map[string]int `json:"destinations,omitempty"`
	LastUsedAt   time.Time      `json:"lastUsedAt,omitempty"`
}

func newMindMap() *MindMap {
	return &MindMap{Areas: make(map[string]*MindMapArea)}
}

func (m *MindMap) clone() *MindMap {
	if m == nil || len(m.Areas) == 0 {
		return nil
	}
	out := &MindMap{Areas: make(map[string]*MindMapArea, len(m.Areas))}
	for key, area := range m.Areas {
		if area == nil {
			continue
		}
		next := *area
		if len(area.FieldMetadata) > 0 {
			next.FieldMetadata = make(map[string]MindMapFieldMetadata, len(area.FieldMetadata))
			for fKey, meta := range area.FieldMetadata {
				next.FieldMetadata[fKey] = meta
			}
		}
		if len(area.KnownWorkingValues) > 0 {
			next.KnownWorkingValues = make(map[string][]string, len(area.KnownWorkingValues))
			for fKey, values := range area.KnownWorkingValues {
				next.KnownWorkingValues[fKey] = append([]string(nil), values...)
			}
		}
		if len(area.KeyPresses) > 0 {
			next.KeyPresses = make(map[string]*MindMapKeyPress, len(area.KeyPresses))
			for aid, keyPress := range area.KeyPresses {
				if keyPress == nil {
					continue
				}
				kp := *keyPress
				if len(keyPress.Destinations) > 0 {
					kp.Destinations = make(map[string]int, len(keyPress.Destinations))
					for to, count := range keyPress.Destinations {
						kp.Destinations[to] = count
					}
				}
				next.KeyPresses[aid] = &kp
			}
		}
		out.Areas[key] = &next
	}
	return out
}

func (m *MindMap) ensureArea(hash string) *MindMapArea {
	if strings.TrimSpace(hash) == "" {
		return nil
	}
	if m.Areas == nil {
		m.Areas = make(map[string]*MindMapArea)
	}
	if existing, ok := m.Areas[hash]; ok && existing != nil {
		if existing.Hash == "" {
			existing.Hash = hash
		}
		return existing
	}
	area := &MindMapArea{
		Hash:               hash,
		FieldMetadata:      make(map[string]MindMapFieldMetadata),
		KnownWorkingValues: make(map[string][]string),
		KeyPresses:         make(map[string]*MindMapKeyPress),
	}
	m.Areas[hash] = area
	return area
}

func (m *MindMap) observeScreen(hash string, screen *host.Screen, seenAt time.Time) {
	area := m.ensureArea(hash)
	if area == nil {
		return
	}
	if area.FirstSeen.IsZero() {
		area.FirstSeen = seenAt
	}
	area.LastSeen = seenAt
	area.Visits++
	if screen == nil {
		return
	}
	label, fieldCount, inputCount, numericCount, hiddenCount, fieldMeta := summarizeScreenArea(screen)
	if label != "" {
		area.Label = label
	}
	area.FieldCount = fieldCount
	area.InputFieldCount = inputCount
	area.NumericFieldCount = numericCount
	area.HiddenFieldCount = hiddenCount
	if len(fieldMeta) > 0 {
		if area.FieldMetadata == nil {
			area.FieldMetadata = make(map[string]MindMapFieldMetadata, len(fieldMeta))
		}
		for key, meta := range fieldMeta {
			area.FieldMetadata[key] = meta
		}
	}
}

func (m *MindMap) recordAttempt(attempt Attempt) {
	fromHash := strings.TrimSpace(attempt.FromHash)
	if fromHash == "" {
		return
	}
	area := m.ensureArea(fromHash)
	if area == nil {
		return
	}
	aidKey := strings.TrimSpace(attempt.AIDKey)
	if aidKey == "" {
		aidKey = "Enter"
	}
	if area.KeyPresses == nil {
		area.KeyPresses = make(map[string]*MindMapKeyPress)
	}
	keyPress, ok := area.KeyPresses[aidKey]
	if !ok || keyPress == nil {
		keyPress = &MindMapKeyPress{Destinations: make(map[string]int)}
		area.KeyPresses[aidKey] = keyPress
	}
	keyPress.Presses++
	keyPress.LastUsedAt = attempt.Time

	toHash := strings.TrimSpace(attempt.ToHash)
	if attempt.Transitioned && toHash != "" {
		keyPress.Progressions++
		if keyPress.Destinations == nil {
			keyPress.Destinations = make(map[string]int)
		}
		keyPress.Destinations[toHash]++
	}

	if !attempt.Transitioned {
		return
	}
	if area.KnownWorkingValues == nil {
		area.KnownWorkingValues = make(map[string][]string)
	}
	for _, fw := range attempt.FieldWrites {
		if !fw.Success {
			continue
		}
		value := strings.TrimSpace(fw.Value)
		if value == "" {
			continue
		}
		fieldKey := mindMapFieldKey(fw.Row, fw.Column, fw.Length)
		area.KnownWorkingValues[fieldKey] = appendUniqueLimited(area.KnownWorkingValues[fieldKey], value, maxKnownValuesPerField)
	}
}

func summarizeScreenArea(screen *host.Screen) (string, int, int, int, int, map[string]MindMapFieldMetadata) {
	if screen == nil {
		return "", 0, 0, 0, 0, nil
	}
	label := areaLabelFromScreen(screen)
	fieldCount := len(screen.Fields)
	fieldMeta := make(map[string]MindMapFieldMetadata)
	inputCount := 0
	numericCount := 0
	hiddenCount := 0
	for _, field := range unprotectedFields(screen) {
		if field == nil {
			continue
		}
		inputCount++
		if field.IsNumeric() {
			numericCount++
		}
		if field.IsHidden() {
			hiddenCount++
		}
		row := field.StartY + 1
		col := field.StartX + 1
		length := fieldLength(field)
		if length <= 0 {
			length = 1
		}
		key := mindMapFieldKey(row, col, length)
		fieldMeta[key] = MindMapFieldMetadata{
			Row:       row,
			Column:    col,
			Length:    length,
			Numeric:   field.IsNumeric(),
			Hidden:    field.IsHidden(),
			MultiLine: field.IsMultiline(),
		}
	}
	return label, fieldCount, inputCount, numericCount, hiddenCount, fieldMeta
}

func areaLabelFromScreen(screen *host.Screen) string {
	if screen == nil {
		return ""
	}
	lines := strings.Split(screen.Text(), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		collapsed := strings.Join(strings.Fields(trimmed), " ")
		if collapsed != "" {
			return truncateForLabel(collapsed, 72)
		}
	}
	return fmt.Sprintf("%dx%d screen", screen.Height, screen.Width)
}

func truncateForLabel(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes-1]) + "…"
}

func mindMapFieldKey(row, column, length int) string {
	return fmt.Sprintf("R%dC%dL%d", row, column, length)
}

func appendUniqueLimited(values []string, candidate string, max int) []string {
	for _, existing := range values {
		if existing == candidate {
			return values
		}
	}
	if max > 0 && len(values) >= max {
		return values
	}
	return append(values, candidate)
}
