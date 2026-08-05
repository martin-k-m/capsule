package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/martin-k-m/capsule/internal/config"
)

func TestShellQuoteMakesOneWord(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"plain", `'plain'`},
		{"two words", `'two words'`},
		{"", `''`},
		// The one character single quotes cannot contain, spelled the only way
		// a shell accepts: close, escaped quote, reopen.
		{"it's", `'it'\''s'`},
		// None of these mean anything inside single quotes, which is the point.
		{"$HOME", `'$HOME'`},
		{"a;rm -rf /", `'a;rm -rf /'`},
		{"`whoami`", "'`whoami`'"},
		{`back\slash`, `'back\slash'`},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestShellQuoteRoundTripsWhateverGoesIn(t *testing.T) {
	// The property behind the table above, stated as what a shell would do:
	// strip the outer quotes and undo the escape, and the original is back.
	//
	// Counting quotes would be the obvious check and is the wrong one, because
	// an escaped quote is not part of a quoted run: quoting a lone quote is
	// correct and produces an odd number of them.
	inputs := []string{"", "'", "''", "a'b'c", "'; echo pwned; '", "$x", "a b"}
	for _, in := range inputs {
		got := shellQuote(in)
		if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
			t.Errorf("shellQuote(%q) = %s is not a quoted run", in, got)
			continue
		}
		inner := got[1 : len(got)-1]
		if back := strings.ReplaceAll(inner, `'\''`, "'"); back != in {
			t.Errorf("shellQuote(%q) = %s unquotes to %q", in, got, back)
		}
	}
}

func TestListTasksNamesWhatIsThere(t *testing.T) {
	c := &config.Capsule{Tasks: map[string]string{
		"test":  "go test ./...",
		"build": "go build ./cmd/capsule",
	}}
	out := captureTaskList(t, c)
	if !strings.Contains(out, "build") || !strings.Contains(out, "go test ./...") {
		t.Errorf("listing does not name the tasks:\n%s", out)
	}
	// Sorted, so the same file prints the same list every time.
	if strings.Index(out, "build") > strings.Index(out, "test") {
		t.Errorf("tasks are not in sorted order:\n%s", out)
	}
}

func TestListTasksShowsTheShapeWhenThereAreNone(t *testing.T) {
	// "no tasks" is not useful on its own. The reader's next question is how to
	// declare one, and the answer is three lines long.
	out := captureTaskList(t, &config.Capsule{Tasks: map[string]string{}})
	if !strings.Contains(out, "[tasks]") {
		t.Errorf("empty listing does not show how to declare a task:\n%s", out)
	}
}

// captureTaskList runs listTasks against a temporary file, since it writes to an
// *os.File rather than an io.Writer: the caller passes os.Stdout or os.Stderr,
// and which one is part of the behaviour.
func captureTaskList(t *testing.T, c *config.Capsule) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	listTasks(f, c)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes.TrimSpace(body))
}
