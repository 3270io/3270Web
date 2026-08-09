package hostpolicy

import "testing"

// The default has to stay "everything", or turning the feature on becomes a
// prerequisite for the product working at all.
func TestEmptyListAllowsEverything(t *testing.T) {
	for _, host := range []string{"mainframe.example.test", "10.0.0.5:23", "[2001:db8::1]:992"} {
		if !Allowed("", host) || !Allowed("   ", host) {
			t.Errorf("Allowed(\"\", %q) = false, want true", host)
		}
	}
}

func TestMatching(t *testing.T) {
	const patterns = "*.corp.example.test, prod-lpar, 10.0.*"

	for _, host := range []string{
		"tso.corp.example.test",
		"TSO.CORP.EXAMPLE.TEST",     // hostnames are case-insensitive
		"tso.corp.example.test:992", // the port is not part of the decision
		"prod-lpar",
		"prod-lpar:23",
		"10.0.0.5",
		"10.0.255.255:3270",
	} {
		if !Allowed(patterns, host) {
			t.Errorf("Allowed(%q) = false, want true", host)
		}
	}

	for _, host := range []string{
		"corp.example.test", // the glob needs a label before it
		"tso.corp.example.test.evil.test",
		"other-lpar",
		"10.1.0.5",
		"prod-lpar.evil.test",
		"",
	} {
		if Allowed(patterns, host) {
			t.Errorf("Allowed(%q) = true, want false", host)
		}
	}
}

// An IPv6 literal is full of colons, so a host/port split that looks for the
// first one leaves "[2001" and matches nothing — every v6 host silently
// refused on a fenced instance.
func TestIPv6(t *testing.T) {
	if !Allowed("2001:db8::1", "[2001:db8::1]:23") {
		t.Error("a v6 literal with a port was not matched")
	}
	if !Allowed("2001:db8::1", "[2001:db8::1]") {
		t.Error("a bracketed v6 literal without a port was not matched")
	}
	if !Allowed("2001:db8::*", "[2001:db8::99]:992") {
		t.Error("a v6 glob was not matched")
	}
	if Allowed("2001:db8::1", "[2001:db8::2]:23") {
		t.Error("a different v6 address was matched")
	}
}

// A profile carries s3270's own prefixes. They describe how to connect, not
// what is being connected to, and must not defeat the allowlist.
func TestS3270TargetPrefixes(t *testing.T) {
	for _, target := range []string{
		"L:mainframe.example.test:992",
		"L:Y:mainframe.example.test:992",
		"LU01@mainframe.example.test:992",
		"L:Y:LU01@mainframe.example.test:992",
	} {
		if !Allowed("mainframe.example.test", target) {
			t.Errorf("%q was not matched", target)
		}
		if Allowed("other.example.test", target) {
			t.Errorf("%q matched the wrong pattern", target)
		}
	}
}

// Somebody writing the list will write a port sooner or later.
func TestPatternsMayCarryAPort(t *testing.T) {
	if !Allowed("mainframe.example.test:992", "mainframe.example.test:992") {
		t.Error("a pattern with a port did not match")
	}
	if !Allowed("mainframe.example.test:992", "mainframe.example.test:23") {
		t.Error("the port in a pattern was treated as part of the host")
	}
}

func TestHostname(t *testing.T) {
	for target, want := range map[string]string{
		"mainframe.example.test":       "mainframe.example.test",
		"mainframe.example.test:992":   "mainframe.example.test",
		"MAINFRAME.example.test":       "mainframe.example.test",
		"[2001:db8::1]:23":             "2001:db8::1",
		"[2001:db8::1]":                "2001:db8::1",
		"L:Y:LU01@host.example.test:9": "host.example.test",
		"  host.example.test  ":        "host.example.test",
		"":                             "",
	} {
		if got := Hostname(target); got != want {
			t.Errorf("Hostname(%q) = %q, want %q", target, got, want)
		}
	}
}

// A list that is set but matches nothing must refuse, not fall open.
func TestUnmatchableListRefuses(t *testing.T) {
	if Allowed(",  , ,", "mainframe.example.test") {
		t.Error("a list of empty patterns allowed a host")
	}
	if Allowed("[", "mainframe.example.test") {
		t.Error("a malformed pattern allowed a host")
	}
}
