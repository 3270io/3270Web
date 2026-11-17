// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

// discoveryTerminalBinary finds an s3270 to drive the bundled sample
// application with, or "" when there is none. Same search as the terminal
// conformance suite in internal/host: S3270_TEST_BINARY, PATH, then the copy
// bundled with the repository.
func discoveryTerminalBinary() string {
	if p := strings.TrimSpace(os.Getenv("S3270_TEST_BINARY")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	}
	name := "s3270"
	if runtime.GOOS == "windows" {
		name = "s3270.exe"
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	bundled := map[string]string{
		"linux/amd64":   "s3270-linux-amd64",
		"windows/amd64": "s3270.exe",
	}[runtime.GOOS+"/"+runtime.GOARCH]
	if bundled == "" {
		return ""
	}
	p, err := filepath.Abs(filepath.Join("..", "..", "s3270-bin", bundled))
	if err != nil {
		return ""
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// startPetStoreHost starts the bundled Pet Store sample application on a free
// port with a real s3270 connected to it. The test is skipped, not failed,
// where there is no s3270 or the application cannot be started.
func startPetStoreHost(t *testing.T) host.Host {
	t.Helper()
	bin := discoveryTerminalBinary()
	if bin == "" {
		t.Skip("no s3270 available to run the Pet Store discovery test against; set S3270_TEST_BINARY to require one")
	}
	h, err := host.NewGoSampleAppHost("petstore", 0, bin, []string{"-model", "3279-4-E"}, "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot create sample app host: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Skipf("cannot start the Pet Store sample app: %v", err)
	}
	t.Cleanup(func() { _ = h.Stop() })
	return h
}

func waitForEngineStop(t *testing.T, e *Engine, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for e.Active() {
		if time.Now().After(deadline) {
			e.Stop()
			t.Fatalf("chaos run did not finish within %s", limit)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestPetStoreDiscoveryReachesBeyondTheMainMenu is the end-to-end measure of
// discovery quality: the default configuration, pointed at the bundled Pet
// Store, has to sign on, find its way off the main menu into several of the
// functions that menu lists, and not keep signing itself off.
//
// Only the step count, the pacing and the seed differ from DefaultConfig: no
// hints, no key changes. Two seeds are run so the result is a property of the
// engine rather than of one lucky sequence.
func TestPetStoreDiscoveryReachesBeyondTheMainMenu(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real s3270 for a few seconds")
	}
	for _, seed := range []int64{1, 20261116} {
		seed := seed
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			h := startPetStoreHost(t)

			cfg := DefaultConfig()
			cfg.MaxSteps = 150
			cfg.StepDelay = 0
			cfg.TimeBudget = 2 * time.Minute
			cfg.Seed = seed
			cfg.ExcludeNoProgressEvents = false

			e := New(h, cfg)
			var mu sync.Mutex
			var all []Attempt
			e.attemptHook = func(a Attempt) {
				mu.Lock()
				all = append(all, a)
				mu.Unlock()
			}
			if err := e.Start(); err != nil {
				t.Fatalf("Start: %v", err)
			}
			waitForEngineStop(t, e, 3*time.Minute)

			st := e.Status()
			if st.Error != "" {
				t.Fatalf("run ended with error %q (reason %s)", st.Error, st.TerminationReason)
			}
			if st.MindMap == nil {
				t.Fatalf("no mind map after %d steps", st.StepsRun)
			}
			first := st.FirstScreenHash
			if first == "" {
				t.Fatalf("first screen hash not recorded")
			}

			// The main menu is wherever a successful sign-on lands.
			labels := make([]string, 0, len(st.MindMap.Areas))
			screenIDs := map[string]bool{}
			for hash, area := range st.MindMap.Areas {
				labels = append(labels, fmt.Sprintf("%s visits=%d %s", hash, area.Visits, area.Label))
				if id := strings.Fields(area.Label); len(id) > 0 && strings.HasPrefix(id[0], "PET") {
					screenIDs[id[0]] = true
				}
				for _, tok := range strings.Fields(area.Label) {
					if len(tok) == 10 && tok[4] == '-' && tok[7] == '-' {
						t.Errorf("area label %q carries a date", area.Label)
					}
				}
			}
			sort.Strings(labels)
			t.Logf("steps=%d transitions=%d screens=%d reason=%s\n%s",
				st.StepsRun, st.Transitions, st.UniqueScreens, st.TerminationReason, strings.Join(labels, "\n"))

			beyond := 0
			for id := range screenIDs {
				if id != "PET010" && id != "PET100" {
					beyond++
				}
			}
			if beyond < 5 {
				t.Errorf("reached %d screens beyond sign-on and the main menu, want at least 5 (screen ids: %v)", beyond, screenIDs)
			}

			// Signing off throws away the session. Count every time the run
			// fell back onto the first screen from somewhere deeper.
			fellBack := 0
			onFirst := 0
			mu.Lock()
			attempts := append([]Attempt(nil), all...)
			mu.Unlock()
			for _, a := range attempts {
				if a.FromHash == first {
					onFirst++
				}
				if a.FromHash != first && a.ToHash == first {
					fellBack++
				}
			}
			if fellBack > 2 {
				t.Errorf("run fell back to the sign-on screen %d times, want at most 2", fellBack)
			}
			if onFirst > st.StepsRun/4 {
				t.Errorf("%d of %d attempts were spent on the sign-on screen", onFirst, st.StepsRun)
			}
			if n := st.AIDKeyCounts["PF(3)"]; n > 0 {
				t.Errorf("PF3 pressed %d times; the default key set does not include it", n)
			}
		})
	}
}

// TestPetStoreMenuIsOneAreaAtBothScreenSizes drives the real application:
// sign on, note the main menu, press PF9 (which switches between the 24x80
// and the alternate screen size) and check the menu is still the same area.
func TestPetStoreMenuIsOneAreaAtBothScreenSizes(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real s3270")
	}
	h := startPetStoreHost(t)
	read := func() *host.Screen {
		t.Helper()
		if err := h.UpdateScreen(); err != nil {
			t.Fatalf("UpdateScreen: %v", err)
		}
		s := h.GetScreenSnapshot()
		if s == nil {
			t.Fatal("no screen")
		}
		return s
	}
	signon := read()
	for _, f := range unprotectedFields(signon) {
		if err := h.WriteStringAt(f.StartY, f.StartX, "TESTER"); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := h.SendKey("Enter"); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	e := New(h, DefaultConfig())
	e.mindMap = newMindMap()
	menu := read()
	if !strings.Contains(menu.Text(), "MAIN MENU") {
		t.Fatalf("not on the main menu:\n%s", menu.Text())
	}
	first := e.canonicalizeScreenHash(hashScreen(menu), menu)
	e.mu.Lock()
	e.observeMindMapAreaLocked(first, menu, time.Now())
	label := e.mindMap.Areas[first].Label
	e.mu.Unlock()
	if label != "PET100 MAIN MENU" {
		t.Errorf("menu label = %q, want %q", label, "PET100 MAIN MENU")
	}

	if err := h.SendKey("PF(9)"); err != nil {
		t.Fatalf("PF9: %v", err)
	}
	resized := read()
	if resized.Height == menu.Height {
		t.Skipf("PF9 did not change the screen size (still %dx%d)", resized.Height, resized.Width)
	}
	if got := e.canonicalizeScreenHash(hashScreen(resized), resized); got != first {
		t.Errorf("the main menu at %dx%d and %dx%d was counted as two areas", menu.Height, menu.Width, resized.Height, resized.Width)
	}
}

// The help screen lays itself out again when the terminal changes size: two
// columns at the larger size, one at the smaller. Its rows do not line up
// between the two, which is the case the row comparison cannot settle and the
// word comparison is there for.
func TestPetStoreHelpIsOneAreaAtBothScreenSizes(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real s3270")
	}
	h := startPetStoreHost(t)
	read := func() *host.Screen {
		t.Helper()
		if err := h.UpdateScreen(); err != nil {
			t.Fatalf("UpdateScreen: %v", err)
		}
		s := h.GetScreenSnapshot()
		if s == nil {
			t.Fatal("no screen")
		}
		return s
	}
	for _, f := range unprotectedFields(read()) {
		if err := h.WriteStringAt(f.StartY, f.StartX, "TESTER"); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	for _, key := range []string{"Enter", "PF(1)"} {
		if err := h.SendKey(key); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}

	e := New(h, DefaultConfig())
	e.mindMap = newMindMap()
	help := read()
	if !strings.Contains(help.Text(), "HELP AND COMMAND REFERENCE") {
		t.Fatalf("not on the help screen:\n%s", help.Text())
	}
	first := e.canonicalizeScreenHash(hashScreen(help), help)
	e.mu.Lock()
	e.observeMindMapAreaLocked(first, help, time.Now())
	e.mu.Unlock()

	if err := h.SendKey("PF(9)"); err != nil {
		t.Fatalf("PF9: %v", err)
	}
	resized := read()
	if resized.Height == help.Height {
		t.Skipf("PF9 did not change the screen size (still %dx%d)", resized.Height, resized.Width)
	}
	if !strings.Contains(resized.Text(), "HELP AND COMMAND REFERENCE") {
		t.Skipf("PF9 left the help screen:\n%s", resized.Text())
	}
	if got := e.canonicalizeScreenHash(hashScreen(resized), resized); got != first {
		t.Errorf("the help screen at %dx%d and %dx%d was counted as two areas", help.Height, help.Width, resized.Height, resized.Width)
	}
}
