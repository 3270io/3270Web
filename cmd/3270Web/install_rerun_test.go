package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Re-running the installer over an install that is already here must update it,
// never replace it.
//
// The failure these tests exist for looks like a wipe and is not one: the
// container comes back reading a different (new, empty) data folder, so it
// finds no accounts and opens first-run setup for whoever reaches it — while
// every account, API token and audit entry is still sitting in the folder it
// stopped reading. The most ordinary way to land there was to run the one-line
// installer from a different working directory the second time: the project
// directory defaults to ./3270web relative to $PWD, and Compose names the
// project after that directory's basename, so a second ./3270web somewhere else
// is the *same* project — `up -d` recreated the running container against an
// empty data folder.

// installEnv is a sandbox with a Docker stand-in on PATH.
type installEnv struct {
	t     *testing.T
	root  string
	bin   string
	state string
	path  string
}

func newInstallEnv(t *testing.T) *installEnv {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the installer is a Linux shell script")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available")
	}

	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("create stub bin: %v", err)
	}

	fake, err := os.ReadFile(filepath.Join("testdata", "fake-docker.sh"))
	if err != nil {
		t.Fatalf("read fake docker: %v", err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatalf("write stub %s: %v", name, err)
		}
	}
	write("docker", string(fake))
	// The health check is the only thing the compose path needs curl for.
	write("curl", "#!/usr/bin/env bash\nexit 0\n")
	// Shadow sudo so prepare_data_dir cannot hand the sandbox to uid 10001 and
	// leave the test unable to clean up after itself. The installer treats a
	// failed chown as a warning, which is the path a non-root run takes anyway.
	write("sudo", "#!/usr/bin/env bash\nexit 0\n")

	return &installEnv{
		t:     t,
		root:  root,
		bin:   bin,
		state: filepath.Join(root, "docker-state"),
		path:  bin + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
}

// dir makes a working directory to run the installer from.
func (e *installEnv) dir(name string) string {
	e.t.Helper()
	path := filepath.Join(e.root, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		e.t.Fatalf("create %s: %v", path, err)
	}
	return path
}

// run drives the installer from cwd and returns its combined output.
func (e *installEnv) run(cwd string, args ...string) (string, error) {
	e.t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "docs", "install.sh"))
	if err != nil {
		e.t.Fatalf("resolve install.sh: %v", err)
	}
	cmd := exec.Command("bash", append([]string{script, "--yes", "--no-color"}, args...)...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"PATH="+e.path,
		"FAKE_DOCKER_STATE="+e.state,
		"HOME="+e.root,
		"NO_COLOR=1",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// containerState reads one field of what the stand-in recorded.
func (e *installEnv) containerState(key string) string {
	e.t.Helper()
	data, err := os.ReadFile(e.state)
	if err != nil {
		return ""
	}
	value := ""
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, key+"="); ok {
			value = rest
		}
	}
	return value
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// The reported failure, in full: install, create accounts, then re-run the
// one-liner from somewhere else.
func TestComposeReRunFromAnotherDirectoryUpdatesTheInstallThatExists(t *testing.T) {
	env := newInstallEnv(t)

	first := env.dir("first")
	if out, err := env.run(first, "--method", "compose", "--auth", "local", "--port", "8443", "--bind", "0.0.0.0"); err != nil {
		t.Fatalf("first install failed: %v\n%s", err, out)
	}

	stack := filepath.Join(first, "3270web", "docker-compose.yml")
	data := filepath.Join(first, "3270web", "data")
	if err := os.WriteFile(filepath.Join(data, "users.json"), []byte(`[{"username":"admin"}]`), 0o644); err != nil {
		t.Fatalf("seed accounts: %v", err)
	}
	before := env.containerState("DATA")

	// The second run: a different directory, and none of the flags repeated.
	second := env.dir("second")
	out, err := env.run(second, "--method", "compose")
	if err != nil {
		t.Fatalf("re-run failed: %v\n%s", err, out)
	}

	if _, err := os.Stat(filepath.Join(second, "3270web")); err == nil {
		t.Errorf("the re-run wrote a second stack in %s; Compose names its project after that directory, so bringing it up recreates the running container against an empty data folder", second)
	}
	if after := env.containerState("DATA"); after != before {
		t.Errorf("the container's data folder moved from %q to %q; every account is in the first one", before, after)
	}
	if _, err := os.Stat(filepath.Join(data, "users.json")); err != nil {
		t.Errorf("the accounts file did not survive the re-run: %v", err)
	}

	rewritten := readFile(t, stack)
	if !strings.Contains(rewritten, "- AUTH_MODE=local") {
		t.Errorf("the re-run dropped AUTH_MODE=local, taking the sign-in page off an instance that had accounts:\n%s", rewritten)
	}
	if !strings.Contains(rewritten, `- "0.0.0.0:8443:3270"`) {
		t.Errorf("the re-run moved the published port back to the default:\n%s", rewritten)
	}
	if !strings.Contains(out, "an install already running from") {
		t.Errorf("the re-run did not say it had found the existing install:\n%s", out)
	}
}

