package cli

import (
	"fmt"
	"os"

	"github.com/martin-k-m/capsule/internal/runtime"
)

func runShell(args []string) error {
	fs := newFlagSet("shell")
	if err := fs.Parse(args); err != nil {
		return err
	}

	_, c, err := projectContext()
	if err != nil {
		return err
	}
	rt, err := runtime.Detect()
	if err != nil {
		return err
	}

	// Filtered to the capsule itself: `capsule shell` opens a second window onto
	// your environment, never onto the database sitting next to it.
	names, err := rt.Lines(runtime.PsArgs(runtime.PsFilter{Name: c.Name, Role: runtime.RoleCapsule}, "{{.Names}}")...)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("no running capsule named %q, start one with `capsule up`", c.Name)
	}

	target := names[0]
	if len(names) > 1 {
		fmt.Fprintf(os.Stderr, "capsule: %d capsules named %s are running; attaching to %s\n",
			len(names), c.Name, target)
	}

	// A TTY is requested only when capsule has one on both ends, the same rule
	// `up`, `exec` and `run` follow. `shell` used to pass `-it` unconditionally,
	// which made it the one command that could not be driven from a script or a
	// CI step: the runtime refuses with "the input device is not a TTY", an
	// error about the caller's terminal rather than about the capsule.
	tty := isTerminal(os.Stdin) && isTerminal(os.Stdout)
	if err := rt.Exec(shellCommandArgs(target, tty, c.Shell)...); err != nil {
		return exitOrErr(err, runtime.ExitCode(err))
	}
	return nil
}

// shellCommandArgs builds the argv for opening a shell in a running capsule.
//
// Pure, like execCommandArgs, so the flags it produces can be asserted without
// a runtime present. `-i` stays on whether or not there is a terminal, so a
// heredoc piped into `capsule shell` still reaches the shell's stdin.
func shellCommandArgs(target string, tty bool, shell string) []string {
	args := []string{"exec", "-i"}
	if tty {
		args = append(args, "-t")
	}
	return append(args, target, shell)
}
