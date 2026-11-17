// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"strings"

	"github.com/jnnngs/3270Web/internal/chaos"
)

// What chaos mode learns about a hidden field never leaves the server.
//
// A 3270 field marked non-display is a password field in everything but name,
// and the engine has to remember what it typed there: a value that got the run
// past a sign-on screen is the most useful thing it knows. That memory lives
// in the mind map, next to every other field's, and the mind map is what the
// status endpoint, the screens list, the report, the export and the attempt
// log are all written from. Each of those used to print it as it stood.
//
// The rule here is that a serializer never chooses for itself. Anything that
// leaves the process goes through one of the two functions below:
//
//   - redactedMindMap, for something a person or a model is going to read. A
//     value is replaced by a fixed mask, so the reader can still see that a
//     working value is known without seeing what it is.
//   - exportableMindMap, for a document that will be fed back in. The value is
//     dropped instead of masked, because an import would otherwise teach the
//     engine that the mask itself is a password that works and type it into
//     the host.
//
// A field is secret when the host marked it hidden, or when somebody
// annotated it as sensitive.

// chaosHiddenValueMask stands in for a value typed into a secret field. Fixed
// length, so it does not give away how long the real value was.
const chaosHiddenValueMask = "********"

// chaosFieldIsSecret reports whether values for the field must be withheld.
func chaosFieldIsSecret(area *chaos.MindMapArea, fieldKey string) bool {
	if area == nil {
		return false
	}
	if meta, ok := area.FieldMetadata[fieldKey]; ok && meta.Hidden {
		return true
	}
	if sem, ok := area.FieldSemantics[fieldKey]; ok && sem.Sensitive {
		return true
	}
	return false
}

// chaosFieldKey builds the mind map's R<row>C<col>L<len> field key.
func chaosFieldKey(row, column, length int) string {
	return fmt.Sprintf("R%dC%dL%d", row, column, length)
}

// chaosAreaSecretFieldKeys lists every field key of the area that is secret,
// whichever of the area's maps mentions it.
func chaosAreaSecretFieldKeys(area *chaos.MindMapArea) map[string]struct{} {
	if area == nil {
		return nil
	}
	var out map[string]struct{}
	add := func(key string) {
		if out == nil {
			out = make(map[string]struct{})
		}
		out[key] = struct{}{}
	}
	for key, meta := range area.FieldMetadata {
		if meta.Hidden {
			add(key)
		}
	}
	for key, sem := range area.FieldSemantics {
		if sem.Sensitive {
			add(key)
		}
	}
	return out
}

// cloneChaosMindMap deep-copies a mind map through the package's own clone,
// which SavedRun.CloneRun exposes.
func cloneChaosMindMap(mm *chaos.MindMap) *chaos.MindMap {
	if mm == nil {
		return nil
	}
	return (&chaos.SavedRun{MindMap: mm}).CloneRun().MindMap
}

// redactedMindMap returns a copy of mm that is safe to show: every value
// recorded against a secret field is replaced by chaosHiddenValueMask. mm
// itself is left alone.
func redactedMindMap(mm *chaos.MindMap) *chaos.MindMap {
	return withoutMindMapSecrets(mm, chaosHiddenValueMask)
}

// exportableMindMap returns a copy of mm with every value recorded against a
// secret field removed outright, for a document meant to be imported again.
func exportableMindMap(mm *chaos.MindMap) *chaos.MindMap {
	return withoutMindMapSecrets(mm, "")
}

func withoutMindMapSecrets(mm *chaos.MindMap, mask string) *chaos.MindMap {
	if mm == nil {
		return nil
	}
	out := cloneChaosMindMap(mm)
	scrubMindMapSecrets(out, mask)
	return out
}

// scrubMindMapSecrets rewrites mm in place. With an empty mask the values are
// removed; otherwise each is replaced by the mask.
func scrubMindMapSecrets(mm *chaos.MindMap, mask string) {
	if mm == nil {
		return
	}
	secretByArea := make(map[string]map[string]struct{}, len(mm.Areas))
	for hash, area := range mm.Areas {
		if area == nil {
			continue
		}
		secrets := chaosAreaSecretFieldKeys(area)
		if len(secrets) == 0 {
			continue
		}
		secretByArea[hash] = secrets
		for key := range secrets {
			area.KnownWorkingValues = scrubValueList(area.KnownWorkingValues, key, mask)
			area.KnownTriedValues = scrubValueList(area.KnownTriedValues, key, mask)
			if fd, ok := area.FieldDiscovery[key]; ok {
				if fd.LastValue != "" {
					fd.LastValue = mask
				}
				if fd.LastWorkedValue != "" {
					fd.LastWorkedValue = mask
				}
				area.FieldDiscovery[key] = fd
			}
			if sem, ok := area.FieldSemantics[key]; ok && sem.Example != "" {
				sem.Example = mask
				area.FieldSemantics[key] = sem
			}
		}
	}
	for _, fn := range mm.BusinessFunctions {
		if fn == nil {
			continue
		}
		for si := range fn.Steps {
			step := &fn.Steps[si]
			secrets := secretByArea[step.ScreenHash]
			if len(secrets) == 0 {
				continue
			}
			for ii := range step.Inputs {
				in := &step.Inputs[ii]
				if _, secret := secrets[in.FieldKey]; secret && in.Value != "" {
					in.Value = mask
				}
			}
		}
		for pi := range fn.Parameters {
			p := &fn.Parameters[pi]
			if _, secret := secretByArea[p.ScreenHash][p.FieldKey]; secret && p.Example != "" {
				p.Example = mask
			}
		}
	}
}

