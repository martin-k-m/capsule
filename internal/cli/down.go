package cli

import (
	"fmt"

	"github.com/martin-k-m/capsule/internal/runtime"
)

func runDown(args []string) error {
	fs := newFlagSet("down")
	all := fs.Bool("all", false, "destroy every capsule on this machine, not just this project's")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rt, err := runtime.Detect()
	if err != nil {
		return err
	}

	// Capsules normally destroy themselves on exit; `down` exists for the cases
	// where that did not happen: a closed laptop lid, a killed terminal.
	name := ""
	if !*all {
		_, c, err := projectContext()
		if err != nil {
			return err
		}
		name = c.Name
	}

	names, err := rt.Lines(runtime.PsArgs(name, "{{.Names}}")...)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Println("No capsules to destroy.")
		return nil
	}

	if _, err := rt.Output(append([]string{"rm", "-f"}, names...)...); err != nil {
		return err
	}
	for _, n := range names {
		fmt.Println("destroyed " + n)
	}
	return nil
}
