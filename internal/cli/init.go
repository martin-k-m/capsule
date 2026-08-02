package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/martin-k-m/capsule/internal/config"
	"github.com/martin-k-m/capsule/internal/version"
)

// preset is the starting point `capsule init` writes for a recognised project.
//
// The persisted volume in each preset is always a *cache*, never source or
// build output. That is the line capsule draws: re-downloading a dependency set
// is waste, so it is worth keeping; anything a build produced is reproducible,
// so it goes away with the capsule.
type preset struct {
	kind   string
	image  string
	shell  string
	volume string // named volume to persist, empty for none
	mount  string // where that volume mounts inside the container
	note   string // what the volume is, for the generated comment
}

// detectors are checked in order; the first marker file present wins.
var detectors = []struct {
	marker string
	preset preset
}{
	{"go.work", preset{"Go workspace", "golang:1-alpine", "/bin/sh", "gomod", "/go/pkg/mod", "Go module cache"}},
	{"go.mod", preset{"Go", "golang:1-alpine", "/bin/sh", "gomod", "/go/pkg/mod", "Go module cache"}},
	{"Cargo.toml", preset{"Rust", "rust:1-slim", "/bin/bash", "cargoreg", "/usr/local/cargo/registry", "crate registry"}},
	{"pyproject.toml", preset{"Python", "python:3-slim", "/bin/bash", "pipcache", "/root/.cache/pip", "pip cache"}},
	{"requirements.txt", preset{"Python", "python:3-slim", "/bin/bash", "pipcache", "/root/.cache/pip", "pip cache"}},
	{"package.json", preset{"Node", "node:22-alpine", "/bin/sh", "npmcache", "/root/.npm", "npm cache"}},
}

var generic = preset{"generic", "debian:bookworm-slim", "/bin/bash", "", "", ""}

func runInit(args []string) error {
	fs := newFlagSet("init")
	force := fs.Bool("force", false, "overwrite an existing "+config.FileName)
	image := fs.String("image", "", "base image to use instead of the detected one")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, config.FileName)
	if _, err := os.Stat(path); err == nil && !*force {
		return fmt.Errorf("%s already exists, pass --force to overwrite it", config.FileName)
	}

	p := detect(dir)
	if *image != "" {
		p.image = *image
	}

	name := filepath.Base(dir)
	body := render(name, p)

	// Parse what we are about to write. `capsule init` producing a file that
	// `capsule up` then rejects would be a bad first five minutes.
	if _, err := config.Parse(body, name); err != nil {
		return fmt.Errorf("generated %s is invalid (please report this): %w", config.FileName, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}

	fmt.Printf("Wrote %s for a %s project (%s).\n", config.FileName, p.kind, p.image)
	if shadowed := shadowedBy(dir); shadowed != "" {
		fmt.Fprintf(os.Stderr, "capsule: note: this takes over from %s, which is what `capsule up` used here until now\n", shadowed)
	}
	fmt.Println("Run `capsule up` to enter it.")
	return nil
}

// shadowedBy returns the capsule.toml above dir that the one just written in dir
// now hides, or "". capsule finds its config by walking upward, so writing one
// in a subdirectory silently shrinks what `capsule up` mounts. That is worth a
// sentence rather than a surprise.
func shadowedBy(dir string) string {
	parent := filepath.Dir(dir)
	if parent == dir {
		return ""
	}
	found, err := config.Find(parent)
	if err != nil {
		return ""
	}
	return filepath.Join(found, config.FileName)
}

func detect(dir string) preset {
	for _, d := range detectors {
		if _, err := os.Stat(filepath.Join(dir, d.marker)); err == nil {
			return d.preset
		}
	}
	return generic
}

func render(name string, p preset) string {
	var b strings.Builder

	b.WriteString("# capsule.toml: a development environment that disappears when you're done.\n")
	b.WriteString("#\n")
	b.WriteString("# `capsule up` starts a container from this file with the project mounted at\n")
	b.WriteString("# workdir, drops you into a shell, and destroys the container on exit.\n")
	b.WriteString("# Nothing survives except the volumes named under [persist].\n\n")

	b.WriteString("# The oldest capsule that can read this file.\n")
	fmt.Fprintf(&b, "capsule = \">=%s\"\n\n", version.Series())

	fmt.Fprintf(&b, "name    = %q\n", name)
	fmt.Fprintf(&b, "image   = %q\n", p.image)
	fmt.Fprintf(&b, "shell   = %q\n", p.shell)
	fmt.Fprintf(&b, "workdir = %q\n\n", config.DefaultWorkdir)

	b.WriteString("# Ports published to the host, written \"host:container\".\n")
	b.WriteString("# ports = [\"8080:8080\"]\n\n")

	b.WriteString("# Installed on start with whichever package manager the image has\n")
	b.WriteString("# (apk, apt-get or dnf). Skipped with a warning if it has none.\n")
	b.WriteString("# packages = [\"git\", \"make\"]\n\n")

	b.WriteString("# Environment variables set inside the capsule.\n")
	b.WriteString("[env]\n")
	b.WriteString("# EXAMPLE = \"1\"\n\n")

	b.WriteString("# Named volumes that survive teardown, the only state a capsule keeps.\n")
	b.WriteString("[persist]\n")
	if p.volume != "" {
		fmt.Fprintf(&b, "%s = %q  # %s\n", p.volume, p.mount, p.note)
	} else {
		b.WriteString("# cache = \"/root/.cache\"\n")
	}

	return b.String()
}