// A token nobody re-typed is a token every client still holds.
func TestComposeReRunKeepsTheAPIToken(t *testing.T) {
	env := newInstallEnv(t)

	dir := env.dir("stack")
	if out, err := env.run(dir, "--method", "compose", "--api-token", "auto", "--mcp-tools", "full"); err != nil {
		t.Fatalf("first install failed: %v\n%s", err, out)
	}
	envfile := filepath.Join(dir, "3270web", ".env")
	before := readFile(t, envfile)

	if out, err := env.run(dir, "--method", "compose"); err != nil {
		t.Fatalf("re-run failed: %v\n%s", err, out)
	}

	if after := readFile(t, envfile); after != before {
		t.Errorf(".env changed across a re-run; every client holding the old token is now refused\nbefore: %q\nafter:  %q", before, after)
	}
	stack := readFile(t, filepath.Join(dir, "3270web", "docker-compose.yml"))
	if !strings.Contains(stack, "- API_TOKEN=${API_TOKEN}") {
		t.Errorf("the re-run commented API_TOKEN out, taking /api/v1 and MCP off an instance that was serving both:\n%s", stack)
	}
	if !strings.Contains(stack, "- MCP_TOOLS=full") {
		t.Errorf("the re-run reset the MCP tool tier to the default:\n%s", stack)
	}
}

// --dir somewhere else is not a second install; Compose would recreate the one
// that is running. Better to stop than to find out afterwards.
func TestComposeRefusesToRecreateARunningInstanceElsewhere(t *testing.T) {
	env := newInstallEnv(t)

	first := env.dir("first")
	if out, err := env.run(first, "--method", "compose"); err != nil {
		t.Fatalf("first install failed: %v\n%s", err, out)
	}

	other := filepath.Join(env.dir("second"), "3270web")
	out, err := env.run(env.root, "--method", "compose", "--dir", other)
	if err == nil {
		t.Fatalf("the installer wrote a second stack over a running instance:\n%s", out)
	}
	if !strings.Contains(out, "already installed at") {
		t.Errorf("the refusal did not explain itself:\n%s", out)
	}
	if _, err := os.Stat(other); err == nil {
		t.Errorf("%s was created despite the refusal", other)
	}
}

