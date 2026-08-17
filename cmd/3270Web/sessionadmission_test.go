// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"sync"
	"testing"

	"github.com/jnnngs/3270Web/internal/authz"
)

// The race the reservation exists to close: many connect requests arrive at
// once, every one of them reads the same session count, and every one of them
// passes a cap that none of them has yet consumed. Starting an s3270 and
// waiting for its first formatted screen takes long enough for that window to
// be wide open in practice.
func TestReserveSessionAdmitsOnlyUpToThePerUserCap(t *testing.T) {
	t.Setenv("MAX_SESSIONS_PER_USER", "3")
	t.Setenv("MAX_TOTAL_SESSIONS", "0")
	app := newSessionsApp(t)

	const attempts = 40
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		admitted int
	)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := app.reserveSession(authz.LocalUserID); err == nil {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if admitted != 3 {
		t.Errorf("%d of %d concurrent requests were admitted against a cap of 3", admitted, attempts)
	}
}

func TestReserveSessionAdmitsOnlyUpToTheTotalCap(t *testing.T) {
	t.Setenv("MAX_TOTAL_SESSIONS", "2")
	app := newSessionsApp(t)

	const attempts = 20
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		admitted int
	)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Different owners, so only the instance-wide cap is in play.
			if _, err := app.reserveSession(string(rune('a' + n))); err == nil {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if admitted != 2 {
		t.Errorf("%d of %d concurrent requests were admitted against a total cap of 2", admitted, attempts)
	}
}

// A refused or abandoned connection must give its place back, or a run of
// failures walls the instance off permanently.
func TestReleasingAReservationFreesTheSlot(t *testing.T) {
	t.Setenv("MAX_SESSIONS_PER_USER", "1")
	app := newSessionsApp(t)

	release, err := app.reserveSession("someone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.reserveSession("someone"); err == nil {
		t.Fatal("a second reservation was admitted against a cap of 1")
	}

	// Idempotent: startHostSession releases once on success and again on the
	// deferred path, and the second call must not hand out a free slot.
	release()
	release()

	again, err := app.reserveSession("someone")
	if err != nil {
		t.Fatalf("the slot was not freed: %v", err)
	}
	again()

	if app.admissionsTotal != 0 || len(app.admissions) != 0 {
		t.Errorf("the ledger did not return to empty: total=%d per-user=%v", app.admissionsTotal, app.admissions)
	}
}

// The reservation counts alongside the sessions that already exist, not
// instead of them.
func TestReservationsCountAlongsideOpenSessions(t *testing.T) {
	t.Setenv("MAX_SESSIONS_PER_USER", "2")
	app := newSessionsApp(t)
	newOwnedMockSession(t, app, authz.LocalUserID, "host:3270")

	release, err := app.reserveSession(authz.LocalUserID)
	if err != nil {
		t.Fatalf("the second of two allowed sessions was refused: %v", err)
	}
	defer release()

	if _, err := app.reserveSession(authz.LocalUserID); err == nil {
		t.Error("one open session plus one in flight was not counted as two")
	}
}
