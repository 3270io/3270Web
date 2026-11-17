// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// bounceState remembers the last submission that moved to another screen,
// so that if the very next step comes straight back, and the screen in
// between read as an error, the submission can be filed as a rejection.
type bounceState struct {
	from      string
	to        string
	writes    []AttemptFieldWrite
	rejecting bool
}

// runState is what the run loop carries from step to step.
type runState struct {
	deadline  time.Time
	firstPass bool
	logFile   *os.File
}

// run is the main exploration loop executed in a goroutine.
//
// A panic anywhere in a step is recovered and recorded as the run's error.
// Unrecovered, it took the whole server down with it; recovered silently, it
// would leave a run that simply stopped. Either way the run is finished off
// properly: reason recorded, log closed, output written.
func (e *Engine) run() {
	rs := &runState{firstPass: true}
	e.mu.Lock()
	rs.logFile = e.transitionLog
	e.transitionLog = nil
	e.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[chaos] run panicked: %v\n%s", r, debug.Stack())
			e.mu.Lock()
			e.lastErr = fmt.Sprintf("chaos run panicked: %v", r)
			e.terminationReason = TerminationReasonError
			e.mu.Unlock()
		}
		e.finishRun(rs)
	}()

	if e.cfg.TimeBudget > 0 {
		rs.deadline = time.Now().Add(e.cfg.TimeBudget)
	}
	for {
		if stop := e.runStep(rs); stop {
			return
		}
		// Inter-step delay (cancellable).
		if e.cfg.StepDelay > 0 {
			select {
			case <-e.stopCh:
				return
			case <-time.After(e.cfg.StepDelay):
			}
		}
		rs.firstPass = false
	}
}

// finishRun records why the run ended, closes the transition log and writes
// the output file.
func (e *Engine) finishRun(rs *runState) {
	e.mu.Lock()
	if e.terminationReason == "" {
		if e.lastErr != "" {
			e.terminationReason = TerminationReasonError
		} else {
			e.terminationReason = TerminationReasonStopped
		}
	}
	outputFile := strings.TrimSpace(e.cfg.OutputFile)
	e.mu.Unlock()

	if rs != nil && rs.logFile != nil {
		_ = rs.logFile.Close()
		rs.logFile = nil
	}

	if outputFile != "" {
		if err := e.writeOutputFile(outputFile); err != nil {
			log.Printf("[chaos] could not write output file %s: %v", outputFile, err)
			e.mu.Lock()
			e.outputFileErr = err.Error()
			e.mu.Unlock()
		}
	}

	e.mu.Lock()
	e.active = false
	e.stoppedAt = time.Now()
	e.mu.Unlock()
}

