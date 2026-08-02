// Package cli implements capsule's command-line interface.
package cli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/martin-k-m/capsule/internal/config"
	"github.com/martin-k-m/capsule/internal/version"
)

// ExitError carries a child process's status so capsule can exit with the same
// code the command inside the capsule returned.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exited with status %d", e.Code) }

type command struct {
	name    string
	summary string
	run     func(args []string) error
}

var commands = []command{
	{"init", "write a starter capsule.toml for this project", runInit},
	{"up", "create an ephemeral environment and enter it", runUp},
	{"shell", "open another shell in a running capsule", runShell},
	{"list", "list running capsules", runList},
	{"down", "destroy running capsules", runDown},
	{"doctor", "check the runtime and this project's capsule.toml", runDoctor},
}

// Run dispatches one command line.
func Run(args []string) error {
	if len(args) == 0 {
		usage(os.Stdout)
		return nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		usage(os.Stdout)
		return nil
	case "-V", "--version", "version":
		fmt.Println("capsule " + version.Current)
		return nil
	}

	for _, c := range commands {
		if c.name != args[0] {
			continue
		}
		err := c.run(args[1:])
		if errors.Is(err, flag.ErrHelp) {
			return nil // the flag package already printed the usage
		}
		return err
	}

	usage(os.Stderr)
	return fmt.Errorf("unknown command %q", args[0])
}

func usage(w io.Writer) {
	fmt.Fprint(w, "capsule: lightweight, isolated development environments that disappear when you're done\n\n"+
		"Usage:\n  capsule <command> [flags]\n\nCommands:\n")

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range commands {
		fmt.Fprintf(tw, "  %s\t%s\n", c.name, c.summary)
	}
	_ = tw.Flush()

	fmt.Fprint(w, "\nRun `capsule <command> -h` for a command's flags.\n")
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("capsule "+name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// projectContext finds the nearest capsule.toml and loads it, returning the
// directory that holds it. That directory, not the working directory, is what
// gets bind-mounted, so running capsule from a subdirectory of a project gives
// the whole project rather than a slice of it.
func projectContext() (string, *config.Capsule, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}
	dir, err := config.Find(cwd)
	if err != nil {
		return "", nil, err
	}
	c, err := config.Load(dir)
	if err != nil {
		return "", nil, err
	}
	return dir, c, nil
}

// newID returns the short suffix that keeps two capsules from the same project
// from claiming the same container name.
func newID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("could not generate a capsule id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// isTerminal reports whether f is a terminal, which decides whether capsule can
// ask the runtime for an interactive TTY. Getting this wrong is the difference
// between a usable shell and "the input device is not a TTY" in CI.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// exitOrErr converts a runtime failure into an ExitError when the container's
// own command was what failed, so capsule's exit status mirrors it.
func exitOrErr(err error, code int) error {
	if code >= 0 {
		return &ExitError{Code: code}
	}
	return err
}
