package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