func scrubValueList(values map[string][]string, key, mask string) map[string][]string {
	list, ok := values[key]
	if !ok {
		return values
	}
	if mask == "" || len(list) == 0 {
		delete(values, key)
		return values
	}
	values[key] = []string{mask}
	return values
}

// dropMaskedMindMapValues removes the display mask wherever it appears as a
// learned value. An imported map should never contain it — exports drop the
// values instead — but a map copied out of a status response does, and the
// engine would otherwise treat the mask as something worth typing.
func dropMaskedMindMapValues(mm *chaos.MindMap) {
	if mm == nil {
		return
	}
	strip := func(values map[string][]string) {
		for key, list := range values {
			kept := list[:0]
			for _, v := range list {
				if v != chaosHiddenValueMask {
					kept = append(kept, v)
				}
			}
			if len(kept) == 0 {
				delete(values, key)
			} else {
				values[key] = kept
			}
		}
	}
	for _, area := range mm.Areas {
		if area == nil {
			continue
		}
		strip(area.KnownWorkingValues)
		strip(area.KnownTriedValues)
		for key, fd := range area.FieldDiscovery {
			changed := false
			if fd.LastValue == chaosHiddenValueMask {
				fd.LastValue = ""
				changed = true
			}
			if fd.LastWorkedValue == chaosHiddenValueMask {
				fd.LastWorkedValue = ""
				changed = true
			}
			if changed {
				area.FieldDiscovery[key] = fd
			}
		}
		for key, sem := range area.FieldSemantics {
			if sem.Example == chaosHiddenValueMask {
				sem.Example = ""
				area.FieldSemantics[key] = sem
			}
		}
	}
	for _, fn := range mm.BusinessFunctions {
		if fn == nil {
			continue
		}
		for si := range fn.Steps {
			for ii := range fn.Steps[si].Inputs {
				if fn.Steps[si].Inputs[ii].Value == chaosHiddenValueMask {
					fn.Steps[si].Inputs[ii].Value = ""
				}
			}
		}
		for pi := range fn.Parameters {
			if fn.Parameters[pi].Example == chaosHiddenValueMask {
				fn.Parameters[pi].Example = ""
			}
		}
	}
}

// chaosAttemptWriteValue returns the value to publish for one field write of
// an attempt: the value itself, or the mask when the field it went into is
// secret. mm is the mind map of the run the attempt belongs to.
//
// An attempt names the screen it started on, and that is normally an area of
// the map. When it is not — the map was trimmed, or the attempt predates the
// area — the field's position is checked against every area instead: masking
// a value that did not need it costs nothing, and the other mistake is the
// one this file exists to prevent.
func chaosAttemptWriteValue(mm *chaos.MindMap, fromHash string, row, column, length int, value string) string {
	if value == "" || mm == nil {
		return value
	}
	key := chaosFieldKey(row, column, length)
	if area, ok := mm.Areas[strings.TrimSpace(fromHash)]; ok && area != nil {
		if chaosFieldIsSecret(area, key) {
			return chaosHiddenValueMask
		}
		if _, known := area.FieldMetadata[key]; known {
			return value
		}
	}
	for _, area := range mm.Areas {
		if chaosFieldIsSecret(area, key) {
			return chaosHiddenValueMask
		}
	}
	return value
}

// redactedChaosAttempts returns a copy of attempts with the values written
// into secret fields masked.
func redactedChaosAttempts(mm *chaos.MindMap, attempts []chaos.Attempt) []chaos.Attempt {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]chaos.Attempt, len(attempts))
	for i, attempt := range attempts {
		out[i] = redactedChaosAttempt(mm, attempt)
	}
	return out
}

func redactedChaosAttempt(mm *chaos.MindMap, attempt chaos.Attempt) chaos.Attempt {
	if len(attempt.FieldWrites) == 0 {
		return attempt
	}
	writes := make([]chaos.AttemptFieldWrite, len(attempt.FieldWrites))
	for i, fw := range attempt.FieldWrites {
		fw.Value = chaosAttemptWriteValue(mm, attempt.FromHash, fw.Row, fw.Column, fw.Length, fw.Value)
		writes[i] = fw
	}
	attempt.FieldWrites = writes
	return attempt
}
