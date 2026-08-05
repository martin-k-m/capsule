// Package config reads capsule.toml, the per-project description of a capsule.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

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

	// DefaultReadyTimeout bounds the wait for one service's `ready` check. It is
	// long enough for a database to initialise a fresh data directory on a cold
	// machine, and short enough that a service which is never coming up says so
	// while the developer is still watching.
	DefaultReadyTimeout = 60 * time.Second
)

// ServicesSection is the table holding the sidecars a capsule needs, written
// one service per subtable: [services.db].
const ServicesSection = "services"

// TasksSection is the table holding a project's named commands, written as
// `[tasks]` with one `name = "command"` per line.
const TasksSection = "tasks"

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

	// Tasks are the project's named commands, keyed by name. `capsule run test`
	// looks one up here and runs it inside the capsule.
	//
	// A map rather than a slice because, unlike a service, a task never reaches
	// argv alongside its siblings: exactly one is looked up and run. Anything
	// that lists them sorts them, which is what TaskNames is for.
	Tasks map[string]string

	// Services are the sidecars started alongside the capsule, sorted by name.
	// A slice rather than a map because everything capsule does with them, from
	// argv to messages, has to come out in the same order every time.
	Services []Service
}

// Service is one sidecar container, declared as [services.db].
//
// It is started on the capsule's own network under its bare name, so the
// capsule reaches it at that hostname: a service named `db` answers on `db`.
// A service is a dependency, not a second view of the project, so nothing from
// the host is mounted into one.
type Service struct {
	// Name is the subtable name, and the hostname the capsule reaches it at.
	Name string

	// Image is the base image the service runs. Required, like the capsule's own.
	Image string

	// Ready is a shell command run inside the service until it exits 0. A
	// container that is running is not the same thing as a database that is
	// accepting connections, and this is the difference.
	Ready string

	// Timeout bounds the wait for Ready. Defaults to DefaultReadyTimeout.
	Timeout time.Duration

	// Ports are published to the host as "host:container", for reaching a
	// service from outside the capsule.
	Ports []string

	// Env is passed into the service container as -e KEY=VALUE.
	Env map[string]string
}

// EnvKeys is the service's environment in sorted order, so its argv is stable.
func (s *Service) EnvKeys() []string { return sortedKeys(s.Env) }

