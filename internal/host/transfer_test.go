package host

import (
	"strings"
	"testing"
)

func TestBuildTransferCommandMinimal(t *testing.T) {
	cmd, err := buildTransferCommand(TransferOptions{
		Direction: TransferSend,
		HostFile:  "USER.DATA",
		LocalFile: "/tmp/x/upload.bin",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Options are emitted in a stable (sorted) order so the command is
	// reproducible in logs and tests.
	want := `Transfer(Direction=send,HostFile="USER.DATA",LocalFile="/tmp/x/upload.bin")`
	if cmd != want {
		t.Errorf("buildTransferCommand() = %q, want %q", cmd, want)
	}
}

func TestBuildTransferCommandFullOptions(t *testing.T) {
	cmd, err := buildTransferCommand(TransferOptions{
		Direction:      TransferSend,
		HostFile:       "USER.DATA(MEMBER)",
		LocalFile:      "/tmp/x/upload.bin",
		HostType:       "TSO",
		Mode:           "ASCII",
		Cr:             "remove",
		Exist:          "replace",
		Recfm:          "fixed",
		Lrecl:          80,
		Blksize:        3120,
		PrimarySpace:   10,
		SecondarySpace: 5,
		Allocation:     "tracks",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Direction=send", `HostFile="USER.DATA(MEMBER)"`, "Host=tso", "Mode=ascii",
		"Cr=remove", "Exist=replace", "Recfm=fixed", "Lrecl=80", "Blksize=3120",
		"PrimarySpace=10", "SecondarySpace=5", "Allocation=tracks",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command is missing %q\ngot: %s", want, cmd)
		}
	}
	// Enumerated values are lowercased so the host sees what s3270 expects
	// regardless of how the form was filled in.
	if strings.Contains(cmd, "TSO") || strings.Contains(cmd, "ASCII") {
		t.Errorf("enumerated values were not normalised: %s", cmd)
	}
}

// Zero-valued numbers mean "not specified" and must not be sent as 0, which
// s3270 would take literally.
func TestBuildTransferCommandOmitsZeroNumbers(t *testing.T) {
	cmd, err := buildTransferCommand(TransferOptions{
		Direction: TransferReceive,
		HostFile:  "USER.DATA",
		LocalFile: "/tmp/x/download.bin",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"Lrecl", "Blksize", "PrimarySpace", "SecondarySpace"} {
		if strings.Contains(cmd, unwanted) {
			t.Errorf("command should omit unset %s: %s", unwanted, cmd)
		}
	}
}

// Transfer options are comma-separated inside an action that goes down the
// same pipe as every other s3270 command. A value carrying a comma, a
// parenthesis or a newline would not merely be wrong — it could append a
// second action.
func TestBuildTransferCommandRejectsInjection(t *testing.T) {
	base := func() TransferOptions {
		return TransferOptions{
			Direction: TransferSend,
			HostFile:  "USER.DATA",
			LocalFile: "/tmp/x/upload.bin",
		}
	}

	tests := []struct {
		name   string
		mutate func(*TransferOptions)
	}{
		{"host file with a comma splitting the option list", func(o *TransferOptions) { o.HostFile = "A,Mode=binary" }},
		{"host file with a newline starting a new command", func(o *TransferOptions) { o.HostFile = "A\nQuit" }},
		{"host file with a semicolon", func(o *TransferOptions) { o.HostFile = "A;Quit" }},
		{"host file with a quote", func(o *TransferOptions) { o.HostFile = "A\"B" }},
		{"empty host file", func(o *TransferOptions) { o.HostFile = "  " }},
		{"local path with a comma", func(o *TransferOptions) { o.LocalFile = "/tmp/a,b" }},
		{"local path with a newline", func(o *TransferOptions) { o.LocalFile = "/tmp/a\nQuit" }},
		{"empty local path", func(o *TransferOptions) { o.LocalFile = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := base()
			tt.mutate(&opts)
			if cmd, err := buildTransferCommand(opts); err == nil {
				t.Errorf("buildTransferCommand accepted a dangerous value and produced %q", cmd)
			}
		})
	}
}

// Enumerated options are checked against an allowlist rather than filtered,
// so nothing outside the exact accepted strings ever reaches the action.
func TestBuildTransferCommandRejectsUnknownEnumValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*TransferOptions)
	}{
		{"unknown mode", func(o *TransferOptions) { o.Mode = "ebcdic" }},
		{"unknown host type", func(o *TransferOptions) { o.HostType = "mvs" }},
		{"unknown cr handling", func(o *TransferOptions) { o.Cr = "strip" }},
		{"unknown exist policy", func(o *TransferOptions) { o.Exist = "overwrite" }},
		{"unknown recfm", func(o *TransferOptions) { o.Recfm = "spanned" }},
		{"unknown allocation", func(o *TransferOptions) { o.Allocation = "blocks" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := TransferOptions{Direction: TransferSend, HostFile: "A", LocalFile: "/tmp/a"}
			tt.mutate(&opts)
			if _, err := buildTransferCommand(opts); err == nil {
				t.Errorf("buildTransferCommand accepted %+v", opts)
			}
		})
	}
}

