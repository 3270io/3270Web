// SPDX-License-Identifier: AGPL-3.0-or-later
package host

import (
	"net"
	"strconv"
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
