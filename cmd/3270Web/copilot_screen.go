// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// What a model is told about a screen beyond its characters.
//
// The screen JSON used to be the buffer and a field list, and two things were
// missing from it that a person reads off the glass without thinking. One is
// what each input field is *for*: a field at row 4, column 20 means nothing
// until you see "ACCOUNT NO" to its left. The other is whether this screen has
// been seen before. Exploration files everything it learns — what a screen is
// for, which keys lead somewhere, what each field means — under a screen hash,
// and nothing told the model the hash of the screen in front of it, so the
// knowledge could be listed but never applied.

// fieldLabelMaxRunes bounds a label so a paragraph of protected text to the
// left of a field does not become the field's name.
const fieldLabelMaxRunes = 48

// fieldLabels gives each unprotected field the nearest protected text that
// precedes it: on the same row first, then on the row above, which is where a
// column heading sits. A field with neither has no label.
func fieldLabels(s *host.Screen) map[*host.Field]string {
	out := map[*host.Field]string{}
	if s == nil || !s.IsFormatted {
		return out
	}
	type caption struct {
		row, start, end int
		text            string
	}
	var captions []caption
	for _, f := range s.Fields {
		if f == nil || !f.IsProtected() || f.IsHidden() || f.IsZeroLength() || f.IsMultiline() {
			continue
		}
		text := cleanLabel(f.GetValue())
		if text == "" {
			continue
		}
		// The caption's extent is its text, not its field: a protected field
		// often runs on for the rest of the row after a short word.
		raw := []rune(f.GetValue())
		lead := 0
		for lead < len(raw) && (raw[lead] == ' ' || raw[lead] == 0) {
			lead++
		}
		trail := len(raw)
		for trail > lead && (raw[trail-1] == ' ' || raw[trail-1] == 0) {
			trail--
		}
		captions = append(captions, caption{row: f.StartY, start: f.StartX + lead, end: f.StartX + trail - 1, text: text})
	}

	for _, f := range s.Fields {
		if f == nil || f.IsProtected() || f.IsZeroLength() {
			continue
		}
		best, found := caption{}, false
		for _, cap := range captions {
			if cap.row == f.StartY && cap.end < f.StartX && (!found || cap.end > best.end) {
				best, found = cap, true
			}
		}
		if !found {
			// The row above: a heading over the field, or failing that the
			// nearest one that starts to its left.
			for _, cap := range captions {
				if cap.row != f.StartY-1 || cap.start > f.EndX {
					continue
				}
				overlaps := cap.end >= f.StartX
				bestOverlaps := found && best.end >= f.StartX
				switch {
				case !found, overlaps && !bestOverlaps, overlaps == bestOverlaps && cap.start > best.start:
					best, found = cap, true
				}
			}
		}
		if found {
			out[f] = best.text
		}
	}
	return out
}

// cleanLabel reduces a run of protected text to the words in it: collapsed
// whitespace, and none of the dot leaders, colons and arrows forms are ruled
// with.
func cleanLabel(text string) string {
	text = strings.ReplaceAll(text, "\x00", " ")
	text = strings.Join(strings.Fields(text), " ")
	text = strings.TrimRight(text, " .:=>-_")
	text = strings.TrimLeft(text, " .:=>-_")
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > fieldLabelMaxRunes {
		text = strings.TrimSpace(string(r[len(r)-fieldLabelMaxRunes:]))
	}
	return text
}

// mindMapFor returns what exploration has learned in this session, or nil.
func (app *App) mindMapFor(s *session.Session) *chaos.MindMap {
	if app == nil || s == nil || app.chaosEngines == nil {
		return nil
	}
	if app.chaosEngines.isRemoved(s.ID) {
		return nil
	}
	return app.sessionChaosMindMap(s)
}

// sessionScreenJSON renders a screen for a model with its identity attached:
// the hash exploration files it under and, when it is a screen exploration
// has already seen, what is known about it.
//
// The identity is taken from the screen as the host painted it; the rendering
// is of the screen as the operator's model setting displays it.
func (app *App) sessionScreenJSON(s *session.Session, screen *host.Screen) gin.H {
	if screen == nil {
		return gin.H{}
	}
	displayed := screen
	if rows, cols, ok := app.modelDimensions(); ok {
		displayed = limitScreenForDisplay(screen, rows, cols)
	}
	out := screenToJSON(displayed)

	mm := app.mindMapFor(s)
	if hash := chaos.CurrentScreenHash(mm, screen); hash != "" {
		out["screenHash"] = hash
	}
	if hash, area, ok := chaos.AreaForScreen(mm, screen); ok {
		out["knownScreen"] = knownScreenSummary(hash, area)
	}
	return out
}

