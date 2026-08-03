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
	pull := fs.Bool("pull", false, "pull the images before starting")
	dry := fs.Bool("dry-run", false, "print the runtime commands instead of running them")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cmd := fs.Args() // everything after `--`

	dir, c, err := projectContext()
	if err != nil {
		return err
	}

	// A dry run only prints, so it does not need a runtime to be installed. That
	// is deliberate: reading what a capsule.toml from a repository would do is
	// most useful on a machine you have not yet let it run on.
	rt, err := runtime.Detect()
	if err != nil && !*dry {
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
	o := runtime.RunOptions{
		Capsule:     c,
		ProjectDir:  dir,
		ID:          id,
		Interactive: interactive,
		Cmd:         cmd,
	}

	if *dry {
		return dryRun(rt, o)
	}

	if *pull {
		for _, image := range images(c) {
			fmt.Fprintf(os.Stderr, "capsule: pulling %s\n", image)
			if err := rt.Exec("pull", image); err != nil {
				return err
			}
		}
	}

	fmt.Fprintf(os.Stderr, "capsule: %s on %s via %s\n", c.Name, c.Image, rt.Name())
	// The mounted directory is the one holding capsule.toml, which is not
	// necessarily the one you ran capsule from. Naming it costs a line and saves
	// wondering why a sibling directory is visible inside the capsule.
	fmt.Fprintf(os.Stderr, "capsule: mounting %s at %s\n", dir, c.Workdir)
	fmt.Fprintf(os.Stderr, "capsule: %s\n", survivalNote(c))

	// Everything from here on has to be undone, on every way out of this
	// function. The deferred teardown is the backstop; trap covers the endings
	// that never reach a defer at all.
	st := &stack{rt: rt, c: c, id: id}
	defer st.teardown()
	if len(c.Services) > 0 {
		stop := trap(st)
		defer stop()
	}

	if err := st.start(); err != nil {
		return err
	}

	runErr := rt.Exec(runtime.RunArgs(o)...)
	st.teardown()
	if runErr != nil {
		return exitOrErr(runErr, runtime.ExitCode(runErr))
	}

	fmt.Fprintf(os.Stderr, "capsule: %s destroyed\n", c.Name)
	return nil
}

// dryRun prints the commands capsule would run, in the order it would run them.
//
// rt may be nil, because a machine with no container runtime can still be shown
// what a capsule.toml turns into. It is also how capsule's own tests cover the
// whole sequence, services and teardown included, without Docker present.
func dryRun(rt *runtime.Runtime, o runtime.RunOptions) error {
	bin := "docker"
	if rt != nil {
		bin = rt.Name()
	} else {
		fmt.Fprintf(os.Stderr, "capsule: no container runtime on PATH, showing the commands as %s would run them\n", bin)
	}
	for _, step := range runtime.Plan(o) {
		fmt.Println(runtime.DisplayStep(bin, step))
	}
	return nil
}

// images is every image this capsule needs, the capsule's own first and its
// services in declaration order, so `--pull` refreshes all of them.
func images(c *config.Capsule) []string {
	out := []string{c.Image}
	for _, s := range c.Services {
		out = append(out, s.Image)
	}
	return out
}

// survivalNote states plainly what will still exist after this capsule exits.
// It is printed on every `capsule up` because the whole value of the tool rests
// on that promise being true and understood.
func survivalNote(c *config.Capsule) string {
	keys := c.PersistKeys()
	if len(keys) == 0 {
		return "nothing outside your project directory survives exit"
	}
	// Services are deliberately absent from this list. A service keeps its data
	// in its own container, which goes away with the capsule, and saying so is
	// less important here than not implying a volume covers it.
	return "on exit only these volumes survive: " + strings.Join(keys, ", ")
}
