package cli

import (
	"fmt"
	"strings"

	"github.com/martin-k-m/capsule/internal/config"
	"github.com/martin-k-m/capsule/internal/runtime"
	"github.com/martin-k-m/capsule/internal/version"
)

// report accumulates check results. A "note" is something the user should know
// but which is not wrong. capsule never dresses a fine situation up as a
// failure, and never hides a real one behind a reassuring summary.
type report struct{ problems int }

func (r *report) ok(label, detail string)   { fmt.Printf("ok    %-20s %s\n", label, detail) }
func (r *report) note(label, detail string) { fmt.Printf("note  %-20s %s\n", label, detail) }
func (r *report) fail(label, detail string) {
	r.problems++
	fmt.Printf("FAIL  %-20s %s\n", label, detail)
}

func runDoctor(args []string) error {
	fs := newFlagSet("doctor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r := &report{}

	rt, err := runtime.Detect()
	if err != nil {
		r.fail("container runtime", err.Error())
		return r.finish()
	}

	if out, verr := rt.Output("--version"); verr == nil {
		r.ok("container runtime", strings.TrimSpace(firstLine(out)))
	} else {
		r.fail("container runtime", verr.Error())
	}

	if rt.Has("info") {
		r.ok("runtime daemon", "reachable")
	} else {
		r.fail("runtime daemon", "not reachable. Is the "+rt.Name()+" daemon running?")
	}

	dir, c, err := projectContext()
	if err != nil {
		r.fail(config.FileName, err.Error())
		return r.finish()
	}
	r.ok(config.FileName, fmt.Sprintf("%s → %s", c.Name, c.Image))
	// capsule.toml is found by walking upward, so which directory a capsule
	// mounts is worth stating rather than assuming it is the one you are in.
	r.ok("project", fmt.Sprintf("%s → %s", dir, c.Workdir))
	if c.Requirement != "" {
		r.ok("requires", fmt.Sprintf("capsule %s, this is capsule %s", c.Requirement, version.Current))
	}

	if rt.Has("image", "inspect", c.Image) {
		r.ok("image", c.Image+" present locally")
	} else {
		r.note("image", c.Image+" not pulled yet, `capsule up` will fetch it")
	}

	keys := c.PersistKeys()
	if len(keys) == 0 {
		r.ok("persisted state", "none, this capsule keeps nothing")
	}
	for _, v := range keys {
		if rt.Has("volume", "inspect", v) {
			r.ok("volume "+v, "exists, mounts at "+c.Persist[v])
		} else {
			r.note("volume "+v, "will be created at first `capsule up`")
		}
	}

	if running, err := rt.Lines(runtime.PsArgs(c.Name, "{{.Names}}")...); err == nil && len(running) > 0 {
		r.note("running now", strings.Join(running, ", "))
	}

	return r.finish()
}

func (r *report) finish() error {
	fmt.Println()
	if r.problems == 0 {
		fmt.Println("No problems found.")
		return nil
	}
	return fmt.Errorf("%d problem(s) found", r.problems)
}
