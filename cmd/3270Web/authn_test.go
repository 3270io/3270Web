package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

func TestAuthenticate_ResolvesLocalPrincipalInModeNone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager(), authMode: authz.ModeNone}

	var got authz.Principal
	r := gin.New()
	r.Use(app.Authenticate())
	r.GET("/probe", func(c *gin.Context) {
		got = principalFrom(c)
		c.Status(http.StatusOK)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/probe", nil))

	if got.UserID != authz.LocalUserID {
		t.Errorf("UserID = %q, want %q", got.UserID, authz.LocalUserID)
	}
	if !got.IsAdmin() {
		t.Error("the single local operator should be an admin")
	}
}

// A route that somehow bypasses the middleware must yield a principal that
// can do nothing, so a wiring mistake denies instead of escalating.
func TestPrincipalFrom_FailsClosedWithoutMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var got authz.Principal
	r := gin.New()
	r.GET("/probe", func(c *gin.Context) {
		got = principalFrom(c)
		c.Status(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/probe", nil))

	if !got.IsAnonymous() {
		t.Errorf("principal = %v, want anonymous", got)
	}
	if got.IsAdmin() {
		t.Error("anonymous principal reported as admin")
	}
}

func TestPrincipalFrom_HandlesNilAndWrongType(t *testing.T) {
	if !principalFrom(nil).IsAnonymous() {
		t.Error("nil context did not yield anonymous")
	}

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(principalContextKey, "not a principal")
	if !principalFrom(c).IsAnonymous() {
		t.Error("a wrongly-typed context value did not yield anonymous")
	}
}

// An unsupported AUTH_MODE must stop startup. Falling back to no
// authentication would leave the operator believing the instance is guarded.
func TestBuildRouter_RejectsUnsupportedAuthMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(authz.ModeEnv, "local")

	app := newApp(t.TempDir())
	if _, err := buildRouter(app); err == nil {
		t.Fatal("buildRouter accepted an unsupported AUTH_MODE")
	} else if !strings.Contains(err.Error(), authz.ModeEnv) {
		t.Errorf("error %q does not name %s", err, authz.ModeEnv)
	}
}

func TestBuildRouter_AcceptsDefaultAndExplicitNone(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, value := range []string{"", "none"} {
		t.Setenv(authz.ModeEnv, value)
		app := newApp(t.TempDir())
		if _, err := buildRouter(app); err != nil {
			t.Fatalf("AUTH_MODE=%q: buildRouter returned %v", value, err)
		}
		if app.authMode != authz.ModeNone {
			t.Errorf("AUTH_MODE=%q: mode = %q, want %q", value, app.authMode, authz.ModeNone)
		}
	}
}

// Sessions opened through the app must carry an owner, so that enforcing
// ownership later is a matter of checking a label that is already there.
func TestStartHostSession_LabelsSessionWithOwner(t *testing.T) {
	app := &App{SessionManager: session.NewManager(), Config: nil}

	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	sess := app.SessionManager.CreateSessionFor(authz.LocalUserID, mh)

	if sess.OwnerID != authz.LocalUserID {
		t.Errorf("OwnerID = %q, want %q", sess.OwnerID, authz.LocalUserID)
	}
	if !authz.Local().Owns(sess.OwnerID) {
		t.Error("the local principal does not own the session it created")
	}
}
