package printer

import (
	"strings"
	"testing"
)

// base is a set of options that validates, so each case below can change one
// thing and the failure names that thing.
func base() Options {
	return Options{
		Host:         "mainframe.example",
		Port:         992,
		LU:           "PRT001",
		SpoolCommand: "/app/3270Web print-spool -dir /data/printer-jobs/abc",
	}
}

func TestOptionsValidate(t *testing.T) {
	cases := []struct {
		name  string
		mutil func(*Options)
		want  string
	}{
		{"a valid set", func(*Options) {}, ""},
		{"no host", func(o *Options) { o.Host = "" }, "no host"},
		{"a host that would read as an option", func(o *Options) { o.Host = "-x" }, "cannot be used"},
		{"a host with a space in it", func(o *Options) { o.Host = "main frame" }, "cannot be used"},
		// The colon is the one that matters: the target is "host:port", so a
		// second colon quietly changes which port is meant.
		{"a host carrying its own colon", func(o *Options) { o.Host = "sampleapp:petstore" }, "cannot be used"},
		{"a bracketed IPv6 literal", func(o *Options) { o.Host = "[2001:db8::1]" }, ""},
		{"no port", func(o *Options) { o.Port = 0 }, "usable port"},
		{"no LU at all", func(o *Options) { o.LU = "" }, "name the printer LU"},
		{"both kinds of LU", func(o *Options) { o.Associate = "TRM001" }, "not both"},
		{"an LU that would read as an option", func(o *Options) { o.LU = "-assoc" }, "valid LU name"},
		{"an LU with a separator in it", func(o *Options) { o.LU = "PRT,001" }, "valid LU name"},
		{"an LU with a newline in it", func(o *Options) { o.LU = "PRT\n001" }, "valid LU name"},
		{"a code page that would read as an option", func(o *Options) { o.CodePage = "-trace" }, "valid name"},
		{"a line length below the floor", func(o *Options) { o.MPP = 10 }, "line length"},
		{"a line length above the ceiling", func(o *Options) { o.MPP = 1000 }, "line length"},
		{"a negative end-of-job timeout", func(o *Options) { o.EOJTimeout = -1 }, "end-of-job timeout"},
		{"no spool command", func(o *Options) { o.SpoolCommand = "" }, "spool command"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := base()
			tc.mutil(&opts)
			err := opts.Validate()
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("Validate() = %v, want no error", err)
			case tc.want == "":
				return
			case err == nil:
				t.Fatalf("Validate() = nil, want an error mentioning %q", tc.want)
			case !strings.Contains(err.Error(), tc.want):
				t.Errorf("Validate() = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestOptionsTarget(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want string
	}{
		{"a named LU in the clear", Options{Host: "h", Port: 23, LU: "PRT001"}, "PRT001@h:23"},
		{"a named LU over TLS", Options{Host: "h", Port: 992, LU: "PRT001", TLS: true}, "L:PRT001@h:992"},
		{
			// An associated printer names the *display* LU in an option, not
			// in the target, so the target carries no LU at all.
			"an associated printer",
			Options{Host: "h", Port: 23, Associate: "TRM001"},
			"h:23",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opts.Target(); got != tc.want {
				t.Errorf("Target() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOptionsArgs(t *testing.T) {
	opts := base()
	opts.Associate = ""
	opts.CodePage = "cp037"
	opts.MPP = 132
	opts.CRLF = true
	opts.FFSkip = true
	opts.EOJTimeout = 30
	opts.TLS = true
	opts.Verify = true

	args, err := opts.Args()
	if err != nil {
		t.Fatalf("Args() = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-command " + opts.SpoolCommand,
		"-codepage cp037",
		"-mpp 132",
		"-crlf",
		"-ffskip",
		"-eojtimeout 30",
		"-verifycert",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("Args() = %q, want it to contain %q", joined, want)
		}
	}
	if got := args[len(args)-1]; got != "L:PRT001@mainframe.example:992" {
		t.Errorf("last argument = %q, want the connection target", got)
	}
	// Options nobody asked for must not appear: pr3287 reads several of these
	// as "do the opposite", and a printer that quietly drops blank lines is a
	// support call nobody can reproduce.
	for _, unwanted := range []string{"-assoc", "-blanklines", "-ffthru", "-ignoreeoj", "-skipcc"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("Args() = %q, should not contain %q", joined, unwanted)
		}
	}
}

// Certificate verification is stated either way round and never left to the
// default, because which default the installed pr3287 has depends on its
// version. A connection in the clear has nothing to verify and says neither.
func TestOptionsArgsVerifyOnlyWithTLS(t *testing.T) {
	for _, tc := range []struct {
		name        string
		tls, verify bool
		want        bool
	}{
		{"TLS and verifying", true, true, true},
		{"TLS without verifying", true, false, false},
		{"no TLS", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := base()
			opts.TLS = tc.tls
			opts.Verify = tc.verify
			args, err := opts.Args()
			if err != nil {
				t.Fatalf("Args() = %v", err)
			}
			joined := " " + strings.Join(args, " ") + " "
			if got := strings.Contains(joined, " -verifycert "); got != tc.want {
				t.Errorf("-verifycert present = %v, want %v", got, tc.want)
			}
			// The other half of the same decision: a TLS session that is not
			// verifying has to say so, because the installed build may check
			// by default.
			wantNo := tc.tls && !tc.verify
			if got := strings.Contains(joined, " -noverifycert "); got != wantNo {
				t.Errorf("-noverifycert present = %v, want %v", got, wantNo)
			}
		})
	}
}

// Args validates first, so a bad LU name never reaches an argument vector.
func TestOptionsArgsRefusesInvalid(t *testing.T) {
	opts := base()
	opts.LU = "-command"
	if _, err := opts.Args(); err == nil {
		t.Fatal("Args() accepted an LU name that would read as an option")
	}
}
