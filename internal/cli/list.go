package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/martin-k-m/capsule/internal/runtime"
)

func runList(args []string) error {
	fs := newFlagSet("list")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rt, err := runtime.Detect()
	if err != nil {
		return err
	}

	// Containers are found by label rather than from a state file capsule keeps.
	// There is nothing to drift out of sync with reality, and a capsule left
	// behind by a killed terminal is still findable.
	lines, err := rt.Lines(runtime.PsArgs("", "{{.Names}}\t{{.Image}}\t{{.Status}}")...)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		fmt.Println("No capsules running.")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tIMAGE\tSTATUS")
	for _, l := range lines {
		fmt.Fprintln(tw, l)
	}
	return tw.Flush()
}
