package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const full = `
# a capsule
name    = "demo"
image   = "golang:1-alpine"   # trailing comment with a # inside
shell   = "/bin/sh"
workdir = "/src"

ports = ["8080:8080", "5432:5432"]
packages = [
  "git",
  "make",
]

[env]
CGO_ENABLED = "0"
GREETING = "hello # world"

[persist]
gomod = "/go/pkg/mod"
`

func TestParseFull(t *testing.T) {
	c, err := Parse(full, "fallback")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Name != "demo" || c.Image != "golang:1-alpine" {
		t.Errorf("name/image = %q/%q", c.Name, c.Image)
	}
	if c.Workdir != "/src" || c.Shell != "/bin/sh" {
		t.Errorf("workdir/shell = %q/%q", c.Workdir, c.Shell)
	}
	if len(c.Ports) != 2 || c.Ports[1] != "5432:5432" {
		t.Errorf("ports = %v", c.Ports)
	}
	if len(c.Packages) != 2 || c.Packages[0] != "git" {
		t.Errorf("packages = %v", c.Packages)
	}
	if c.Env["CGO_ENABLED"] != "0" {
		t.Errorf("env CGO_ENABLED = %q", c.Env["CGO_ENABLED"])
	}
	// A # inside a quoted value is data, not the start of a comment.
	if c.Env["GREETING"] != "hello # world" {
		t.Errorf("env GREETING = %q", c.Env["GREETING"])
	}
	if c.Persist["gomod"] != "/go/pkg/mod" {
		t.Errorf("persist = %v", c.Persist)
	}
}

