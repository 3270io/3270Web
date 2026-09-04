// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/jnnngs/3270Web/internal/session"
)

// normalizeKey canonicalizes a caller-supplied AID/navigation key name (e.g.
// "pf3", "PF(3)", "enter") to the exact form host.Host.SendKey expects. It
// returns ok=false for empty, malformed, or unrecognized input — callers
// must reject the request rather than substitute a default, since silently
// falling back to "Enter" for anything unrecognized means a typo or a
// malformed request (from a script, an API caller, or an AI model choosing
// a key by name) submits the current screen instead of failing loudly. That
// matters most for machine-driven callers (the Copilot send_key tool, the
// REST API): a request for a key that doesn't exist should be rejected, not
// silently reinterpreted as "press Enter and submit".
func normalizeKey(key string) (normalized string, ok bool) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "", false
	}

	// Reject characters that could be interpreted as s3270 script commands.
	if strings.ContainsAny(trimmed, "\n\r\t;") {
		log.Printf("Security warning: detected potential command injection in key: %q", key)
		return "", false
	}

	upper := strings.ToUpper(trimmed)
	lower := strings.ToLower(trimmed)

	if strings.HasPrefix(upper, "PF(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PF("), ")")
		if n, err := strconv.Atoi(inner); err == nil {
			if n >= 1 && n <= 24 {
				return fmt.Sprintf("PF(%d)", n), true
			}
		}
	}
	if strings.HasPrefix(upper, "PA(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PA("), ")")
		if n, err := strconv.Atoi(inner); err == nil {
			if n >= 1 && n <= 3 {
				return fmt.Sprintf("PA(%d)", n), true
			}
		}
	}
	if strings.HasPrefix(upper, "PF") {
		if n, err := strconv.Atoi(strings.TrimPrefix(upper, "PF")); err == nil {
			if n >= 1 && n <= 24 {
				return fmt.Sprintf("PF(%d)", n), true
			}
		}
	}
	if strings.HasPrefix(upper, "PA") {
		if n, err := strconv.Atoi(strings.TrimPrefix(upper, "PA")); err == nil {
			if n >= 1 && n <= 3 {
				return fmt.Sprintf("PA(%d)", n), true
			}
		}
	}
	if strings.HasPrefix(upper, "F") {
		if n, err := strconv.Atoi(strings.TrimPrefix(upper, "F")); err == nil {
			if n >= 1 && n <= 24 {
				return fmt.Sprintf("PF(%d)", n), true
			}
		}
	}

	switch lower {
	case "enter":
		return "Enter", true
	case "tab":
		return "Tab", true
	case "backtab":
		return "BackTab", true
	case "clear":
		return "Clear", true
	case "reset":
		return "Reset", true
	case "eraseeof", "erase_eof":
		return "EraseEOF", true
	case "eraseinput", "erase_input":
		return "EraseInput", true
	case "dup":
		return "Dup", true
	case "fieldmark", "field_mark":
		return "FieldMark", true
	case "sysreq", "sys_req":
		return "SysReq", true
	case "attn":
		return "Attn", true
	case "newline", "new_line":
		return "Newline", true
	case "backspace":
		return "BackSpace", true
	case "delete":
		return "Delete", true
	case "insert":
		return "Insert", true
	case "home":
		return "Home", true
	case "up":
		return "Up", true
	case "down":
		return "Down", true
	case "left":
		return "Left", true
	case "right":
		return "Right", true
	}

	return "", false
}

