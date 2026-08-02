// Package runtime drives the container CLI a developer already has installed.
//
// capsule deliberately shells out to `docker` or `podman` rather than talking to
// a daemon socket itself. It keeps the tool small, it means capsule inherits
// whatever context, credentials and remote the developer has already set up, and
// it makes every action capsule takes something they could have typed by hand.
package runtime

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runtime is a resolved container CLI.
type Runtime struct {
	Bin string
}

// Detect finds a usable container runtime, preferring docker.
func Detect() (*Runtime, error) {
	for _, bin := range []string{"docker", "podman"} {
		if path, err := exec.LookPath(bin); err == nil {
			return &Runtime{Bin: path}, nil
		}
	}
	return nil, errors.New("no container runtime on PATH — install Docker or Podman")
}

// Name is the short name of the runtime binary, for messages.
func (r *Runtime) Name() string {
	return strings.TrimSuffix(filepath.Base(r.Bin), filepath.Ext(r.Bin))
}

// Exec runs the runtime wired to the current terminal.
func (r *Runtime) Exec(args ...string) error {
	cmd := exec.Command(r.Bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Output runs the runtime and captures stdout, folding stderr into the error so
// a failure reports what the runtime actually said.
func (r *Runtime) Output(args ...string) (string, error) {
	var stderr strings.Builder
	cmd := exec.Command(r.Bin, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%s %s: %s", r.Name(), args[0], msg)
		}
		return "", fmt.Errorf("%s %s: %w", r.Name(), args[0], err)
	}
	return string(out), nil
}

// Lines runs the runtime and returns non-empty trimmed stdout lines.
func (r *Runtime) Lines(args ...string) ([]string, error) {
	out, err := r.Output(args...)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// Has reports whether a runtime subcommand succeeded, discarding its output.
// Used for presence checks like `image inspect`, where failure is information
// rather than an error.
func (r *Runtime) Has(args ...string) bool {
	cmd := exec.Command(r.Bin, args...)
	cmd.Stdout, cmd.Stderr = nil, nil
	return cmd.Run() == nil
}

// ExitCode extracts a child process's status, or -1 if err is not an exit error.
func ExitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
