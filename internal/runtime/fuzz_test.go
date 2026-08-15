package runtime

import (
	"strings"
	"testing"

	"github.com/martin-k-m/capsule/internal/config"
)

// seeds are capsule.toml fragments the fuzzer starts from. They are chosen to
// put the mutator near the parts of the document that reach argv: the image,
// the mount paths, the ports, and the service subtables.
var seeds = []string{
	`image = "alpine"`,
	"image = 'alpine'\nname = \"demo\"\nworkdir = \"/src\"\nshell = \"/bin/bash\"",
	"image = \"alpine\"\nports = [\"8080:8080\", \"5432:5432\"]\npackages = [\"git\", \"make\"]",
	"image = \"alpine\"\n[env]\nA = \"1\"\nB = \"hello # world\"",
	"image = \"alpine\"\n[persist]\ngomod = \"/go/pkg/mod\"",
	"image = \"alpine\"\n[tasks]\ntest = \"go test ./...\"",
	"image = \"alpine\"\n[services.db]\nimage = \"postgres:16\"\nready = \"pg_isready\"\ntimeout = \"90s\"\nports = [\"5432:5432\"]\n[services.db.env]\nPOSTGRES_PASSWORD = \"x\"",
	"capsule = \">=0.2\"\nimage = \"alpine\"",
	"image = \"alpine\"\npackages = [\n  \"git\",\n  \"make\",\n]",
	"# comment only",
	"image = \"a\\\"b\"",
	"[services]\nimage = \"x\"",
}

// FuzzParseToArgv drives capsule.toml text through the config reader and, when
// it parses, through every argv builder a `capsule up` uses.
//
// capsule.toml is not always written by the person running capsule: the README's
// own framing is that you use capsule to run someone else's repo. That makes the
// config an input, and these are the properties that have to hold for any input
// the reader accepts, not just the ones a test author thought to write down.
func FuzzParseToArgv(f *testing.F) {
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, src string) {
		c, err := config.Parse(src, "proj")
		if err != nil {
			return // rejecting a document is always an acceptable outcome
		}

		o := RunOptions{Capsule: c, ProjectDir: "/home/m/proj", ID: "abcd1234"}
		args := RunArgs(o)

		// The one promise. A capsule that is not started with --rm is a capsule
		// that can outlive its shell.
		if len(args) < 2 || args[0] != "run" || args[1] != "--rm" {
			t.Fatalf("argv must begin `run --rm`, got %v", args)
		}

		// Nothing a capsule.toml author writes may reach argv in a position the
		// runtime reads as a flag. A runtime stops parsing its own options at the
		// image name, so that is where this check ends too: everything after it
		// is the container's command, where a leading dash is just a word.
		// RunArgs is flags, then the image, then the entrypoint, so the image sits
		// exactly one place before the entrypoint it appended. Computing the
		// boundary beats searching for the image, which a shell of the same name
		// would otherwise shadow.
		image := len(args) - len(entrypoint(c, o.Cmd)) - 1
		if image < 0 || args[image] != c.Image {
			t.Fatalf("image %q is not where argv should hold it: %v", c.Image, args)
		}
		for i := 0; i < image; i++ {
			a := args[i]
			// The argument after a flag that takes one is that flag's value, and a
			// runtime consumes it as such however it starts. Only a token in flag
			// position itself has to be one capsule chose.
			if i > 0 && takesValue[args[i-1]] {
				continue
			}
			if strings.HasPrefix(a, "-") && !knownFlags[a] {
				t.Fatalf("config-derived argument %q reached argv at %d, before the image, where the runtime reads it as a flag: %v", a, i, args)
			}
		}

		// The same input has to produce the same command every time, or a
		// capsule.toml stops describing one capsule.
		if second := RunArgs(o); !equal(args, second) {
			t.Fatalf("RunArgs is not deterministic:\n%v\n%v", args, second)
		}

		// --dry-run claims to print the command capsule would run, so every
		// planned step has to survive rendering without losing an argument.
		for _, step := range Plan(o) {
			if line := DisplayStep(bin, step); strings.TrimSpace(line) == "" {
				t.Fatalf("step rendered to nothing: %v", step.Args)
			}
		}

		// A mount argument is split by the runtime on ":". A workdir or a persist
		// target carrying one turns a bind mount into a mount with options the
		// capsule.toml author chose, silently.
		for i := 0; i < len(args)-1; i++ {
			if args[i] != "-v" {
				continue
			}
			if n := strings.Count(args[i+1], ":"); n > mountColons(args[i+1]) {
				t.Fatalf("mount %q carries more than the src:dst separator", args[i+1])
			}
		}
	})
}

// knownFlags is every dashed argument RunArgs is allowed to emit. It is written
// out rather than derived so that adding a flag to RunArgs is a decision that
// has to be made twice.
var knownFlags = map[string]bool{
	"--rm": true, "-it": true, "--name": true, "--hostname": true,
	"--label": true, "-v": true, "-w": true, "--network": true,
	"-e": true, "-p": true, "-c": true,
}

// takesValue is the subset of knownFlags that consumes the argument after it,
// which is therefore a value rather than a token in flag position.
var takesValue = map[string]bool{
	"--name": true, "--hostname": true, "--label": true,
	"-v": true, "-w": true, "--network": true, "-e": true, "-p": true,
}

const bin = "docker"

// mountColons is how many ":" a mount argument may legitimately contain. A
// Windows host path keeps its drive letter colon, so `C:/x:/workspace` is three
// fields and still only one separator's worth of meaning.
func mountColons(v string) int {
	if len(v) > 1 && v[1] == ':' && isDriveLetter(v[0]) {
		return 2
	}
	return 1
}

func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
