package main

import (
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// TestStopAllSessionsStopsEveryHostAndClearsManager guards graceful
// shutdown: previously, srv.Shutdown only stopped accepting new HTTP
// requests — every connected session's s3270 subprocess was left running,
// orphaned rather than killed, on a clean server exit (SIGINT, /app/restart).
func TestStopAllSessionsStopsEveryHostAndClearsManager(t *testing.T) {
	app := &App{
		SessionManager: session.NewManager(),
		chaosEngines:   newChaosEngineStore(),
	}

	hosts := make([]*host.MockHost, 0, 3)
	for i := 0; i < 3; i++ {
		mh, err := host.NewMockHost("")
		if err != nil {
			t.Fatalf("NewMockHost: %v", err)
		}
		mh.Connected = true
		app.SessionManager.CreateSession(mh)
		hosts = append(hosts, mh)
	}

	if got := len(app.SessionManager.ListSessions()); got != 3 {
		t.Fatalf("expected 3 sessions before shutdown, got %d", got)
	}

	app.stopAllSessions()

	if got := len(app.SessionManager.ListSessions()); got != 0 {
		t.Fatalf("expected 0 sessions after stopAllSessions, got %d", got)
	}
	for i, mh := range hosts {
		if mh.Connected {
			t.Errorf("host %d: expected Stop() to have been called (Connected=false), still connected", i)
		}
	}
}
