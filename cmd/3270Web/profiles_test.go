// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileS3270Target(t *testing.T) {
	tests := []struct {
		name    string
		profile ConnectionProfile
		want    string
	}{
		{
			name:    "plain host and port",
			profile: ConnectionProfile{Host: "mainframe.example.com", Port: 23},
			want:    "mainframe.example.com:23",
		},
		{
			name:    "port defaults to 3270",
			profile: ConnectionProfile{Host: "mainframe"},
			want:    "mainframe:3270",
		},
		{
			name:    "TLS adds the L prefix",
			profile: ConnectionProfile{Host: "secure.example.com", Port: 992, TLS: true},
			want:    "L:secure.example.com:992",
		},
		{
			name:    "skipping verification adds Y after L",
			profile: ConnectionProfile{Host: "selfsigned", Port: 992, TLS: true, SkipVerify: true},
			want:    "L:Y:selfsigned:992",
		},
		{
			name:    "an LU name is prefixed with @",
			profile: ConnectionProfile{Host: "mainframe", Port: 23, LUName: "TCP00042"},
			want:    "TCP00042@mainframe:23",
		},
		{
			name:    "everything at once, in s3270's order",
			profile: ConnectionProfile{Host: "mainframe", Port: 992, TLS: true, SkipVerify: true, LUName: "LU01"},
			want:    "L:Y:LU01@mainframe:992",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.profile.s3270Target(); got != tt.want {
				t.Errorf("s3270Target() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProfileOverrideArgs(t *testing.T) {
	p := ConnectionProfile{Model: "3279-2-E", CodePage: "cp285"}
	got := p.overrideArgs()
	want := []string{"-model", "3279-2-E", "-codepage", "cp285"}
	if len(got) != len(want) {
		t.Fatalf("overrideArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got[i], want[i])
		}
	}

	if empty := (ConnectionProfile{}).overrideArgs(); len(empty) != 0 {
		t.Errorf("a profile with no overrides produced %v", empty)
	}
}

// These values become argv elements rather than a shell command line, so
// quoting is not the hazard — being read as a FLAG is. A value beginning with
// "-" would turn an innocuous-looking profile into option injection against
// s3270.
func TestValidateProfileRejectsOptionInjection(t *testing.T) {
	tests := []struct {
		name    string
		profile ConnectionProfile
	}{
		{"LU name as a flag", ConnectionProfile{Name: "p", Host: "h", LUName: "-trace"}},
		{"model as a flag", ConnectionProfile{Name: "p", Host: "h", Model: "-scriptport"}},
		{"code page as a flag", ConnectionProfile{Name: "p", Host: "h", CodePage: "-proxy"}},
		{"LU name with a space smuggling a second token", ConnectionProfile{Name: "p", Host: "h", LUName: "LU01 -trace"}},
		{"LU name with @ escaping its field", ConnectionProfile{Name: "p", Host: "h", LUName: "a@b"}},
		{"model with a colon", ConnectionProfile{Name: "p", Host: "h", Model: "a:b"}},
		{"code page with a newline", ConnectionProfile{Name: "p", Host: "h", CodePage: "cp037\nx"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := tt.profile
			if err := validateProfile(&profile); err == nil {
				t.Errorf("validateProfile accepted %+v", tt.profile)
			}
		})
	}
}

func TestValidateProfileRequirements(t *testing.T) {
	tests := []struct {
		name    string
		profile ConnectionProfile
		wantErr bool
	}{
		{"a valid minimal profile", ConnectionProfile{Name: "TSO", Host: "mainframe"}, false},
		{"a full profile", ConnectionProfile{
			Name: "CICS Prod", Host: "cics.example.com", Port: 992,
			TLS: true, LUName: "LU01", Model: "3279-4-E", CodePage: "cp037",
		}, false},
		{"no name", ConnectionProfile{Host: "mainframe"}, true},
		{"no host", ConnectionProfile{Name: "TSO"}, true},
		{"port out of range", ConnectionProfile{Name: "TSO", Host: "h", Port: 70000}, true},
		{"negative port", ConnectionProfile{Name: "TSO", Host: "h", Port: -1}, true},
		{"host carrying its own port", ConnectionProfile{Name: "TSO", Host: "mainframe:992"}, true},
		{"host carrying an LU", ConnectionProfile{Name: "TSO", Host: "lu@mainframe"}, true},
		// "Encrypted but unverified" is materially weaker than TLS, so it
		// must not be settable by accident on a plaintext profile.
		{"skip-verify without TLS", ConnectionProfile{Name: "TSO", Host: "h", SkipVerify: true}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := tt.profile
			err := validateProfile(&profile)
			if tt.wantErr && err == nil {
				t.Errorf("validateProfile(%+v) = nil, want an error", tt.profile)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateProfile(%+v) = %v, want nil", tt.profile, err)
			}
		})
	}
}

func TestValidateProfileDefaultsPort(t *testing.T) {
	p := ConnectionProfile{Name: "TSO", Host: "mainframe"}
	if err := validateProfile(&p); err != nil {
		t.Fatal(err)
	}
	if p.Port != 3270 {
		t.Errorf("port = %d, want the 3270 default", p.Port)
	}
}

func TestProfileStoreRoundTrip(t *testing.T) {
	store := newConnectionProfileStore(t.TempDir())

	if got, err := store.load(); err != nil || len(got) != 0 {
		t.Fatalf("empty store: got %v, err %v", got, err)
	}

	if _, err := store.upsert(ConnectionProfile{Name: "TSO", Host: "a", Port: 23}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.upsert(ConnectionProfile{Name: "CICS", Host: "b", Port: 992, TLS: true}); err != nil {
		t.Fatal(err)
	}

	profiles, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Fatalf("got %d profiles, want 2", len(profiles))
	}
	// Sorted case-insensitively by name, so the list is stable in the UI.
	if profiles[0].Name != "CICS" || profiles[1].Name != "TSO" {
		t.Errorf("profiles are not sorted by name: %v", []string{profiles[0].Name, profiles[1].Name})
	}

	// Saving the same name replaces rather than duplicating.
	if _, err := store.upsert(ConnectionProfile{Name: "tso", Host: "changed", Port: 23}); err != nil {
		t.Fatal(err)
	}
	profiles, _ = store.load()
	if len(profiles) != 2 {
		t.Fatalf("a case-different name duplicated the profile: %d entries", len(profiles))
	}
	found, ok := store.find("TSO")
	if !ok || found.Host != "changed" {
		t.Errorf("find(TSO) = %+v, ok=%v; want the updated host", found, ok)
	}

	if _, err := store.delete("CICS"); err != nil {
		t.Fatal(err)
	}
	profiles, _ = store.load()
	if len(profiles) != 1 || profiles[0].Name != "tso" {
		t.Errorf("after delete: %+v", profiles)
	}
}

func TestProfileStoreSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	first := newConnectionProfileStore(dir)
	if _, err := first.upsert(ConnectionProfile{Name: "TSO", Host: "mainframe", Port: 23, TLS: true, LUName: "LU01"}); err != nil {
		t.Fatal(err)
	}

	// A profile is deployment configuration; it has to outlive the process.
	second := newConnectionProfileStore(dir)
	got, ok := second.find("TSO")
	if !ok {
		t.Fatal("profile did not survive reopening the store")
	}
	if got.LUName != "LU01" || !got.TLS {
		t.Errorf("round-tripped profile lost fields: %+v", got)
	}
	if _, err := filepath.Abs(filepath.Join(dir, profilesFileName)); err != nil {
		t.Fatal(err)
	}
}

func TestProfileFindMissing(t *testing.T) {
	store := newConnectionProfileStore(t.TempDir())
	if _, ok := store.find("nope"); ok {
		t.Error("find returned ok for a profile that does not exist")
	}
}

// The connect form accepts "sampleapp:app1", so profiles must too — the two
// paths disagreeing about what counts as a valid host would be its own bug.
// A sample-app preset with no port has to end up on a port a sample app
// actually listens on.
//
// This used to default to 3270 like any other host, and the target it produced
// parsed perfectly — which is all the previous version of this test asked. It
// could not connect: 3270 is what the web interface itself listens on, the
// sample apps start at 3271, and isAllowedSampleAppPort refuses anything else.
// So the natural thing to write, a preset with the port left blank, was the
// one preset guaranteed to fail on selection.
func TestValidateProfileAcceptsSampleApps(t *testing.T) {
	p := ConnectionProfile{Name: "Demo", Host: "sampleapp:app1"}
	if err := validateProfile(&p); err != nil {
		t.Fatalf("validateProfile rejected a sample-app host: %v", err)
	}
	if p.Port != defaultSampleAppPort {
		t.Errorf("Port = %d, want the sample apps' own default %d", p.Port, defaultSampleAppPort)
	}
	id, port, ok := parseSampleAppHost(p.displayTarget())
	if !ok || id != "app1" || port != defaultSampleAppPort {
		t.Errorf("parseSampleAppHost(%q) = (%q, %d, %v)", p.displayTarget(), id, port, ok)
	}
	if !isAllowedSampleAppPort(port) {
		t.Errorf("port %d is not one a sample app may listen on, so the preset cannot connect", port)
	}
}

// A port no sample app listens on is refused when the preset is written, not
// when somebody connects: a preset is stored once and used by everybody it was
// assigned to, so the failure would otherwise arrive one operator at a time.
func TestValidateProfileRefusesAnImpossibleSampleAppPort(t *testing.T) {
	p := ConnectionProfile{Name: "Demo", Host: "sampleapp:app1", Port: 3270}
	err := validateProfile(&p)
	if err == nil {
		t.Fatal("validateProfile accepted a sample app on a port none of them listen on")
	}
	if !strings.Contains(err.Error(), "3271") {
		t.Errorf("the refusal does not say which ports work: %v", err)
	}
}

func TestValidateProfileRefusesAnUnknownSampleApp(t *testing.T) {
	p := ConnectionProfile{Name: "Demo", Host: "sampleapp:app9"}
	if err := validateProfile(&p); err == nil {
		t.Error("validateProfile accepted a sample app that does not exist")
	}
}

// A real host still must not smuggle a port or LU past the structured fields.
func TestValidateProfileStillRejectsHostWithPort(t *testing.T) {
	p := ConnectionProfile{Name: "X", Host: "mainframe:992"}
	if err := validateProfile(&p); err == nil {
		t.Error("validateProfile accepted a host carrying its own port")
	}
}
