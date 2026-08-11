package runtime

import (
	"errors"
	"strings"
	"testing"
)

func TestNameStripsThePathAndExtension(t *testing.T) {
	// Name is what capsule puts in its messages, so it should read like the
	// command a developer typed, not the resolved absolute path LookPath returns.
	cases := []struct {
		bin  string
		want string
	}{
		{"docker", "docker"},
		{"/usr/bin/docker", "docker"},
		{"/usr/local/bin/podman", "podman"},
		{`C:\Program Files\Docker\docker.exe`, "docker"},
		{"docker.exe", "docker"},
	}
	for _, tc := range cases {
		if got := (&Runtime{Bin: tc.bin}).Name(); got != tc.want {
			t.Errorf("Runtime{%q}.Name() = %q, want %q", tc.bin, got, tc.want)
		}
	}
}

func TestExitCodeOnlyReportsAChildStatus(t *testing.T) {
	// A nil error and a plain error are both "not a child that exited", so they
	// return -1 and exitOrErr keeps treating the failure as capsule's own rather
	// than passing a bogus status through to the caller.
	if got := ExitCode(nil); got != -1 {
		t.Errorf("ExitCode(nil) = %d, want -1", got)
	}
	if got := ExitCode(errors.New("some other failure")); got != -1 {
		t.Errorf("ExitCode(plain error) = %d, want -1", got)
	}
}

func TestDetectFailsWithNoRuntimeOnPath(t *testing.T) {
	// With an empty PATH, neither docker nor podman resolves, and Detect has to
	// say so rather than hand back a Runtime that cannot run anything.
	t.Setenv("PATH", "")
	rt, err := Detect()
	if err == nil {
		t.Fatalf("Detect() = %v, want an error when no runtime is on PATH", rt)
	}
	if !strings.Contains(err.Error(), "no container runtime") {
		t.Errorf("Detect() error = %q, want it to name the missing runtime", err)
	}
}