// writeOutputFile writes the learned workflow to path atomically: a run that
// is killed or runs out of disk part-way leaves the previous file, or none,
// never half of one.
func (e *Engine) writeOutputFile(path string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("export panicked: %v", r)
		}
	}()
	data, err := e.ExportWorkflow("", 0)
	if err != nil {
		return fmt.Errorf("export workflow: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	return WriteFileAtomic(path, data, 0600)
}

// endWithError stops the run with an error and the matching reason.
func (e *Engine) endWithError(msg string) {
	e.mu.Lock()
	e.lastErr = msg
	if e.terminationReason == "" {
		e.terminationReason = TerminationReasonError
	}
	e.mu.Unlock()
}

func (e *Engine) observeMindMapAreaLocked(hash string, screen *host.Screen, seenAt time.Time) {
	if e.mindMap == nil {
		e.mindMap = newMindMap()
	}
	e.mindMap.observeScreen(hash, screen, seenAt)
	e.touchMindMapLocked()
}

// hostErases reports whether the host can erase to end of field with a key
// press that does not reach the application. The Host interface has no call
// for it, so this is known only for the terminal-backed hosts; any other
// host gets the blank-overwrite path.
func (e *Engine) hostErases() bool {
	if e.eraseUnsupported {
		return false
	}
	switch e.h.(type) {
	case *host.S3270, *host.GoSampleAppHost:
		return true
	}
	return false
}

// eraseField empties a field.
func (e *Engine) eraseField(f *host.Field) error {
	if e.hostErases() {
		if err := e.h.MoveCursor(f.StartY, f.StartX); err == nil {
			if err := e.h.SendKey("EraseEOF"); err == nil {
				return nil
			}
		}
		e.eraseUnsupported = true
	}
	return e.clearFieldBeforeWrite(f)
}

// writeField puts value into a field, replacing whatever is there, and
// returns the text actually written ("" when there was nothing to write).
//
// A value that fills the field needs one write. A shorter one is written and
// the remainder erased with EraseEOF where the host can do that; otherwise
// the field is blanked first. Either way that is fewer round trips than the
// blank-then-write every field used to get, and it leaves a numeric value
// exactly as generated instead of padded out with digits.
func (e *Engine) writeField(f *host.Field, value string, keepSame bool) (string, error) {
	text := value
	if !e.cfg.ForceOverrideExistingInputs {
		if text == "" {
			return "", nil
		}
		return text, e.h.WriteStringAt(f.StartY, f.StartX, text)
	}
	text = e.prepareFieldWriteValueWithPolicy(f, value, keepSame)
	if text == "" {
		return "", nil
	}
	width := fieldLength(f)
	if len([]rune(text)) >= width {
		return text, e.h.WriteStringAt(f.StartY, f.StartX, text)
	}
	if e.hostErases() {
		if err := e.h.WriteStringAt(f.StartY, f.StartX, text); err != nil {
			return text, err
		}
		// The cursor is left just past the text, still inside the field.
		if err := e.h.SendKey("EraseEOF"); err != nil {
			e.eraseUnsupported = true
		}
		return text, nil
	}
	if err := e.h.WriteStringAt(f.StartY, f.StartX, strings.Repeat(" ", width)); err != nil {
		if !f.IsNumeric() {
			return text, fmt.Errorf("clear field: %v", err)
		}
		// Some hosts reject blanks in a numeric field. Leading zeros replace
		// the old content without changing the number.
		if pad := e.effectiveFieldWriteLength(f) - len([]rune(text)); pad > 0 {
			text = strings.Repeat("0", pad) + text
		}
	}
	return text, e.h.WriteStringAt(f.StartY, f.StartX, text)
}

// snapshotGenContextLocked gathers what the value generator needs to know
// about the current screen. Caller must hold e.mu.
func (e *Engine) snapshotGenContextLocked(hash string, screenHint *ScreenHint, forceHints bool) *genContext {
	ctx := &genContext{
		known:      e.snapshotAreaValuesLocked(hash),
		tried:      e.snapshotAreaTriedValuesLocked(hash),
		screenHint: screenHint,
		forceHints: forceHints,
	}
	// Real values of non-display fields that worked live only here.
	for key, values := range e.hiddenValues[hash] {
		if ctx.known == nil {
			ctx.known = make(map[string][]string)
		}
		ctx.known[key] = append([]string(nil), values...)
	}
	if e.mindMap != nil {
		if area := e.mindMap.Areas[hash]; area != nil {
			ctx.rejected = cloneStringListMap(area.RejectedValues)
			ctx.candidates = cloneStringListMap(area.InputCandidates)
			ctx.candidateTried = cloneStringListMap(area.CandidateTried)
			ctx.commandField = area.CommandFieldKey
		}
	}
	ctx.commandPool = append([]string(nil), e.commandPool...)
	// Away from the first screen, nothing that ended the session is typed
	// again. On the first screen the same word may simply be a user id.
	if hash != e.firstScreenHash && len(e.sessionEndingValues) > 0 {
		ctx.sessionEnding = make(map[string]struct{}, len(e.sessionEndingValues))
		for v := range e.sessionEndingValues {
			ctx.sessionEnding[v] = struct{}{}
		}
	}
	return ctx
}

// fieldByKey finds the input field a mind-map field key refers to.
func fieldByKey(fields []*host.Field, key string) *host.Field {
	row, col, _, ok := parseMindMapFieldKey(key)
	if !ok {
		return nil
	}
	for _, f := range fields {
		if f != nil && f.StartY+1 == row && f.StartX+1 == col {
			return f
		}
	}
	return nil
}

// runStep performs one exploration step: read the screen, decide what to do
// on it, do it, and record what happened. It reports whether the run should
// stop.
func (e *Engine) runStep(rs *runState) (stop bool) {
	select {
	case <-e.stopCh:
		return true
	default:
	}

	e.mu.Lock()
	steps := e.stepsRun
	e.mu.Unlock()
	isFirstAttempt := rs.firstPass

	if e.cfg.MaxSteps > 0 && steps >= e.cfg.MaxSteps {
		e.mu.Lock()
		if e.terminationReason == "" {
			e.terminationReason = TerminationReasonMaxSteps
		}
		e.mu.Unlock()
		return true
	}
	if !rs.deadline.IsZero() && time.Now().After(rs.deadline) {
		e.mu.Lock()
		if e.terminationReason == "" {
			e.terminationReason = TerminationReasonTimeBudget
		}
		e.mu.Unlock()
		return true
	}

	// Read the current screen state (retried on transient host errors).
	if _, err := e.retryHostOp(e.h.UpdateScreen); err != nil {
		e.endWithError(err.Error())
		return true
	}
	screen := e.screenSnapshot()
	if screen == nil {
		// No screen is not a normal way for a run to end: say so, rather
		// than stopping with nothing to show why.
		e.endWithError("host returned no screen")
		return true
	}

	rawHash := hashScreen(screen)
	currentHash := e.canonicalizeScreenHash(rawHash, screen)
	mined := mineScreen(screen)
	inputFields := unprotectedFields(screen)
	now := time.Now()

	attempt := Attempt{
		Attempt:  steps + 1,
		Time:     now,
		FromHash: currentHash,
	}

	// Everything that reads or writes shared state for the decision happens
	// under one hold of the lock; the host I/O that follows happens outside.
	e.mu.Lock()
	// Capture the current screen before any writes/key presses so the mind map
	// always records the true starting screen for this attempt.
	e.observeMindMapAreaLocked(currentHash, screen, now)
	e.noteContentVariantLocked(currentHash, rawHash)
	e.absorbMiningLocked(currentHash, mined)
	e.recordLegendKeysLocked(currentHash, screen)
	applyFirstScreenHint, firstScreenHintReason := e.shouldApplyFirstScreenHintForHashLocked(currentHash, isFirstAttempt)
	if currentHash == e.firstScreenHash && screenHasHiddenInput(screen) {
		e.firstScreenIsGate = true
	}
	screenHint := e.snapshotScreenHintLocked(currentHash)
	if applyFirstScreenHint {
		firstScreenHint := e.snapshotScreenHintLocked(FirstScreenHintKey)
		screenHint = mergeScreenHints(firstScreenHint, screenHint)
		attempt.FirstScreenHintApplied = true
		attempt.FirstScreenHintReason = firstScreenHintReason
	}
	blocked := e.blockedKeysForAreaLocked(currentHash, screenHint)
	forceHintValues := applyFirstScreenHint && e.hasUserFieldHints(screenHint)
	gen := e.snapshotGenContextLocked(currentHash, screenHint, forceHintValues)
	plan := e.planStepLocked(currentHash, blocked, forceHintValues)
	e.mu.Unlock()

	// Which fields to fill.
	var fields []*host.Field
	switch plan.kind {
	case planInput, planNav:
		for key := range plan.inputs {
			if f := fieldByKey(inputFields, key); f != nil {
				fields = append(fields, f)
			}
		}
		sortFieldsByPosition(fields)
	case planKey:
		// Nothing typed: the key is pressed on its own.
	default:
		switch {
		case forceHintValues:
			fields = e.selectFirstScreenTargetFields(inputFields)
		case screenHasHiddenInput(screen) && len(inputFields) <= 8:
			// A sign-on screen wants all of its fields. Filling one of two
			// only ever earns "a password is required".
			fields = inputFields
		default:
			fields = e.selectTargetFieldsForScreen(currentHash, inputFields)
		}
	}
	attempt.FieldsTargeted = len(fields)
	switch plan.kind {
	case planInput:
		attempt.Plan = "input"
	case planKey:
		attempt.Plan = "key"
	case planNav:
		attempt.Plan = "navigate"
	}

	// snapshotKeyBoostsLocked reads e.mindMap under the lock; it must be
	// called with e.mu held. AnnotateArea/UpsertBusinessFunction can mutate
	// e.mindMap.Areas concurrently (reachable from the Copilot tools while a
	// run is active), so reading it unlocked is a data race / panic.
	var keyBoosts, explorationBoosts map[string]int
	if plan.kind == planDefault {
		e.mu.Lock()
		keyBoosts = e.snapshotKeyBoostsLocked(currentHash, attempt.FieldsTargeted)
		// Exploration steering is kept separate from the learned boosts: it is
		// deliberately NOT scaled by LearnedKeyReuseBias, because turning that
		// bias down means "explore more", which must not also mute the
		// exploration boosts themselves.
		explorationBoosts = mergeKeyBoostMaps(e.noveltyKeyBoostsLocked(currentHash), e.frontierKeyBoostsLocked(currentHash))
		e.mu.Unlock()
		keyBoosts = mergeKeyBoostMaps(keyBoosts, e.hintKeyBoostsForScreen(screen))
		keyBoosts = mergeKeyBoostMaps(keyBoosts, e.screenHintKeyBoostsForScreen(screen, screenHint))
		// What the legend says may re-weight keys the configuration allows;
		// it may not bring in one it does not — except a key this screen
		// labels as paging, which is admitted here and nowhere else.
		allowedKeys := e.candidateKeys
		if e.cfg.AutoPreferNavigationKeys {
			if paging := inferPagingKeys(screen); len(paging) > 0 {
				allowedKeys = mergeSortedUniqueStrings(append([]string(nil), e.candidateKeys...), paging)
			}
		}
		helpBoosts := restrictBoostsToKeys(inferScreenHelpKeyBoosts(screen), allowedKeys)
		if !e.cfg.AutoPreferNavigationKeys {
			// The operator declined the navigation-label steering, not the
			// protection: penalties for exit/logoff-style labels stay live.
			helpBoosts = negativeKeyBoostsOnly(helpBoosts)
		}
		keyBoosts = mergeKeyBoostMaps(keyBoosts, helpBoosts)
	}

	// A planned step types into one field and relies on the rest being as
	// the application left them. Anything the engine itself typed there on
	// an earlier attempt, and the application is still showing, is cleared
	// first: stale junk on a command line outranks the option being tried.
	if plan.kind == planInput || plan.kind == planNav {
		for _, f := range inputFields {
			if f == nil || f.IsHidden() || containsField(fields, f) {
				continue
			}
			current := strings.TrimSpace(f.GetValue())
			if current == "" {
				continue
			}
			key := mindMapFieldKey(f.StartY+1, f.StartX+1, fieldLength(f))
			if containsString(gen.tried[key], current) {
				_ = e.eraseField(f)
			}
		}
	}

	// Fill the chosen fields.
	var batchSteps []session.WorkflowStep
	var hiddenSteps []int
	realInputs := make(map[string]string)
	type typedValue struct {
		key    string
		value  string
		hidden bool
	}
	var typed []typedValue
	for idx, f := range fields {
		length := fieldLength(f)
		key := mindMapFieldKey(f.StartY+1, f.StartX+1, length)
		hidden := f.IsHidden()
		value, planned := plan.inputs[key]
		if !planned || value == "" || value == maskedFieldValue {
			// A replayed edge whose non-display value is not known any more
			// (it was masked in a saved run) gets a generated one.
			planned = planned && value != "" && value != maskedFieldValue
			value = e.generateFieldValue(f, idx == 0, gen)
		}
		if value == "" {
			continue
		}
		fieldAttempt := AttemptFieldWrite{
			Row:    f.StartY + 1,
			Column: f.StartX + 1,
			Length: length,
			Masked: hidden,
		}
		written, err := e.writeField(f, value, forceHintValues || planned)
		if written == "" && err == nil {
			continue
		}
		fieldAttempt.TypedLength = len([]rune(written))
		fieldAttempt.Value = written
		if hidden {
			fieldAttempt.Value = maskedFieldValue
		}
		if err != nil {
			// Non-fatal: skip this field.
			fieldAttempt.Error = err.Error()
			attempt.FieldWrites = append(attempt.FieldWrites, fieldAttempt)
			continue
		}
		fieldAttempt.Success = true
		attempt.FieldWrites = append(attempt.FieldWrites, fieldAttempt)
		attempt.FieldsWritten++
		if trimmed := strings.TrimSpace(written); trimmed != "" {
			realInputs[key] = trimmed
			typed = append(typed, typedValue{key: key, value: trimmed, hidden: hidden})
		}
		if hidden {
			hiddenSteps = append(hiddenSteps, len(batchSteps))
		}
		batchSteps = append(batchSteps, session.WorkflowStep{
			Type: "FillString",
			Coordinates: &session.WorkflowCoordinates{
				Row:    f.StartY + 1, // workflow uses 1-based coordinates
				Column: f.StartX + 1,
			},
			Text: written,
		})
	}

	// Choose and send an AID key. A plan names its key; otherwise the choice
	// is weighted, preferring keys that previously caused screen transitions
	// from the current area, and drawn only from keys that may be pressed
	// here.
	aidKey := plan.key
	if aidKey == "" || isBlacklistedKeyInSet(blocked, aidKey) {
		aidKey = e.chooseAIDKeyBlocked(keyBoosts, explorationBoosts, blocked)
	}
	attempt.AIDKey = aidKey
	if strings.TrimSpace(aidKey) == "" {
		// No usable key for this screen: every candidate is blacklisted.
		// This is a recoverable, expected stop, not a host failure — mark it
		// "blocked" (without setting lastErr) so the caller can relax the
		// blacklist / add hints and resume instead of seeing an error run.
		attempt.Error = "no non-blacklisted AID key available"
		attempt.Outcome = OutcomeError
		e.mu.Lock()
		if e.terminationReason == "" {
			e.terminationReason = TerminationReasonBlocked
		}
		e.recordMindMapAttemptLocked(attempt)
		e.appendAttemptLocked(attempt)
		e.mu.Unlock()
		e.afterAttempt(rs, attempt)
		return true
	}
	sendRetries, err := e.retryHostOp(func() error { return e.h.SendKey(aidKey) })
	attempt.Retries += sendRetries
	if err == nil {
		batchSteps = append(batchSteps, session.WorkflowStep{Type: aidKeyToStepType(aidKey)})
		// Refresh the screen after the key press (retried on transient errors).
		var refreshRetries int
		refreshRetries, err = e.retryHostOp(e.h.UpdateScreen)
		attempt.Retries += refreshRetries
	}
	if err != nil {
		attempt.Error = err.Error()
		attempt.Outcome = OutcomeError
		e.mu.Lock()
		e.lastErr = err.Error()
		e.recordMindMapAttemptLocked(attempt)
		e.appendAttemptLocked(attempt)
		e.mu.Unlock()
		e.afterAttempt(rs, attempt)
		return true
	}

	newScreen := e.screenSnapshot()
	newHash := ""
	newRawHash := ""
	if newScreen != nil {
		newRawHash = hashScreen(newScreen)
		newHash = e.canonicalizeScreenHash(newRawHash, newScreen)
	}
	attempt.ToHash = newHash
	attempt.Transitioned = newHash != "" && newHash != currentHash

	// What did the press do? Decided outside the lock: it only reads the two
	// screen copies.
	landedRejecting := false
	switch {
	case attempt.Transitioned:
		attempt.Outcome = OutcomeTransition
		landedRejecting = textLooksLikeRejection(messageZoneText(newScreen))
	case newHash != "":
		outcome, message := sameAreaOutcome(screen, newScreen)
		attempt.Outcome = outcome
		attempt.Message = message
		attempt.Rejected = outcome == OutcomeRejected
		attempt.ContentChanged = outcome == OutcomeContent
	default:
		attempt.Outcome = OutcomeNoChange
	}

	// Record the step and any state transition.
	e.mu.Lock()
	if attempt.Transitioned && e.firstScreenIsGate && newHash == e.firstScreenHash && currentHash != e.firstScreenHash {
		// Back on the sign-on screen from somewhere deeper: whatever was
		// just submitted ended the session. That is the opposite of a
		// value that works, and it must not be learned as one.
		attempt.SessionEnded = true
		attempt.Outcome = OutcomeSessionEnded
		if aidKey == "Enter" {
			for _, tv := range typed {
				if e.sessionEndingValues == nil {
					e.sessionEndingValues = make(map[string]struct{})
				}
				e.sessionEndingValues[strings.ToUpper(tv.value)] = struct{}{}
			}
		}
	}
	progressed := attempt.Transitioned && !attempt.SessionEnded

	newScreenObserved := false
	if newHash != "" {
		if _, ok := e.mindMap.Areas[newHash]; !ok {
			newScreenObserved = true
		}
		e.observeMindMapAreaLocked(newHash, newScreen, attempt.Time)
	}
	newContentVariant := false
	if attempt.ContentChanged {
		newContentVariant = e.noteContentVariantLocked(currentHash, newRawHash)
	}

	// A submission that went to an error screen which came straight back is
	// a rejection, known only now.
	if bounce := e.lastBounce; bounce != nil && attempt.Transitioned &&
		bounce.rejecting && bounce.to == currentHash && bounce.from == newHash {
		e.mindMap.markValuesRejected(bounce.from, bounce.writes)
	}
	e.lastBounce = nil
	if progressed && attempt.FieldsWritten > 0 {
		e.lastBounce = &bounceState{
			from:      currentHash,
			to:        newHash,
			writes:    append([]AttemptFieldWrite(nil), attempt.FieldWrites...),
			rejecting: landedRejecting,
		}
	}

	e.recordMindMapAttemptLocked(attempt)
	if plan.kind == planInput {
		// A candidate whose field is not on this variant of the screen can
		// never be submitted; retire it rather than plan it for ever.
		if area := e.mindMap.Areas[currentHash]; area != nil {
			for key, value := range plan.inputs {
				if _, wrote := realInputs[key]; wrote {
					continue
				}
				if area.CandidateTried == nil {
					area.CandidateTried = make(map[string][]string)
				}
				area.CandidateTried[key] = appendCandidate(area.CandidateTried[key], value)
			}
		}
	}
	if progressed {
		e.rememberEdgeInputsLocked(currentHash, aidKey, newHash, realInputs)
		for _, tv := range typed {
			if !tv.hidden {
				continue
			}
			if e.hiddenValues == nil {
				e.hiddenValues = make(map[string]map[string][]string)
			}
			if e.hiddenValues[currentHash] == nil {
				e.hiddenValues[currentHash] = make(map[string][]string)
			}
			e.hiddenValues[currentHash][tv.key] = appendUniqueLimited(e.hiddenValues[currentHash][tv.key], tv.value, 4)
		}
	}
	if plan.kind == planNav {
		e.advanceNavPlanLocked(plan.nav, newHash)
	}
	if attempt.SessionEnded {
		e.navPlan = nil
		e.navTarget = ""
	}

	// A step is "productive" if it added a new screen, a new (from,to,aid)
	// transition tuple, a new field value that caused a transition, or a
	// body of the same screen not seen before (a list paged). Computed
	// before the appends below so both e.steps and e.transitions can be
	// bounded by it: without this, a long-running (or many-times-resumed via
	// "extend limits") exploration re-recorded the same already-discovered
	// transition edge, and every non-productive repeat step, forever.
	tupleKey := currentHash + "|" + newHash + "|" + aidKey
	newTuple := false
	if progressed && !e.transitionTuples[tupleKey] {
		e.transitionTuples[tupleKey] = true
		newTuple = true
	}
	newProductiveValue := false
	for _, tv := range typed {
		e.noteUniqueInputLocked(tv.value, tv.hidden)
		if progressed && e.noteProductiveValueLocked(tv.value) {
			newProductiveValue = true
		}
	}
	productive := newScreenObserved || newTuple || newProductiveValue || newContentVariant

	e.stepsRun++
	// e.steps is an unconditional, flat step log used only as a
	// last-resort export fallback (see ExportWorkflowWithSuccessBalance)
	// when neither transitions nor attempts yield anything — e.g. a run
	// that saturates without ever making progress. 3270Connect's
	// playback pipeline requires *some* non-empty workflow in that case,
	// so this can't be limited to productive steps the way e.transitions
	// is; a trailing window bounds its memory instead.
	for i, bs := range batchSteps {
		e.steps = append(e.steps, bs)
		e.stepsHidden = append(e.stepsHidden, containsInt(hiddenSteps, i))
	}
	if len(e.steps) > maxRecentRawSteps {
		e.steps = e.steps[len(e.steps)-maxRecentRawSteps:]
	}
	if len(e.stepsHidden) > len(e.steps) {
		e.stepsHidden = e.stepsHidden[len(e.stepsHidden)-len(e.steps):]
	}
	e.screenHashes[currentHash] = true
	if newHash != "" {
		e.screenHashes[newHash] = true
	}
	e.aidKeyCounts[aidKey]++
	if newTuple {
		e.transitions = append(e.transitions, Transition{
			FromHash:    currentHash,
			ToHash:      newHash,
			Steps:       batchSteps,
			AIDKey:      aidKey,
			HiddenSteps: hiddenSteps,
		})
	}
	recordAttempt := !e.cfg.ExcludeNoProgressEvents || attempt.Transitioned || attempt.Error != ""
	if recordAttempt {
		e.appendAttemptLocked(attempt)
	}

	// Saturation: the streak grows with every unproductive step. Past half
	// of SaturationSteps the planner stops rolling dice and works through
	// what is left (see planStepLocked). The run is only declared saturated
	// once there is nothing left that it can reach.
	appendSlidingBool(&e.newScreensWindow, newScreenObserved, coverageWindowSize)
	appendSlidingBool(&e.newTransWindow, newTuple, coverageWindowSize)
	if productive {
		e.saturationStreak = 0
	} else {
		e.saturationStreak++
	}
	saturated := false
	if e.cfg.SaturationSteps > 0 && e.saturationStreak >= e.cfg.SaturationSteps {
		standing := newHash
		if standing == "" {
			standing = currentHash
		}
		if e.saturationStreak >= e.cfg.SaturationSteps*saturationBackstopFactor || !e.frontierReachableLocked(standing) {
			saturated = true
		}
	}
	if saturated && e.terminationReason == "" {
		e.terminationReason = TerminationReasonSaturated
		// If this run discovered no transitions at all, resuming it will
		// only re-saturate on the same screen. Flag it so the caller stops
		// the resume loop and switches to updating hints / manual nav.
		if len(e.transitions) == 0 {
			e.saturatedNoProgress = true
		}
	}
	e.mu.Unlock()

	e.afterAttempt(rs, attempt)
	return saturated
}

// afterAttempt does the per-attempt work that needs no lock: the transition
// log line and the test hook. The log is written here rather than under
// e.mu so that a slow disk stalls only the run, not every status request
// queued behind the lock.
func (e *Engine) afterAttempt(rs *runState, attempt Attempt) {
	if rs != nil && rs.logFile != nil {
		if entry, err := json.Marshal(attempt); err == nil {
			entry = append(entry, '\n')
			if _, writeErr := rs.logFile.Write(entry); writeErr != nil {
				// Disable the log on the first failure (disk full, closed
				// file, ...) instead of silently dropping every attempt.
				log.Printf("[chaos] transition log write failed, disabling: %v", writeErr)
				_ = rs.logFile.Close()
				rs.logFile = nil
			}
		}
	}
	if e.attemptHook != nil {
		e.attemptHook(attempt)
	}
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func containsField(fields []*host.Field, f *host.Field) bool {
	for _, x := range fields {
		if x == f {
			return true
		}
	}
	return false
}

func sortFieldsByPosition(fields []*host.Field) {
	for i := 1; i < len(fields); i++ {
		for j := i; j > 0; j-- {
			a, b := fields[j-1], fields[j]
			if a.StartY < b.StartY || (a.StartY == b.StartY && a.StartX <= b.StartX) {
				break
			}
			fields[j-1], fields[j] = fields[j], fields[j-1]
		}
	}
}