func TestParseDefaults(t *testing.T) {
	c, err := Parse(`image = "alpine"`, "myproject")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Name != "myproject" {
		t.Errorf("name = %q, want the directory name", c.Name)
	}
	if c.Shell != DefaultShell || c.Workdir != DefaultWorkdir {
		t.Errorf("defaults not applied: shell=%q workdir=%q", c.Shell, c.Workdir)
	}
	if len(c.Persist) != 0 {
		t.Errorf("a capsule with no [persist] must keep nothing, got %v", c.Persist)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"no image", `name = "x"`, "`image` is required"},
		{"unknown key", "image = \"a\"\nimages = \"b\"", `unknown key "images"`},
		{"unknown section", "image = \"a\"\n[nope]\nk = \"v\"", "unknown section [nope]"},
		{"string where array expected", `image = "a"` + "\n" + `ports = "8080:8080"`, `"ports" must be an array`},
		{"array where string expected", `image = ["a"]`, `"image" must be a string`},
		{"bad port", `image = "a"` + "\n" + `ports = ["8080"]`, `must be written "host:container"`},
		{"port out of range", `image = "a"` + "\n" + `ports = ["0:80"]`, "1-65535"},
		{"relative workdir", `image = "a"` + "\n" + `workdir = "src"`, "must be an absolute path"},
		{"relative persist", "image = \"a\"\n[persist]\nv = \"rel\"", "must mount an absolute path"},
		{"bad name", `image = "a"` + "\n" + `name = "-nope"`, "invalid name"},
		// A colon does not extend a mount destination, it ends it. Accepting one
		// let a capsule.toml mount the developer's own project read-only without
		// saying so anywhere. Found by FuzzParseToArgv.
		{"workdir carries mount options", `image = "a"` + "\n" + `workdir = "/src:ro"`, `must not contain ":"`},
		{"persist target carries mount options", "image = \"a\"\n[persist]\ncache = \"/data:ro\"", `must not contain ":"`},
		{"image in flag position", `image = "-v/etc:/etc"`, `must not start with "-"`},
		{"requirement is not a range", `capsule = "0.2"` + "\n" + `image = "a"`, `must be written ">=MAJOR.MINOR"`},
		{"requirement is not a version", `capsule = ">=latest"` + "\n" + `image = "a"`, "is not a version"},
		{"requirement as array", `capsule = [">=0.2"]` + "\n" + `image = "a"`, `"capsule" must be a string`},
		{"duplicate key", "image = \"a\"\nimage = \"b\"", "duplicate key"},
		{"missing value", "image =", "missing value"},
		{"unterminated string", `image = "a`, "unterminated string"},
		{"unterminated array", `image = "a"` + "\n" + `ports = ["8080:8080"`, "unterminated array"},
		{"not a pair", "image = \"a\"\nnonsense", "expected `key = value`"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.src, "fallback")
			if err == nil {
				t.Fatalf("expected an error mentioning %q, got none", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

const withServices = `
image = "node:18"

[services.db]
image   = "postgres:14"
ready   = "pg_isready -U app"
timeout = "90s"
ports   = ["5432:5432"]

[services.db.env]
POSTGRES_PASSWORD = "dev"
POSTGRES_USER     = "app"

[services.cache]
image = "redis:7"
`

func TestParseServices(t *testing.T) {
	c, err := Parse(withServices, "fallback")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(c.Services) != 2 {
		t.Fatalf("services = %v, want two", c.Services)
	}
	// Sorted, not written order: everything capsule prints or passes to a
	// runtime has to come out the same way every time.
	if c.Services[0].Name != "cache" || c.Services[1].Name != "db" {
		t.Errorf("services are not in sorted order: %q, %q", c.Services[0].Name, c.Services[1].Name)
	}

	db := c.Services[1]
	if db.Image != "postgres:14" {
		t.Errorf("db image = %q", db.Image)
	}
	if db.Ready != "pg_isready -U app" {
		t.Errorf("db ready = %q", db.Ready)
	}
	if db.Timeout != 90*time.Second {
		t.Errorf("db timeout = %s", db.Timeout)
	}
	if len(db.Ports) != 1 || db.Ports[0] != "5432:5432" {
		t.Errorf("db ports = %v", db.Ports)
	}
	if db.Env["POSTGRES_PASSWORD"] != "dev" || db.Env["POSTGRES_USER"] != "app" {
		t.Errorf("db env = %v", db.Env)
	}
	if strings.Join(db.EnvKeys(), ",") != "POSTGRES_PASSWORD,POSTGRES_USER" {
		t.Errorf("db env keys are not sorted: %v", db.EnvKeys())
	}

	cache := c.Services[0]
	if cache.Timeout != DefaultReadyTimeout {
		t.Errorf("a service that names no timeout should get the default, got %s", cache.Timeout)
	}
	if cache.Ready != "" {
		t.Errorf("cache ready = %q, want none declared", cache.Ready)
	}
}

func TestNoServicesIsFine(t *testing.T) {
	c, err := Parse(`image = "alpine"`, "x")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(c.Services) != 0 {
		t.Errorf("services = %v, want none", c.Services)
	}
}

func TestServiceErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"no image", "image = \"a\"\n[services.db]\nready = \"true\"", "services.db: `image` is required"},
		{"unknown key", "image = \"a\"\n[services.db]\nimage = \"p\"\nreadyy = \"true\"", `unknown key "readyy" in [services.db]`},
		{"unknown subtable", "image = \"a\"\n[services.db.envv]\nA = \"1\"", "unknown section [services.db.envv]"},
		{"too deep", "image = \"a\"\n[services.db.env.more]\nA = \"1\"", "unknown section [services.db.env.more]"},
		{"key directly under services", "image = \"a\"\n[services]\ndb = \"postgres:14\"", "a service is declared as [services.NAME]"},
		{"bad duration", "image = \"a\"\n[services.db]\nimage = \"p\"\ntimeout = \"soon\"", "must be a duration"},
		{"array where string expected", "image = \"a\"\n[services.db]\nimage = [\"p\"]", "services.db.image must be a string"},
		{"string where array expected", "image = \"a\"\n[services.db]\nimage = \"p\"\nports = \"5432:5432\"", "services.db.ports must be an array"},
		{"bad port", "image = \"a\"\n[services.db]\nimage = \"p\"\nports = [\"5432\"]", `services.db: port "5432" must be written "host:container"`},
		{"image in flag position", "image = \"a\"\n[services.db]\nimage = \"-v/etc:/etc\"", `services.db: image "-v/etc:/etc" must not start with "-"`},
		{"name is not a hostname", "image = \"a\"\n[services.my_db]\nimage = \"p\"", `invalid service name "my_db"`},
		{"env as array", "image = \"a\"\n[services.db]\nimage = \"p\"\n[services.db.env]\nA = [\"1\"]", "services.db.env.A must be a string"},
		{"env named [services.db.env] only", "image = \"a\"\n[services.db.env]\nA = \"1\"", "services.db: `image` is required"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.src, "fallback")
			if err == nil {
				t.Fatalf("expected an error mentioning %q, got none", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestServicesAreDeterministic(t *testing.T) {
	first, err := Parse(withServices, "x")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := ""
	for _, s := range first.Services {
		want += s.Name + ":" + strings.Join(s.EnvKeys(), ",") + ";"
	}
	for i := 0; i < 50; i++ {
		again, err := Parse(withServices, "x")
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got := ""
		for _, s := range again.Services {
			got += s.Name + ":" + strings.Join(s.EnvKeys(), ",") + ";"
		}
		if got != want {
			t.Fatalf("service order changed between parses: %q vs %q", want, got)
		}
	}
}

func TestParseIsDeterministic(t *testing.T) {
	// Key order comes from maps, so the same input must still produce the same
	// ordered output every time, capsule's runtime flags depend on it.
	c, err := Parse(full, "fallback")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	first := strings.Join(c.EnvKeys(), ",")
	for i := 0; i < 50; i++ {
		again, err := Parse(full, "fallback")
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if got := strings.Join(again.EnvKeys(), ","); got != first {
			t.Fatalf("env key order changed between parses: %q vs %q", first, got)
		}
	}
}

// unreachable is a version no capsule will ever be, so these tests keep saying
// what they mean after the real version moves on.
const unreachable = ">=999.0"

func TestSatisfiedRequirementIsKept(t *testing.T) {
	c, err := Parse("capsule = \">=0.0\"\nimage = \"alpine\"", "x")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Requirement != ">=0.0" {
		t.Errorf("Requirement = %q, want it recorded as written", c.Requirement)
	}
	if c.Image != "alpine" {
		t.Errorf("the rest of the file was not read: image = %q", c.Image)
	}
}

func TestNoRequirementIsFine(t *testing.T) {
	c, err := Parse(`image = "alpine"`, "x")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Requirement != "" {
		t.Errorf("Requirement = %q, want empty when the file declares none", c.Requirement)
	}
}

// The version requirement exists so an older capsule reading a newer file says
// so. Every one of these files would otherwise fail on something that sends the
// reader looking for a mistake they did not make.
func TestRequirementIsReportedBeforeAnythingElse(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		instead string // what the file would have been rejected for
	}{
		{"unknown key", "capsule = \"" + unreachable + "\"\nimage = \"a\"\nsandbox = \"strict\"", "unknown key"},
		{"unknown section", "capsule = \"" + unreachable + "\"\nimage = \"a\"\n[mounts]\nx = \"/x\"", "unknown section"},
		// The reason [services] declares a requirement: a capsule too old to
		// know the section has to say so, not call it a mistake.
		{"a section this build does know", "capsule = \"" + unreachable + "\"\nimage = \"a\"\n[services.db]\nimage = \"postgres:14\"", "unknown section"},
		{"missing image", "capsule = \"" + unreachable + "\"", "`image` is required"},
		// A newer capsule may add syntax this reader cannot read at all, so the
		// requirement is looked for in the raw text before a parse error is
		// reported.
		{"syntax it cannot parse", "capsule = \"" + unreachable + "\"\nimage = \"a\"\nmounts = { host = \"/x\" }", "malformed value"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.src, "fallback")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), "needs capsule "+unreachable) {
				t.Errorf("error = %q, want it to name the version requirement", err)
			}
			if strings.Contains(err.Error(), tc.instead) {
				t.Errorf("error = %q, want the requirement reported instead of %q", err, tc.instead)
			}
		})
	}
}