// workflowStepForKey turns a key an operator pressed into the recording step
// that reproduces it. It goes through normalizeKey first and then covers
// every key that produces, which is also every key workflowKeyForStepType
// can turn back into a SendKey call — the pair round-trip each other.
//
// This used to recognise only Enter, Tab and PF(n), and recordActionKey
// treats a nil here as "there is nothing to record". Recording therefore
// pressed PA1, Clear, SysReq, Attn, and every other key on the keypad
// exactly as the operator did — the host received it and the screen
// changed — while the recording silently gained no step for it. Replaying
// the recording later left that keypress out with no error anywhere, and
// the only sign was a step missing from a list nobody was counting.
func workflowStepForKey(key string) *session.WorkflowStep {
	normalized, ok := normalizeKey(key)
	if !ok {
		return nil
	}
	switch normalized {
	case "Enter":
		return &session.WorkflowStep{Type: "PressEnter"}
	case "Tab":
		return &session.WorkflowStep{Type: "PressTab"}
	case "BackTab":
		return &session.WorkflowStep{Type: "PressBackTab"}
	case "Clear":
		return &session.WorkflowStep{Type: "PressClear"}
	case "Reset":
		return &session.WorkflowStep{Type: "PressReset"}
	case "EraseEOF":
		return &session.WorkflowStep{Type: "PressEraseEOF"}
	case "EraseInput":
		return &session.WorkflowStep{Type: "PressEraseInput"}
	case "Dup":
		return &session.WorkflowStep{Type: "PressDup"}
	case "FieldMark":
		return &session.WorkflowStep{Type: "PressFieldMark"}
	case "SysReq":
		return &session.WorkflowStep{Type: "PressSysReq"}
	case "Attn":
		return &session.WorkflowStep{Type: "PressAttn"}
	case "Newline":
		return &session.WorkflowStep{Type: "PressNewline"}
	case "BackSpace":
		return &session.WorkflowStep{Type: "PressBackspace"}
	case "Delete":
		return &session.WorkflowStep{Type: "PressDelete"}
	case "Insert":
		return &session.WorkflowStep{Type: "PressInsert"}
	case "Home":
		return &session.WorkflowStep{Type: "PressHome"}
	case "Up":
		return &session.WorkflowStep{Type: "PressUp"}
	case "Down":
		return &session.WorkflowStep{Type: "PressDown"}
	case "Left":
		return &session.WorkflowStep{Type: "PressLeft"}
	case "Right":
		return &session.WorkflowStep{Type: "PressRight"}
	}
	if strings.HasPrefix(normalized, "PF(") && strings.HasSuffix(normalized, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(normalized, "PF("), ")")
		if n, err := strconv.Atoi(inner); err == nil && n >= 1 && n <= 24 {
			return &session.WorkflowStep{Type: fmt.Sprintf("PressPF%d", n)}
		}
	}
	if strings.HasPrefix(normalized, "PA(") && strings.HasSuffix(normalized, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(normalized, "PA("), ")")
		if n, err := strconv.Atoi(inner); err == nil && n >= 1 && n <= 3 {
			return &session.WorkflowStep{Type: fmt.Sprintf("PressPA%d", n)}
		}
	}
	return nil
}

func workflowKeyForStepType(stepType string) (string, bool) {
	trimmed := strings.TrimSpace(stepType)
	switch trimmed {
	case "PressEnter":
		return "Enter", true
	case "PressTab":
		return "Tab", true
	case "PressBackTab":
		return "BackTab", true
	case "PressClear":
		return "Clear", true
	case "PressReset":
		return "Reset", true
	case "PressEraseEOF":
		return "EraseEOF", true
	case "PressEraseInput":
		return "EraseInput", true
	case "PressDup":
		return "Dup", true
	case "PressFieldMark":
		return "FieldMark", true
	case "PressSysReq":
		return "SysReq", true
	case "PressAttn":
		return "Attn", true
	case "PressNewline":
		return "Newline", true
	case "PressBackspace":
		return "BackSpace", true
	case "PressDelete":
		return "Delete", true
	case "PressInsert":
		return "Insert", true
	case "PressHome":
		return "Home", true
	case "PressUp":
		return "Up", true
	case "PressDown":
		return "Down", true
	case "PressLeft":
		return "Left", true
	case "PressRight":
		return "Right", true
	}

	if strings.HasPrefix(trimmed, "PressPF") {
		inner := strings.TrimPrefix(trimmed, "PressPF")
		if n, err := strconv.Atoi(inner); err == nil && n >= 1 && n <= 24 {
			return fmt.Sprintf("PF(%d)", n), true
		}
	}
	if strings.HasPrefix(trimmed, "PressPA") {
		inner := strings.TrimPrefix(trimmed, "PressPA")
		if n, err := strconv.Atoi(inner); err == nil && n >= 1 && n <= 3 {
			return fmt.Sprintf("PA(%d)", n), true
		}
	}
	return "", false
}
