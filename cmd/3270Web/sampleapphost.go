// SPDX-License-Identifier: AGPL-3.0-or-later

package main

// `3270Web sampleapp` — the bundled sample applications as an ordinary TN3270
// host, on a port anything can dial.
//
// The samples already run inside this process: a session pointed at
// "sampleapp:petstore" gets a listener on loopback, started for it and stopped
// when the last session using it leaves. That is right for the terminal and
// useless for everything else. Loopback means "this process's machine", and
// the thing most likely to want a 3270 host to practise against is not on it —
// an automation run in a container beside this one, a colleague's laptop, a CI
// job that needs a host that behaves like a host and does not need booking.
//
// So this serves the same applications the same way, in the foreground, until
// it is stopped. Nothing here is a mainframe: they are demonstration screens
// with no data behind them, which is exactly why they are safe to expose and
// why the terminal's own copies stay where they are.

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/jnnngs/3270Web/internal/sampleapps"
)

// The interface the subcommand binds when nothing says otherwise.
//
// Every other listener in this program defaults to loopback and is opted out
// of. This one is the opposite way round because there is no other reason to
// run it: a sample host nothing else can reach is the thing you already had.
// It is announced at startup rather than left to be discovered.
const defaultSampleAppHostBind = "0.0.0.0"

// sampleAppSpec is one application and the port it is served on.
type sampleAppSpec struct {
	ID   string
	Port int
}

// repeatedFlag collects a flag given more than once, so several applications
// can be served by one process.
type repeatedFlag []string

func (r *repeatedFlag) String() string { return strings.Join(*r, ",") }

func (r *repeatedFlag) Set(value string) error {
	*r = append(*r, value)
	return nil
}

// parseSampleAppSpecs turns --app values into applications and ports.
//
// A value is an id ("petstore") or an id and a port ("petstore:3271"), and
// either form may carry several separated by commas. Anything without a port
// takes the next free one from defaultSampleAppPort upwards, so serving three
// applications needs no arithmetic from whoever is starting them.
func parseSampleAppSpecs(values []string) ([]sampleAppSpec, error) {
	requested := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				requested = append(requested, part)
			}
		}
	}
	if len(requested) == 0 {
		requested = []string{defaultSampleAppID}
	}

	specs := make([]sampleAppSpec, 0, len(requested))
	taken := make(map[int]string, len(requested))

	// Two passes, because an explicit port has to be reserved before any
	// automatic one is handed out: "--app app1 --app petstore:3272" must not
	// give app1 the port petstore was named for and then fail.
	//
	// A zero port in this first pass means "not chosen yet"; the second pass
	// fills those in from the first free port upwards.
	parsed := make([]sampleAppSpec, 0, len(requested))

	for _, raw := range requested {
		id := raw
		port := 0

		// LastIndex, so a future IPv6-ish spelling would still split on the
		// port rather than on the address.
		if idx := strings.LastIndex(raw, ":"); idx >= 0 {
			id = strings.TrimSpace(raw[:idx])
			text := strings.TrimSpace(raw[idx+1:])
			value, err := strconv.Atoi(text)
			if err != nil || value < 1 || value > 65535 {
				return nil, fmt.Errorf("%q is not a port between 1 and 65535 (in %q)", text, raw)
			}
			port = value
		}

		if _, ok := sampleAppConfig(id); !ok {
			return nil, fmt.Errorf("unknown sample app %q; run \"3270Web sampleapp --list\" for the ones there are", id)
		}
		if port != 0 {
			if other, clash := taken[port]; clash {
				return nil, fmt.Errorf("port %d is already serving %q, so %q cannot use it", port, other, id)
			}
			taken[port] = id
		}
		parsed = append(parsed, sampleAppSpec{ID: id, Port: port})
	}

	next := defaultSampleAppPort
	for _, item := range parsed {
		port := item.Port
		if port == 0 {
			for taken[next] != "" {
				next++
			}
			if next > 65535 {
				return nil, errors.New("ran out of ports to assign")
			}
			port = next
			taken[port] = item.ID
			next++
		}
		specs = append(specs, sampleAppSpec{ID: item.ID, Port: port})
	}
	return specs, nil
}