func TestSatisfiedRequirementDoesNotHideARealError(t *testing.T) {
	// The raw-text fallback must not swallow a syntax error in a file this
	// capsule is perfectly entitled to read.
	_, err := Parse("capsule = \">=0.0\"\nimage = \"a\"\nmounts = { host = \"/x\" }", "x")
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if !strings.Contains(err.Error(), "malformed value") {
		t.Errorf("error = %q, want the syntax error itself", err)
	}
}

func TestFindWalksUpward(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "cmd", "server")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, root, `image = "alpine"`)

	got, err := Find(deep)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got != root {
		t.Errorf("Find(%q) = %q, want the project root %q", deep, got, root)
	}
}

func TestFindPrefersTheNearest(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "tool")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, root, `image = "alpine"`)
	write(t, sub, `image = "alpine"`)

	got, err := Find(sub)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got != sub {
		t.Errorf("Find(%q) = %q, want the nearest %s to win", sub, got, FileName)
	}
}

func TestFindSaysItSearchedUpward(t *testing.T) {
	// The message has to explain the walk, or someone with a capsule.toml one
	// directory over reads it as "capsule cannot see the file that is right here".
	_, err := Find(t.TempDir())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "or any parent directory") {
		t.Errorf("error = %q, want it to say the search walked upward", err)
	}
}

