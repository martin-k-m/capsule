package cli

import (
	"strings"
	"testing"
)

func TestCopyOutOfTheCapsule(t *testing.T) {
	from, to, err := copyEnds("capsule:/tmp/report.html", "./report.html", "demo-a1b2")
	if err != nil {
		t.Fatal(err)
	}
	if from != "demo-a1b2:/tmp/report.html" {
		t.Errorf("from = %q", from)
	}
	if to != "./report.html" {
		t.Errorf("to = %q", to)
	}
}

func TestCopyIntoTheCapsule(t *testing.T) {
	from, to, err := copyEnds("./fixture.json", "capsule:/workspace/fixture.json", "demo-a1b2")
	if err != nil {
		t.Fatal(err)
	}
	if from != "./fixture.json" {
		t.Errorf("from = %q", from)
	}
	if to != "demo-a1b2:/workspace/fixture.json" {
		t.Errorf("to = %q", to)
	}
}

func TestACopyWithNoCapsuleSideIsRefused(t *testing.T) {
	// Two host paths is `cp`, and doing it silently would be a tool quietly
	// answering a question it was not asked.
	_, _, err := copyEnds("a.txt", "b.txt", "demo")
	if err == nil {
		t.Fatal("a host-to-host copy was accepted")
	}
	// The message has to show the shape, since the mistake is the syntax.
	if !strings.Contains(err.Error(), "capsule:") {
		t.Errorf("error does not show the prefix: %v", err)
	}
}

func TestACopyBetweenTwoCapsuleSidesIsRefused(t *testing.T) {
	_, _, err := copyEnds("capsule:/a", "capsule:/b", "demo")
	if err == nil {
		t.Fatal("a capsule-to-capsule copy was accepted")
	}
}

func TestAPrefixWithNoPathIsRefused(t *testing.T) {
	// `capsule:` alone would become `name:` and the runtime would read the
	// empty path as the container's root, which is not what anybody typed.
	if _, _, err := copyEnds("capsule:", "./out", "demo"); err == nil {
		t.Error("an empty container path was accepted on the source")
	}
	if _, _, err := copyEnds("./in", "capsule:", "demo"); err == nil {
		t.Error("an empty container path was accepted on the destination")
	}
}

func TestAContainerPathKeepsItsOwnSeparators(t *testing.T) {
	// The path is inside the container, so the host's separator is not the one
	// that applies. Normalising it on Windows would rewrite a correct path.
	from, _, err := copyEnds(`capsule:/var/log/app.log`, ".", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if from != "demo:/var/log/app.log" {
		t.Errorf("from = %q", from)
	}
}

func TestAHostPathWithAColonIsStillAHostPath(t *testing.T) {
	// `C:\src` on Windows contains a colon and is not a capsule path. Only the
	// literal `capsule:` prefix names the container side.
	from, to, err := copyEnds(`C:\src\fixture.json`, "capsule:/workspace/f.json", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if from != `C:\src\fixture.json` {
		t.Errorf("from = %q", from)
	}
	if to != "demo:/workspace/f.json" {
		t.Errorf("to = %q", to)
	}
}
