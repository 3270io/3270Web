package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveListenAddr(t *testing.T) {
	tests := []struct {
		name     string
		bind     string
		port     string
		expected string
	}{
		{name: "both unset defaults to loopback", bind: "", port: "", expected: "127.0.0.1:8080"},
		{name: "port only keeps loopback default", bind: "", port: "9090", expected: "127.0.0.1:9090"},
		{name: "bind all interfaces", bind: "0.0.0.0", port: "", expected: "0.0.0.0:8080"},
		{name: "bind and port", bind: "0.0.0.0", port: "3270", expected: "0.0.0.0:3270"},
		{name: "specific interface", bind: "192.168.1.10", port: "8080", expected: "192.168.1.10:8080"},
		{name: "ipv6 is bracketed", bind: "::", port: "8080", expected: "[::]:8080"},
		{name: "ipv6 loopback is bracketed", bind: "::1", port: "", expected: "[::1]:8080"},
		{name: "surrounding whitespace is trimmed", bind: "  0.0.0.0 ", port: " 8081 ", expected: "0.0.0.0:8081"},
		{name: "whitespace only falls back to defaults", bind: "   ", port: "  ", expected: "127.0.0.1:8080"},
		{name: "ephemeral port for the bind-failure fallback", bind: "0.0.0.0", port: "0", expected: "0.0.0.0:0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveListenAddr(tt.bind, tt.port); got != tt.expected {
				t.Fatalf("resolveListenAddr(%q, %q) = %q, want %q", tt.bind, tt.port, got, tt.expected)
			}
		})
	}
}

// The default must stay on loopback so a desktop or `go run` install does not
// silently publish the (password-less) UI to the local network.
func TestResolveListenAddr_DefaultsToLoopback(t *testing.T) {
	addr := resolveListenAddr("", "")
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		t.Fatalf("default bind host %q is not an IP", host)
	}
	if !ip.IsLoopback() {
		t.Fatalf("default bind host %q must be loopback", host)
	}
}

// Regression test for the container being unreachable through a published port.
// The server defaults to a loopback-only listener, which a published port can
// never reach — Docker forwards to the container's external interface, so every
// connection from the host is refused while the container still reports healthy
// (its HEALTHCHECK curls 127.0.0.1 from inside). The image must override the
// default; exposure is controlled by the `ports:` mapping instead.
func TestDockerfileBindsAllInterfaces(t *testing.T) {
	path := filepath.Join("..", "..", "Dockerfile")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}

	var bind string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "ENV WEBUI_BIND="); ok {
			bind = strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	if bind == "" {
		t.Fatal("Dockerfile does not set ENV WEBUI_BIND; the published image would bind 127.0.0.1 and be unreachable through a published port")
	}

	ip := net.ParseIP(bind)
	if ip == nil {
		t.Fatalf("Dockerfile sets WEBUI_BIND=%q, which is not a valid IP", bind)
	}
	if ip.IsLoopback() {
		t.Fatalf("Dockerfile sets WEBUI_BIND=%q; a loopback bind is unreachable through a published port", bind)
	}

	// The value has to survive resolveListenAddr as the host half of the address.
	addr := resolveListenAddr(bind, "")
	if want := net.JoinHostPort(bind, defaultWebUIPort); addr != want {
		t.Fatalf("resolveListenAddr(%q, \"\") = %q, want %q", bind, addr, want)
	}
}

// The installer's container methods must set WEBUI_BIND too. They are how most
// people get 3270Web running, and the compose file they generate is an artifact
// users keep and edit, so the listen address has to be explicit and correct
// there rather than relying on the reader to know the image default.
func TestInstallScriptSetsBindForContainers(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "install.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	script := string(data)

	// `docker run` (--method docker) and the generated compose file
	// (--method compose) spell the same setting differently.
	for _, want := range []string{"-e WEBUI_BIND=0.0.0.0", "- WEBUI_BIND=0.0.0.0"} {
		if !strings.Contains(script, want) {
			t.Errorf("install.sh does not contain %q; containers it creates would bind loopback and be unreachable through the published port", want)
		}
	}

	// A loopback value anywhere would reintroduce the bug. The port mapping is
	// the only place the installer should restrict exposure.
	if strings.Contains(script, "WEBUI_BIND=127.0.0.1") || strings.Contains(script, "WEBUI_BIND=localhost") {
		t.Error("install.sh binds WEBUI_BIND to loopback for a container; restrict the host side of the port mapping instead")
	}
}

// A non-loopback listener must actually accept connections addressed to a
// non-loopback interface — that is the behaviour the published port depends on.
func TestListenerOnAllInterfacesAcceptsNonLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", resolveListenAddr("0.0.0.0", "0"))
	if err != nil {
		t.Skipf("cannot bind 0.0.0.0 in this environment: %v", err)
	}
	defer ln.Close()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", ln.Addr().String(), err)
	}

	target := nonLoopbackIPv4(t)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	conn, err := net.Dial("tcp", net.JoinHostPort(target, port))
	if err != nil {
		t.Fatalf("dial %s:%s on an all-interfaces listener: %v", target, port, err)
	}
	conn.Close()
}

func nonLoopbackIPv4(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("cannot enumerate interfaces: %v", err)
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if ip4 := ipNet.IP.To4(); ip4 != nil {
			return ip4.String()
		}
	}
	t.Skip("no non-loopback IPv4 interface available")
	return ""
}
