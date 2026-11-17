// SPDX-License-Identifier: AGPL-3.0-or-later
package host

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestAutomaticSamplePortsShareAndRelease(t *testing.T) {
	first, err := acquireSampleAppServer("petstore", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, text, err := net.SplitHostPort(first.Addr())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(text)
	if err != nil {
		t.Fatal(err)
	}
	second, err := acquireSampleAppServer("petstore", 0)
	if err != nil {
		releaseSampleAppServer(port, first)
		t.Fatal(err)
	}
	if first != second || sampleAppRefs(port) != 2 {
		t.Fatal("automatic samples did not share a bound listener")
	}
	releaseSampleAppServer(port, first)
	if sampleAppRefs(port) != 1 {
		t.Fatal("first release stopped the shared listener")
	}
	releaseSampleAppServer(port, second)
	if sampleAppRefs(port) != 0 {
		t.Fatal("sample lease leaked")
	}
}

// A port held by a different app is refused with an error a caller can act on:
// it names the holder and the app that asked, rather than being a sentence to
// pattern-match.
func TestSamplePortHeldByAnotherAppIsATypedError(t *testing.T) {
	first, err := acquireSampleAppServer("app1", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, text, _ := net.SplitHostPort(first.Addr())
	port, _ := strconv.Atoi(text)
	defer releaseSampleAppServer(port, first)

	_, err = acquireSampleAppServer("app2", port)
	var busy *SampleAppPortInUseError
	if !errors.As(err, &busy) {
		t.Fatalf("err = %v, want a *SampleAppPortInUseError", err)
	}
	if busy.Port != port || busy.HeldBy != "app1" || busy.Requested != "app2" {
		t.Errorf("busy = %+v, want port %d held by app1 for app2", busy, port)
	}
	if want := "already serving"; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %q, want it to keep saying %q", err, want)
	}
	if sampleAppRefs(port) != 1 {
		t.Errorf("a refused claim changed the holder's reference count to %d", sampleAppRefs(port))
	}
}

func TestSampleAppPortInUseReportsWhatThePoolHolds(t *testing.T) {
	first, err := acquireSampleAppServer("app1", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, text, _ := net.SplitHostPort(first.Addr())
	port, _ := strconv.Atoi(text)
	if !SampleAppPortInUse(port) {
		t.Errorf("SampleAppPortInUse(%d) = false while a lease holds it", port)
	}
	releaseSampleAppServer(port, first)
	if SampleAppPortInUse(port) {
		t.Errorf("SampleAppPortInUse(%d) = true after the last lease was released", port)
	}
}
