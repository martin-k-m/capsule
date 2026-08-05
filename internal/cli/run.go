package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/martin-k-m/capsule/internal/config"
	"github.com/martin-k-m/capsule/internal/runtime"
)

// runRun runs a task declared in capsule.toml inside the running capsule.
//
// `capsule exec` already runs an arbitrary command, so this is not about
// reaching the container. It is about the command being written down. The
// invocation a project actually uses is rarely `go test ./...`; it is that with
// three flags, an environment variable and a tag nobody remembers, and it lives
// in somebody's shell history until it does not. A task puts it in the file the
// environment is already described by, which is also the file a new contributor
// reads first.
//
// A task is a shell command rather than an argv, because that is what people
// paste in: pipes, `&&` and variable expansion all work, and pretending
// otherwise would mean every non-trivial task starts with `sh -c` anyway.
//
// Like exec, this exits with the task's own status, or `capsule run test` in CI
// passes no matter what the tests did.
func runRun(args []string) error {
	fs := newFlagSet("run")
	var service string
	fs.StringVar(&service, "service", "", "run in this `[services.NAME]` sidecar instead of the capsule")
	if err := fs.Parse(args); err != nil {
		return err
	}

	_, c, err := projectContext()
	if err != nil {
		return err
	}

	rest := fs.Args()
	// No task named: list what there is. A tool that answers "which one?" with
	// its usage text is asking a question it already knows the answer to.
	if len(rest) == 0 {
		listTasks(os.Stdout, c)
		return fmt.Errorf("capsule run: name a task")
	}

	name := rest[0]
	command, ok := c.Tasks[name]
	if !ok {
		listTasks(os.Stderr, c)
		return fmt.Errorf("no task %q in %s", name, config.FileName)
	}

	// Extra arguments are appended to the task's command, so a task can be a
	// prefix: `test = "go test"` makes `capsule run test ./internal/...` work.
	//
	// They are quoted rather than pasted in raw. The task text is the project's
	// own and is trusted; what follows it came off this command line, and a
	// filename with a space in it is a great deal more likely than an attack but
	// breaks in exactly the same place.
	script := command
	for _, arg := range rest[1:] {
		script += " " + shellQuote(arg)
	}

	rt, err := runtime.Detect()
	if err != nil {
		return err
	}
	target, err := resolveTarget(rt, c, service)
	if err != nil {
		return err
	}

	// The capsule's own shell, so a task written against `bash` gets bash rather
	// than whatever `sh` happens to be in the image.
	shell := c.Shell
	if service != "" {
		// A sidecar is somebody else's image and need not have the capsule's
		// shell in it. `sh` is the one thing that is reliably there.
		shell = "sh"
	}

	execArgs := []string{"exec", "-i"}
	if isTerminal(os.Stdin) && isTerminal(os.Stdout) {
		execArgs = append(execArgs, "-t")
	}
	execArgs = append(execArgs, target, shell, "-c", script)

	if err := rt.Exec(execArgs...); err != nil {
		return exitOrErr(err, runtime.ExitCode(err))
	}
	return nil
}

// listTasks prints the declared tasks, so a typo says what to type instead.
func listTasks(w *os.File, c *config.Capsule) {
	names := c.TaskNames()
	if len(names) == 0 {
		fmt.Fprintf(w, "this %s declares no tasks; add a [%s] table:\n\n  [%s]\n  test = \"go test ./...\"\n",
			config.FileName, config.TasksSection, config.TasksSection)
		return
	}
	fmt.Fprintln(w, "tasks:")
	for _, name := range names {
		fmt.Fprintf(w, "  %-12s %s\n", name, c.Tasks[name])
	}
}

// shellQuote wraps an argument in single quotes so the shell inside the capsule
// sees it as one word.
//
// Single quotes because inside them a shell interprets nothing at all, which is
// the property wanted here. The only character that cannot appear is the single
// quote itself, and the backslash-quote dance below is how that is spelled: close the
// quoted run, an escaped quote, open a new one.
func shellQuote(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}
