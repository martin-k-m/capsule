package runtime

import (
	"strings"
	"testing"

	"github.com/martin-k-m/capsule/internal/config"
)

func parse(t *testing.T, src string) *config.Capsule {
	t.Helper()
	c, err := config.Parse(src, "proj")
	if err != nil {
		t.Fatalf("config.Parse: %v", err)
	}
	return c
}

func joined(args []string) string { return strings.Join(args, " ") }

func TestRunArgsIsAlwaysEphemeral(t *testing.T) {
	c := parse(t, `image = "alpine"`)
	args := RunArgs(c, "/home/m/proj", "abcd1234", true, nil)

	// --rm is the entire promise of this tool. If it ever stops being emitted,
	// capsules stop disappearing.
	if args[0] != "run" || args[1] != "--rm" {
		t.Fatalf("run args must start with `run --rm`, got %v", args[:2])
	}
	if !strings.Contains(joined(args), "--label "+Label+"=1") {
		t.Error("every capsule container must carry the capsule label")
	}
	if !strings.Contains(joined(args), "-it") {
		t.Error("an interactive capsule should request a TTY")
	}
}

func TestRunArgsNonInteractive(t *testing.T) {
	c := parse(t, `image = "alpine"`)
	args := RunArgs(c, "/home/m/proj", "id", false, []string{"go", "test", "./..."})
	if strings.Contains(joined(args), " -it ") {
		t.Error("a non-interactive capsule must not ask for a TTY")
	}
	// The command must reach the container through the shell, quoted.
	if !strings.Contains(joined(args), "exec 'go' 'test' './...'") {
		t.Errorf("command not passed through correctly: %v", args)
	}
}

func TestRunArgsFullCapsule(t *testing.T) {
	c := parse(t, `
image    = "golang:1-alpine"
workdir  = "/src"
ports    = ["8080:8080"]
packages = ["git"]

[env]
B = "2"
A = "1"

[persist]
gomod = "/go/pkg/mod"
`)
	got := joined(RunArgs(c, "/home/m/proj", "id", false, nil))

	for _, want := range []string{
		"-v /home/m/proj:/src",
		"-w /src",
		"-e A=1 -e B=2", // sorted, not map order
		"-p 8080:8080",
		"-v gomod:/go/pkg/mod",
		"golang:1-alpine",
		"apk add --no-cache 'git'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRunArgsIsDeterministic(t *testing.T) {
	c := parse(t, `
image = "alpine"
[env]
Z = "1"
A = "2"
M = "3"
[persist]
z = "/z"
a = "/a"
`)
	first := joined(RunArgs(c, "/p", "id", false, nil))
	for i := 0; i < 50; i++ {
		if got := joined(RunArgs(c, "/p", "id", false, nil)); got != first {
			t.Fatalf("run args differ between calls:\n%s\n%s", first, got)
		}
	}
}

func TestHostPathNormalisesWindowsSeparators(t *testing.T) {
	// Backslashes confuse the runtime's src:dst splitting.
	if got := HostPath(`C:\Users\m\proj`); strings.Contains(got, `\`) {
		t.Errorf("HostPath kept backslashes: %q", got)
	}
}

func TestShellQuoteEscapesQuotes(t *testing.T) {
	if got := shellQuote("don't"); got != `'don'\''t'` {
		t.Errorf("shellQuote(%q) = %q", "don't", got)
	}
}

func TestPlainShellWhenNothingToBootstrap(t *testing.T) {
	c := parse(t, `image = "alpine"`)
	args := RunArgs(c, "/p", "id", true, nil)
	last := args[len(args)-1]
	if last != config.DefaultShell {
		t.Errorf("a capsule with no packages and no command should exec a plain shell, got %q", last)
	}
}

func TestInstallFallbackMessageKeepsQuotingIntact(t *testing.T) {
	c := parse(t, `
image    = "alpine"
packages = ["git", "make"]
`)
	got := joined(RunArgs(c, "/p", "id", false, nil))

	// The package names must reach echo as separate quoted arguments. Pasting
	// them inside the quoted message would terminate it early.
	if !strings.Contains(got, `skipping packages:' 'git' 'make' >&2`) {
		t.Errorf("fallback message has broken quoting:\n%s", got)
	}
}

func TestDisplayIsCopyPasteable(t *testing.T) {
	c := parse(t, `
image    = "alpine"
packages = ["git"]
`)
	line := Display("docker", RunArgs(c, "/home/m/proj", "id", false, nil))

	if strings.Contains(line, "\n") {
		t.Error("a dry-run command line must stay on one line")
	}
	// Plain arguments should not be needlessly quoted, or the output stops
	// looking like something a person would type.
	if !strings.Contains(line, "docker run --rm --name capsule-proj-id") {
		t.Errorf("simple arguments were quoted unnecessarily:\n%s", line)
	}
}

func TestPsArgsFiltersByLabel(t *testing.T) {
	all := joined(PsArgs("", "{{.Names}}"))
	if !strings.Contains(all, "label="+Label+"=1") {
		t.Errorf("PsArgs must filter to capsule's own containers: %s", all)
	}
	if strings.Contains(all, LabelName) {
		t.Error("PsArgs with no name should not filter by name")
	}
	one := joined(PsArgs("demo", "{{.Names}}"))
	if !strings.Contains(one, "label="+LabelName+"=demo") {
		t.Errorf("PsArgs should narrow to the named capsule: %s", one)
	}
}
