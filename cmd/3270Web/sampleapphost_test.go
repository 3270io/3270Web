// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseSampleAppSpecsDefaultsToOneApp(t *testing.T) {
	specs, err := parseSampleAppSpecs(nil)
	if err != nil {
		t.Fatalf("parseSampleAppSpecs(nil) returned %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1: %+v", len(specs), specs)
	}
	if specs[0].ID != defaultSampleAppID {
		t.Errorf("got app %q, want %q", specs[0].ID, defaultSampleAppID)
	}
	if specs[0].Port != defaultSampleAppPort {
		t.Errorf("got port %d, want %d", specs[0].Port, defaultSampleAppPort)
	}
}

func TestParseSampleAppSpecsAssignsPortsInOrder(t *testing.T) {
	specs, err := parseSampleAppSpecs([]string{"app1", "app2,app3"})
	if err != nil {
		t.Fatalf("parseSampleAppSpecs returned %v", err)
	}
	want := []sampleAppSpec{
		{ID: "app1", Port: defaultSampleAppPort},
		{ID: "app2", Port: defaultSampleAppPort + 1},
		{ID: "app3", Port: defaultSampleAppPort + 2},
	}
	if len(specs) != len(want) {
		t.Fatalf("got %+v, want %+v", specs, want)
	}
	for i := range want {
		if specs[i] != want[i] {
			t.Errorf("spec %d = %+v, want %+v", i, specs[i], want[i])
		}
	}
}

// An explicit port is reserved before any automatic one is handed out, so an
// application named later in the list cannot have its port taken by one named
// earlier without a port of its own.
func TestParseSampleAppSpecsSkipsPortsClaimedLater(t *testing.T) {
	specs, err := parseSampleAppSpecs([]string{"app1", "petstore:" + strconv.Itoa(defaultSampleAppPort)})
	if err != nil {
		t.Fatalf("parseSampleAppSpecs returned %v", err)
	}
	if got := specs[1]; got.ID != "petstore" || got.Port != defaultSampleAppPort {
		t.Errorf("petstore = %+v, want the port it asked for (%d)", got, defaultSampleAppPort)
	}
	if got := specs[0]; got.Port == defaultSampleAppPort {
		t.Errorf("app1 was given %d, the port petstore was named for", got.Port)
	}
}

func TestParseSampleAppSpecsRejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		wantSub string
	}{
		{name: "unknown app", values: []string{"nosuchapp"}, wantSub: "unknown sample app"},
		{name: "unknown app with port", values: []string{"nosuchapp:3271"}, wantSub: "unknown sample app"},
		{name: "port is not a number", values: []string{"app1:threetwoseventyone"}, wantSub: "not a port"},
		{name: "port out of range", values: []string{"app1:70000"}, wantSub: "not a port"},
		{name: "port zero", values: []string{"app1:0"}, wantSub: "not a port"},
		{name: "two apps on one port", values: []string{"app1:3271", "app2:3271"}, wantSub: "already serving"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSampleAppSpecs(tc.values)
			if err == nil {
				t.Fatalf("parseSampleAppSpecs(%q) returned no error", tc.values)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not mention %q", err, tc.wantSub)
			}
		})
	}
}

// Port 0 leaves the choice to the kernel, which is how a test avoids
// colliding with whatever else is on the machine.
func TestStartSampleAppHostsServesEveryApp(t *testing.T) {
	specs := []sampleAppSpec{{ID: "app1", Port: 0}, {ID: "petstore", Port: 0}}
	servers, err := startSampleAppHosts(specs, "127.0.0.1")
	if err != nil {
		t.Fatalf("startSampleAppHosts returned %v", err)
	}
	defer func() {
		for _, server := range servers {
			_ = server.Stop()
		}
	}()

	if len(servers) != len(specs) {
		t.Fatalf("got %d servers, want %d", len(servers), len(specs))
	}
	for i, server := range servers {
		conn, err := net.DialTimeout("tcp", server.Addr(), 3*time.Second)
		if err != nil {
			t.Errorf("sample app %q on %s did not accept a connection: %v", specs[i].ID, server.Addr(), err)
			continue
		}
		_ = conn.Close()
	}
}

// A partial start would report the process up with the one application
// somebody wanted missing, so a failure takes the rest down with it.
func TestStartSampleAppHostsIsAllOrNothing(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not hold a port: %v", err)
	}
	defer held.Close()
	busy := held.Addr().(*net.TCPAddr).Port

	// The first one starts, the second cannot: the port is already held.
	first := findFreePort(t)
	specs := []sampleAppSpec{{ID: "app1", Port: first}, {ID: "app2", Port: busy}}

	servers, err := startSampleAppHosts(specs, "127.0.0.1")
	if err == nil {
		for _, server := range servers {
			_ = server.Stop()
		}
		t.Fatal("startSampleAppHosts succeeded with a port that was already held")
	}
	if !strings.Contains(err.Error(), "app2") {
		t.Errorf("error %q does not name the app that could not be served", err)
	}

	// The one that did start must have been stopped, so its port is free.
	probe, listenErr := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(first)))
	if listenErr != nil {
		t.Fatalf("port %d is still held, so the started listener was left running: %v", first, listenErr)
	}
	_ = probe.Close()
}

func TestRunSampleAppHostListsApps(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runSampleAppHost([]string{"--list"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	for _, cfg := range sampleAppConfigs {
		if !strings.Contains(out, cfg.ID) {
			t.Errorf("--list output does not mention %q:\n%s", cfg.ID, out)
		}
	}
}

func TestRunSampleAppHostRejectsUnknownApp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSampleAppHost([]string{"--app", "nosuchapp"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown sample app") {
		t.Errorf("stderr does not explain the problem:\n%s", stderr.String())
	}
}

func findFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not find a free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("could not release the probe port: %v", err)
	}
	return port
}
