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

	names, err := rt.Lines(runtime.PsArgs(c.Name, "{{.Names}}")...)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("no running capsule named %q — start one with `capsule up`", c.Name)
	}

	target := names[0]
	if len(names) > 1 {
		fmt.Fprintf(os.Stderr, "capsule: %d capsules named %s are running; attaching to %s\n",
			len(names), c.Name, target)
	}

	if err := rt.Exec("exec", "-it", target, c.Shell); err != nil {
		return exitOrErr(err, runtime.ExitCode(err))
	}
	return nil
}
