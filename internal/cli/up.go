package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/martin-k-m/capsule/internal/config"
	"github.com/martin-k-m/capsule/internal/runtime"
)

func runUp(args []string) error {
	fs := newFlagSet("up")
	pull := fs.Bool("pull", false, "pull the image before starting")
	dry := fs.Bool("dry-run", false, "print the runtime command instead of running it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cmd := fs.Args() // everything after `--`

	dir, c, err := projectContext()
	if err != nil {
		return err
	}
	rt, err := runtime.Detect()
	if err != nil {
		return err
	}

	id, err := newID()
	if err != nil {
		return err
	}

	// A TTY is only available when capsule itself has one on both ends. Asking
	// for `-it` in CI is the single most common way a container tool fails with
	// an error that has nothing to do with the user's project.
	interactive := len(cmd) == 0 && isTerminal(os.Stdin) && isTerminal(os.Stdout)
	runArgs := runtime.RunArgs(c, dir, id, interactive, cmd)

	if *dry {
		fmt.Println(runtime.Display(rt.Name(), runArgs))
		return nil
	}

	if *pull {
		fmt.Fprintf(os.Stderr, "capsule: pulling %s\n", c.Image)
		if err := rt.Exec("pull", c.Image); err != nil {
			return err
		}
	}

	fmt.Fprintf(os.Stderr, "capsule: %s on %s via %s\n", c.Name, c.Image, rt.Name())
	fmt.Fprintf(os.Stderr, "capsule: %s\n", survivalNote(c))

	if err := rt.Exec(runArgs...); err != nil {
		return exitOrErr(err, runtime.ExitCode(err))
	}

	fmt.Fprintf(os.Stderr, "capsule: %s destroyed\n", c.Name)
	return nil
}

// survivalNote states plainly what will still exist after this capsule exits.
// It is printed on every `capsule up` because the whole value of the tool rests
// on that promise being true and understood.
func survivalNote(c *config.Capsule) string {
	keys := c.PersistKeys()
	if len(keys) == 0 {
		return "nothing outside your project directory survives exit"
	}
	return "on exit only these volumes survive: " + strings.Join(keys, ", ")
}