// knownScreenSummary is the short form attached to every screen result: enough
// to act on, small enough to send every time.
func knownScreenSummary(hash string, area *chaos.MindMapArea) gin.H {
	out := gin.H{
		"hash":  hash,
		"label": area.Label,
	}
	if area.BusinessPurpose != "" {
		out["businessPurpose"] = area.BusinessPurpose
	}
	if semantics := safeFieldSemantics(area); len(semantics) > 0 {
		out["fieldSemantics"] = semantics
	}
	if keys := chaos.ProductiveKeys(area); len(keys) > 0 {
		out["productiveKeys"] = keys
	}
	return out
}

// sensitiveFieldKeys names the fields of an area whose values must not be
// shown: the ones the host marked non-display, and the ones an annotation
// marked sensitive.
func sensitiveFieldKeys(area *chaos.MindMapArea) map[string]bool {
	out := map[string]bool{}
	if area == nil {
		return out
	}
	for key, meta := range area.FieldMetadata {
		if meta.Hidden {
			out[key] = true
		}
	}
	for key, sem := range area.FieldSemantics {
		if sem.Sensitive {
			out[key] = true
		}
	}
	return out
}

// safeFieldSemantics returns an area's field annotations with the example
// value removed from any field that is hidden or marked sensitive. An example
// for a password field is a password.
func safeFieldSemantics(area *chaos.MindMapArea) map[string]chaos.BusinessFieldSemantic {
	if area == nil || len(area.FieldSemantics) == 0 {
		return nil
	}
	sensitive := sensitiveFieldKeys(area)
	out := make(map[string]chaos.BusinessFieldSemantic, len(area.FieldSemantics))
	for key, sem := range area.FieldSemantics {
		if sensitive[key] {
			sem.Example = ""
			sem.Sensitive = true
		}
		out[key] = sem
	}
	return out
}

