// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import "sort"

// noveltyKeyBoost is the flat boost applied to a candidate AID key that has
// never been pressed from the current area. It is deliberately smaller than
// the default Enter weight (70) so a well-known productive key still leads,
// but large enough that low-weight PF keys (5) stop being starved once a
// screen has been visited a few times.
const noveltyKeyBoost = 25

// frontierProximityBoost is the maximum boost applied to a key whose known
// destinations lead toward the exploration frontier (areas that still have
// untried candidate keys). The boost decays with graph distance:
// frontierProximityBoost / (1 + distance).
const frontierProximityBoost = 60

// fallbackChaosKeyCandidates is the ordered key list fallbackChaosKey walks,
// and the candidate baseline used when no AID key weights are configured.
var fallbackChaosKeyCandidates = []string{
	"Enter", "PF(1)", "PF(2)", "PF(4)", "PF(7)", "PF(8)", "PF(12)", "Tab",
}

// CandidateKeys returns the normalized, sorted AID-key candidate set implied
// by the given weights map. An empty map falls back to the same list
// fallbackChaosKey walks, so callers analysing a run always have a non-empty
// baseline to measure "untried" against.
func CandidateKeys(weights map[string]int) []string {
	seen := make(map[string]struct{}, len(weights))
	if len(weights) == 0 {
		for _, k := range fallbackChaosKeyCandidates {
			seen[k] = struct{}{}
		}
	} else {
		for raw := range weights {
			key := normalizeChaosKeyName(raw)
			if key == "" {
				continue
			}
			seen[key] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CandidateKeys returns a copy of the engine's candidate AID-key set.
func (e *Engine) CandidateKeys() []string {
	if e == nil {
		return nil
	}
	return append([]string(nil), e.candidateKeys...)
}

// UntriedCandidateKeys returns the candidate keys that have never been pressed
// from the area, excluding keys the run auto-blocked on that area. A nil area
// reports every candidate as untried.
func UntriedCandidateKeys(area *MindMapArea, candidates []string) []string {
	blocked := make(map[string]struct{})
	if area != nil {
		for _, k := range area.AutoBlockedKeys {
			blocked[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(candidates))
	for _, k := range candidates {
		if _, isBlocked := blocked[k]; isBlocked {
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

// untriedCandidateKeyCountLocked counts the candidate keys never pressed from
// the area, ignoring globally blacklisted keys. Caller must hold e.mu.
func (e *Engine) untriedCandidateKeyCountLocked(area *MindMapArea) int {
	count := 0
	for _, k := range UntriedCandidateKeys(area, e.candidateKeys) {
		if isBlacklistedKeyInSet(e.blacklistedKeys, k) {
			continue
		}
		count++
	}
	return count
}

// noveltyKeyBoostsLocked returns boosts for candidate keys that have never
// been pressed from the given area. A brand-new area (no presses recorded at
// all) returns nil: with nothing learned yet the configured weights are
// already the right distribution, and boosting everything equally would only
// flatten them. Caller must hold e.mu.
func (e *Engine) noveltyKeyBoostsLocked(hash string) map[string]int {
	if e.mindMap == nil || hash == "" {
		return nil
	}
	area, ok := e.mindMap.Areas[hash]
	if !ok || area == nil || len(area.KeyPresses) == 0 {
		return nil
	}
	boosts := make(map[string]int)
	for _, k := range UntriedCandidateKeys(area, e.candidateKeys) {
		boosts[k] = noveltyKeyBoost
	}
	if len(boosts) == 0 {
		return nil
	}
	return boosts
}

// frontierKeyBoostsLocked steers the run back toward unexplored territory.
// When the current area has no untried candidate keys left, keys whose known
// destinations lead (over the discovered transition graph) toward areas that
// still have untried keys get a boost that decays with distance. Without this
// a run that wanders into a well-explored corner of the application keeps
// re-rolling the same exhausted screen until saturation, even though it has
// already learned a path back to screens with untried actions.
// Caller must hold e.mu.
func (e *Engine) frontierKeyBoostsLocked(currentHash string) map[string]int {
	if e.mindMap == nil || currentHash == "" {
		return nil
	}
	current, ok := e.mindMap.Areas[currentHash]
	if !ok || current == nil || len(current.KeyPresses) == 0 {
		return nil
	}
	// Local exploration first: while this screen still has untried keys, the
	// novelty boost is the right steering, not a trip elsewhere.
	if e.untriedCandidateKeyCountLocked(current) > 0 {
		return nil
	}

	// Frontier = every other area with at least one untried candidate key.
	frontier := make(map[string]bool)
	for hash, area := range e.mindMap.Areas {
		if hash == currentHash || area == nil {
			continue
		}
		if e.untriedCandidateKeyCountLocked(area) > 0 {
			frontier[hash] = true
		}
	}
	if len(frontier) == 0 {
		return nil
	}

	dist := frontierDistances(e.mindMap, frontier)

	boosts := make(map[string]int)
	for key, kp := range current.KeyPresses {
		if kp == nil || len(kp.Destinations) == 0 {
			continue
		}
		best := -1
		for to := range kp.Destinations {
			d, ok := dist[to]
			if !ok {
				continue
			}
			if best < 0 || d < best {
				best = d
			}
		}
		if best < 0 {
			continue
		}
		if b := frontierProximityBoost / (1 + best); b > 0 {
			boosts[key] = b
		}
	}
	if len(boosts) == 0 {
		return nil
	}
	return boosts
}

// frontierDistances runs a multi-source BFS from the frontier set over the
// reversed transition graph, returning for each area the length of the
// shortest known forward path from it to any frontier area. Frontier areas
// themselves have distance 0.
func frontierDistances(mm *MindMap, frontier map[string]bool) map[string]int {
	// Reverse adjacency: for each observed transition from -> to, record
	// to -> from so BFS from the frontier walks paths backwards.
	reverse := make(map[string][]string, len(mm.Areas))
	for fromHash, area := range mm.Areas {
		if area == nil {
			continue
		}
		for _, kp := range area.KeyPresses {
			if kp == nil {
				continue
			}
			for toHash := range kp.Destinations {
				reverse[toHash] = append(reverse[toHash], fromHash)
			}
		}
	}

	dist := make(map[string]int, len(mm.Areas))
	queue := make([]string, 0, len(frontier))
	for hash := range frontier {
		dist[hash] = 0
		queue = append(queue, hash)
	}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		for _, prev := range reverse[node] {
			if _, seen := dist[prev]; seen {
				continue
			}
			dist[prev] = dist[node] + 1
			queue = append(queue, prev)
		}
	}
	return dist
}

// frontierStatsLocked summarises the exploration frontier for CoverageStats:
// how many discovered areas still have untried candidate keys, and how many
// untried keys remain in total. Caller must hold e.mu.
func (e *Engine) frontierStatsLocked() (frontierAreas, untriedKeys int) {
	if e.mindMap == nil {
		return 0, 0
	}
	for _, area := range e.mindMap.Areas {
		if area == nil {
			continue
		}
		n := e.untriedCandidateKeyCountLocked(area)
		if n > 0 {
			frontierAreas++
			untriedKeys += n
		}
	}
	return frontierAreas, untriedKeys
}
