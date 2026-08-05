package cli

import (
	"fmt"

	"github.com/martin-k-m/capsule/internal/runtime"
)

// Lines of history shown when the caller does not say. Enough to hold a stack
// trace, short enough to read.
const defaultLogTail = 200

// runLogs prints a container's output.
//
// A service that will not start is the commonest way a capsule fails, and until
// now the only way to see why was to wait for `capsule up` to report the
// readiness timeout, which prints a tail once and then throws it away. Between
// runs the output existed and nothing surfaced it.
//
// The runtime already keeps the logs; this is the command that asks for them.
func runLogs(args []string) error {
	fs := newFlagSet("logs")
	var service string
	var follow bool
	var tail int
	fs.StringVar(&service, "service", "", "read this `[services.NAME]` sidecar instead of the capsule")
	fs.BoolVar(&follow, "follow", false, "stream output until interrupted")
	fs.IntVar(&tail, "tail", defaultLogTail, "how many trailing lines to show")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("capsule logs takes no arguments; did you mean `--service %s`?", fs.Arg(0))
	}
	if tail < 0 {
		return fmt.Errorf("--tail must be zero or more, got %d", tail)
	}

	_, c, err := projectContext()
	if err != nil {
		return err
	}
	rt, err := runtime.Detect()
	if err != nil {
		return err
	}

	target, err := resolveTarget(rt, c, service)
	if err != nil {
		return err
	}

	// Streamed rather than captured, so `--follow` shows output as it arrives
	// and Ctrl-C ends it the way it would end the runtime's own command.
	if err := rt.Exec(runtime.LogsArgs(target, tail, follow)...); err != nil {
		return exitOrErr(err, runtime.ExitCode(err))
	}
	return nil
}
