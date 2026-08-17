// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/profiler"
	"github.com/jnnngs/3270Web/internal/session"
)

// TestReapIdleSessions_RemovesIdleSession guards against the resource leak
// where a session whose browser tab closed without hitting /disconnect kept
// its s3270 subprocess, chaos engine, and cached profile alive forever
// (nothing previously scanned for or removed idle sessions).
func TestReapIdleSessions_RemovesIdleSession(t *testing.T) {
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	app, sess := newTestAppWithSession(t, mh)
	sess.LastAccess = time.Now().Add(-3 * time.Hour)

	app.profiles.set(sess.ID, &profiler.CompatibilityProfile{RunID: "test-run"})

	app.reapIdleSessions(2 * time.Hour)

	if _, ok := app.SessionManager.GetSession(sess.ID); ok {
		t.Fatal("idle session was not removed")
	}
	if got := app.profiles.get(sess.ID); got != nil {
		t.Fatal("profile cache entry was not cleared for reaped session")
	}
}

// TestReapIdleSessions_KeepsFreshSession ensures the reaper only acts on
// sessions past the idle cutoff.
func TestReapIdleSessions_KeepsFreshSession(t *testing.T) {
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	app, sess := newTestAppWithSession(t, mh)
	sess.LastAccess = time.Now()

	app.reapIdleSessions(2 * time.Hour)

	if _, ok := app.SessionManager.GetSession(sess.ID); !ok {
		t.Fatal("fresh session was incorrectly removed")
	}
}

// TestReapIdleSessions_SkipsActiveChaosRun ensures a long, unattended chaos
// exploration is not killed out from under itself just because the
// session's LastAccess (an HTTP-activity signal) has gone stale.
func TestReapIdleSessions_SkipsActiveChaosRun(t *testing.T) {
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	app, sess := newTestAppWithSession(t, mh)
	sess.LastAccess = time.Now().Add(-3 * time.Hour)

	cfg := chaos.DefaultConfig()
	cfg.MaxSteps = 0 // run until stopped
	cfg.StepDelay = time.Hour
	eng := chaos.New(mh, cfg)
	if err := eng.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer eng.Stop()
	app.chaosEngines.set(sess.ID, eng)

	app.reapIdleSessions(2 * time.Hour)

	if _, ok := app.SessionManager.GetSession(sess.ID); !ok {
		t.Fatal("session with an active chaos run was reaped")
	}
}

// TestReapIdleSessions_SkipsActivePlayback ensures an in-progress workflow
// playback is not interrupted by the idle reaper.
func TestReapIdleSessions_SkipsActivePlayback(t *testing.T) {
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	app, sess := newTestAppWithSession(t, mh)
	sess.LastAccess = time.Now().Add(-3 * time.Hour)
	withSessionLock(sess, func() {
		sess.Playback = &session.WorkflowPlayback{Active: true}
	})

	app.reapIdleSessions(2 * time.Hour)

	if _, ok := app.SessionManager.GetSession(sess.ID); !ok {
		t.Fatal("session with active playback was reaped")
	}
}