func TestLoadNamesTheDirectoryHoldingTheFile(t *testing.T) {
	// The default name follows capsule.toml, not the working directory, so a
	// capsule started from a subdirectory is still the same capsule.
	dir := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, `image = "alpine"`)

	c, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Name != "myproj" {
		t.Errorf("name = %q, want the directory holding %s", c.Name, FileName)
	}
}

func write(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLiteralStringsAreNotUnescaped(t *testing.T) {
	c, err := Parse("image = \"a\"\n[env]\nP = 'C:\\path\\n'", "x")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Env["P"] != `C:\path\n` {
		t.Errorf("literal string was altered: %q", c.Env["P"])
	}
}

// A capsule.toml written by a Windows editor, or by PowerShell's own
// `Set-Content -Encoding utf8`, begins with a UTF-8 BOM. It is a byte-order
// mark rather than content, but it lands on the first key of the file, so
// before it was stripped a perfectly correct config failed with
// `unknown key "\uFEFFimage"` (with the mark shown escaped): an error naming a key that looks right on
// screen and cannot be edited into a key that works.
func TestALeadingByteOrderMarkIsNotPartOfTheFirstKey(t *testing.T) {
	c, err := Parse("\uFEFFimage = \"alpine\"\nname = \"demo\"", "x")
	if err != nil {
		t.Fatalf("Parse rejected a config whose only oddity is a BOM: %v", err)
	}
	if c.Image != "alpine" {
		t.Errorf("image = %q, want %q", c.Image, "alpine")
	}
}

// The BOM is only a mark at the very start of a document. Anywhere else the
// same rune is a zero-width no-break space, which is a character in a value and
// must survive rather than be quietly trimmed out of one.
func TestAByteOrderMarkIsOnlyStrippedFromTheStart(t *testing.T) {
	c, err := Parse("image = \"alpine\"\n[env]\nA = \"x\uFEFFy\"", "x")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Env["A"] != "x\uFEFFy" {
		t.Errorf("value was altered: %q", c.Env["A"])
	}
}

func TestServiceNamesAndHasService(t *testing.T) {
	c, err := Parse(`
image = "alpine"

[services.db]
image = "postgres:16"

[services.cache]
image = "redis:7"
`, "proj")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Sorted, not in declaration order: Services itself is sorted by name so
	// that argv and anything printed from it is stable between runs.
	names := c.ServiceNames()
	if len(names) != 2 || names[0] != "cache" || names[1] != "db" {
		t.Errorf("ServiceNames() = %v, want [cache db]", names)
	}
	if !c.HasService("db") || !c.HasService("cache") {
		t.Error("HasService should find a declared service")
	}
	if c.HasService("nope") {
		t.Error("HasService should not invent one")
	}
}

func TestServiceHelpersOnACapsuleWithNoServices(t *testing.T) {
	c, err := Parse(`image = "alpine"`, "proj")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(c.ServiceNames()) != 0 {
		t.Errorf("ServiceNames() = %v, want empty", c.ServiceNames())
	}
	if c.HasService("db") {
		t.Error("HasService should be false when nothing is declared")
	}
}

func mustParse(t *testing.T, src string) *Capsule {
	t.Helper()
	c, err := Parse(src, "project")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return c
}

func TestParseReadsTasks(t *testing.T) {
	c := mustParse(t, `
image = "golang:1.26"

[tasks]
test = "go test ./..."
lint = "gofmt -l . && go vet ./..."
`)
	if got := c.Tasks["test"]; got != "go test ./..." {
		t.Errorf("test task = %q", got)
	}
	// A task is a shell command, not an argv, so the operators in it are the
	// point rather than something to escape.
	if got := c.Tasks["lint"]; got != "gofmt -l . && go vet ./..." {
		t.Errorf("lint task = %q", got)
	}
}

func TestTaskNamesAreSorted(t *testing.T) {
	c := mustParse(t, `
image = "x"

[tasks]
zebra = "z"
alpha = "a"
middle = "m"
`)
	got := c.TaskNames()
	want := []string{"alpha", "middle", "zebra"}
	if len(got) != len(want) {
		t.Fatalf("TaskNames() = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TaskNames() = %v, want %v", got, want)
		}
	}
}

