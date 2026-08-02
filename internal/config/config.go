// Package config reads capsule.toml, the per-project description of a capsule.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/martin-k-m/capsule/internal/version"
)

// FileName is the per-project capsule definition capsule looks for.
const FileName = "capsule.toml"

// RequirementKey names the oldest capsule that can read a file, written
// `capsule = ">=0.2"`. It is checked before anything else in the document so a
// file from a newer capsule says so, instead of failing on whichever key this
// build happens not to know yet.
const RequirementKey = "capsule"

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

	// Requirement is the file's `capsule` key, empty when it declares none. It
	// has already been checked against this build by the time a caller sees it.
	Requirement string

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
		RequirementKey: true,
	}
	listKeys = map[string]bool{"ports": true, "packages": true}
)

// Find returns the directory holding the nearest capsule.toml, starting at dir
// and walking upward.
//
// A project is described by the directory its capsule.toml sits in, not by the
// directory you happen to be standing in, so `capsule up` from three levels down
// mounts the project rather than a fragment of it.
func Find(dir string) (string, error) {
	d, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(d, FileName)); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("no %s in %s or any parent directory, run `capsule init` to create one", FileName, dir)
		}
		d = parent
	}
}

// Load reads and validates capsule.toml from dir.
func Load(dir string) (*Capsule, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no %s here, run `capsule init` to create one", FileName)
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
		// A file written for a newer capsule may use syntax this reader does not
		// know at all, and "line 12: expected `key = value`" is not something its
		// author can act on when the real answer is that this capsule is too old.
		if verr := requirementFromText(src); verr != nil {
			return nil, verr
		}
		return nil, err
	}

	// Before the sections and before the keys: everything below this line
	// assumes the schema of *this* capsule, and saying "unknown key" about a key
	// a newer capsule understands perfectly well sends the reader hunting for a
	// typo that is not there.
	requirement, err := checkRequirement(doc[""])
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
		Name:        defaultName,
		Shell:       DefaultShell,
		Workdir:     DefaultWorkdir,
		Requirement: requirement,
		Env:         map[string]string{},
		Persist:     map[string]string{},
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
		case RequirementKey:
			// Already checked, ahead of every other key in the file.
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

// checkRequirement reads the `capsule` key out of a root table and reports
// whether this build satisfies it.
func checkRequirement(root table) (string, error) {
	v, ok := root[RequirementKey]
	if !ok {
		return "", nil
	}
	if v.isList {
		return "", fmt.Errorf("%q must be a string", RequirementKey)
	}
	if err := satisfies(v.str); err != nil {
		return "", err
	}
	return v.str, nil
}

// satisfies compares a `capsule` requirement against the running build.
//
// Only ">=" is accepted. A file states the oldest capsule that can read it,
// which is a fact about the file; pinning an exact version or excluding a range
// would instead lock a config away from capsules perfectly able to read it.
func satisfies(spec string) error {
	spec = strings.TrimSpace(spec)
	rest, ok := strings.CutPrefix(spec, ">=")
	if !ok {
		return fmt.Errorf("%s = %q must be written \">=MAJOR.MINOR\", for example %q",
			RequirementKey, spec, ">="+version.Series())
	}
	want, err := version.Parse(rest)
	if err != nil {
		return fmt.Errorf("%s = %q: %w", RequirementKey, spec, err)
	}
	have, err := version.Parse(version.Current)
	if err != nil {
		// A build stamped with something unreadable is this binary's problem, not
		// the config author's. Refusing every capsule.toml over it would be worse
		// than trusting the file.
		return nil
	}
	if have.Less(want) {
		return fmt.Errorf("needs capsule %s, but this is capsule %s; upgrade from https://github.com/martin-k-m/capsule/releases",
			spec, version.Current)
	}
	return nil
}

// requirementFromText finds a `capsule` requirement in raw capsule.toml text,
// for the case where the document did not parse at all. It returns an error only
// when the file both declares a requirement and this build fails it, so a plain
// syntax error still reports as a syntax error.
func requirementFromText(src string) error {
	for _, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(stripComment(line))
		if strings.HasPrefix(line, "[") {
			break // a root key precedes every table, so there is nothing left to find
		}
		key, raw, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != RequirementKey {
			continue
		}
		spec, err := parseScalar(raw)
		if err != nil {
			return nil
		}
		return satisfies(spec)
	}
	return nil
}

func (c *Capsule) validate() error {
	image := strings.TrimSpace(c.Image)
	if image == "" {
		return fmt.Errorf("`image` is required: set it to the base image the capsule runs")
	}
	// image is the one free-form config string that reaches the runtime in flag
	// position, so a leading dash there is a flag the capsule.toml author gets to
	// choose. Nothing else in this file lands anywhere a dash would be read as
	// one, which is why this check has no counterpart for the other keys.
	if strings.HasPrefix(image, "-") {
		return fmt.Errorf("image %q must not start with \"-\": it is passed to the container runtime as an argument, where a leading dash would be read as a flag", c.Image)
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