// A stack somebody has edited by hand is still their stack. The installer
// writes the list form of an environment block; the mapping form is what people
// type, and a re-run that could not read it would drop every setting in it.
func TestComposeReRunReadsAHandEditedStack(t *testing.T) {
	env := newInstallEnv(t)

	dir := env.dir("stack")
	project := filepath.Join(dir, "3270web")
	state := filepath.Join(dir, "state")
	for _, d := range []string{project, state} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("create %s: %v", d, err)
		}
	}
	handWritten := "services:\n" +
		"  3270web:\n" +
		"    image: ghcr.io/3270io/3270web:v0.4.0\n" +
		"    container_name: 3270web\n" +
		"    ports:\n" +
		"      - \"0.0.0.0:8080:3270\"\n" +
		"    environment:\n" +
		"      GIN_MODE: release\n" +
		"      AUTH_MODE: local\n" +
		"      MCP_TOOLS: readonly\n" +
		"    volumes:\n" +
		"      - " + state + ":/data\n" +
		"    restart: unless-stopped\n"
	stack := filepath.Join(project, "docker-compose.yml")
	if err := os.WriteFile(stack, []byte(handWritten), 0o644); err != nil {
		t.Fatalf("write stack: %v", err)
	}

	if out, err := env.run(dir, "--method", "compose"); err != nil {
		t.Fatalf("re-run failed: %v\n%s", err, out)
	}

	rewritten := readFile(t, stack)
	for _, want := range []string{
		"- " + state + ":/data",
		"- AUTH_MODE=local",
		`- "0.0.0.0:8080:3270"`,
		"image: ghcr.io/3270io/3270web:v0.4.0",
	} {
		if !strings.Contains(rewritten, want) {
			t.Errorf("the re-run dropped %q from a hand-edited stack:\n%s", want, rewritten)
		}
	}
	if _, err := os.Stat(stack + ".bak"); err != nil {
		t.Errorf("the file the re-run replaced was not kept beside it: %v", err)
	}
}

// The single-container method upgrades by removing the container and running a
// new one, which is the same hazard with none of the compose file to read it
// back off: everything has to come from the container that is about to go.
func TestDockerReRunKeepsTheContainersData(t *testing.T) {
	env := newInstallEnv(t)

	dir := env.dir("work")
	if out, err := env.run(dir, "--method", "docker", "--api-token", "auto", "--port", "8081"); err != nil {
		t.Fatalf("first install failed: %v\n%s", err, out)
	}
	beforeData := env.containerState("DATA")
	beforePorts := env.containerState("PORTS")
	beforeEnv := env.containerState("ENV")
	if beforeData == "" {
		t.Fatal("the first install recorded no data mount")
	}

	if out, err := env.run(dir, "--method", "docker"); err != nil {
		t.Fatalf("re-run failed: %v\n%s", err, out)
	}

	if got := env.containerState("DATA"); got != beforeData {
		t.Errorf("the recreated container reads %q, not the %q the accounts are in", got, beforeData)
	}
	if got := env.containerState("PORTS"); got != beforePorts {
		t.Errorf("the recreated container publishes %q, not %q", got, beforePorts)
	}
	if got := env.containerState("ENV"); got != beforeEnv {
		t.Errorf("the recreated container's environment changed\nbefore: %q\nafter:  %q", beforeEnv, got)
	}
}

// A named volume must be declared external. Compose otherwise prefixes a
// volume name with the project's and mounts a new, empty one — which is the
// same wipe by another route.
func TestComposeAdoptsANamedVolumeAsExternal(t *testing.T) {
	env := newInstallEnv(t)

	seed := "EXISTS=1\nWORKDIR=\nDATA=3270web-data\nPORTS=127.0.0.1:3270\n" +
		"IMAGE=ghcr.io/3270io/3270web:latest\nENV=GIN_MODE=release\n"
	if err := os.WriteFile(env.state, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed container state: %v", err)
	}

	dir := env.dir("work")
	if out, err := env.run(dir, "--method", "compose"); err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}

	stack := readFile(t, filepath.Join(dir, "3270web", "docker-compose.yml"))
	if !strings.Contains(stack, "- 3270web-data:/data") {
		t.Errorf("the stack does not mount the volume the instance already uses:\n%s", stack)
	}
	if !strings.Contains(stack, "external: true") {
		t.Errorf("the volume is not declared external, so Compose would create an empty project-prefixed one:\n%s", stack)
	}
}
