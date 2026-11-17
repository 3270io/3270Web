// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func loadWith(t *testing.T, r *gin.Engine, path, sessionID, conversation string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if sessionID != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
	}
	if conversation != "" {
		req.Header.Set(ConversationHeader, conversation)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return out
}

// Clearing the chat starts a new conversation against the same terminal
// session. The dedup was keyed on the session, so the new conversation — which
// had been sent nothing — was told it already had every skill the old one had
// loaded, and there was no request that would get it the text.
func TestANewConversationOnTheSameSessionGetsTheSkill(t *testing.T) {
	app, r := skillsTestApp(t)
	sess := app.SessionManager.CreateSession(nil)
	const path = "/skills/load?name=chaos-monkey"

	first := loadWith(t, r, path, sess.ID, "conv-1")
	if already, _ := first["already_load"].(bool); already {
		t.Fatal("the first load should return the body")
	}
	if again := loadWith(t, r, path, sess.ID, "conv-1"); again["already_load"] != true {
		t.Error("a repeat in the same conversation should be a reminder")
	}

	// Clear: same session cookie, new conversation id.
	cleared := loadWith(t, r, path, sess.ID, "conv-2")
	if already, _ := cleared["already_load"].(bool); already {
		t.Error("a new conversation on the same session was told it already had the skill")
	}
	if body, _ := cleared["body"].(string); len(body) < 200 {
		t.Errorf("a new conversation should get the full body, got %d chars", len(body))
	}

	// And the same for instructions, which share the mechanism.
	const inst = "/instructions/load?name=aid-key-safety"
	if got := loadWith(t, r, inst, sess.ID, "conv-1"); got["already_load"] == true {
		t.Error("the first instruction load should return the body")
	}
	if got := loadWith(t, r, inst, sess.ID, "conv-2"); got["already_load"] == true {
		t.Error("a new conversation was told it already had the instruction")
	}
}

// A model whose earlier turns were trimmed no longer has the skill, whatever
// the server remembers sending. One reminder is cheap and usually right; a
// request after that is taken at its word.
func TestAskingAgainAfterTheReminderGetsTheBody(t *testing.T) {
	for _, tc := range []struct{ name, session, conversation string }{
		{"by conversation header", "", "conv-trim"},
		{"by session cookie alone", "use-session", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, r := skillsTestApp(t)
			sessionID := ""
			if tc.session != "" {
				sessionID = app.SessionManager.CreateSession(nil).ID
			}
			const path = "/skills/load?name=chaos-monkey"

			first := loadWith(t, r, path, sessionID, tc.conversation)
			full, _ := first["body"].(string)

			reminder := loadWith(t, r, path, sessionID, tc.conversation)
			text, _ := reminder["body"].(string)
			if reminder["already_load"] != true || len(text) >= len(full) {
				t.Fatalf("the second request should be a short reminder, got %d chars", len(text))
			}
			if !strings.Contains(text, "request it once more") {
				t.Errorf("the reminder should say how to get the text if it has gone, got %q", text)
			}

			third := loadWith(t, r, path, sessionID, tc.conversation)
			if body, _ := third["body"].(string); body != full {
				t.Errorf("asking again after the reminder should return the body, got %d chars", len(body))
			}
			if third["reloaded"] != true {
				t.Errorf("a resent body should be marked: %v", third["reloaded"])
			}
			if note, _ := third["note"].(string); note == "" {
				t.Error("a resent body should carry a note saying it was already loaded")
			}

			// It is not a way to switch the reminder off: the next repeat is
			// a reminder again.
			if fourth := loadWith(t, r, path, sessionID, tc.conversation); fourth["already_load"] != true {
				t.Error("the request after a resend should be a reminder again")
			}
		})
	}
}

// Conversation ids are whatever a client sends, and nothing says one is over.
func TestConversationTrackersAreBounded(t *testing.T) {
	app, _ := skillsTestApp(t)
	for i := 0; i < maxTrackedConversations*2+10; i++ {
		app.loadSessionFor(conversationKeyPrefix + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + itoa(i))
	}
	if n := len(app.loadSessions); n > maxTrackedConversations {
		t.Errorf("%d conversation trackers held, want at most %d", n, maxTrackedConversations)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
