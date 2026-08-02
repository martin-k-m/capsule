package cli

import (
	"strings"
	"testing"

	"github.com/martin-k-m/capsule/internal/config"
	"github.com/martin-k-m/capsule/internal/version"
)

func TestGeneratedConfigDeclaresThisCapsule(t *testing.T) {
	body := render("demo", generic)

	want := "capsule = \">=" + version.Series() + "\""
	if !strings.Contains(body, want) {
		t.Errorf("generated %s should declare %s:\n%s", config.FileName, want, body)
	}
	// The requirement has to come before every other key, or an older capsule
	// reaches a key it does not know before it reaches the explanation.
	if i, j := strings.Index(body, "capsule = "), strings.Index(body, "name    = "); i < 0 || i > j {
		t.Errorf("the version requirement must precede the other keys:\n%s", body)
	}
}

func TestEveryPresetGeneratesAParseableConfig(t *testing.T) {
	// `capsule init` writing something `capsule up` then rejects would be a bad
	// first five minutes, and the presets are the only configs capsule authors.
	presets := []preset{generic}
	for _, d := range detectors {
		presets = append(presets, d.preset)
	}

	for _, p := range presets {
		body := render("demo", p)
		c, err := config.Parse(body, "demo")
		if err != nil {
			t.Fatalf("%s preset generated an invalid %s: %v\n%s", p.kind, config.FileName, err, body)
		}
		if c.Requirement == "" {
			t.Errorf("%s preset generated no version requirement", p.kind)
		}
		if c.Image != p.image {
			t.Errorf("%s preset: image = %q, want %q", p.kind, c.Image, p.image)
		}
		if p.volume != "" && c.Persist[p.volume] != p.mount {
			t.Errorf("%s preset: persist %q = %q, want %q", p.kind, p.volume, c.Persist[p.volume], p.mount)
		}
		// Presets persist caches only. Source and build output are never worth
		// keeping, and a preset is where that line would quietly move.
		if len(c.Persist) > 1 {
			t.Errorf("%s preset persists more than one volume: %v", p.kind, c.Persist)
		}
	}
}
