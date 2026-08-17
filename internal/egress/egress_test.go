// SPDX-License-Identifier: AGPL-3.0-or-later

package egress

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A name is not an address, which is the whole reason this package exists: a
// destination check that only looks at IP literals never sees the ones that
// arrive by resolution.
func TestCheckHostRefusesNamesResolvingToBlockedAddresses(t *testing.T) {
	policy := Policy{Lookup: fixedLookup(map[string][]string{
		"metadata.example.test": {"169.254.169.254"},
		"mixed.example.test":    {"93.184.216.34", "169.254.169.254"},
		"public.example.test":   {"93.184.216.34"},
	})}

	for _, name := range []string{"metadata.example.test", "mixed.example.test"} {
		var blocked *ErrBlocked
		if err := policy.CheckHost(context.Background(), name); !errors.As(err, &blocked) {
			t.Errorf("CheckHost(%q) = %v, want a refusal — every answer must be tested, not just the first", name, err)
		}
	}
	if err := policy.CheckHost(context.Background(), "public.example.test"); err != nil {
		t.Errorf("CheckHost on a routable name = %v, want nil", err)
	}
}

// A lookup that fails is not a refusal: the connection is about to fail on its
// own with a message that says why, and reporting "forbidden" for "DNS is
// briefly down" would be both wrong and hard to act on.
func TestCheckHostAllowsUnresolvableNames(t *testing.T) {
	policy := Policy{Lookup: fixedLookup(nil)}
	if err := policy.CheckHost(context.Background(), "nowhere.example.test"); err != nil {
		t.Errorf("CheckHost on an unresolvable name = %v, want nil", err)
	}
}

func TestAddressPolicy(t *testing.T) {
	open := Policy{}
	fenced := Policy{DenyLoopback: true, DenyPrivate: true}

	cases := []struct {
		ip            string
		openBlocked   bool
		fencedBlocked bool
	}{
		{"169.254.169.254", true, true},
		{"fe80::1", true, true},
		{"0.0.0.0", true, true},
		{"::", true, true},
		{"127.0.0.1", false, true},
		{"10.1.2.3", false, true},
		{"93.184.216.34", false, false},
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if got := open.Address(ip) != ""; got != tc.openBlocked {
			t.Errorf("Default policy on %s: blocked=%v, want %v", tc.ip, got, tc.openBlocked)
		}
		if got := fenced.Address(ip) != ""; got != tc.fencedBlocked {
			t.Errorf("Fenced policy on %s: blocked=%v, want %v", tc.ip, got, tc.fencedBlocked)
		}
	}
}

// The dialer is where the check has to live, because a redirect re-enters it.
// A first request to somewhere permitted followed by a 302 to somewhere that
// is not must fail on the hop, not follow it.
func TestHTTPClientRefusesRedirectToBlockedAddress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://blocked.example.test/", http.StatusFound)
	}))
	defer upstream.Close()

	_, port, err := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}

	policy := Policy{Lookup: fixedLookup(map[string][]string{
		"blocked.example.test": {"169.254.169.254"},
		"allowed.example.test": {"127.0.0.1"},
	})}
	client := policy.HTTPClient(0)

	resp, err := client.Get("http://allowed.example.test:" + port + "/")
	if err == nil {
		resp.Body.Close()
		t.Fatal("the redirect to a link-local address was followed")
	}
	if !strings.Contains(err.Error(), "link-local") {
		t.Errorf("error = %v, want the refusal to name why", err)
	}
}

// A permitted destination still has to work, or the check is just an outage.
func TestHTTPClientAllowsPermittedDestination(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	resp, err := Default().HTTPClient(0).Get(upstream.URL)
	if err != nil {
		t.Fatalf("Get on a loopback test server = %v, want success (loopback is deliberately permitted)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// fixedLookup answers from a table, so a test can describe a hostile DNS
// answer without depending on what the machine running it can resolve.
func fixedLookup(table map[string][]string) func(context.Context, string) ([]net.IP, error) {
	return func(_ context.Context, host string) ([]net.IP, error) {
		answers, ok := table[host]
		if !ok {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		out := make([]net.IP, 0, len(answers))
		for _, a := range answers {
			out = append(out, net.ParseIP(a))
		}
		return out, nil
	}
}
