package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/jnnngs/3270Web/internal/session"
)

func normalizeKey(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "Enter"
	}

	// Sanitize to prevent command injection
	if strings.ContainsAny(trimmed, "\n\r\t;") {
		log.Printf("Security warning: detected potential command injection in key: %q", key)
		return "Enter"
	}

	upper := strings.ToUpper(trimmed)
	lower := strings.ToLower(trimmed)

	if strings.HasPrefix(upper, "PF(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PF("), ")")
		if n, err := strconv.Atoi(inner); err == nil {
			if n >= 1 && n <= 24 {
				return fmt.Sprintf("PF(%d)", n)
			}
		}
	}
	if strings.HasPrefix(upper, "PA(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PA("), ")")
		if n, err := strconv.Atoi(inner); err == nil {
			if n >= 1 && n <= 3 {
				return fmt.Sprintf("PA(%d)", n)
			}
		}
	}
	if strings.HasPrefix(upper, "PF") {
		if n, err := strconv.Atoi(strings.TrimPrefix(upper, "PF")); err == nil {
			if n >= 1 && n <= 24 {
				return fmt.Sprintf("PF(%d)", n)
			}
		}
	}
	if strings.HasPrefix(upper, "PA") {
		if n, err := strconv.Atoi(strings.TrimPrefix(upper, "PA")); err == nil {
			if n >= 1 && n <= 3 {
				return fmt.Sprintf("PA(%d)", n)
			}
		}
	}
	if strings.HasPrefix(upper, "F") {
		if n, err := strconv.Atoi(strings.TrimPrefix(upper, "F")); err == nil {
			if n >= 1 && n <= 24 {
				return fmt.Sprintf("PF(%d)", n)
			}
		}
	}

	switch lower {
	case "enter":
		return "Enter"
	case "tab":
		return "Tab"
	case "backtab":
		return "BackTab"
	case "clear":
		return "Clear"
	case "reset":
		return "Reset"
	case "eraseeof", "erase_eof":
		return "EraseEOF"
	case "eraseinput", "erase_input":
		return "EraseInput"
	case "dup":
		return "Dup"
	case "fieldmark", "field_mark":
		return "FieldMark"
	case "sysreq", "sys_req":
		return "SysReq"
	case "attn":
		return "Attn"
	case "newline", "new_line":
		return "Newline"
	case "backspace":
		return "BackSpace"
	case "delete":
		return "Delete"
	case "insert":
		return "Insert"
	case "home":
		return "Home"
	case "up":
		return "Up"
	case "down":
		return "Down"
	case "left":
		return "Left"
	case "right":
		return "Right"
	}

	// Default to Enter for any unrecognized key to prevent command injection
	return "Enter"
}

func workflowStepForKey(key string) *session.WorkflowStep {
	upper := strings.ToUpper(strings.TrimSpace(key))
	if upper == "" {
		return nil
	}
	if upper == "ENTER" {
		return &session.WorkflowStep{Type: "PressEnter"}
	}
	if upper == "TAB" {
		return &session.WorkflowStep{Type: "PressTab"}
	}
	if strings.HasPrefix(upper, "PF(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PF("), ")")
		if n, err := strconv.Atoi(inner); err == nil && n >= 1 && n <= 24 {
			return &session.WorkflowStep{Type: fmt.Sprintf("PressPF%d", n)}
		}
	}
	if strings.HasPrefix(upper, "PF") {
		inner := strings.TrimPrefix(upper, "PF")
		if n, err := strconv.Atoi(inner); err == nil && n >= 1 && n <= 24 {
			return &session.WorkflowStep{Type: fmt.Sprintf("PressPF%d", n)}
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
