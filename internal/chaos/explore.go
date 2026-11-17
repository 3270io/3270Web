// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"hash/fnv"
	"sort"
	"strings"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// Exploration planning: which keys are off limits on a screen, what on it is
// still untried, and how to get back to somewhere that has something left.

// maxTrackedValueHashes bounds the hashed sets used to count unique inputs
// and productive values. A hash costs eight bytes where the value itself
// could cost forty and was kept for the whole campaign; past the bound the
// count stops rising rather than the set growing.
const maxTrackedValueHashes = 1 << 16

// maxSavedInputSample bounds the sample of distinct input values written
// into a saved run (SavedRun.UniqueInputValues). The count is carried
// separately in SavedRunMeta.UniqueInputs.
const maxSavedInputSample = 256

// maxContentVariantsPerArea bounds how many distinct bodies of one area are
// remembered (and so how many times paging the same list counts as progress).
const maxContentVariantsPerArea = 12

// maxNavFailures is how many times a planned route to an area may fail
// before the area is treated as unreachable for the rest of the run.
const maxNavFailures = 2

// saturationBackstopFactor: a run whose no-progress streak reaches this many
// times SaturationSteps stops as saturated even if frontier areas remain on
// paper. It is a backstop against a frontier that can be planned to but
// never actually reached.
const saturationBackstopFactor = 6

// seekIdleSteps is how many unproductive steps the engine spends on a screen
// that has nothing untried left before heading for the frontier.
const seekIdleSteps = 3

func hashValue(value string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(value))
	return h.Sum64()
}

// noteUniqueInputLocked counts a typed value once. Non-display values are
// counted by hash like any other, so the count is right without the value
// being kept. Caller must hold e.mu.
func (e *Engine) noteUniqueInputLocked(value string, hidden bool) {
	if value == "" {
		return
	}
	if e.uniqueInputSeen == nil {
		e.uniqueInputSeen = make(map[uint64]struct{})
	}
	key := hashValue(value)
	if _, seen := e.uniqueInputSeen[key]; seen {
		return
	}
	if len(e.uniqueInputSeen) >= maxTrackedValueHashes {
		return
	}
	e.uniqueInputSeen[key] = struct{}{}
	e.uniqueInputCount++
	if !hidden && len(e.uniqueInputSample) < maxSavedInputSample {
		if e.uniqueInputSample == nil {
			e.uniqueInputSample = make(map[string]bool)
		}
		e.uniqueInputSample[value] = true
	}
}

// noteProductiveValueLocked reports whether value is new among the values
// that have caused a transition. Caller must hold e.mu.
func (e *Engine) noteProductiveValueLocked(value string) bool {
	if value == "" {
		return false
	}
	if e.productiveSeen == nil {
		e.productiveSeen = make(map[uint64]struct{})
	}
	key := hashValue(value)
	if _, seen := e.productiveSeen[key]; seen {
		return false
	}
	if len(e.productiveSeen) >= maxTrackedValueHashes {
		return false
	}
	e.productiveSeen[key] = struct{}{}
	return true
}

// touchMindMapLocked marks the mind map as changed. Status carries the
// counter so a poller can tell, without comparing two graphs, whether the
// mind map it already has is still current. Caller must hold e.mu.
func (e *Engine) touchMindMapLocked() {
	e.mindMapVersion++
}