func TestHasTask(t *testing.T) {
	c := mustParse(t, "image = \"x\"\n\n[tasks]\ntest = \"go test\"\n")
	if !c.HasTask("test") {
		t.Error("HasTask(test) = false")
	}
	if c.HasTask("Test") {
		// Unlike a service name, a task name is typed verbatim after
		// `capsule run`, so matching loosely would make the listing a lie.
		t.Error("HasTask is case-insensitive")
	}
}

func TestACapsuleWithNoTasksHasAnEmptyMap(t *testing.T) {
	// Nil would make `capsule run` panic on a file that simply declares none,
	// which is most of them.
	c := mustParse(t, "image = \"x\"\n")
	if c.Tasks == nil {
		t.Fatal("Tasks is nil")
	}
	if len(c.TaskNames()) != 0 {
		t.Errorf("TaskNames() = %v", c.TaskNames())
	}
}

func TestAnEmptyTaskIsRejected(t *testing.T) {
	// A task that runs nothing exits 0, which is the worst possible answer for
	// something a CI job is about to believe.
	_, err := Parse("image = \"x\"\n\n[tasks]\ntest = \"   \"\n", "p")
	if err == nil {
		t.Fatal("an empty task was accepted")
	}
}

func TestAnUntypeableTaskNameIsRejected(t *testing.T) {
	for _, name := range []string{`"go test"`, `"-flag"`, `"a b"`} {
		src := "image = \"x\"\n\n[tasks]\n" + name + " = \"go test\"\n"
		if _, err := Parse(src, "p"); err == nil {
			t.Errorf("task name %s was accepted", name)
		}
	}
}

func TestATaskMustBeAString(t *testing.T) {
	_, err := Parse("image = \"x\"\n\n[tasks]\ntest = [\"go\", \"test\"]\n", "p")
	if err == nil {
		t.Fatal("a list task was accepted")
	}
}
