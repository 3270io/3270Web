package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/session"
)

func TestDisconnectMethod(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Setup minimal app
	app := &App{
		SessionManager: session.NewManager(),
	}

	r := gin.New()
	// Register the handler as per the changes
	r.POST("/disconnect", app.DisconnectHandler)

	// Enable HandleMethodNotAllowed to return 405 instead of 404
	r.HandleMethodNotAllowed = true

	// Test GET request (should fail with 405 Method Not Allowed)
	t.Run("GET request rejected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/disconnect", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected 405 Method Not Allowed for GET /disconnect, got %d", w.Code)
		}
	})

	// Test POST request (should succeed)
	t.Run("POST request allowed", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/disconnect", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// DisconnectHandler redirects to "/"
		if w.Code != http.StatusFound {
			t.Errorf("Expected 302 for POST /disconnect, got %d", w.Code)
		}
		if loc := w.Header().Get("Location"); loc != "/" {
			t.Errorf("Expected redirect to /, got %s", loc)
		}
	})
}