// safeKnownValues returns the values exploration found to work in each field,
// leaving out the fields whose values must not be shown. What exploration
// typed into a password field and found accepted is a working password.
func safeKnownValues(area *chaos.MindMapArea) map[string][]string {
	if area == nil || len(area.KnownWorkingValues) == 0 {
		return nil
	}
	sensitive := sensitiveFieldKeys(area)
	out := map[string][]string{}
	for key, values := range area.KnownWorkingValues {
		if sensitive[key] || len(values) == 0 {
			continue
		}
		out[key] = append([]string(nil), values...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// maskAreaPreview returns an area's stored preview with every hidden field's
// cells starred. The engine masks them when it captures the preview; this does
// it again from the field metadata, so a preview saved by an older build, or
// imported from a file, is not trusted to have been masked already.
func maskAreaPreview(area *chaos.MindMapArea) string {
	if area == nil || area.PreviewText == "" {
		return ""
	}
	lines := strings.Split(area.PreviewText, "\n")
	grid := make([][]rune, len(lines))
	for i, line := range lines {
		grid[i] = []rune(line)
	}
	width := area.ScreenWidth
	for _, meta := range area.FieldMetadata {
		if !meta.Hidden {
			continue
		}
		row, col := meta.Row-1, meta.Column-1
		for i := 0; i < meta.Length; i++ {
			if row >= 0 && row < len(grid) && col >= 0 && col < len(grid[row]) {
				grid[row][col] = '*'
			}
			col++
			if width > 0 && col >= width {
				col = 0
				row++
			}
		}
	}
	for i := range grid {
		lines[i] = strings.TrimRight(string(grid[i]), " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// knownScreenDetail is one discovered screen in full, as chaos_get_screen
// returns it.
func knownScreenDetail(mm *chaos.MindMap, hash string, area *chaos.MindMapArea) gin.H {
	out := gin.H{
		"hash":            hash,
		"label":           area.Label,
		"visits":          area.Visits,
		"inputFieldCount": area.InputFieldCount,
	}
	if area.BusinessPurpose != "" {
		out["businessPurpose"] = area.BusinessPurpose
	}
	if area.BusinessNotes != "" {
		out["notes"] = area.BusinessNotes
	}

	sensitive := sensitiveFieldKeys(area)
	semantics := safeFieldSemantics(area)
	known := safeKnownValues(area)
	keys := make([]string, 0, len(area.FieldMetadata))
	for key := range area.FieldMetadata {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := area.FieldMetadata[keys[i]], area.FieldMetadata[keys[j]]
		if a.Row != b.Row {
			return a.Row < b.Row
		}
		return a.Column < b.Column
	})
	fields := make([]gin.H, 0, len(keys))
	for _, key := range keys {
		meta := area.FieldMetadata[key]
		entry := gin.H{
			"fieldKey": key,
			// The key is 1-indexed; these are the 0-indexed coordinates
			// write_field and get_screen use.
			"row":     meta.Row - 1,
			"col":     meta.Column - 1,
			"length":  meta.Length,
			"numeric": meta.Numeric,
			"hidden":  meta.Hidden,
		}
		if sem, ok := semantics[key]; ok {
			entry["semantics"] = sem
		}
		if values := known[key]; len(values) > 0 {
			entry["knownWorkingValues"] = values
		}
		if sensitive[key] {
			entry["sensitive"] = true
		}
		fields = append(fields, entry)
	}
	out["fields"] = fields

	productive := gin.H{}
	var dead []string
	for key, press := range area.KeyPresses {
		if press == nil {
			continue
		}
		if press.Progressions <= 0 {
			if press.Presses > 0 {
				dead = append(dead, key)
			}
			continue
		}
		destinations := make([]gin.H, 0, len(press.Destinations))
		for dest, count := range press.Destinations {
			entry := gin.H{"hash": dest, "count": count}
			if target, ok := mm.Areas[dest]; ok && target != nil {
				entry["label"] = target.Label
				if target.BusinessPurpose != "" {
					entry["businessPurpose"] = target.BusinessPurpose
				}
			}
			destinations = append(destinations, entry)
		}
		sort.Slice(destinations, func(i, j int) bool {
			ci, _ := destinations[i]["count"].(int)
			cj, _ := destinations[j]["count"].(int)
			if ci != cj {
				return ci > cj
			}
			hi, _ := destinations[i]["hash"].(string)
			hj, _ := destinations[j]["hash"].(string)
			return hi < hj
		})
		productive[key] = gin.H{
			"presses":      press.Presses,
			"progressions": press.Progressions,
			"destinations": destinations,
		}
	}
	if len(productive) > 0 {
		out["productiveKeys"] = productive
	}
	if len(dead) > 0 {
		sort.Strings(dead)
		out["deadKeys"] = dead
	}
	if len(area.AutoBlockedKeys) > 0 {
		out["blockedKeys"] = area.AutoBlockedKeys
	}
	if preview := maskAreaPreview(area); preview != "" {
		out["preview"] = preview
	}
	return out
}

// KnownScreenHandler handles GET /copilot/known-screen?hash=<hash> — one
// discovered screen: what it is for, its fields and what they mean, which
// keys led where, and a preview with hidden values masked. 404 when
// exploration has no screen under that hash. Backs the chaos_get_screen tool.
//
// With no hash it answers for the screen the session is on now.
func (app *App) KnownScreenHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	mm := app.mindMapFor(s)
	hash := strings.TrimSpace(c.Query("hash"))

	if hash == "" {
		if h := app.sessionHost(s); h != nil && mm != nil {
			if err := h.UpdateScreen(); err == nil {
				if key, _, ok := chaos.AreaForScreen(mm, hostScreenSnapshot(h)); ok {
					hash = key
				}
			}
		}
		if hash == "" {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "the current screen is not one exploration has discovered; pass a hash from get_screen, chaos_list_screens or business_app_overview",
			})
			return
		}
	}
	if mm == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "nothing has been discovered in this session yet"})
		return
	}
	area, ok := mm.Areas[hash]
	if !ok || area == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no discovered screen has that hash; chaos_list_screens lists the ones there are"})
		return
	}
	c.JSON(http.StatusOK, knownScreenDetail(mm, hash, area))
}
