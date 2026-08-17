// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"fmt"
	"strings"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

const maxKnownValuesPerField = 12
const maxScreenPreviewRows = 24
const maxScreenPreviewCols = 80

// MindMap captures a lightweight graph of discovered application areas.
// Areas are keyed by screen hash (or a synthetic ID when seeded from a recording).
// BusinessFunctions is the catalog of named business operations built up by
// the AI chat (keyed by normalized function name); it persists with saved
// runs and travels through mind-map export/import.
type MindMap struct {
	Areas             map[string]*MindMapArea      `json:"areas,omitempty"`
	BusinessFunctions map[string]*BusinessFunction `json:"businessFunctions,omitempty"`
}

// MindMapArea represents one discovered application area.
type MindMapArea struct {
	Hash                    string                           `json:"hash"`
	Label                   string                           `json:"label,omitempty"`
	Visits                  int                              `json:"visits"`
	FirstSeen               time.Time                        `json:"firstSeen,omitempty"`
	LastSeen                time.Time                        `json:"lastSeen,omitempty"`
	ScreenWidth             int                              `json:"screenWidth,omitempty"`
	ScreenHeight            int                              `json:"screenHeight,omitempty"`
	PreviewText             string                           `json:"previewText,omitempty"`
	PreviewWidth            int                              `json:"previewWidth,omitempty"`
	PreviewHeight           int                              `json:"previewHeight,omitempty"`
	FieldCount              int                              `json:"fieldCount"`
	InputFieldCount         int                              `json:"inputFieldCount"`
	NumericFieldCount       int                              `json:"numericFieldCount"`
	HiddenFieldCount        int                              `json:"hiddenFieldCount"`
	FieldMetadata           map[string]MindMapFieldMetadata  `json:"fieldMetadata,omitempty"`
	FieldDiscovery          map[string]MindMapFieldDiscovery `json:"fieldDiscovery,omitempty"`
	KnownTriedValues        map[string][]string              `json:"knownTriedValues,omitempty"`
	KnownWorkingValues      map[string][]string              `json:"knownWorkingValues,omitempty"`
	FieldCountProgressions  map[int]int                      `json:"fieldCountProgressions,omitempty"`
	SingleFieldProgressions int                              `json:"singleFieldProgressions,omitempty"`
	MultiFieldProgressions  int                              `json:"multiFieldProgressions,omitempty"`
	KeyPresses              map[string]*MindMapKeyPress      `json:"keyPresses,omitempty"`
	DedupSignature          string                           `json:"dedupSignature,omitempty"`
	AutoBlockedKeys         []string                         `json:"autoBlockedKeys,omitempty"`
	AutoKnownKeys           []string                         `json:"autoKnownKeys,omitempty"`
	BusinessPurpose         string                           `json:"businessPurpose,omitempty"`
	BusinessNotes           string                           `json:"businessNotes,omitempty"`
	FieldSemantics          map[string]BusinessFieldSemantic `json:"fieldSemantics,omitempty"`
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

// MindMapFieldDiscovery captures per-field discovery statistics for a screen.
// A "progression" means the overall attempt transitioned to a different screen
// after a successful write to this field.
type MindMapFieldDiscovery struct {
	Writes          int       `json:"writes"`
	WriteSuccesses  int       `json:"writeSuccesses"`
	Progressions    int       `json:"progressions"`
	LastTriedAt     time.Time `json:"lastTriedAt,omitempty"`
	LastWorkedAt    time.Time `json:"lastWorkedAt,omitempty"`
	LastValue       string    `json:"lastValue,omitempty"`
	LastWorkedValue string    `json:"lastWorkedValue,omitempty"`
}

// MindMapKeyPress captures how a key is used from an area.
type MindMapKeyPress struct {
	Presses                 int            `json:"presses"`
	Progressions            int            `json:"progressions"`
	SingleFieldProgressions int            `json:"singleFieldProgressions,omitempty"`
	MultiFieldProgressions  int            `json:"multiFieldProgressions,omitempty"`
	Destinations            map[string]int `json:"destinations,omitempty"`
	LastUsedAt              time.Time      `json:"lastUsedAt,omitempty"`
}

func newMindMap() *MindMap {
	return &MindMap{Areas: make(map[string]*MindMapArea)}
}

// clone returns a copy safe to hand out while the engine keeps running.
//
// Every map is rebuilt, including empty ones. `next := *area` copies each map
// *header*, so an empty-but-non-nil map would otherwise be the same map in
// both copies — and the engine writes the first entry into it moments after
// creating it. A status request landing in that window marshalled a map the
// engine was writing to: a data race, and a possible panic inside
// encoding/json rather than anywhere it could be caught. Guarding on
// non-nil rather than non-empty is the whole fix.
func (m *MindMap) clone() *MindMap {
	if m == nil || (len(m.Areas) == 0 && len(m.BusinessFunctions) == 0) {
		return nil
	}
	out := &MindMap{Areas: make(map[string]*MindMapArea, len(m.Areas))}
	if len(m.BusinessFunctions) > 0 {
		out.BusinessFunctions = make(map[string]*BusinessFunction, len(m.BusinessFunctions))
		for key, fn := range m.BusinessFunctions {
			if fn == nil {
				continue
			}
			fnCopy := cloneBusinessFunction(fn)
			out.BusinessFunctions[key] = &fnCopy
		}
	}
	for key, area := range m.Areas {
		if area == nil {
			continue
		}
		next := *area
		if area.FieldMetadata != nil {
			next.FieldMetadata = make(map[string]MindMapFieldMetadata, len(area.FieldMetadata))
			for fKey, meta := range area.FieldMetadata {
				next.FieldMetadata[fKey] = meta
			}
		}
		if area.FieldDiscovery != nil {
			next.FieldDiscovery = make(map[string]MindMapFieldDiscovery, len(area.FieldDiscovery))
			for fKey, meta := range area.FieldDiscovery {
				next.FieldDiscovery[fKey] = meta
			}
		}
		if area.KnownTriedValues != nil {
			next.KnownTriedValues = make(map[string][]string, len(area.KnownTriedValues))
			for fKey, values := range area.KnownTriedValues {
				next.KnownTriedValues[fKey] = append([]string(nil), values...)
			}
		}
		if area.KnownWorkingValues != nil {
			next.KnownWorkingValues = make(map[string][]string, len(area.KnownWorkingValues))
			for fKey, values := range area.KnownWorkingValues {
				next.KnownWorkingValues[fKey] = append([]string(nil), values...)
			}
		}
		if area.FieldCountProgressions != nil {
			next.FieldCountProgressions = make(map[int]int, len(area.FieldCountProgressions))
			for count, progressions := range area.FieldCountProgressions {
				next.FieldCountProgressions[count] = progressions
			}
		}
		if area.FieldSemantics != nil {
			next.FieldSemantics = make(map[string]BusinessFieldSemantic, len(area.FieldSemantics))
			for fKey, sem := range area.FieldSemantics {
				next.FieldSemantics[fKey] = sem
			}
		}
		if area.KeyPresses != nil {
			next.KeyPresses = make(map[string]*MindMapKeyPress, len(area.KeyPresses))
			for aid, keyPress := range area.KeyPresses {
				if keyPress == nil {
					continue
				}
				kp := *keyPress
				if keyPress.Destinations != nil {
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
		Hash:                   hash,
		FieldMetadata:          make(map[string]MindMapFieldMetadata),
		FieldDiscovery:         make(map[string]MindMapFieldDiscovery),
		KnownTriedValues:       make(map[string][]string),
		KnownWorkingValues:     make(map[string][]string),
		FieldCountProgressions: make(map[int]int),
		KeyPresses:             make(map[string]*MindMapKeyPress),
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
	area.ScreenWidth, area.ScreenHeight = screenDimensions(screen)
	previewText, previewWidth, previewHeight := screenPreview(screen)
	if previewText != "" {
		area.PreviewText = previewText
		area.PreviewWidth = previewWidth
		area.PreviewHeight = previewHeight
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
	area.DedupSignature = buildAreaDedupSignature(screen)
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
	successfulWrites := 0
	for _, fw := range attempt.FieldWrites {
		if fw.Success {
			successfulWrites++
		}
	}
	learningFieldCount := successfulWrites
	if attempt.FieldsTargeted > 0 {
		learningFieldCount = attempt.FieldsTargeted
	}

	toHash := strings.TrimSpace(attempt.ToHash)
	if attempt.Transitioned && toHash != "" {
		keyPress.Progressions++
		if successfulWrites > 0 && learningFieldCount == 1 {
			keyPress.SingleFieldProgressions++
		} else if successfulWrites > 0 && learningFieldCount > 1 {
			keyPress.MultiFieldProgressions++
		}
		if keyPress.Destinations == nil {
			keyPress.Destinations = make(map[string]int)
		}
		keyPress.Destinations[toHash]++
	}

	if !attempt.Transitioned {
		// Keep field discovery metadata even when the attempt does not transition.
	}
	if area.FieldDiscovery == nil {
		area.FieldDiscovery = make(map[string]MindMapFieldDiscovery)
	}
	if area.KnownTriedValues == nil {
		area.KnownTriedValues = make(map[string][]string)
	}
	if area.KnownWorkingValues == nil {
		area.KnownWorkingValues = make(map[string][]string)
	}
	if area.FieldCountProgressions == nil {
		area.FieldCountProgressions = make(map[int]int)
	}
	if attempt.Transitioned && successfulWrites > 0 && learningFieldCount > 0 {
		area.FieldCountProgressions[learningFieldCount]++
		if learningFieldCount == 1 {
			area.SingleFieldProgressions++
		} else {
			area.MultiFieldProgressions++
		}
	}
	for _, fw := range attempt.FieldWrites {
		fieldKey := mindMapFieldKey(fw.Row, fw.Column, fw.Length)
		fd := area.FieldDiscovery[fieldKey]
		fd.Writes++
		fd.LastTriedAt = attempt.Time
		if fw.Success {
			fd.WriteSuccesses++
		}
		value := strings.TrimSpace(fw.Value)
		if value != "" {
			fd.LastValue = value
			if fw.Success {
				area.KnownTriedValues[fieldKey] = appendUniqueLimited(area.KnownTriedValues[fieldKey], value, maxKnownValuesPerField*2)
			}
		}
		if attempt.Transitioned && fw.Success {
			fd.Progressions++
			fd.LastWorkedAt = attempt.Time
			if value != "" {
				fd.LastWorkedValue = value
				area.KnownWorkingValues[fieldKey] = appendUniqueLimited(area.KnownWorkingValues[fieldKey], value, maxKnownValuesPerField)
			}
		}
		area.FieldDiscovery[fieldKey] = fd
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

func areaTitleDedupSignatureFromScreen(screen *host.Screen) string {
	return normalizeAreaTitleDedupSignature(areaLabelFromScreen(screen))
}

func normalizeAreaTitleDedupSignature(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	// Preserve the actual title words (unlike the broader screen dedup signature)
	// while normalizing common dynamic fragments such as numbers and spacing.
	collapsed := strings.Join(strings.Fields(trimmed), " ")
	var b strings.Builder
	b.Grow(len(collapsed))
	prevSpace := false
	for _, r := range strings.ToLower(collapsed) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune('9')
			prevSpace = false
		case r == ' ':
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

func screenDimensions(screen *host.Screen) (int, int) {
	if screen == nil {
		return 0, 0
	}
	width := screen.Width
	height := screen.Height
	if height <= 0 {
		height = len(screen.Buffer)
	}
	if width <= 0 {
		for _, row := range screen.Buffer {
			if len(row) > width {
				width = len(row)
			}
		}
	}
	return width, height
}

func screenPreview(screen *host.Screen) (string, int, int) {
	if screen == nil {
		return "", 0, 0
	}
	screenWidth, screenHeight := screenDimensions(screen)
	if screenWidth <= 0 || screenHeight <= 0 {
		return "", 0, 0
	}
	previewWidth := screenWidth
	if previewWidth > maxScreenPreviewCols {
		previewWidth = maxScreenPreviewCols
	}
	previewHeight := screenHeight
	if previewHeight > maxScreenPreviewRows {
		previewHeight = maxScreenPreviewRows
	}
	if previewWidth <= 0 || previewHeight <= 0 {
		return "", 0, 0
	}

	grid := make([][]rune, previewHeight)
	for y := 0; y < previewHeight; y++ {
		row := make([]rune, previewWidth)
		for x := 0; x < previewWidth; x++ {
			ch := screen.CharAt(x, y)
			if ch == 0 {
				ch = ' '
			}
			row[x] = ch
		}
		grid[y] = row
	}

	// Preserve visible input values so the preview shows a real captured example.
	// Hidden fields are still masked.
	for _, f := range unprotectedFields(screen) {
		if f == nil {
			continue
		}
		if !f.IsHidden() {
			continue
		}
		curX, curY := f.StartX, f.StartY
		endX, endY := f.EndX, f.EndY
		for {
			if curY >= 0 && curY < previewHeight && curX >= 0 && curX < previewWidth {
				grid[curY][curX] = '*'
			}
			if curX == endX && curY == endY {
				break
			}
			curX++
			if curX >= screenWidth {
				curX = 0
				curY++
				if curY >= screenHeight {
					break
				}
			}
		}
	}

	var b strings.Builder
	for y := 0; y < previewHeight; y++ {
		b.WriteString(string(grid[y]))
		if y < previewHeight-1 {
			b.WriteByte('\n')
		}
	}
	return b.String(), previewWidth, previewHeight
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
		// Evict the oldest entry so the engine continues to learn new working
		// values rather than silently dropping them once the cap is reached.
		values = values[1:]
	}
	return append(values, candidate)
}

// normalizeDedupSignature rewrites a stored dedup signature into the current
// format. Signatures written before display attributes were dropped from the
// field fragments carry 7 comma-separated values per field
// ("y,x,y,x,code,color,highlight"); the current format keeps the first five.
// The screen-body portion of the signature is left untouched.
func normalizeDedupSignature(sig string) string {
	idx := strings.LastIndex(sig, "|fields:")
	if idx < 0 {
		return sig
	}
	parts := strings.Split(sig[idx+1:], "|")
	var b strings.Builder
	b.Grow(len(sig))
	b.WriteString(sig[:idx])
	for i, part := range parts {
		b.WriteByte('|')
		if i == 0 {
			b.WriteString(part) // "fields:N"
			continue
		}
		vals := strings.Split(part, ",")
		if len(vals) > 5 {
			vals = vals[:5]
		}
		b.WriteString(strings.Join(vals, ","))
	}
	return b.String()
}

// normalizeMindMapSignatures upgrades every stored area signature to the
// current format so runs and mind maps saved before a signature-format
// change keep matching newly observed screens and compare cleanly.
func normalizeMindMapSignatures(mm *MindMap) {
	if mm == nil {
		return
	}
	for _, area := range mm.Areas {
		if area == nil || area.DedupSignature == "" {
			continue
		}
		area.DedupSignature = normalizeDedupSignature(area.DedupSignature)
	}
}

func buildAreaDedupSignature(screen *host.Screen) string {
	if screen == nil {
		return ""
	}
	width, height := screenDimensions(screen)
	if width <= 0 || height <= 0 {
		return ""
	}
	maskedInputCells := make([]bool, width*height)
	for _, f := range screen.Fields {
		if f == nil || f.IsProtected() {
			continue
		}
		curX, curY := f.StartX, f.StartY
		for {
			if curX >= 0 && curX < width && curY >= 0 && curY < height {
				maskedInputCells[(curY*width)+curX] = true
			}
			if curX == f.EndX && curY == f.EndY {
				break
			}
			curX++
			if curX >= width {
				curX = 0
				curY++
				if curY >= height {
					break
				}
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%dx%d|", width, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			ch := screen.CharAt(x, y)
			if ch == 0 {
				ch = ' '
			}
			if maskedInputCells[(y*width)+x] {
				ch = ' '
			}
			// Preserve layout and punctuation, but abstract alphanumerics so
			// echoed values don't fragment the mind map into near-duplicates.
			switch {
			case ch >= 'A' && ch <= 'Z':
				ch = 'A'
			case ch >= 'a' && ch <= 'z':
				ch = 'a'
			case ch >= '0' && ch <= '9':
				ch = '9'
			}
			b.WriteRune(ch)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "|fields:%d", len(screen.Fields))
	for _, f := range screen.Fields {
		if f == nil {
			continue
		}
		// Only geometry and field attributes (protection/numeric bits) are
		// structural. Display attributes such as color and highlighting change
		// with host state (e.g. error highlighting) and would split screens
		// that are functionally the same.
		fmt.Fprintf(&b, "|%d,%d,%d,%d,%d", f.StartY, f.StartX, f.EndY, f.EndX, f.FieldCode)
	}
	return b.String()
}
