package runtime

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/martin-k-m/capsule/internal/config"
)

// Labels capsule stamps on everything it creates, containers and networks
// alike. They are the only way it finds its own objects again. capsule never
// keeps a state file, so there is nothing to go stale, and `capsule down --all`
// can always sweep a machine clean.
const (
	Label        = "me.blinkdev.capsule"
	LabelName    = "me.blinkdev.capsule.name"
	LabelRole    = "me.blinkdev.capsule.role"
	LabelService = "me.blinkdev.capsule.service"
)

// Roles separate a capsule's own container from the sidecars started beside it.
// `capsule shell` has to attach to the capsule rather than to its database, and
// a runtime offers no way to filter on the absence of a label, so the capsule
// container carries one that says what it is.
const (
	RoleCapsule = "capsule"
	RoleService = "service"
)

// ContainerName names a capsule's container. The id suffix keeps two capsules
// started from the same project from colliding.
func ContainerName(capsuleName, id string) string {
	return "capsule-" + capsuleName + "-" + id
}

// ServiceContainerName names one sidecar of a capsule. It carries the same id
// as the capsule that owns it, so two `capsule up` runs of the same project
// have separate databases rather than a name collision.
func ServiceContainerName(capsuleName, id, service string) string {
	return ContainerName(capsuleName, id) + "-" + service
}

// NetworkName names the network a capsule and its services share. One network
// per capsule instance, so services are reachable by name from their own
// capsule and from nowhere else.
func NetworkName(capsuleName, id string) string {
	return ContainerName(capsuleName, id) + "-net"
}

// RunOptions is everything that decides a capsule's argv.
//
// It is a struct rather than a parameter list because these are the inputs to
// the tool's entire security surface, and a call reading
// RunArgs(c, dir, id, false, nil) does not say which of those the reader should
// be checking.
type RunOptions struct {
	// Capsule is the parsed capsule.toml.
	Capsule *config.Capsule

	// ProjectDir is the host directory bind-mounted at the capsule's workdir. It
	// is the directory holding capsule.toml, which is not necessarily the one the
	// developer ran capsule from.
	ProjectDir string

	// ID is the random suffix that keeps two capsules from the same project from
	// claiming one container name.
	ID string

	// Interactive requests a TTY. Only true when capsule has one on both ends.
	Interactive bool

	// Cmd is the command to run instead of dropping into a shell.
	Cmd []string
}

// RunArgs builds the argv for `docker run` / `podman run`.
//
// It is a pure function on purpose: the exact flags a capsule.toml turns into
// are the whole security surface of this tool, so they are testable without a
// container runtime anywhere in sight.
func RunArgs(o RunOptions) []string {
	c := o.Capsule

	args := []string{"run", "--rm"}
	if o.Interactive {
		args = append(args, "-it")
	}

	args = append(args,
		"--name", ContainerName(c.Name, o.ID),
		"--hostname", c.Name,
		"--label", Label+"=1",
		"--label", LabelName+"="+c.Name,
		"--label", LabelRole+"="+RoleCapsule,
		"-v", HostPath(o.ProjectDir)+":"+c.Workdir,
		"-w", c.Workdir,
	)

	// The network is derived here rather than passed in, so the capsule cannot
	// be joined to a different network from the one its services are on.
	if len(c.Services) > 0 {
		args = append(args, "--network", NetworkName(c.Name, o.ID))
	}

	for _, k := range c.EnvKeys() {
		args = append(args, "-e", k+"="+c.Env[k])
	}
	for _, p := range c.Ports {
		args = append(args, "-p", p)
	}
	for _, v := range c.PersistKeys() {
		args = append(args, "-v", v+":"+c.Persist[v])
	}

	// c.Image is validated not to start with "-", so it cannot turn this position
	// into a flag the capsule.toml author chose.
	args = append(args, c.Image)
	return append(args, entrypoint(c, o.Cmd)...)
}

// NetworkCreateArgs builds the private network a capsule shares with its
// services. It is labelled like a container so `capsule down` can sweep it up
// after a capsule that did not get to tear itself down.
func NetworkCreateArgs(c *config.Capsule, id string) []string {
	return []string{
		"network", "create",
		"--label", Label + "=1",
		"--label", LabelName + "=" + c.Name,
		NetworkName(c.Name, id),
	}
}

// NetworkRmArgs removes networks by name.
func NetworkRmArgs(names ...string) []string {
	return append([]string{"network", "rm"}, names...)
}

