package runtime

import (
	"path/filepath"
	"strings"

	"github.com/martin-k-m/capsule/internal/config"
)

// Labels capsule stamps on every container it starts. They are the only way it
// finds its own containers again. capsule never keeps a state file, so there is
// nothing to go stale, and `capsule down --all` can always sweep a machine clean.
const (
	Label     = "me.blinkdev.capsule"
	LabelName = "me.blinkdev.capsule.name"
)

// ContainerName names a capsule's container. The id suffix keeps two capsules
// started from the same project from colliding.
func ContainerName(capsuleName, id string) string {
	return "capsule-" + capsuleName + "-" + id
}

// RunArgs builds the argv for `docker run` / `podman run`.
//
// It is a pure function on purpose: the exact flags a capsule.toml turns into
// are the whole security surface of this tool, so they are testable without a
// container runtime anywhere in sight.
func RunArgs(c *config.Capsule, projectDir, id string, interactive bool, cmd []string) []string {
	args := []string{"run", "--rm"}
	if interactive {
		args = append(args, "-it")
	}

	args = append(args,
		"--name", ContainerName(c.Name, id),
		"--hostname", c.Name,
		"--label", Label+"=1",
		"--label", LabelName+"="+c.Name,
		"-v", HostPath(projectDir)+":"+c.Workdir,
		"-w", c.Workdir,
	)

	for _, k := range c.EnvKeys() {
		args = append(args, "-e", k+"="+c.Env[k])
	}
	for _, p := range c.Ports {
		args = append(args, "-p", p)
	}
	for _, v := range c.PersistKeys() {
		args = append(args, "-v", v+":"+c.Persist[v])
	}

	args = append(args, c.Image)
	return append(args, entrypoint(c, cmd)...)
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

// PsArgs lists capsule's own containers, optionally narrowed to one name.
func PsArgs(name, format string) []string {
	args := []string{"ps", "--filter", "label=" + Label + "=1"}
	if name != "" {
		args = append(args, "--filter", "label="+LabelName+"="+name)
	}
	return append(args, "--format", format)
}
