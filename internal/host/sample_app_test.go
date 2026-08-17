// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"net"
	"strconv"
	"testing"
	"time"
)

// Two people opening the same bundled sample app.
//
// The reported failure is ordinary use: a sample-app preset assigned to a
// group is opened by one person, then by another, and the second session dies
// with an address-in-use error because it tried to bind a port the first was
// already holding. The application itself was always able to serve both.

// freeSampleAppPort finds a loopback port nothing is listening on.
//
// Asked for and given back immediately, which is the usual way to pick one
// that is not in use — the alternative is hard-coding a number and hoping.
func freeSampleAppPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not find a free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("could not release the probe listener: %v", err)
	}
	return port
}

// canReach reports whether something is accepting connections on the port.
func canReach(t *testing.T, port int) bool {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func TestSampleAppServerIsSharedByConcurrentSessions(t *testing.T) {
	port := freeSampleAppPort(t)

	first, err := acquireSampleAppServer("app1", port)
	if err != nil {
		t.Fatalf("the first session could not start the sample app: %v", err)
	}
	// Released here as well as at the end, so a failed assertion below cannot
	// leave a listener behind for the rest of the package's tests.
	defer releaseSampleAppServer(port, first)

	second, err := acquireSampleAppServer("app1", port)
	if err != nil {
		t.Fatalf("a second session for the same sample app was refused: %v", err)
	}
	defer releaseSampleAppServer(port, second)

	if first != second {
		t.Error("the second session started its own listener rather than joining the first")
	}
	if got := sampleAppRefs(port); got != 2 {
		t.Errorf("references = %d, want both sessions counted", got)
	}

	// The first session closing must not take the application away from the
	// second, which is the whole point of counting rather than owning.
	releaseSampleAppServer(port, first)
	if got := sampleAppRefs(port); got != 1 {
		t.Errorf("references = %d after one session left, want the other still counted", got)
	}
	if !canReach(t, port) {
		t.Error("the listener stopped while a session was still using it")
	}

	// And the last one out closes it, or the port stays held until the process
	// ends and the next preset pointed at it cannot start.
	releaseSampleAppServer(port, second)
	if got := sampleAppRefs(port); got != 0 {
		t.Errorf("references = %d after the last session left, want none", got)
	}
	if canReach(t, port) {
		t.Error("the listener is still accepting connections after the last session closed")
	}

	// Releasing again — a second Stop, or a session whose start failed — must
	// be harmless rather than closing whatever is on that port next.
	releaseSampleAppServer(port, second)
	releaseSampleAppServer(port, nil)
	if got := sampleAppRefs(port); got != 0 {
		t.Errorf("references = %d after releasing a claim twice", got)
	}
}

func TestSampleAppServerRefusesADifferentAppOnTheSamePort(t *testing.T) {
	port := freeSampleAppPort(t)

	server, err := acquireSampleAppServer("app1", port)
	if err != nil {
		t.Fatalf("could not start the first sample app: %v", err)
	}
	defer releaseSampleAppServer(port, server)

	// Handing back the running server would connect somebody to an application
	// their preset does not name, which is a worse answer than the error.
	other, err := acquireSampleAppServer("app2", port)
	if err == nil {
		releaseSampleAppServer(port, other)
		t.Fatal("a different sample app was allowed onto an occupied port")
	}
	if other != nil {
		t.Error("the refusal still handed back a server")
	}
	if got := sampleAppRefs(port); got != 1 {
		t.Errorf("references = %d, want the refusal to have counted for nothing", got)
	}
}

// A host that fails to start must not keep its claim on the shared listener,
// or a port stays held by a session that never opened.
func TestAFailedSampleAppStartGivesTheListenerBack(t *testing.T) {
	port := freeSampleAppPort(t)

	h, err := NewGoSampleAppHost("app1", port, "s3270-that-does-not-exist", nil,
		net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("NewGoSampleAppHost: %v", err)
	}
	if err := h.Start(); err == nil {
		_ = h.Stop()
		t.Skip("s3270 started from a path that should not exist; nothing to assert")
	}
	if got := sampleAppRefs(port); got != 0 {
		t.Errorf("references = %d after a failed start, want the claim given back", got)
	}
	if canReach(t, port) {
		t.Error("the listener is still open after the session failed to start")
	}

	// And stopping a host that never started is harmless.
	if err := h.Stop(); err != nil {
		t.Errorf("Stop after a failed start: %v", err)
	}
	if got := sampleAppRefs(port); got != 0 {
		t.Errorf("references = %d after stopping a host that never started", got)
	}
}
