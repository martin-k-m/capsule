package cli

import (
	"fmt"
	"os"

	"github.com/martin-k-m/capsule/internal/runtime"
)

// runExec runs one command inside a running capsule and exits with its status.
//
// `capsule shell` covers the interactive case and nothing covered the scripted
// one. A CI job, a git hook, a Makefile target or an editor's "run tests" button
// all want to run a single command in the project's environment and read its
// exit code, and the only way to do that was to open an interactive shell and
// type into it. That is not something a script can do.
//
// The exit code is the point. `capsule exec go test ./...` has to fail when the
// tests fail, or every use of it in CI silently passes.
func runExec(args []string) error {
	fs := newFlagSet("exec")
	var noTTY bool
	var service string
	fs.BoolVar(&noTTY, "no-tty", false, "do not request a TTY even when stdin is one")
	fs.StringVar(&service, "service", "", "run in this `[services.NAME]` sidecar instead of the capsule")
	if err := fs.Parse(args); err != nil {
		return err
	}

	command := fs.Args()
	if len(command) == 0 {
		return fmt.Errorf("capsule exec: give a command to run, for example `capsule exec go test ./...`")
	}

	_, c, err := projectContext()
	if err != nil {
		return err
	}
	rt, err := runtime.Detect()
	if err != nil {
		return err
	}

	// A sidecar is addressed by name so `capsule exec --service db psql` reaches
	// the database rather than the capsule that talks to it.
	filter := runtime.PsFilter{Name: c.Name, Role: runtime.RoleCapsule}
	what := fmt.Sprintf("capsule named %q", c.Name)
	if service != "" {
		if !c.HasService(service) {
			return fmt.Errorf(
				"capsule exec: no service %q in capsule.toml; %s",
				service, describeServices(c.ServiceNames()))
		}
		filter = runtime.PsFilter{Name: c.Name, Role: runtime.RoleService, Service: service}
		what = fmt.Sprintf("service %q", service)
	}

	names, err := rt.Lines(runtime.PsArgs(filter, "{{.Names}}")...)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("no running %s, start one with `capsule up`", what)
	}

	target := names[0]
	if len(names) > 1 {
		fmt.Fprintf(os.Stderr, "capsule: %d containers match %s; using %s\n",
			len(names), what, target)
	}

	// Interactive only when there is a terminal on both ends and the caller did
	// not refuse it. A TTY in a pipeline is worse than useless: the runtime
	// turns on line editing and colour, and the caller reading stdout gets
	// escape sequences mixed into what it is parsing.
	//
	// `-i` stays on regardless, so `echo input | capsule exec cat` works.
	execArgs := []string{"exec", "-i"}
	if !noTTY && isTerminal(os.Stdin) && isTerminal(os.Stdout) {
		execArgs = append(execArgs, "-t")
	}

	// `--` before the command, so a command starting with a dash is not read as
	// a flag to the runtime. `capsule exec -- ls -la` and `capsule exec ls -la`
	// both have to reach `ls`.
	execArgs = append(execArgs, target)
	execArgs = append(execArgs, command...)

	if err := rt.Exec(execArgs...); err != nil {
		return exitOrErr(err, runtime.ExitCode(err))
	}
	return nil
}

// describeServices names what is available, so a typo says what to type instead.
func describeServices(names []string) string {
	if len(names) == 0 {
		return "this capsule.toml defines no services"
	}
	out := "known services:"
	for i, n := range names {
		if i > 0 {
			out += ","
		}
		out += " " + n
	}
	return out
}
