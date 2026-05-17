package main

import (
	"encoding/json"
	"testing"
)

// TestChaosScreenHintUpsertRequestBindsSnakeCase verifies the bug fix for
// chaos_save_screen_hint: the public MCP tool descriptor advertises
// snake_case keys (screen_hash, known_data, known_keys, blocked_keys), and
// the handler must accept them.
func TestChaosScreenHintUpsertRequestBindsSnakeCase(t *testing.T) {
	body := []byte(`{
		"screen_hash": "7f5ce57e112fdfff",
		"known_keys": ["Enter"],
		"blocked_keys": ["PF3"],
		"known_data": ["ALICE","SMITH"]
	}`)
	var req chaosScreenHintUpsertRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ScreenHash != "7f5ce57e112fdfff" {
		t.Errorf("ScreenHash = %q, want 7f5ce57e112fdfff", req.ScreenHash)
	}
	if got := req.KnownKeys; len(got) != 1 || got[0] != "Enter" {
		t.Errorf("KnownKeys = %v, want [Enter]", got)
	}
	if got := req.BlockedKeys; len(got) != 1 || got[0] != "PF3" {
		t.Errorf("BlockedKeys = %v, want [PF3]", got)
	}
	if got := req.KnownData; len(got) != 2 || got[0] != "ALICE" || got[1] != "SMITH" {
		t.Errorf("KnownData = %v, want [ALICE SMITH]", got)
	}
}

// TestChaosScreenHintUpsertRequestBindsCamelCase verifies the in-browser UI's
// existing camelCase payload still binds (backward compatibility).
func TestChaosScreenHintUpsertRequestBindsCamelCase(t *testing.T) {
	body := []byte(`{
		"screenHash": "abc123",
		"knownKeys": ["PF1","PF2"],
		"blockedKeys": ["PF3"],
		"knownData": ["VALUE"],
		"keyAssignments": {"Exit": "PF3"}
	}`)
	var req chaosScreenHintUpsertRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ScreenHash != "abc123" {
		t.Errorf("ScreenHash = %q, want abc123", req.ScreenHash)
	}
	if len(req.KnownKeys) != 2 {
		t.Errorf("KnownKeys = %v", req.KnownKeys)
	}
	if req.BlockedKeys[0] != "PF3" {
		t.Errorf("BlockedKeys = %v", req.BlockedKeys)
	}
	if req.KnownData[0] != "VALUE" {
		t.Errorf("KnownData = %v", req.KnownData)
	}
	if req.KeyAssignments["Exit"] != "PF3" {
		t.Errorf("KeyAssignments = %v", req.KeyAssignments)
	}
}

// TestChaosScreenHintUpsertRequestSnakeWinsOverCamel: when both are present
// for the same field, snake_case (the public form) takes precedence.
func TestChaosScreenHintUpsertRequestSnakeWinsOverCamel(t *testing.T) {
	body := []byte(`{
		"screen_hash": "snake",
		"screenHash": "camel"
	}`)
	var req chaosScreenHintUpsertRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ScreenHash != "snake" {
		t.Errorf("ScreenHash = %q, want snake (snake_case must win)", req.ScreenHash)
	}
}

// TestChaosStartRequestBindsSnakeCase: chaos_start tool advertises snake_case
// (max_steps, time_budget_sec, step_delay_sec, seed, max_field_length,
// screen_dedup_similarity); handler must bind them.
func TestChaosStartRequestBindsSnakeCase(t *testing.T) {
	body := []byte(`{
		"max_steps": 25,
		"time_budget_sec": 60,
		"step_delay_sec": 0.5,
		"seed": 42,
		"max_field_length": 12,
		"screen_dedup_similarity": 0.9,
		"dedup_mode": "structural",
		"saturation_steps": 10,
		"auto_block_exit_keys": true,
		"key_blacklist": ["PF3"],
		"force_override_existing_inputs": true
	}`)
	var req chaosStartRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.MaxSteps != 25 {
		t.Errorf("MaxSteps = %d, want 25", req.MaxSteps)
	}
	if req.TimeBudgetSec != 60 {
		t.Errorf("TimeBudgetSec = %v, want 60", req.TimeBudgetSec)
	}
	if req.StepDelaySec != 0.5 {
		t.Errorf("StepDelaySec = %v, want 0.5", req.StepDelaySec)
	}
	if req.Seed != 42 {
		t.Errorf("Seed = %d, want 42", req.Seed)
	}
	if req.MaxFieldLength != 12 {
		t.Errorf("MaxFieldLength = %d, want 12", req.MaxFieldLength)
	}
	if req.ScreenDedupSimilarity != 0.9 {
		t.Errorf("ScreenDedupSimilarity = %v, want 0.9", req.ScreenDedupSimilarity)
	}
	if req.DedupMode != "structural" {
		t.Errorf("DedupMode = %q, want structural", req.DedupMode)
	}
	if req.SaturationSteps != 10 {
		t.Errorf("SaturationSteps = %d, want 10", req.SaturationSteps)
	}
	if req.AutoBlockExitKeys == nil || !*req.AutoBlockExitKeys {
		t.Errorf("AutoBlockExitKeys = %v, want true", req.AutoBlockExitKeys)
	}
	if len(req.KeyBlacklist) != 1 || req.KeyBlacklist[0] != "PF3" {
		t.Errorf("KeyBlacklist = %v", req.KeyBlacklist)
	}
	if req.ForceOverrideExistingInputs == nil || !*req.ForceOverrideExistingInputs {
		t.Errorf("ForceOverrideExistingInputs = %v, want true", req.ForceOverrideExistingInputs)
	}
}

// TestChaosStartRequestBindsCamelCase: existing in-browser UI's camelCase
// payload still binds (backward compatibility).
func TestChaosStartRequestBindsCamelCase(t *testing.T) {
	body := []byte(`{
		"maxSteps": 50,
		"timeBudgetSec": 120,
		"stepDelaySec": 0.25,
		"maxFieldLength": 20,
		"screenDedupSimilarity": 0.8,
		"dedupMode": "exact",
		"saturationSteps": 5,
		"autoBlockExitKeys": false
	}`)
	var req chaosStartRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.MaxSteps != 50 {
		t.Errorf("MaxSteps = %d, want 50", req.MaxSteps)
	}
	if req.TimeBudgetSec != 120 {
		t.Errorf("TimeBudgetSec = %v, want 120", req.TimeBudgetSec)
	}
	if req.StepDelaySec != 0.25 {
		t.Errorf("StepDelaySec = %v, want 0.25", req.StepDelaySec)
	}
	if req.MaxFieldLength != 20 {
		t.Errorf("MaxFieldLength = %d, want 20", req.MaxFieldLength)
	}
	if req.ScreenDedupSimilarity != 0.8 {
		t.Errorf("ScreenDedupSimilarity = %v, want 0.8", req.ScreenDedupSimilarity)
	}
	if req.DedupMode != "exact" {
		t.Errorf("DedupMode = %q, want exact", req.DedupMode)
	}
	if req.SaturationSteps != 5 {
		t.Errorf("SaturationSteps = %d, want 5", req.SaturationSteps)
	}
	if req.AutoBlockExitKeys == nil || *req.AutoBlockExitKeys {
		t.Errorf("AutoBlockExitKeys = %v, want false", req.AutoBlockExitKeys)
	}
}
