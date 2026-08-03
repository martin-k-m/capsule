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

	// A capsule's services carry their capsule's name, so this finds them too:
	// a capsule that outlived its shell may have left a database running beside
	// it, and clearing one without the other would be half a cleanup.
	names, err := rt.Lines(runtime.PsArgs(runtime.PsFilter{Name: name}, "{{.Names}}")...)
	if err != nil {
		return err
	}
	networks, err := rt.Lines(runtime.NetworkLsArgs(name)...)
	if err != nil {
		return err
	}
	if len(names) == 0 && len(networks) == 0 {
		fmt.Println("No capsules to destroy.")
		return nil
	}

	if len(names) > 0 {
		if _, err := rt.Output(runtime.RmArgs(names...)...); err != nil {
			return err
		}
		for _, n := range names {
			fmt.Println("destroyed " + n)
		}
	}

	// Networks last: a runtime will not remove one while a container is still
	// attached, so this only succeeds once the containers above are really gone.
	for _, n := range networks {
		if _, err := rt.Output(runtime.NetworkRmArgs(n)...); err != nil {
			return err
		}
		fmt.Println("removed network " + n)
	}
	return nil
}