// NetworkLsArgs lists capsule's own networks, optionally narrowed to one
// capsule name, the same way PsArgs narrows containers.
func NetworkLsArgs(name string) []string {
	args := []string{"network", "ls", "--filter", "label=" + Label + "=1"}
	if name != "" {
		args = append(args, "--filter", "label="+LabelName+"="+name)
	}
	return append(args, "--format", "{{.Name}}")
}

// ServiceRunArgs builds the argv for one sidecar.
//
// It is detached, because the capsule's shell is what the developer is here
// for, and `--rm` like everything else capsule starts. The service answers on
// the capsule network under its bare name, so a `db` service is reachable at
// `db` from inside the capsule.
func ServiceRunArgs(c *config.Capsule, s config.Service, id string) []string {
	args := []string{
		"run", "-d", "--rm",
		"--name", ServiceContainerName(c.Name, id, s.Name),
		"--hostname", s.Name,
		"--network", NetworkName(c.Name, id),
		"--network-alias", s.Name,
		"--label", Label + "=1",
		"--label", LabelName + "=" + c.Name,
		"--label", LabelRole + "=" + RoleService,
		"--label", LabelService + "=" + s.Name,
	}
	args = append(args, envAndPorts(s)...)

	// s.Image is validated not to start with "-", exactly like the capsule's own
	// image, so it cannot turn this position into a flag a capsule.toml chose.
	// Nothing from the project is mounted: a service is a dependency, not a
	// second door onto the source.
	return append(args, s.Image)
}

// ServiceDebugArgs is the same service reduced to what a developer would type
// to watch it start by hand. capsule prints it when a service died before it
// was ready and took its logs with it.
func ServiceDebugArgs(s config.Service) []string {
	args := []string{"run", "--rm"}
	args = append(args, envAndPorts(s)...)
	return append(args, s.Image)
}

func envAndPorts(s config.Service) []string {
	var args []string
	for _, k := range s.EnvKeys() {
		args = append(args, "-e", k+"="+s.Env[k])
	}
	for _, p := range s.Ports {
		args = append(args, "-p", p)
	}
	return args
}

// ReadyArgs runs a service's readiness check inside its own container.
//
// The check is a shell command rather than a port test on purpose: a database
// with an open port is not the same thing as a database accepting connections,
// and only something running next to it can tell the two apart.
func ReadyArgs(container, probe string) []string {
	return []string{"exec", container, config.DefaultShell, "-c", probe}
}

// StateArgs asks the runtime for a container's status word, so the wait for a
// service can tell "still starting" from "died two seconds ago".
func StateArgs(container string) []string {
	return []string{"inspect", "--format", "{{.State.Status}}", container}
}

// LogsArgs reads the tail of a container's output, for explaining a service
// that never became ready.
func LogsArgs(container string, lines int) []string {
	return []string{"logs", "--tail", strconv.Itoa(lines), container}
}

// RmArgs force-removes containers by name.
func RmArgs(names ...string) []string {
	return append([]string{"rm", "-f"}, names...)
}

// HostPath normalises a host path for a bind mount. Docker accepts forward
// slashes on Windows and backslashes confuse its `src:dst` splitting, so the
// separator is normalised rather than passed through.
func HostPath(dir string) string {
	return filepath.ToSlash(dir)
}

// entrypoint decides what actually runs as PID 1 in the capsule.
func entrypoint(c *config.Capsule, cmd []string) []string {
	// The common case, an interactive capsule with nothing to install, gets a
	// plain shell rather than a shell wrapped around a script, so job control and
	// the prompt behave exactly as they would outside the capsule.
	if len(c.Packages) == 0 && len(cmd) == 0 {
		return []string{c.Shell}
	}
	return []string{c.Shell, "-c", bootstrapScript(c, cmd)}
}

// bootstrapScript installs any declared packages and then execs the real work,
// so the shell does not linger as a parent process.
//
// It is deliberately a single line. `capsule up --dry-run` prints the command it
// would run, and a multi-line argument makes that output something you have to
// reassemble by hand rather than paste.
func bootstrapScript(c *config.Capsule, cmd []string) string {
	steps := []string{"set -e"}

	if len(c.Packages) > 0 {
		steps = append(steps, installSnippet(shellJoin(c.Packages)))
	}

	if len(cmd) > 0 {
		steps = append(steps, "exec "+shellJoin(cmd))
	} else {
		steps = append(steps, "exec "+shellQuote(c.Shell))
	}
	return strings.Join(steps, "; ")
}

