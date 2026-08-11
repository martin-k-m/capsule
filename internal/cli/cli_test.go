package cli

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/martin-k-m/capsule/internal/version"
)

func TestRunWithNoArgumentsPrintsUsage(t *testing.T) {
	out, err := capsuleRun(t, "help")
	if err != nil {
		t.Fatalf("capsule help: %v", err)
	}
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "capsule") {
		t.Errorf("help output does not look like usage:\n%s", out)
	}
	// Every command should be listed, so the help is a complete map rather than
	// a sample of what capsule can do.
	for _, c := range commands {
		if !strings.Contains(out, c.name) {
			t.Errorf("usage does not list %q:\n%s", c.name, out)
		}
	}
}

func TestRunReportsAnUnknownCommand(t *testing.T) {
	_, err := capsuleRun(t, "wat")
	if err == nil {
		t.Fatal("expected an error for an unknown command")
	}
	if !strings.Contains(err.Error(), `unknown command "wat"`) {
		t.Errorf("error = %q, want it to name the unknown command", err)
	}
}

func TestRunPrintsTheVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-V"} {
		out, err := capsuleRun(t, arg)
		if err != nil {
			t.Fatalf("capsule %s: %v", arg, err)
		}
		if !strings.Contains(out, version.Current) {
			t.Errorf("capsule %s did not print the version %q:\n%s", arg, version.Current, out)
		}
	}
}

func TestFirstLine(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"one line", "one line"},
		{"first\nsecond", "first"},
		{"\nsecond", ""},
		{"", ""},
		{"trailing\n", "trailing"},
	}
	for _, tc := range cases {
		if got := firstLine(tc.in); got != tc.want {
			t.Errorf("firstLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExitOrErrPassesAChildStatusThrough(t *testing.T) {
	// A non-negative code is the container's own command failing, and capsule
	// exits with it so `capsule up -- go test` is usable in a pipeline.
	underlying := errors.New("exit status 2")
	err := exitOrErr(underlying, 2)
	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("exitOrErr(_, 2) = %v, want an *ExitError", err)
	}
	if exit.Code != 2 {
		t.Errorf("ExitError.Code = %d, want 2", exit.Code)
	}
}

func TestExitOrErrKeepsCapsulesOwnFailure(t *testing.T) {
	// A negative code means the runtime itself failed, not the command inside,
	// so the original error is returned unchanged rather than dressed up as a
	// clean exit status.
	underlying := errors.New("docker: daemon not reachable")
	if got := exitOrErr(underlying, -1); got != underlying {
		t.Errorf("exitOrErr(_, -1) = %v, want the original error", got)
	}
}

func TestNewIDIsEightHexDigitsAndChanges(t *testing.T) {
	first, err := newID()
	if err != nil {
		t.Fatalf("newID: %v", err)
	}
	if len(first) != 8 {
		t.Errorf("newID() = %q, want 8 hex digits", first)
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Errorf("newID() = %q is not hex: %v", first, err)
	}
	// The id keeps two capsules of the same project from colliding, so two of
	// them in a row must not be equal. A repeat is astronomically unlikely
	// rather than impossible, so retry once before believing it.
	second, _ := newID()
	if first == second {
		if third, _ := newID(); first == third {
			t.Errorf("newID() returned %q twice, ids must vary", first)
		}
	}
}
