package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
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
		{name: "both unset defaults to loopback", bind: "", port: "", expected: "127.0.0.1:3270"},
		{name: "port only keeps loopback default", bind: "", port: "9090", expected: "127.0.0.1:9090"},
		{name: "bind all interfaces", bind: "0.0.0.0", port: "", expected: "0.0.0.0:3270"},
		{name: "bind and port", bind: "0.0.0.0", port: "3270", expected: "0.0.0.0:3270"},
		{name: "specific interface", bind: "192.168.1.10", port: "8080", expected: "192.168.1.10:8080"},
		{name: "ipv6 is bracketed", bind: "::", port: "8080", expected: "[::]:8080"},
		{name: "ipv6 loopback is bracketed", bind: "::1", port: "", expected: "[::1]:3270"},
		{name: "surrounding whitespace is trimmed", bind: "  0.0.0.0 ", port: " 8081 ", expected: "0.0.0.0:8081"},
		{name: "whitespace only falls back to defaults", bind: "   ", port: "  ", expected: "127.0.0.1:3270"},
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

// Every page must declare a viewport, or a mobile browser lays it out at a
// ~980px virtual viewport and scales the result down: the desktop layout
// rendered in miniature, and — worse — every max-width media query in the
// stylesheets silently dead, because the viewport reports 980px on a 412px
// phone. The responsive rules exist; without this tag none of them can match.
func TestTemplatesDeclareViewport(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "web", "templates", "*.html"))
	if err != nil {
		t.Fatalf("glob templates: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no templates found")
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(data)
		// Partials (brand.html) define template fragments and have no <head>.
		if !strings.Contains(body, "<head>") {
			continue
		}
		if !strings.Contains(body, `name="viewport"`) {
			t.Errorf("%s has a <head> but no viewport meta; mobile browsers would render it at ~980px and scale down", filepath.Base(path))
			continue
		}
		if !strings.Contains(body, "width=device-width") {
			t.Errorf("%s declares a viewport without width=device-width, so the layout viewport stays at the desktop default", filepath.Base(path))
		}
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

// The web UI and the bundled sample apps run in the same place — the same host
// for a binary install, the same container for a Docker one — so they cannot
// want the same port. They did once: both defaulted to 3270, and starting a
// sample app on a default install failed with "address already in use".
func TestSampleAppPortsDoNotCollideWithTheWebUI(t *testing.T) {
	for _, port := range allowedSampleAppPortsList {
		if strconv.Itoa(port) == defaultWebUIPort {
			t.Errorf("sample app port %d is the web UI's own default port; a default install cannot bind both", port)
		}
	}
	if _, ok := allowedSampleAppPortSet[defaultSampleAppPort]; !ok {
		t.Errorf("defaultSampleAppPort %d is not in the allowed list, so the default choice would be rejected", defaultSampleAppPort)
	}
}

// The installer must say why it failed.
//
// `compose up` ran with both streams sent to /dev/null and, on failure, the
// script told the operator to run it again themselves to find out why — at the
// one moment it held the answer and they did not. Worse, the spinner was
// stopped first, and spin_stop's last command was a `[ ... ] && step` that
// evaluates false when called with no label: under `set -e` that ended the
// script before the explanation printed, so every failure came out as the same
// bare "installation did not complete".
func TestInstallScriptExplainsFailures(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "install.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	script := string(data)

	if strings.Contains(script, "up -d >/dev/null 2>&1") {
		t.Error("install.sh discards compose's output; a failed `up -d` cannot then be explained")
	}
	if !strings.Contains(script, `[ "$#" -gt 0 ]; then`) {
		t.Error("spin_stop must not end on a bare `[ ... ] && step`; with no label it returns 1 and `set -e` kills the script before the failure is reported")
	}
	if !strings.Contains(script, "  return 0\n}") {
		t.Error("spin_stop must return 0 explicitly, or stopping the spinner on a failure path aborts the script")
	}
}
