// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"fmt"
	"regexp"
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

	// RejectedValues are values the application refused in a field (an error
	// message, or a bounce through an error screen). Kept apart from
	// KnownWorkingValues and never reused.
	RejectedValues map[string][]string `json:"rejectedValues,omitempty"`
	// SessionEndingValues are values that, submitted from this screen, threw
	// the run back onto its first screen. Never reused anywhere in the run.
	SessionEndingValues map[string][]string `json:"sessionEndingValues,omitempty"`
	// SessionEndingKeys are keys that ended the session from this screen.
	// They are also listed in AutoBlockedKeys.
	SessionEndingKeys []string `json:"sessionEndingKeys,omitempty"`
	// InputCandidates are values read off the screen itself (menu options,
	// listed commands, bracketed choices), per field key, in the order they
	// should be tried. CandidateTried records which have been submitted.
	InputCandidates map[string][]string `json:"inputCandidates,omitempty"`
	CandidateTried  map[string][]string `json:"candidateTried,omitempty"`
	// OptionFieldKey and CommandFieldKey name the field that takes a menu
	// option and the screen's command line, where the screen has them.
	OptionFieldKey  string `json:"optionFieldKey,omitempty"`
	CommandFieldKey string `json:"commandFieldKey,omitempty"`
	// ContentVariants counts the distinct bodies seen under this area: a
	// list that pages is one area with several variants.
	ContentVariants int `json:"contentVariants,omitempty"`
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

	// Inputs records, per destination, the field values that were on the
	// screen when this key led there (field key -> value; non-display fields
	// masked). A menu's Enter key reaches a different screen for every
	// option, so the key alone does not identify the edge. Bounded to
	// maxEdgeInputSets sets per destination, most recent last.
	Inputs map[string][]map[string]string `json:"inputs,omitempty"`
	// ContentChanges counts presses that stayed on the area but changed its
	// body; Rejections counts presses the application answered with an
	// error; SessionEnds counts presses that threw the run back to its first
	// screen.
	ContentChanges int `json:"contentChanges,omitempty"`
	Rejections     int `json:"rejections,omitempty"`
	SessionEnds    int `json:"sessionEnds,omitempty"`
}

// maxEdgeInputSets bounds the input sets remembered per (key, destination).
const maxEdgeInputSets = 3