// installSnippet installs packages with whichever package manager the base image
// actually has. If it recognises none, it says so and carries on rather than
// failing the capsule or pretending the packages are present.
func installSnippet(pkgs string) string {
	return "if command -v apk >/dev/null 2>&1; then apk add --no-cache " + pkgs +
		"; elif command -v apt-get >/dev/null 2>&1; then apt-get update -qq && apt-get install -y -qq --no-install-recommends " + pkgs +
		"; elif command -v dnf >/dev/null 2>&1; then dnf install -y -q " + pkgs +
		// The package names are already shell-quoted words, so they are passed to
		// echo as further arguments rather than pasted inside its string.
		// Nesting them in the quoted message would break the quoting.
		"; else echo 'capsule: no apk/apt-get/dnf in this image; skipping packages:' " + pkgs + " >&2; fi"
}

// shellQuote wraps s in single quotes so the container shell treats it as one
// literal word, whatever it contains.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Step is one runtime command in a capsule's lifecycle, with the note
// `capsule up --dry-run` prints beside it.
type Step struct {
	Args    []string
	Comment string
}

// Plan is every runtime command one `capsule up` runs, in the order it runs
// them: create the capsule's network, start each service, wait for each to be
// ready, start the capsule, then take all of it down again.
//
// `capsule up --dry-run` prints this. It is a truthful account rather than a
// description of one because the commands come from the same builders that the
// real run uses, so a plan that has drifted from what capsule does is a plan
// that no longer compiles.
func Plan(o RunOptions) []Step {
	c := o.Capsule
	if len(c.Services) == 0 {
		return []Step{{Args: RunArgs(o)}}
	}

	network := NetworkName(c.Name, o.ID)
	steps := []Step{{
		Args:    NetworkCreateArgs(c, o.ID),
		Comment: "a network for this capsule alone",
	}}

	// Every service is started before any of them is waited on, so two slow
	// images initialise at the same time instead of one after the other.
	for _, s := range c.Services {
		steps = append(steps, Step{Args: ServiceRunArgs(c, s, o.ID)})
	}
	for _, s := range c.Services {
		container := ServiceContainerName(c.Name, o.ID, s.Name)
		if s.Ready == "" {
			continue
		}
		steps = append(steps, Step{
			Args:    ReadyArgs(container, s.Ready),
			Comment: "polled until it succeeds, giving up after " + s.Timeout.String(),
		})
	}

	steps = append(steps, Step{Args: RunArgs(o)})

	for i, s := range c.Services {
		step := Step{Args: RmArgs(ServiceContainerName(c.Name, o.ID, s.Name))}
		if i == 0 {
			step.Comment = "however the capsule ended"
		}
		steps = append(steps, step)
	}
	return append(steps, Step{Args: NetworkRmArgs(network)})
}

// DisplayStep renders one planned command and its note as a shell line. The
// note is a `#` comment so the whole plan stays something you can paste.
func DisplayStep(bin string, s Step) string {
	line := Display(bin, s.Args)
	if s.Comment == "" {
		return line
	}
	return line + "   # " + s.Comment
}

// Display renders a runtime invocation as a single copy-pasteable command line.
// `capsule up --dry-run` claims to print the command it would run, so the output
// has to survive being pasted into a shell, and the bootstrap script argument spans
// several lines and would otherwise fall apart.
func Display(bin string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, bin)
	for _, a := range args {
		if needsQuoting(a) {
			a = shellQuote(a)
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	return strings.ContainsAny(s, " \t\n'\"\\$`&|;<>()*?[]#~!")
}

func shellJoin(parts []string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = shellQuote(p)
	}
	return strings.Join(out, " ")
}

// PsFilter narrows a `ps` query to some of capsule's own containers. An empty
// field matches everything, so a zero PsFilter is every container capsule
// started on this machine.
type PsFilter struct {
	// Name is a capsule name. A capsule's services carry their capsule's name
	// too, so filtering by it finds a capsule and everything started for it.
	Name string

	// Role is RoleCapsule or RoleService.
	Role string
}

// PsArgs lists capsule's own containers, narrowed by f.
func PsArgs(f PsFilter, format string) []string {
	args := []string{"ps", "--filter", "label=" + Label + "=1"}
	if f.Name != "" {
		args = append(args, "--filter", "label="+LabelName+"="+f.Name)
	}
	if f.Role != "" {
		args = append(args, "--filter", "label="+LabelRole+"="+f.Role)
	}
	return append(args, "--format", format)
}