func TestBuildTransferCommandRequiresKnownDirection(t *testing.T) {
	for _, direction := range []TransferDirection{"", "upload", "SEND"} {
		if _, err := buildTransferCommand(TransferOptions{
			Direction: direction, HostFile: "A", LocalFile: "/tmp/a",
		}); err == nil {
			t.Errorf("direction %q was accepted", direction)
		}
	}
}

func TestBuildTransferCommandRejectsOutOfRangeNumbers(t *testing.T) {
	opts := TransferOptions{Direction: TransferSend, HostFile: "A", LocalFile: "/tmp/a", Lrecl: -1}
	if _, err := buildTransferCommand(opts); err == nil {
		t.Error("a negative Lrecl was accepted")
	}
	opts = TransferOptions{Direction: TransferSend, HostFile: "A", LocalFile: "/tmp/a", Blksize: 1 << 40}
	if _, err := buildTransferCommand(opts); err == nil {
		t.Error("an absurd Blksize was accepted")
	}
}

// A PDS member name is the most common thing anyone transfers, so the
// parentheses in "USER.DATA(MEMBER)" must survive — they are made safe by
// quoting the value rather than by banning the character.
func TestBuildTransferCommandAllowsPdsMemberNames(t *testing.T) {
	cmd, err := buildTransferCommand(TransferOptions{
		Direction: TransferReceive,
		HostFile:  "USER.SOURCE(PROG1)",
		LocalFile: "/tmp/x/download.bin",
	})
	if err != nil {
		t.Fatalf("a PDS member name was rejected: %v", err)
	}
	if !strings.Contains(cmd, `HostFile="USER.SOURCE(PROG1)"`) {
		t.Errorf("member name was not quoted intact: %s", cmd)
	}
}

// Quoting is only safe while nothing can escape it.
func TestBuildTransferCommandRejectsQuoteEscape(t *testing.T) {
	for _, bad := range []string{`A" Quit("`, `A\"B`, `A\`} {
		if _, err := buildTransferCommand(TransferOptions{
			Direction: TransferSend, HostFile: bad, LocalFile: "/tmp/a",
		}); err == nil {
			t.Errorf("host file %q escaped the quoting", bad)
		}
	}
}

func TestBuildTransferCommandRejectsOverlongHostFile(t *testing.T) {
	opts := TransferOptions{
		Direction: TransferSend,
		HostFile:  strings.Repeat("A", 129),
		LocalFile: "/tmp/a",
	}
	if _, err := buildTransferCommand(opts); err == nil {
		t.Error("an overlong host file name was accepted")
	}
}
