package config

import (
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

func TestLiteralStringsAreNotUnescaped(t *testing.T) {
	c, err := Parse("image = \"a\"\n[env]\nP = 'C:\\path\\n'", "x")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Env["P"] != `C:\path\n` {
		t.Errorf("literal string was altered: %q", c.Env["P"])
	}
}
