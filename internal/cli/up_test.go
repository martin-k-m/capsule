package cli

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/martin-k-m/capsule/internal/config"
)

// The example the documentation gives for [services], run through the command
// line exactly as a reader would type it. If this stops working, the docs are
// wrong rather than merely out of date.
const servicesExample = `
capsule = ">=0.2"

name  = "demo"
image = "node:18"

[services.db]
image   = "postgres:14"
ready   = "pg_isready -U app"
timeout = "90s"

[services.db.env]
POSTGRES_USER     = "app"
POSTGRES_PASSWORD = "dev"

[services.cache]
image = "redis:7"
`

// project writes a capsule.toml into a temporary directory and moves there, so
// a test drives capsule the way a developer does: from inside a project, with
// nothing passed in but a command line.
//
// PATH is emptied as well. `up --dry-run` is meant to work with no container
// runtime installed, and the only way to prove it is to take it away.
func project(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("PATH", "")
	return dir
}

// capsuleRun runs one capsule command line and returns everything it printed.
func capsuleRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w

	runErr := Run(args)

	os.Stdout, os.Stderr = stdout, stderr
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	return string(out), runErr
}

func TestUpDryRunPlansTheWholeCapsule(t *testing.T) {
	project(t, servicesExample)

	out, err := capsuleRun(t, "up", "--dry-run")
	if err != nil {
		t.Fatalf("capsule up --dry-run: %v\n%s", err, out)
	}

	// Everything belonging to one capsule carries one id, which is what keeps
	// two of them running at once from touching each other.
	id := regexp.MustCompile(`capsule-demo-([0-9a-f]{8})\b`).FindStringSubmatch(out)
	if id == nil {
		t.Fatalf("no capsule container in the plan:\n%s", out)
	}
	network := "capsule-demo-" + id[1] + "-net"

	want := []string{
		"network create",
		network,
		"run -d --rm --name capsule-demo-" + id[1] + "-cache",
		"run -d --rm --name capsule-demo-" + id[1] + "-db",
		"--network-alias db",
		"exec capsule-demo-" + id[1] + "-db /bin/sh -c 'pg_isready -U app'",
		"giving up after 1m30s",
		"--name capsule-demo-" + id[1] + " ",
		"rm -f capsule-demo-" + id[1] + "-cache",
		"rm -f capsule-demo-" + id[1] + "-db",
		"network rm " + network,
	}
	at := -1
	for _, w := range want {
		i := strings.Index(out, w)
		if i < 0 {
			t.Fatalf("the plan never mentions %q:\n%s", w, out)
		}
		if i < at {
			t.Errorf("the plan has %q out of order:\n%s", w, out)
		}
		at = i
	}

	// A service with no `ready` check gets no probe, rather than a probe that
	// pretends to check something.
	if strings.Contains(out, "-cache /bin/sh -c") {
		t.Errorf("cache declares no ready check, so nothing should be polled:\n%s", out)
	}
	// The capsule and its services have to be on the same network, or none of
	// this reaches anything.
	if strings.Count(out, "--network "+network) != 3 {
		t.Errorf("the capsule and both services should share %s:\n%s", network, out)
	}
	if !strings.Contains(out, "no container runtime on PATH") {
		t.Errorf("a dry run without a runtime should say which runtime it is showing:\n%s", out)
	}
}

func TestUpDryRunOfAPlainCapsuleStaysOneLine(t *testing.T) {
	project(t, "image = \"alpine\"\nname = \"demo\"\n")

	out, err := capsuleRun(t, "up", "--dry-run")
	if err != nil {
		t.Fatalf("capsule up --dry-run: %v\n%s", err, out)
	}
	var commands []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(l, "capsule:") {
			continue // capsule's own commentary, not a command
		}
		commands = append(commands, l)
	}
	if len(commands) != 1 {
		t.Errorf("a capsule with no services should print one command, got %d:\n%s", len(commands), out)
	}
	if strings.Contains(out, "network") {
		t.Errorf("a capsule with no services should create no network:\n%s", out)
	}
}

func TestUpNeedsARuntimeWhenItIsGoingToRunSomething(t *testing.T) {
	project(t, servicesExample)

	// Only --dry-run is excused from having a runtime. Anything that would
	// actually start a container still has to find one.
	if _, err := capsuleRun(t, "up"); err == nil {
		t.Fatal("expected capsule up to fail with no container runtime on PATH")
	} else if !strings.Contains(err.Error(), "no container runtime") {
		t.Errorf("error = %q, want it to name the missing runtime", err)
	}
}

func TestUpReportsTheVersionRequirementBeforeTheSections(t *testing.T) {
	// A capsule.toml written for a capsule newer than this one, using a section
	// this build has never heard of. The requirement is the answer; "unknown
	// section" would send its author looking for a typo that is not there.
	project(t, "capsule = \">=999.0\"\nimage = \"alpine\"\n[sidecars.db]\nimage = \"postgres:14\"\n")

	_, err := capsuleRun(t, "up", "--dry-run")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "needs capsule >=999.0") {
		t.Errorf("error = %q, want it to name the version requirement", err)
	}
	if strings.Contains(err.Error(), "unknown section") {
		t.Errorf("error = %q, want the requirement reported instead of the schema complaint", err)
	}
}

func TestUpReportsABrokenServiceThroughTheCommandLine(t *testing.T) {
	project(t, "image = \"alpine\"\n[services.db]\nready = \"pg_isready\"\n")

	_, err := capsuleRun(t, "up", "--dry-run")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "services.db: `image` is required") {
		t.Errorf("error = %q, want it to name the service and what is missing", err)
	}
}
