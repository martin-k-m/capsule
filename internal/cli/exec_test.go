package cli

import (
	"strings"
	"testing"
)

func TestExecCommandArgs(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		tty     bool
		command []string
		want    []string
	}{
		{
			name:    "no tty, plain command",
			target:  "capsule-demo-abcd1234",
			command: []string{"go", "test", "./..."},
			want:    []string{"exec", "-i", "capsule-demo-abcd1234", "--", "go", "test", "./..."},
		},
		{
			name:    "tty requested",
			target:  "capsule-demo-abcd1234",
			tty:     true,
			command: []string{"bash"},
			want:    []string{"exec", "-i", "-t", "capsule-demo-abcd1234", "--", "bash"},
		},
		{
			// The whole reason for the `--`: a command whose first word starts
			// with a dash must reach the program, not the runtime's own exec.
			name:    "command starting with a dash",
			target:  "demo",
			command: []string{"--version"},
			want:    []string{"exec", "-i", "demo", "--", "--version"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := execCommandArgs(tc.target, tc.tty, tc.command)
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("execCommandArgs(%q, %v, %v) = %v, want %v",
					tc.target, tc.tty, tc.command, got, tc.want)
			}
		})
	}
}

func TestExecCommandArgsAlwaysKeepsStdinOpen(t *testing.T) {
	// `-i` stays on regardless of the TTY, so `echo x | capsule exec cat` works
	// even in a pipeline where no terminal is present.
	args := execCommandArgs("demo", false, []string{"cat"})
	if args[0] != "exec" || args[1] != "-i" {
		t.Errorf("exec args must start with `exec -i`, got %v", args[:2])
	}
	if strings.Contains(strings.Join(args, " "), " -t ") {
		t.Errorf("a non-interactive exec must not request a TTY: %v", args)
	}
}

func TestDescribeServices(t *testing.T) {
	if got := describeServices(nil); !strings.Contains(got, "no services") {
		t.Errorf("describeServices(nil) = %q, want it to say there are none", got)
	}
	got := describeServices([]string{"db", "cache"})
	if !strings.Contains(got, "db") || !strings.Contains(got, "cache") {
		t.Errorf("describeServices = %q, want it to name each service", got)
	}
	if !strings.Contains(got, "known services:") {
		t.Errorf("describeServices = %q, want it to introduce the list", got)
	}
}
