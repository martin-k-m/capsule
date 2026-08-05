package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/martin-k-m/capsule/internal/runtime"
)

// runCp copies a file or directory into or out of a running capsule.
//
// The bind mount covers the project directory and nothing else, which is the
// point: a capsule sees the project, not the home directory it sits in. That
// leaves a gap every so often, when what you want is a file the container made
// somewhere else, a coverage report under /tmp or a binary built into a path
// the mount does not reach. Until now the answer was to find the container name
// and type the runtime command by hand.
//
// The `capsule:` prefix names the capsule side, so the direction is written out
// rather than guessed at:
//
//	capsule cp capsule:/tmp/report.html ./report.html
//	capsule cp ./fixture.json capsule:/workspace/fixture.json
//
// A path with no prefix is a host path. That means a copy with prefixes on both
// sides or neither is refused rather than resolved, because both readings are
// plausible and doing the wrong one writes over a file.
func runCp(args []string) error {
	fs := newFlagSet("cp")
	var service string
	fs.StringVar(&service, "service", "", "use this `[services.NAME]` sidecar as the container side")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rest := fs.Args()
	if len(rest) != 2 {
		return fmt.Errorf("capsule cp: give a source and a destination, for example `capsule cp capsule:/tmp/report.html .`")
	}
	source, destination := rest[0], rest[1]

	// Checked before anything is looked up, so a malformed command says so
	// without first failing to find a running capsule.
	if _, _, err := copyEnds(source, destination, "check"); err != nil {
		return err
	}

	_, c, err := projectContext()
	if err != nil {
		return err
	}
	rt, err := runtime.Detect()
	if err != nil {
		return err
	}
	target, err := resolveTarget(rt, c, service)
	if err != nil {
		return err
	}

	from, to, err := copyEnds(source, destination, target)
	if err != nil {
		return err
	}

	if err := rt.Exec(runtime.CpArgs(from, to)...); err != nil {
		return exitOrErr(err, runtime.ExitCode(err))
	}

	// Say what happened. A copy that prints nothing is indistinguishable from a
	// copy that did nothing, and the whole point of the command is that the file
	// is now somewhere the developer can reach.
	if strings.HasPrefix(source, containerPrefix) {
		fmt.Fprintf(os.Stderr, "capsule: copied %s out of %s\n", strings.TrimPrefix(source, containerPrefix), target)
	} else {
		fmt.Fprintf(os.Stderr, "capsule: copied %s into %s\n", source, target)
	}
	return nil
}

// containerPrefix marks the side of a copy that lives inside the capsule.
//
// The word rather than the container's real name, which has a random suffix
// nobody can be expected to type, and which is what the command exists to look
// up.
const containerPrefix = "capsule:"

// copyEnds turns the two paths a caller typed into the two the runtime wants,
// resolving the `capsule:` prefix against the container's real name.
//
// Split out from the command because this is the whole decision: which side is
// inside, and whether the pair means anything at all. Everything around it needs
// a container runtime and a project on disk, and this needs neither.
func copyEnds(source, destination, container string) (string, string, error) {
	sourceInside := strings.HasPrefix(source, containerPrefix)
	destInside := strings.HasPrefix(destination, containerPrefix)

	// A pair with prefixes on both sides or neither is refused rather than
	// resolved. Both readings are plausible and picking one writes over a file.
	switch {
	case sourceInside && destInside:
		return "", "", fmt.Errorf("both paths name the capsule; one side has to be a host path")
	case !sourceInside && !destInside:
		return "", "", fmt.Errorf(
			"neither path names the capsule; prefix one with %q, as in `capsule cp %s/tmp/report.html .`",
			containerPrefix, containerPrefix)
	case sourceInside:
		inside := strings.TrimPrefix(source, containerPrefix)
		if inside == "" {
			return "", "", fmt.Errorf("%s needs a path after it", containerPrefix)
		}
		return runtime.ContainerPath(container, inside), destination, nil
	default:
		inside := strings.TrimPrefix(destination, containerPrefix)
		if inside == "" {
			return "", "", fmt.Errorf("%s needs a path after it", containerPrefix)
		}
		return source, runtime.ContainerPath(container, inside), nil
	}
}
