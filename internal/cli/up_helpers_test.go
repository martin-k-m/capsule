package cli

import (
	"strings"
	"testing"

	"github.com/martin-k-m/capsule/internal/config"
)

// mustParseCLI parses capsule.toml text for a test, failing if it does not.
func mustParseCLI(t *testing.T, src string) *config.Capsule {
	t.Helper()
	c, err := config.Parse(src, "proj")
	if err != nil {
		t.Fatalf("config.Parse: %v", err)
	}
	return c
}

func TestImagesListsTheCapsuleFirstThenServices(t *testing.T) {
	c := mustParseCLI(t, `
image = "golang:1-alpine"

[services.db]
image = "postgres:16"

[services.cache]
image = "redis:7"
`)
	got := images(c)
	// The capsule's own image comes first, then the services in the order
	// config keeps them (sorted by name), so `--pull` refreshes all of them.
	want := []string{"golang:1-alpine", "redis:7", "postgres:16"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("images() = %v, want %v", got, want)
	}
}

func TestImagesOfAPlainCapsuleIsJustItsOwn(t *testing.T) {
	c := mustParseCLI(t, `image = "alpine"`)
	got := images(c)
	if len(got) != 1 || got[0] != "alpine" {
		t.Errorf("images() = %v, want [alpine]", got)
	}
}

func TestSurvivalNoteWhenNothingPersists(t *testing.T) {
	c := mustParseCLI(t, `image = "alpine"`)
	note := survivalNote(c)
	// The promise of the tool, stated plainly on every up.
	if !strings.Contains(note, "nothing outside your project directory survives") {
		t.Errorf("survivalNote = %q, want it to say nothing survives", note)
	}
}

func TestSurvivalNoteNamesThePersistedVolumes(t *testing.T) {
	c := mustParseCLI(t, `
image = "alpine"

[persist]
gomod   = "/go/pkg/mod"
apkache = "/var/cache/apk"
`)
	note := survivalNote(c)
	// Both volumes, in sorted order, since that is the order PersistKeys yields.
	if !strings.Contains(note, "apkache") || !strings.Contains(note, "gomod") {
		t.Errorf("survivalNote = %q, want it to name each persisted volume", note)
	}
	if strings.Index(note, "apkache") > strings.Index(note, "gomod") {
		t.Errorf("survivalNote = %q, want volumes in sorted order", note)
	}
}
