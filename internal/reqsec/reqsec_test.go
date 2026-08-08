package reqsec

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"
)

func TestIsTLS_DirectConnection(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	if IsTLS(req) {
		t.Error("plain request reported as TLS")
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.TLS = &tls.ConnectionState{}
	if !IsTLS(req) {
		t.Error("TLS request reported as plain")
	}
}

// The forwarding header is attacker-controlled unless a proxy is known to be
// in front, so it must do nothing until the operator opts in.
func TestIsTLS_IgnoresForwardedProtoByDefault(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	if IsTLS(req) {
		t.Error("X-Forwarded-Proto was believed without TRUST_PROXY_HEADERS")
	}
}

func TestIsTLS_HonoursForwardedProtoWhenTrusted(t *testing.T) {
	t.Setenv(TrustProxyHeadersEnv, "true")

	tests := map[string]bool{
		"https":       true,
		"HTTPS":       true,
		"  https  ":   true,
		"https, http": true, // client-facing hop is first
		"http":        false,
		"http, https": false,
		"":            false,
		"httpsx":      false,
		"ws":          false,
	}

	for header, want := range tests {
		req := httptest.NewRequest("GET", "/", nil)
		if header != "" {
			req.Header.Set("X-Forwarded-Proto", header)
		}
		if got := IsTLS(req); got != want {
			t.Errorf("X-Forwarded-Proto=%q: IsTLS = %v, want %v", header, got, want)
		}
	}
}

func TestIsTLS_NilRequest(t *testing.T) {
	if IsTLS(nil) {
		t.Error("nil request reported as TLS")
	}
}

func TestTrustProxyHeaders(t *testing.T) {
	t.Setenv(TrustProxyHeadersEnv, "")
	if TrustProxyHeaders() {
		t.Error("unset should not trust")
	}
	t.Setenv(TrustProxyHeadersEnv, "TRUE")
	if !TrustProxyHeaders() {
		t.Error("TRUE should trust")
	}
	t.Setenv(TrustProxyHeadersEnv, "1")
	if TrustProxyHeaders() {
		t.Error("only an explicit true should trust")
	}
}

func TestClientIP_DirectConnection(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.7:54321"

	if got := ClientIP(req); got != "203.0.113.7" {
		t.Errorf("ClientIP = %q, want %q", got, "203.0.113.7")
	}
}

// Without the opt-in the header is just something the client said.
func TestClientIP_IgnoresForwardedForByDefault(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header.Set("X-Forwarded-For", "10.9.9.9")

	if got := ClientIP(req); got != "203.0.113.7" {
		t.Errorf("ClientIP = %q, want the peer address %q", got, "203.0.113.7")
	}
}

func TestClientIP_HonoursForwardedForWhenTrusted(t *testing.T) {
	t.Setenv(TrustProxyHeadersEnv, "true")

	tests := map[string]string{
		"10.9.9.9":                    "10.9.9.9",
		"10.9.9.9, 172.16.0.1":        "10.9.9.9",
		"  10.9.9.9  ,  172.16.0.1  ": "10.9.9.9",
		"":                            "203.0.113.7",
		"   ":                         "203.0.113.7",
	}

	for header, want := range tests {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.7:54321"
		if header != "" {
			req.Header.Set("X-Forwarded-For", header)
		}
		if got := ClientIP(req); got != want {
			t.Errorf("X-Forwarded-For=%q: ClientIP = %q, want %q", header, got, want)
		}
	}
}

func TestClientIP_IPv6AndOddRemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "[2001:db8::1]:443"
	if got := ClientIP(req); got != "2001:db8::1" {
		t.Errorf("IPv6 ClientIP = %q, want %q", got, "2001:db8::1")
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "not-host-port"
	if got := ClientIP(req); got != "not-host-port" {
		t.Errorf("odd RemoteAddr ClientIP = %q, want it passed through", got)
	}

	if got := ClientIP(nil); got != "" {
		t.Errorf("ClientIP(nil) = %q, want empty", got)
	}
}
