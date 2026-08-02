// Package config reads capsule.toml, the per-project description of a capsule.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// FileName is the per-project capsule definition capsule looks for.
const FileName = "capsule.toml"

// Defaults applied when capsule.toml leaves a field out.
const (
	DefaultShell   = "/bin/sh"
	DefaultWorkdir = "/workspace"
)

// Capsule is a validated capsule.toml.
//
// Everything here describes a container that is thrown away on exit. The one
// exception is Persist, which is why it has to be declared explicitly rather
// than inferred: a capsule keeps nothing you did not ask it to keep.
type Capsule struct {
	Name     string
	Image    string
	Shell    string
	Workdir  string
	Ports    []string
	Packages []string

	// Env is passed into the container as -e KEY=VALUE.
	Env map[string]string

	// Persist maps a named volume to an absolute path in the container. These
	// survive teardown; nothing else does.
	Persist map[string]string
}

var (
	// nameRE matches what a container runtime accepts as a name component.
	nameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

	rootKeys = map[string]bool{
		"name": true, "image": true, "shell": true,
		"workdir": true, "ports": true, "packages": true,
	}
	listKeys = map[string]bool{"ports": true, "packages": true}
)

// Load reads and validates capsule.toml from dir.
func Load(dir string) (*Capsule, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no %s here — run `capsule init` to create one", FileName)
		}
		return nil, err
	}
	c, err := Parse(string(data), filepath.Base(dir))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return c, nil
}

// Parse turns capsule.toml text into a validated Capsule. defaultName is used
// when the file omits `name`; callers normally pass the project directory name.
func Parse(src, defaultName string) (*Capsule, error) {
	doc, err := parseTOML(src)
	if err != nil {
		return nil, err
	}

	for _, section := range sortedKeys(doc) {
		switch section {
		case "", "env", "persist":
		default:
			return nil, fmt.Errorf("unknown section [%s]", section)
		}
	}

	c := &Capsule{
		Name:    defaultName,
		Shell:   DefaultShell,
		Workdir: DefaultWorkdir,
		Env:     map[string]string{},
		Persist: map[string]string{},
	}

	root := doc[""]
	for _, key := range sortedKeys(root) {
		v := root[key]
		if !rootKeys[key] {
			return nil, fmt.Errorf("unknown key %q", key)
		}
		if v.isList != listKeys[key] {
			if listKeys[key] {
				return nil, fmt.Errorf("%q must be an array", key)
			}
			return nil, fmt.Errorf("%q must be a string", key)
		}
		switch key {
		case "name":
			c.Name = v.str
		case "image":
			c.Image = v.str
		case "shell":
			c.Shell = v.str
		case "workdir":
			c.Workdir = v.str
		case "ports":
			c.Ports = v.list
		case "packages":
			c.Packages = v.list
		}
	}

	for _, key := range sortedKeys(doc["env"]) {
		v := doc["env"][key]
		if v.isList {
			return nil, fmt.Errorf("env.%s must be a string", key)
		}
		c.Env[key] = v.str
	}

	for _, key := range sortedKeys(doc["persist"]) {
		v := doc["persist"][key]
		if v.isList {
			return nil, fmt.Errorf("persist.%s must be a string", key)
		}
		c.Persist[key] = v.str
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Capsule) validate() error {
	if strings.TrimSpace(c.Image) == "" {
		return fmt.Errorf("`image` is required — set it to the base image the capsule runs")
	}
	if !nameRE.MatchString(c.Name) {
		return fmt.Errorf("invalid name %q: use letters, digits, dot, dash or underscore", c.Name)
	}
	if !strings.HasPrefix(c.Workdir, "/") {
		return fmt.Errorf("workdir %q must be an absolute path inside the container", c.Workdir)
	}
	if strings.TrimSpace(c.Shell) == "" {
		return fmt.Errorf("`shell` cannot be empty")
	}
	for _, p := range c.Ports {
		if err := validatePort(p); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(c.Persist) {
		if !nameRE.MatchString(name) {
			return fmt.Errorf("invalid persist volume name %q", name)
		}
		if target := c.Persist[name]; !strings.HasPrefix(target, "/") {
			return fmt.Errorf("persist.%s must mount an absolute path, got %q", name, target)
		}
	}
	for _, k := range sortedKeys(c.Env) {
		if k == "" || strings.ContainsAny(k, "= \t") {
			return fmt.Errorf("invalid env key %q", k)
		}
	}
	return nil
}

func validatePort(p string) error {
	host, container, ok := strings.Cut(p, ":")
	if !ok {
		return fmt.Errorf("port %q must be written \"host:container\"", p)
	}
	for _, part := range []string{host, container} {
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("port %q must be \"host:container\" with numbers in 1-65535", p)
		}
	}
	return nil
}

// EnvKeys and PersistKeys expose the sorted key order used everywhere capsule
// turns a capsule into runtime flags, so callers stay deterministic too.
func (c *Capsule) EnvKeys() []string     { return sortedKeys(c.Env) }
func (c *Capsule) PersistKeys() []string { return sortedKeys(c.Persist) }