var (
	// nameRE matches what a container runtime accepts as a name component.
	nameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

	// taskNameRE is what can be typed after `capsule run`. Deliberately narrow:
	// a task name reaches no shell and no argv, but a name with a space in it
	// cannot be typed unambiguously, and one starting with a dash would be read
	// as a flag.
	taskNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

	// serviceNameRE is stricter than nameRE: a service name is resolved as a
	// hostname on the capsule network, and a dot or an underscore is not one.
	serviceNameRE = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$`)

	rootKeys = map[string]bool{
		"name": true, "image": true, "shell": true,
		"workdir": true, "ports": true, "packages": true,
		RequirementKey: true,
	}
	listKeys = map[string]bool{"ports": true, "packages": true}

	serviceKeys = map[string]bool{
		"image": true, "ready": true, "timeout": true, "ports": true,
	}
	serviceListKeys = map[string]bool{"ports": true}
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

	if err := checkSections(doc); err != nil {
		return nil, err
	}

	c := &Capsule{
		Name:        defaultName,
		Shell:       DefaultShell,
		Workdir:     DefaultWorkdir,
		Requirement: requirement,
		Env:         map[string]string{},
		Persist:     map[string]string{},
		Tasks:       map[string]string{},
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

	for _, key := range sortedKeys(doc[TasksSection]) {
		v := doc[TasksSection][key]
		if v.isList {
			return nil, fmt.Errorf("%s.%s must be a string", TasksSection, key)
		}
		c.Tasks[key] = v.str
	}

	if c.Services, err = parseServices(doc); err != nil {
		return nil, err
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// checkSections rejects a table capsule does not know, before any of them are
// read. [services.NAME] and [services.NAME.env] are the only nested tables.
func checkSections(doc map[string]table) error {
	for _, section := range sortedKeys(doc) {
		switch section {
		case "", "env", "persist", TasksSection:
			continue
		case ServicesSection:
			// [services] is a heading for the subtables under it. A key sitting
			// directly beneath it names no service, so it is a mistake worth
			// showing the shape for rather than accepting and ignoring.
			if len(doc[section]) > 0 {
				return fmt.Errorf("a service is declared as [%s.NAME], for example [%s.db]",
					ServicesSection, ServicesSection)
			}
			continue
		}
		if _, _, ok := splitServiceSection(section); !ok {
			return fmt.Errorf("unknown section [%s]", section)
		}
	}
	return nil
}

// splitServiceSection reads a [services.NAME] or [services.NAME.env] header,
// reporting which it is. ok is false for any other table name.
func splitServiceSection(section string) (name string, isEnv, ok bool) {
	parts := strings.Split(section, ".")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != ServicesSection || parts[1] == "" {
		return "", false, false
	}
	if len(parts) == 3 && parts[2] != "env" {
		return "", false, false
	}
	return parts[1], len(parts) == 3, true
}

// parseServices collects the [services.*] tables into sorted Services.
func parseServices(doc map[string]table) ([]Service, error) {
	byName := map[string]*Service{}

	// sortedKeys puts [services.db] ahead of [services.db.env], so a service is
	// always created by its own table before its environment is read into it.
	for _, section := range sortedKeys(doc) {
		name, isEnv, ok := splitServiceSection(section)
		if !ok {
			continue
		}
		s := byName[name]
		if s == nil {
			s = &Service{Name: name, Timeout: DefaultReadyTimeout, Env: map[string]string{}}
			byName[name] = s
		}
		if isEnv {
			for _, key := range sortedKeys(doc[section]) {
				v := doc[section][key]
				if v.isList {
					return nil, fmt.Errorf("%s.%s must be a string", section, key)
				}
				s.Env[key] = v.str
			}
			continue
		}
		if err := readService(s, section, doc[section]); err != nil {
			return nil, err
		}
	}

	services := make([]Service, 0, len(byName))
	for _, name := range sortedKeys(byName) {
		services = append(services, *byName[name])
	}
	return services, nil
}

func readService(s *Service, section string, t table) error {
	for _, key := range sortedKeys(t) {
		v := t[key]
		if !serviceKeys[key] {
			return fmt.Errorf("unknown key %q in [%s]", key, section)
		}
		if v.isList != serviceListKeys[key] {
			if serviceListKeys[key] {
				return fmt.Errorf("%s.%s must be an array", section, key)
			}
			return fmt.Errorf("%s.%s must be a string", section, key)
		}
		switch key {
		case "image":
			s.Image = v.str
		case "ready":
			s.Ready = v.str
		case "ports":
			s.Ports = v.list
		case "timeout":
			d, err := time.ParseDuration(v.str)
			if err != nil {
				return fmt.Errorf("%s.timeout %q must be a duration such as \"90s\" or \"2m\"", section, v.str)
			}
			s.Timeout = d
		}
	}
	return nil
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
	for _, name := range c.TaskNames() {
		if !taskNameRE.MatchString(name) {
			return fmt.Errorf("invalid task name %q: use letters, digits, dash or underscore, so it can be typed as `capsule run NAME`", name)
		}
		if strings.TrimSpace(c.Tasks[name]) == "" {
			return fmt.Errorf("%s.%s is empty: give it the command to run", TasksSection, name)
		}
	}
	for i := range c.Services {
		if err := c.Services[i].validate(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) validate() error {
	if !serviceNameRE.MatchString(s.Name) {
		return fmt.Errorf("invalid service name %q: it is the hostname the capsule reaches the service at, so use letters, digits and dashes", s.Name)
	}
	if strings.TrimSpace(s.Image) == "" {
		return fmt.Errorf("%s.%s: `image` is required: set it to the image the service runs", ServicesSection, s.Name)
	}
	// The same rule as the capsule's own image, for the same reason: it is
	// handed to the runtime as an argument, where a leading dash is a flag.
	if strings.HasPrefix(s.Image, "-") {
		return fmt.Errorf("%s.%s: image %q must not start with \"-\": it is passed to the container runtime as an argument, where a leading dash would be read as a flag", ServicesSection, s.Name, s.Image)
	}
	if s.Timeout <= 0 {
		return fmt.Errorf("%s.%s: timeout must be positive", ServicesSection, s.Name)
	}
	for _, p := range s.Ports {
		if err := validatePort(p); err != nil {
			return fmt.Errorf("%s.%s: %w", ServicesSection, s.Name, err)
		}
	}
	for _, k := range s.EnvKeys() {
		if k == "" || strings.ContainsAny(k, "= \t") {
			return fmt.Errorf("%s.%s: invalid env key %q", ServicesSection, s.Name, k)
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
// ServiceNames lists the declared sidecars, sorted by name like Services
// itself, so anything printed from it is stable between runs.
func (c *Capsule) ServiceNames() []string {
	names := make([]string, 0, len(c.Services))
	for _, s := range c.Services {
		names = append(names, s.Name)
	}
	return names
}

// HasService reports whether name is a declared sidecar.
func (c *Capsule) HasService(name string) bool {
	for _, s := range c.Services {
		if s.Name == name {
			return true
		}
	}
	return false
}

// TaskNames lists the declared tasks in sorted order, so `capsule run` with no
// argument prints the same list every time.
func (c *Capsule) TaskNames() []string { return sortedKeys(c.Tasks) }

// HasTask reports whether a task is declared.
func (c *Capsule) HasTask(name string) bool {
	_, ok := c.Tasks[name]
	return ok
}

func (c *Capsule) EnvKeys() []string     { return sortedKeys(c.Env) }
func (c *Capsule) PersistKeys() []string { return sortedKeys(c.Persist) }