// startSampleAppHosts brings up every listener, or none of them.
//
// A partial start is the worst outcome: the process would report itself up
// while the one application somebody actually wanted was the one whose port
// was busy. So a failure stops whatever already started and reports the
// application that could not be served.
func startSampleAppHosts(specs []sampleAppSpec, bind string) ([]*sampleapps.Server, error) {
	servers := make([]*sampleapps.Server, 0, len(specs))
	for _, spec := range specs {
		addr := net.JoinHostPort(bind, strconv.Itoa(spec.Port))
		server, err := sampleapps.StartServerOn(spec.ID, addr)
		if err != nil {
			for _, started := range servers {
				_ = started.Stop()
			}
			return nil, fmt.Errorf("sample app %q on %s: %w", spec.ID, addr, err)
		}
		servers = append(servers, server)
	}
	return servers, nil
}

func sampleAppHostUsage(out io.Writer) {
	fmt.Fprint(out, `Usage: 3270Web sampleapp [options]

Serve the bundled sample applications as a TN3270 host, so a terminal, an
automation run or a CI job on another machine can connect to them.

Options
  --app <id[:port]>   Application to serve; repeatable, and accepts a
                      comma-separated list. Ports not given are assigned
                      from `+strconv.Itoa(defaultSampleAppPort)+` upwards.
                      (default: `+defaultSampleAppID+`)
  --bind <address>    Interface to listen on (default: `+defaultSampleAppHostBind+`,
                      every interface). Use 127.0.0.1 to keep it local.
  --list              List the available applications and exit.
  --help, -h          This text.

Examples
  3270Web sampleapp
  3270Web sampleapp --app petstore:3271 --app app1:3272
  3270Web sampleapp --app wordle,snake,pong --bind 127.0.0.1
`)
}

// runSampleAppHost serves sample applications until the process is signalled.
func runSampleAppHost(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sampleapp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { sampleAppHostUsage(stderr) }

	var apps repeatedFlag
	fs.Var(&apps, "app", "sample app to serve, as id or id:port; repeatable")
	bind := fs.String("bind", defaultSampleAppHostBind, "interface to listen on")
	list := fs.Bool("list", false, "list the available sample apps and exit")
	help := fs.Bool("help", false, "show usage")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			sampleAppHostUsage(stdout)
			return 0
		}
		return 2
	}
	if *help {
		sampleAppHostUsage(stdout)
		return 0
	}
	if *list {
		printSampleAppList(stdout)
		return 0
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "3270Web sampleapp: unexpected argument %q\n", fs.Arg(0))
		sampleAppHostUsage(stderr)
		return 2
	}

	specs, err := parseSampleAppSpecs(apps)
	if err != nil {
		fmt.Fprintf(stderr, "3270Web sampleapp: %v\n", err)
		return 2
	}

	servers, err := startSampleAppHosts(specs, *bind)
	if err != nil {
		fmt.Fprintf(stderr, "3270Web sampleapp: %v\n", err)
		return 1
	}
	defer func() {
		for _, server := range servers {
			_ = server.Stop()
		}
	}()

	for i, spec := range specs {
		name := spec.ID
		if cfg, ok := sampleAppConfig(spec.ID); ok {
			name = cfg.Name
		}
		fmt.Fprintf(stdout, "%s\ttn3270://%s\t%s\n", spec.ID, servers[i].Addr(), name)
	}
	if *bind == "0.0.0.0" || *bind == "::" || *bind == "" {
		fmt.Fprintln(stdout, "Listening on every interface. These are demonstration screens with no data behind them; nothing here is a mainframe.")
	}
	fmt.Fprintln(stdout, "Press Ctrl+C to stop.")

	// SIGTERM as well as SIGINT: in a container this is stopped by the
	// orchestrator, not by a keyboard, and a process that ignores SIGTERM is
	// one every `docker compose down` waits ten seconds to kill.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	sig := <-signals
	fmt.Fprintf(stdout, "\nStopping (%s).\n", sig)
	return 0
}

func printSampleAppList(out io.Writer) {
	options := availableSampleApps()
	sort.SliceStable(options, func(i, j int) bool { return options[i].ID < options[j].ID })
	for _, option := range options {
		fmt.Fprintf(out, "%s\t%s\n", option.ID, option.Name)
	}
}