// userHintedKeys returns the keys the user named for this screen. Naming a
// key is an explicit instruction, and overrides the engine's own caution
// about what the key's legend says.
func userHintedKeys(screenHint *ScreenHint) map[string]struct{} {
	if screenHint == nil {
		return nil
	}
	out := make(map[string]struct{})
	for _, raw := range screenHint.KnownKeys {
		if k := normalizeChaosKeyName(raw); k != "" {
			out[k] = struct{}{}
		}
	}
	for _, raw := range screenHint.KeyAssignments {
		if k := normalizeChaosKeyName(raw); k != "" {
			out[k] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// screenHintForAreaLocked returns the hint that applies to an area: its own,
// merged with the first-screen hint when the area is the first screen.
// Caller must hold e.mu.
func (e *Engine) screenHintForAreaLocked(hash string) *ScreenHint {
	hint := e.snapshotScreenHintLocked(hash)
	if hash != "" && hash == e.firstScreenHash {
		hint = mergeScreenHints(e.snapshotScreenHintLocked(FirstScreenHintKey), hint)
	}
	return hint
}

// blockedKeysForAreaLocked returns every key that must not be pressed on an
// area: the global blacklist, the keys the user blocked for that screen, and
// the keys the run blocked there itself (exit-style and destructive legend
// labels, and keys that ended the session). An auto-block lasts for the rest
// of the run; only the user naming the key in a hint for that screen lifts
// it. Caller must hold e.mu.
func (e *Engine) blockedKeysForAreaLocked(hash string, screenHint *ScreenHint) map[string]struct{} {
	var area *MindMapArea
	if e.mindMap != nil {
		area = e.mindMap.Areas[hash]
	}
	return e.blockedKeysWithAreaLocked(hash, area, screenHint)
}

// blockedKeysWithAreaLocked is blockedKeysForAreaLocked for a caller that
// already holds the area. Caller must hold e.mu.
func (e *Engine) blockedKeysWithAreaLocked(hash string, area *MindMapArea, screenHint *ScreenHint) map[string]struct{} {
	if screenHint == nil {
		screenHint = e.screenHintForAreaLocked(hash)
	}
	blocked := e.blacklistedKeys
	if screenHint != nil && len(screenHint.BlockedKeys) > 0 {
		blocked = mergeChaosKeySets(blocked, normalizeChaosKeySet(screenHint.BlockedKeys))
	}
	if area == nil || len(area.AutoBlockedKeys) == 0 {
		return blocked
	}
	hinted := userHintedKeys(screenHint)
	auto := make(map[string]struct{}, len(area.AutoBlockedKeys))
	for _, k := range area.AutoBlockedKeys {
		if _, ok := hinted[k]; ok {
			continue
		}
		auto[k] = struct{}{}
	}
	return mergeChaosKeySets(blocked, auto)
}

// recordLegendKeysLocked reads the screen's key legend and records on the
// area which keys the run will not press there and which look like
// navigation. Caller must hold e.mu.
func (e *Engine) recordLegendKeysLocked(hash string, screen *host.Screen) {
	if e.mindMap == nil || screen == nil {
		return
	}
	area := e.mindMap.Areas[hash]
	if area == nil {
		return
	}
	var autoBlocked []string
	if e.cfg.AutoBlockExitKeys {
		autoBlocked = inferAutoBlockedKeys(screen)
	}
	// Destructive keys are blocked whether or not exit-key blocking is on:
	// declining protection against signing off is not consent to delete.
	autoBlocked = append(autoBlocked, inferDestructiveKeys(screen)...)
	if len(autoBlocked) > 0 {
		area.AutoBlockedKeys = mergeSortedUniqueStrings(area.AutoBlockedKeys, autoBlocked)
	}
	if autoKnown := inferAutoNavigationKeys(screen); len(autoKnown) > 0 {
		area.AutoKnownKeys = mergeSortedUniqueStrings(area.AutoKnownKeys, autoKnown)
	}
}

// absorbMiningLocked stores what a screen offered on its area and in the
// run-wide pools. Caller must hold e.mu.
func (e *Engine) absorbMiningLocked(hash string, mined screenMining) {
	e.commandPool = appendBoundedPool(e.commandPool, mined.commands, maxCommandPool)
	e.identifierPool = appendBoundedPool(e.identifierPool, mined.identifiers, maxIdentifierPool)
	if e.mindMap == nil {
		return
	}
	area := e.mindMap.Areas[hash]
	if area == nil {
		return
	}
	if mined.optionField != "" && area.OptionFieldKey == "" {
		area.OptionFieldKey = mined.optionField
	}
	if mined.commandField != "" && area.CommandFieldKey == "" {
		area.CommandFieldKey = mined.commandField
	}
	for key, values := range mined.candidates {
		if area.InputCandidates == nil {
			area.InputCandidates = make(map[string][]string)
		}
		if _, exists := area.InputCandidates[key]; !exists && len(area.InputCandidates) >= maxCandidateFieldsPerArea {
			continue
		}
		area.InputCandidates[key] = mergeCandidateLists(area.InputCandidates[key], values)
	}
}

// untriedInput is one mined candidate that has not been submitted yet.
type untriedInput struct {
	fieldKey string
	value    string
}

// sessionEndingValueLocked reports whether value is one that ended the
// session earlier in the run. Caller must hold e.mu.
func (e *Engine) sessionEndingValueLocked(value string) bool {
	if len(e.sessionEndingValues) == 0 {
		return false
	}
	_, banned := e.sessionEndingValues[strings.ToUpper(strings.TrimSpace(value))]
	return banned
}

// untriedInputsLocked returns the area's mined candidates that have not been
// submitted, the option field's first. Caller must hold e.mu.
func (e *Engine) untriedInputsLocked(area *MindMapArea) []untriedInput {
	if area == nil || len(area.InputCandidates) == 0 {
		return nil
	}
	keys := make([]string, 0, len(area.InputCandidates))
	for key := range area.InputCandidates {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if (keys[i] == area.OptionFieldKey) != (keys[j] == area.OptionFieldKey) {
			return keys[i] == area.OptionFieldKey
		}
		return keys[i] < keys[j]
	})
	var out []untriedInput
	for _, key := range keys {
		for _, v := range area.InputCandidates[key] {
			if containsString(area.CandidateTried[key], v) ||
				containsString(area.RejectedValues[key], v) ||
				containsString(area.SessionEndingValues[key], v) ||
				e.sessionEndingValueLocked(v) {
				continue
			}
			out = append(out, untriedInput{fieldKey: key, value: v})
		}
	}
	return out
}

// untriedKeysLocked returns the candidate keys never pressed from the area
// and not blocked there. Caller must hold e.mu.
func (e *Engine) untriedKeysLocked(hash string, area *MindMapArea) []string {
	blocked := e.blockedKeysWithAreaLocked(hash, area, nil)
	var out []string
	for _, k := range e.candidateKeys {
		if isBlacklistedKeyInSet(blocked, k) {
			continue
		}
		if area != nil {
			if kp, ok := area.KeyPresses[k]; ok && kp != nil && kp.Presses > 0 {
				continue
			}
		}
		out = append(out, k)
	}
	return out
}

// areaHasUntriedLocked reports whether an area still has a key or a mined
// input the run has not tried. Caller must hold e.mu.
func (e *Engine) areaHasUntriedLocked(hash string, area *MindMapArea) bool {
	if area == nil {
		return false
	}
	if len(e.untriedKeysLocked(hash, area)) > 0 {
		return true
	}
	if isBlacklistedKeyInSet(e.blockedKeysWithAreaLocked(hash, area, nil), "Enter") {
		return false
	}
	return len(e.untriedInputsLocked(area)) > 0
}

// navStep is one hop of a planned route: on screen from, put inputs into
// their fields and press key, expecting to land on to.
type navStep struct {
	from   string
	key    string
	to     string
	inputs map[string]string
}

func edgeKey(from, key, to string) string {
	return from + "|" + key + "|" + to
}

// rememberEdgeInputsLocked keeps the real values (non-display ones included)
// that took the run along an edge, so the edge can be replayed later in the
// same run. These never leave the process. Caller must hold e.mu.
func (e *Engine) rememberEdgeInputsLocked(from, key, to string, inputs map[string]string) {
	if len(inputs) == 0 {
		return
	}
	if e.edgeInputs == nil {
		e.edgeInputs = make(map[string]map[string]string)
	}
	e.edgeInputs[edgeKey(from, key, to)] = inputs
}

// edgeInputsLocked returns the inputs to replay for an edge: the real values
// from this run if there are any, otherwise the most recent set recorded in
// the mind map (whose non-display values are masked). Caller must hold e.mu.
func (e *Engine) edgeInputsLocked(from, key, to string, kp *MindMapKeyPress) map[string]string {
	if real, ok := e.edgeInputs[edgeKey(from, key, to)]; ok {
		return real
	}
	if kp != nil {
		if sets := kp.Inputs[to]; len(sets) > 0 {
			return sets[len(sets)-1]
		}
	}
	return nil
}

// planPathLocked finds the shortest known route from an area to the nearest
// area accepted by isTarget, over the edges the run has observed. Each hop
// carries the inputs that produced it, because on a menu the key alone does
// not say where Enter goes. Keys blocked on a screen are not used, and edges
// that ended the session are not in the graph at all. Caller must hold e.mu.
func (e *Engine) planPathLocked(from string, isTarget func(hash string, area *MindMapArea) bool) []navStep {
	if e.mindMap == nil || from == "" {
		return nil
	}
	type visit struct {
		hash string
		prev int
		step navStep
	}
	queue := []visit{{hash: from, prev: -1}}
	seen := map[string]bool{from: true}
	for i := 0; i < len(queue); i++ {
		hash := queue[i].hash
		area := e.mindMap.Areas[hash]
		if area == nil {
			continue
		}
		blocked := e.blockedKeysForAreaLocked(hash, nil)
		keys := make([]string, 0, len(area.KeyPresses))
		for key, kp := range area.KeyPresses {
			if kp != nil && len(kp.Destinations) > 0 && !isBlacklistedKeyInSet(blocked, key) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			kp := area.KeyPresses[key]
			dests := make([]string, 0, len(kp.Destinations))
			for dest := range kp.Destinations {
				dests = append(dests, dest)
			}
			sort.Strings(dests)
			for _, dest := range dests {
				if seen[dest] || e.mindMap.Areas[dest] == nil {
					continue
				}
				seen[dest] = true
				queue = append(queue, visit{
					hash: dest,
					prev: i,
					step: navStep{from: hash, key: key, to: dest, inputs: e.edgeInputsLocked(hash, key, dest, kp)},
				})
				if isTarget(dest, e.mindMap.Areas[dest]) {
					var path []navStep
					for idx := len(queue) - 1; idx > 0; idx = queue[idx].prev {
						path = append(path, queue[idx].step)
					}
					for l, r := 0, len(path)-1; l < r; l, r = l+1, r-1 {
						path[l], path[r] = path[r], path[l]
					}
					return path
				}
			}
		}
	}
	return nil
}

// frontierTargetLocked is the predicate planPathLocked uses for "somewhere
// with something left to try". Caller must hold e.mu.
func (e *Engine) frontierTargetLocked(hash string, area *MindMapArea) bool {
	if e.navFailures[hash] >= maxNavFailures {
		return false
	}
	return e.areaHasUntriedLocked(hash, area)
}

// frontierReachableLocked reports whether there is anything left to try that
// the run can still get to from where it is: on this screen, or on one it
// knows a route to. Caller must hold e.mu.
func (e *Engine) frontierReachableLocked(hash string) bool {
	if e.mindMap == nil || hash == "" {
		return false
	}
	if e.areaHasUntriedLocked(hash, e.mindMap.Areas[hash]) {
		return true
	}
	return len(e.planPathLocked(hash, e.frontierTargetLocked)) > 0
}

// Step plans.
const (
	planDefault = iota // weighted key, generated values
	planInput          // one mined candidate into one field, then Enter
	planKey            // one untried key, nothing typed
	planNav            // one hop of a route towards the frontier
)

type stepPlan struct {
	kind   int
	key    string
	inputs map[string]string
	nav    *navStep
}

// seekThresholdLocked is the no-progress streak past which the run stops
// rolling dice on the current screen and heads for the frontier: half of
// SaturationSteps, or sooner when the screen has nothing untried left.
// Caller must hold e.mu.
func (e *Engine) seekThresholdLocked(locallyExhausted bool) int {
	half := e.cfg.SaturationSteps / 2
	if e.cfg.SaturationSteps <= 0 {
		half = 8
	}
	if locallyExhausted && half > seekIdleSteps {
		return seekIdleSteps
	}
	return half
}

// planStepLocked decides what kind of step to take on the current screen.
// Caller must hold e.mu.
func (e *Engine) planStepLocked(hash string, blocked map[string]struct{}, forceHints bool) stepPlan {
	// A route in progress comes first, as long as it still starts here.
	if len(e.navPlan) > 0 {
		if e.navPlan[0].from == hash && !isBlacklistedKeyInSet(blocked, e.navPlan[0].key) {
			step := e.navPlan[0]
			return stepPlan{kind: planNav, key: step.key, inputs: step.inputs, nav: &step}
		}
		e.abandonNavPlanLocked()
	}
	if forceHints || e.mindMap == nil {
		return stepPlan{kind: planDefault}
	}
	area := e.mindMap.Areas[hash]
	if area == nil {
		return stepPlan{kind: planDefault}
	}
	enterBlocked := isBlacklistedKeyInSet(blocked, "Enter")
	var inputs []untriedInput
	if !enterBlocked {
		inputs = e.untriedInputsLocked(area)
	}
	var keys []string
	for _, k := range e.candidateKeys {
		if isBlacklistedKeyInSet(blocked, k) {
			continue
		}
		if kp, ok := area.KeyPresses[k]; ok && kp != nil && kp.Presses > 0 {
			continue
		}
		keys = append(keys, k)
	}
	locallyExhausted := len(inputs) == 0 && len(keys) == 0
	seeking := e.saturationStreak > e.seekThresholdLocked(locallyExhausted)

	if len(inputs) > 0 {
		next := inputs[0]
		// A menu is worked through option by option. Other mined values are
		// tried most of the time, leaving room for the unplanned.
		if next.fieldKey == area.OptionFieldKey || seeking || e.rng.Intn(100) < 70 {
			return stepPlan{kind: planInput, key: "Enter", inputs: map[string]string{next.fieldKey: next.value}}
		}
	}
	// Enter is left to the default step, which fills fields before pressing
	// it; an untried function key is pressed on its own.
	var bareKeys []string
	for _, k := range keys {
		if k != "Enter" {
			bareKeys = append(bareKeys, k)
		}
	}
	if len(bareKeys) > 0 && len(area.KeyPresses) > 0 && (seeking || e.rng.Intn(100) < 50) {
		return stepPlan{kind: planKey, key: bareKeys[e.rng.Intn(len(bareKeys))]}
	}
	if seeking && locallyExhausted {
		if path := e.planPathLocked(hash, e.frontierTargetLocked); len(path) > 0 {
			e.navPlan = path
			e.navTarget = path[len(path)-1].to
			step := path[0]
			return stepPlan{kind: planNav, key: step.key, inputs: step.inputs, nav: &step}
		}
	}
	return stepPlan{kind: planDefault}
}

// abandonNavPlanLocked drops the route in progress and counts the failure
// against where it was heading. Caller must hold e.mu.
func (e *Engine) abandonNavPlanLocked() {
	if e.navTarget != "" {
		if e.navFailures == nil {
			e.navFailures = make(map[string]int)
		}
		e.navFailures[e.navTarget]++
	}
	e.navPlan = nil
	e.navTarget = ""
}

// advanceNavPlanLocked moves the route on after a hop landed where it was
// meant to, or abandons it if it did not. Caller must hold e.mu.
func (e *Engine) advanceNavPlanLocked(step *navStep, landed string) {
	if step == nil || len(e.navPlan) == 0 {
		return
	}
	if landed != step.to {
		e.abandonNavPlanLocked()
		return
	}
	e.navPlan = e.navPlan[1:]
	if len(e.navPlan) == 0 {
		e.navTarget = ""
	}
}

// noteContentVariantLocked records a body seen under an area and reports
// whether it is new. Caller must hold e.mu.
func (e *Engine) noteContentVariantLocked(hash, rawHash string) bool {
	if hash == "" || rawHash == "" {
		return false
	}
	if e.contentSeen == nil {
		e.contentSeen = make(map[string]map[string]struct{})
	}
	seen := e.contentSeen[hash]
	if seen == nil {
		seen = make(map[string]struct{})
		e.contentSeen[hash] = seen
	}
	if _, ok := seen[rawHash]; ok {
		return false
	}
	if len(seen) >= maxContentVariantsPerArea {
		return false
	}
	seen[rawHash] = struct{}{}
	if e.mindMap != nil {
		if area := e.mindMap.Areas[hash]; area != nil {
			area.ContentVariants = len(seen)
		}
	}
	return true
}

// maskedTransitions returns a copy of the transitions with the text of every
// fill step that went into a non-display field replaced by the mask.
func maskedTransitions(in []Transition) []Transition {
	out := make([]Transition, len(in))
	for i, tr := range in {
		out[i] = tr
		if len(tr.HiddenSteps) == 0 {
			continue
		}
		steps := make([]session.WorkflowStep, len(tr.Steps))
		copy(steps, tr.Steps)
		for _, idx := range tr.HiddenSteps {
			if idx >= 0 && idx < len(steps) {
				steps[idx].Text = maskedFieldValue
			}
		}
		out[i].Steps = steps
	}
	return out
}