// maxRejectedValuesPerField bounds the rejected values remembered per field.
const maxRejectedValuesPerField = 24

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
		next.RejectedValues = cloneStringListMap(area.RejectedValues)
		next.SessionEndingValues = cloneStringListMap(area.SessionEndingValues)
		next.InputCandidates = cloneStringListMap(area.InputCandidates)
		next.CandidateTried = cloneStringListMap(area.CandidateTried)
		if area.SessionEndingKeys != nil {
			next.SessionEndingKeys = append([]string(nil), area.SessionEndingKeys...)
		}
		if area.AutoBlockedKeys != nil {
			next.AutoBlockedKeys = append([]string(nil), area.AutoBlockedKeys...)
		}
		if area.AutoKnownKeys != nil {
			next.AutoKnownKeys = append([]string(nil), area.AutoKnownKeys...)
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
				if keyPress.Inputs != nil {
					kp.Inputs = make(map[string][]map[string]string, len(keyPress.Inputs))
					for to, sets := range keyPress.Inputs {
						kp.Inputs[to] = cloneInputSets(sets)
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

// observeScreen records a visit to an area.
//
// What identifies the area is fixed by the screen that founded it: label,
// preview, dimensions and dedup signature are written once and never again.
// Rewriting them on every visit let the signature drift with whatever was
// last on the screen, so an area that had absorbed one near-duplicate would
// then match the next slightly different screen, and so on down a chain of
// screens that had nothing to do with the first. Field metadata accumulates,
// since a merged area can legitimately show fields its founder did not.
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
	founding := area.DedupSignature == ""
	label, fieldCount, inputCount, numericCount, hiddenCount, fieldMeta := summarizeScreenArea(screen)
	if founding {
		area.ScreenWidth, area.ScreenHeight = screenDimensions(screen)
		area.FieldCount = fieldCount
		area.InputFieldCount = inputCount
		area.NumericFieldCount = numericCount
		area.HiddenFieldCount = hiddenCount
		area.DedupSignature = buildAreaDedupSignature(screen)
	}
	if area.PreviewText == "" {
		previewText, previewWidth, previewHeight := screenPreview(screen)
		if previewText != "" {
			area.PreviewText = previewText
			area.PreviewWidth = previewWidth
			area.PreviewHeight = previewHeight
		}
	}
	if area.Label == "" && label != "" {
		area.Label = label
	}
	if len(fieldMeta) > 0 {
		if area.FieldMetadata == nil {
			area.FieldMetadata = make(map[string]MindMapFieldMetadata, len(fieldMeta))
		}
		for key, meta := range fieldMeta {
			area.FieldMetadata[key] = meta
		}
	}
}

// recordAttempt folds one attempt into the area it started from.
//
// Field writes are keyed by the field's real length (fw.Length), the same
// key FieldMetadata uses, so what was learned about a field can be looked up
// again. Values of non-display fields arrive masked and are counted but
// never stored. An attempt that ended the session, or that the application
// rejected, teaches the opposite of a working value and is filed as such.
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

	progressed := attempt.Transitioned && !attempt.SessionEnded
	toHash := strings.TrimSpace(attempt.ToHash)
	if progressed && toHash != "" {
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
		if inputs := attemptInputSet(attempt); len(inputs) > 0 {
			if keyPress.Inputs == nil {
				keyPress.Inputs = make(map[string][]map[string]string)
			}
			keyPress.Inputs[toHash] = appendInputSet(keyPress.Inputs[toHash], inputs)
		}
	}
	if attempt.ContentChanged {
		keyPress.ContentChanges++
	}
	if attempt.Rejected {
		keyPress.Rejections++
	}
	if attempt.SessionEnded {
		keyPress.SessionEnds++
		if aidKey != "Enter" {
			area.SessionEndingKeys = mergeSortedUniqueStrings(area.SessionEndingKeys, []string{aidKey})
			area.AutoBlockedKeys = mergeSortedUniqueStrings(area.AutoBlockedKeys, []string{aidKey})
		}
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
	if progressed && successfulWrites > 0 && learningFieldCount > 0 {
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
		if fw.Masked {
			// Counted, never stored: the mask would otherwise be offered back
			// to the field as if it were something that had worked.
			if value != "" {
				fd.LastValue = maskedFieldValue
			}
			if progressed && fw.Success {
				fd.Progressions++
				fd.LastWorkedAt = attempt.Time
				fd.LastWorkedValue = maskedFieldValue
			}
			area.FieldDiscovery[fieldKey] = fd
			continue
		}
		if value != "" {
			fd.LastValue = value
			if fw.Success {
				area.KnownTriedValues[fieldKey] = appendUniqueLimited(area.KnownTriedValues[fieldKey], value, maxKnownValuesPerField*2)
				if aidKey == "Enter" && containsString(area.InputCandidates[fieldKey], value) {
					if area.CandidateTried == nil {
						area.CandidateTried = make(map[string][]string)
					}
					area.CandidateTried[fieldKey] = appendCandidate(area.CandidateTried[fieldKey], value)
				}
			}
		}
		if progressed && fw.Success {
			fd.Progressions++
			fd.LastWorkedAt = attempt.Time
			if value != "" {
				fd.LastWorkedValue = value
				area.KnownWorkingValues[fieldKey] = appendUniqueLimited(area.KnownWorkingValues[fieldKey], value, maxKnownValuesPerField)
			}
		}
		if fw.Success && value != "" {
			// Blame is only assigned when one field was written: with several,
			// there is no telling which one the application objected to.
			if attempt.Rejected && successfulWrites == 1 {
				if area.RejectedValues == nil {
					area.RejectedValues = make(map[string][]string)
				}
				area.RejectedValues[fieldKey] = appendUniqueLimited(area.RejectedValues[fieldKey], value, maxRejectedValuesPerField)
				area.KnownWorkingValues[fieldKey] = removeString(area.KnownWorkingValues[fieldKey], value)
			}
			if attempt.SessionEnded && aidKey == "Enter" {
				if area.SessionEndingValues == nil {
					area.SessionEndingValues = make(map[string][]string)
				}
				area.SessionEndingValues[fieldKey] = appendUniqueLimited(area.SessionEndingValues[fieldKey], value, maxRejectedValuesPerField)
				area.KnownWorkingValues[fieldKey] = removeString(area.KnownWorkingValues[fieldKey], value)
			}
		}
		area.FieldDiscovery[fieldKey] = fd
	}
}

// markValuesRejected files values as rejected after the fact: used when a
// submission led to an error screen that bounced straight back, which is
// only known one step later. Any credit the values were given as "working"
// is withdrawn.
func (m *MindMap) markValuesRejected(hash string, writes []AttemptFieldWrite) {
	if m == nil {
		return
	}
	area := m.Areas[strings.TrimSpace(hash)]
	if area == nil {
		return
	}
	successful := 0
	for _, fw := range writes {
		if fw.Success {
			successful++
		}
	}
	if successful != 1 {
		return
	}
	for _, fw := range writes {
		value := strings.TrimSpace(fw.Value)
		if !fw.Success || fw.Masked || value == "" {
			continue
		}
		fieldKey := mindMapFieldKey(fw.Row, fw.Column, fw.Length)
		if area.RejectedValues == nil {
			area.RejectedValues = make(map[string][]string)
		}
		area.RejectedValues[fieldKey] = appendUniqueLimited(area.RejectedValues[fieldKey], value, maxRejectedValuesPerField)
		area.KnownWorkingValues[fieldKey] = removeString(area.KnownWorkingValues[fieldKey], value)
	}
}

// attemptInputSet returns the field values an attempt submitted, keyed by
// field key, with non-display values masked.
func attemptInputSet(attempt Attempt) map[string]string {
	var out map[string]string
	for _, fw := range attempt.FieldWrites {
		if !fw.Success {
			continue
		}
		value := strings.TrimSpace(fw.Value)
		if fw.Masked {
			value = maskedFieldValue
		}
		if value == "" {
			continue
		}
		if out == nil {
			out = make(map[string]string)
		}
		out[mindMapFieldKey(fw.Row, fw.Column, fw.Length)] = value
	}
	return out
}

func inputSetsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// appendInputSet adds an input set to a bounded most-recent-last list,
// moving it to the end if it is already there.
func appendInputSet(sets []map[string]string, add map[string]string) []map[string]string {
	for i, existing := range sets {
		if inputSetsEqual(existing, add) {
			sets = append(sets[:i], sets[i+1:]...)
			break
		}
	}
	sets = append(sets, add)
	if len(sets) > maxEdgeInputSets {
		sets = sets[len(sets)-maxEdgeInputSets:]
	}
	return sets
}

func cloneInputSets(sets []map[string]string) []map[string]string {
	if sets == nil {
		return nil
	}
	out := make([]map[string]string, 0, len(sets))
	for _, set := range sets {
		c := make(map[string]string, len(set))
		for k, v := range set {
			c[k] = v
		}
		out = append(out, c)
	}
	return out
}

func cloneStringListMap(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func containsString(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

func removeString(list []string, value string) []string {
	for i, v := range list {
		if v == value {
			return append(list[:i:i], list[i+1:]...)
		}
	}
	return list
}

// normalizeMindMapFieldKeys repairs field keys written by versions that
// keyed a field write by the length of the text typed rather than the length
// of the field. Such a key ("R11C20L3" for a three-character value in an
// eight-character field) never matched the field's metadata key
// ("R11C20L8"), so nothing learned under it could be found again. Entries
// whose row and column match exactly one known field are moved under that
// field's key; anything else is left alone.
func normalizeMindMapFieldKeys(mm *MindMap) {
	if mm == nil {
		return
	}
	for _, area := range mm.Areas {
		if area == nil || len(area.FieldMetadata) == 0 {
			continue
		}
		type pos struct{ row, col int }
		byPos := make(map[pos]string, len(area.FieldMetadata))
		ambiguous := make(map[pos]bool)
		for key, meta := range area.FieldMetadata {
			p := pos{meta.Row, meta.Column}
			if _, dup := byPos[p]; dup {
				ambiguous[p] = true
			}
			byPos[p] = key
		}
		target := func(key string) (string, bool) {
			if _, ok := area.FieldMetadata[key]; ok {
				return "", false
			}
			row, col, _, ok := parseMindMapFieldKey(key)
			if !ok {
				return "", false
			}
			p := pos{row, col}
			if ambiguous[p] {
				return "", false
			}
			to, ok := byPos[p]
			return to, ok && to != key
		}
		rekeyLists := func(m map[string][]string, max int) {
			moves := make(map[string]string)
			for key := range m {
				if to, ok := target(key); ok {
					moves[key] = to
				}
			}
			for key, to := range moves {
				for _, v := range m[key] {
					m[to] = appendUniqueLimited(m[to], v, max)
				}
				delete(m, key)
			}
		}
		rekeyLists(area.KnownTriedValues, maxKnownValuesPerField*2)
		rekeyLists(area.KnownWorkingValues, maxKnownValuesPerField)
		rekeyLists(area.RejectedValues, maxRejectedValuesPerField)
		moves := make(map[string]string)
		for key := range area.FieldDiscovery {
			if to, ok := target(key); ok {
				moves[key] = to
			}
		}
		for key, to := range moves {
			in := area.FieldDiscovery[key]
			fd := area.FieldDiscovery[to]
			fd.Writes += in.Writes
			fd.WriteSuccesses += in.WriteSuccesses
			fd.Progressions += in.Progressions
			if in.LastTriedAt.After(fd.LastTriedAt) || fd.LastValue == "" {
				fd.LastTriedAt = in.LastTriedAt
				fd.LastValue = in.LastValue
			}
			if in.LastWorkedAt.After(fd.LastWorkedAt) || fd.LastWorkedValue == "" {
				fd.LastWorkedAt = in.LastWorkedAt
				fd.LastWorkedValue = in.LastWorkedValue
			}
			area.FieldDiscovery[to] = fd
			delete(area.FieldDiscovery, key)
		}
	}
}

// mergeAreaInto folds src's learning into dst. Counters and learned values
// are additive (presses, progressions, destinations, known values, field
// discovery); descriptive fields (label, preview, business annotations) keep
// dst's value when set and fill from src otherwise, so importing an older map
// never clobbers what the local run has already observed first-hand.
func mergeAreaInto(dst, src *MindMapArea) {
	if dst == nil || src == nil {
		return
	}
	dst.Visits += src.Visits
	if dst.FirstSeen.IsZero() || (!src.FirstSeen.IsZero() && src.FirstSeen.Before(dst.FirstSeen)) {
		dst.FirstSeen = src.FirstSeen
	}
	if src.LastSeen.After(dst.LastSeen) {
		dst.LastSeen = src.LastSeen
	}
	if dst.Label == "" {
		dst.Label = src.Label
	}
	if dst.PreviewText == "" {
		dst.PreviewText = src.PreviewText
		dst.PreviewWidth = src.PreviewWidth
		dst.PreviewHeight = src.PreviewHeight
	}
	if dst.ScreenWidth == 0 && dst.ScreenHeight == 0 {
		dst.ScreenWidth = src.ScreenWidth
		dst.ScreenHeight = src.ScreenHeight
	}
	if dst.FieldCount == 0 && dst.InputFieldCount == 0 {
		dst.FieldCount = src.FieldCount
		dst.InputFieldCount = src.InputFieldCount
		dst.NumericFieldCount = src.NumericFieldCount
		dst.HiddenFieldCount = src.HiddenFieldCount
	}
	if dst.DedupSignature == "" {
		dst.DedupSignature = src.DedupSignature
	}
	if len(src.FieldMetadata) > 0 {
		if dst.FieldMetadata == nil {
			dst.FieldMetadata = make(map[string]MindMapFieldMetadata, len(src.FieldMetadata))
		}
		for key, meta := range src.FieldMetadata {
			if _, ok := dst.FieldMetadata[key]; !ok {
				dst.FieldMetadata[key] = meta
			}
		}
	}
	if len(src.FieldDiscovery) > 0 {
		if dst.FieldDiscovery == nil {
			dst.FieldDiscovery = make(map[string]MindMapFieldDiscovery, len(src.FieldDiscovery))
		}
		for key, in := range src.FieldDiscovery {
			fd := dst.FieldDiscovery[key]
			fd.Writes += in.Writes
			fd.WriteSuccesses += in.WriteSuccesses
			fd.Progressions += in.Progressions
			if in.LastTriedAt.After(fd.LastTriedAt) {
				fd.LastTriedAt = in.LastTriedAt
			}
			if in.LastWorkedAt.After(fd.LastWorkedAt) {
				fd.LastWorkedAt = in.LastWorkedAt
			}
			if fd.LastValue == "" {
				fd.LastValue = in.LastValue
			}
			if fd.LastWorkedValue == "" {
				fd.LastWorkedValue = in.LastWorkedValue
			}
			dst.FieldDiscovery[key] = fd
		}
	}
	if len(src.KnownTriedValues) > 0 {
		if dst.KnownTriedValues == nil {
			dst.KnownTriedValues = make(map[string][]string, len(src.KnownTriedValues))
		}
		for key, values := range src.KnownTriedValues {
			for _, v := range values {
				dst.KnownTriedValues[key] = appendUniqueLimited(dst.KnownTriedValues[key], v, maxKnownValuesPerField*2)
			}
		}
	}
	if len(src.KnownWorkingValues) > 0 {
		if dst.KnownWorkingValues == nil {
			dst.KnownWorkingValues = make(map[string][]string, len(src.KnownWorkingValues))
		}
		for key, values := range src.KnownWorkingValues {
			for _, v := range values {
				dst.KnownWorkingValues[key] = appendUniqueLimited(dst.KnownWorkingValues[key], v, maxKnownValuesPerField)
			}
		}
	}
	if len(src.FieldCountProgressions) > 0 {
		if dst.FieldCountProgressions == nil {
			dst.FieldCountProgressions = make(map[int]int, len(src.FieldCountProgressions))
		}
		for count, score := range src.FieldCountProgressions {
			dst.FieldCountProgressions[count] += score
		}
	}
	dst.SingleFieldProgressions += src.SingleFieldProgressions
	dst.MultiFieldProgressions += src.MultiFieldProgressions
	if len(src.KeyPresses) > 0 {
		if dst.KeyPresses == nil {
			dst.KeyPresses = make(map[string]*MindMapKeyPress, len(src.KeyPresses))
		}
		for key, in := range src.KeyPresses {
			if in == nil {
				continue
			}
			kp, ok := dst.KeyPresses[key]
			if !ok || kp == nil {
				kp = &MindMapKeyPress{Destinations: make(map[string]int)}
				dst.KeyPresses[key] = kp
			}
			kp.Presses += in.Presses
			kp.Progressions += in.Progressions
			kp.SingleFieldProgressions += in.SingleFieldProgressions
			kp.MultiFieldProgressions += in.MultiFieldProgressions
			kp.ContentChanges += in.ContentChanges
			kp.Rejections += in.Rejections
			kp.SessionEnds += in.SessionEnds
			for to, sets := range in.Inputs {
				if kp.Inputs == nil {
					kp.Inputs = make(map[string][]map[string]string, len(in.Inputs))
				}
				for _, set := range cloneInputSets(sets) {
					kp.Inputs[to] = appendInputSet(kp.Inputs[to], set)
				}
			}
			if in.LastUsedAt.After(kp.LastUsedAt) {
				kp.LastUsedAt = in.LastUsedAt
			}
			if len(in.Destinations) > 0 {
				if kp.Destinations == nil {
					kp.Destinations = make(map[string]int, len(in.Destinations))
				}
				for to, count := range in.Destinations {
					kp.Destinations[to] += count
				}
			}
		}
	}
	if len(src.AutoBlockedKeys) > 0 {
		dst.AutoBlockedKeys = mergeSortedUniqueStrings(dst.AutoBlockedKeys, src.AutoBlockedKeys)
	}
	if len(src.SessionEndingKeys) > 0 {
		dst.SessionEndingKeys = mergeSortedUniqueStrings(dst.SessionEndingKeys, src.SessionEndingKeys)
	}
	mergeLists := func(dstMap *map[string][]string, srcMap map[string][]string, max int) {
		if len(srcMap) == 0 {
			return
		}
		if *dstMap == nil {
			*dstMap = make(map[string][]string, len(srcMap))
		}
		for key, values := range srcMap {
			for _, v := range values {
				(*dstMap)[key] = appendUniqueLimited((*dstMap)[key], v, max)
			}
		}
	}
	mergeLists(&dst.RejectedValues, src.RejectedValues, maxRejectedValuesPerField)
	mergeLists(&dst.SessionEndingValues, src.SessionEndingValues, maxRejectedValuesPerField)
	mergeLists(&dst.InputCandidates, src.InputCandidates, maxCandidatesPerField)
	mergeLists(&dst.CandidateTried, src.CandidateTried, maxCandidatesPerField)
	if dst.OptionFieldKey == "" {
		dst.OptionFieldKey = src.OptionFieldKey
	}
	if dst.CommandFieldKey == "" {
		dst.CommandFieldKey = src.CommandFieldKey
	}
	if src.ContentVariants > dst.ContentVariants {
		dst.ContentVariants = src.ContentVariants
	}
	if len(src.AutoKnownKeys) > 0 {
		dst.AutoKnownKeys = mergeSortedUniqueStrings(dst.AutoKnownKeys, src.AutoKnownKeys)
	}
	if dst.BusinessPurpose == "" {
		dst.BusinessPurpose = src.BusinessPurpose
	}
	if dst.BusinessNotes == "" {
		dst.BusinessNotes = src.BusinessNotes
	}
	if len(src.FieldSemantics) > 0 {
		if dst.FieldSemantics == nil {
			dst.FieldSemantics = make(map[string]BusinessFieldSemantic, len(src.FieldSemantics))
		}
		for key, sem := range src.FieldSemantics {
			if _, ok := dst.FieldSemantics[key]; !ok {
				dst.FieldSemantics[key] = sem
			}
		}
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
		collapsed := stripVolatileLabelTokens(trimmed)
		if collapsed != "" {
			return truncateForLabel(collapsed, 72)
		}
	}
	return fmt.Sprintf("%dx%d screen", screen.Height, screen.Width)
}

// volatileLabelTokenRe matches the dates and times of day applications print
// in their title row. A label that carries one names a different screen every
// day ("PET100 MAIN MENU 2026-11-16").
var volatileLabelTokenRe = regexp.MustCompile(
	`^(?:\d{4}[-/.]\d{1,2}[-/.]\d{1,2}|\d{1,2}[-/.]\d{1,2}[-/.]\d{2,4}|\d{1,2}:\d{2}(?::\d{2})?(?:[AaPp][Mm])?|\d{2}\.\d{3}|\d{1,2}[A-Za-z]{3}\d{2,4})$`)

// stripVolatileLabelTokens collapses whitespace and drops date and time
// tokens from a title row. If nothing else is left the row is returned as it
// was: a label made only of a date is still better than none.
func stripVolatileLabelTokens(line string) string {
	tokens := strings.Fields(line)
	kept := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if volatileLabelTokenRe.MatchString(strings.Trim(tok, "()[],;")) {
			continue
		}
		kept = append(kept, tok)
	}
	if len(kept) == 0 {
		return strings.Join(tokens, " ")
	}
	return strings.Join(kept, " ")
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
