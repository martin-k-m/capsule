// Package e2e drives the built binary against a real container runtime.
//
// Every other test in this repository runs capsule against a fake `docker` on
// PATH, which is fast and hermetic and cannot tell whether the argv capsule
// builds means what capsule thinks it means. A colon in workdir once turned the
// project mount read-only, and no amount of argv assertion would have caught it
// because the argv was exactly what the test expected. These tests ask the
// runtime instead.
//
// They skip when no runtime is on PATH, so a machine without docker or podman
// still runs the rest of the suite.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const image = "debian:bookworm-slim"

// binary builds capsule once and returns its path.
func binary(t *testing.T) string {
	t.Helper()
	name := "capsule"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	build := exec.Command("go", "build", "-o", path, "../../cmd/capsule")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building capsule: %v\n%s", err, out)
	}
	return path
}

func needRuntime(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"docker", "podman"} {
		if _, err := exec.LookPath(bin); err == nil {
			if err := exec.Command(bin, "info").Run(); err == nil {
				return
			}
		}
	}
	t.Skip("no working container runtime on PATH")
}

// project writes a capsule.toml and returns the directory holding it.
func project(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	toml := "capsule = \">=1.1\"\n" +
		"name = \"e2e\"\n" +
		"image = \"" + image + "\"\n" +
		"shell = \"/bin/bash\"\n" +
		"workdir = \"/workspace\"\n" + extra
	if err := os.WriteFile(filepath.Join(dir, "capsule.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// up runs `capsule up -- sh -c script` in dir and returns its output and code.
func up(t *testing.T, bin, dir, script string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, "up", "--", "sh", "-c", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running capsule: %v\n%s", err, out)
	}
	return string(out), code
}

func TestTheProjectIsMountedReadWriteAndTheCommandRuns(t *testing.T) {
	needRuntime(t)
	bin := binary(t)
	dir := project(t, "")
	if err := os.WriteFile(filepath.Join(dir, "input.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := up(t, bin, dir, "cat input.txt && touch made-inside.txt")
	if code != 0 {
		t.Fatalf("capsule up exited %d: %s", code, out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("the project was not readable inside the capsule: %s", out)
	}
	// The write is the half a read-only mount would silently lose, which is
	// what made the workdir bug invisible to every argv-level test.
	if _, err := os.Stat(filepath.Join(dir, "made-inside.txt")); err != nil {
		t.Errorf("a file written inside the capsule did not reach the project: %v", err)
	}
}

func TestTheCommandsExitCodeIsCapsulesExitCode(t *testing.T) {
	needRuntime(t)
	bin := binary(t)
	dir := project(t, "")

	if _, code := up(t, bin, dir, "exit 42"); code != 42 {
		t.Errorf("capsule up exited %d, want the command's 42", code)
	}
	if _, code := up(t, bin, dir, "exit 0"); code != 0 {
		t.Errorf("capsule up exited %d, want 0", code)
	}
}

func TestNothingIsLeftRunningAfterwards(t *testing.T) {
	needRuntime(t)
	bin := binary(t)
	dir := project(t, "")

	if _, code := up(t, bin, dir, "true"); code != 0 {
		t.Fatal("capsule up failed")
	}

	engine := "docker"
	if _, err := exec.LookPath(engine); err != nil {
		engine = "podman"
	}
	out, err := exec.Command(engine, "ps", "-a", "--filter", "label=capsule", "--format", "{{.Names}}").Output()
	if err != nil {
		t.Fatalf("listing containers: %v", err)
	}
	if left := strings.TrimSpace(string(out)); left != "" {
		t.Errorf("capsule left containers behind: %s", left)
	}
}

func TestAWorkdirWithAColonIsRefusedRatherThanMounted(t *testing.T) {
	needRuntime(t)
	bin := binary(t)
	dir := project(t, "")
	toml := filepath.Join(dir, "capsule.toml")
	body, err := os.ReadFile(toml)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(body), `workdir = "/workspace"`, `workdir = "/work:space"`, 1)
	if err := os.WriteFile(toml, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	// The runtime reads everything after a colon in a mount destination as
	// options, so this used to mount the project read-only and say nothing.
	out, code := up(t, bin, dir, "true")
	if code == 0 {
		t.Fatalf("a workdir containing a colon was accepted: %s", out)
	}
	if !strings.Contains(out, "must not contain") {
		t.Errorf("the refusal did not say why: %s", out)
	}
}

// engine returns the runtime binary these tests should ask directly.
func engineBin(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err == nil {
		return "docker"
	}
	return "podman"
}

// TestExecRunsACommandInARunningCapsule is the test that was missing.
//
// `capsule exec` was checked only against its own argv assertions, and those
// assertions encoded a belief about the runtime that was false: that a `--`
// between the container and the command separates them. `docker exec` stops
// parsing flags at the container name, so the `--` was passed through as the
// program, and every `capsule exec` failed with
// `exec: "--": executable file not found in $PATH`. Only the runtime can say
// so, which is why this test lives here.
func TestExecRunsACommandInARunningCapsule(t *testing.T) {
	needRuntime(t)
	bin := binary(t)
	dir := project(t, "")

	held := exec.Command(bin, "up", "--", "sh", "-c", "sleep 120")
	held.Dir = dir
	if err := held.Start(); err != nil {
		t.Fatalf("starting a capsule to exec into: %v", err)
	}
	t.Cleanup(func() {
		_ = held.Process.Kill()
		_, _ = held.Process.Wait()
		out, _ := exec.Command(engineBin(t), "ps", "-q",
			"--filter", "label=me.blinkdev.capsule.name=e2e").Output()
		for _, id := range strings.Fields(string(out)) {
			_ = exec.Command(engineBin(t), "rm", "-f", id).Run()
		}
	})

	// Wait for the capsule's container rather than sleeping a fixed time.
	var running bool
	for i := 0; i < 120; i++ {
		out, err := exec.Command(engineBin(t), "ps", "--format", "{{.Names}}",
			"--filter", "label=me.blinkdev.capsule.role=capsule",
			"--filter", "label=me.blinkdev.capsule.name=e2e").Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			running = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !running {
		t.Skip("the capsule did not come up in 60s; not an exec failure")
	}

	run := func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(bin, append([]string{"exec", "--no-tty"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		code := 0
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatalf("running capsule exec: %v\n%s", err, out)
		}
		return string(out), code
	}

	out, code := run("echo", "capsule-exec-works")
	if code != 0 {
		t.Fatalf("capsule exec echo exited %d: %s", code, out)
	}
	if !strings.Contains(out, "capsule-exec-works") {
		t.Errorf("capsule exec did not run the command: %s", out)
	}

	// The exit code is the reason `capsule exec` exists, so it is checked
	// against the runtime and not only against a fake.
	if _, code := run("sh", "-c", "exit 42"); code != 42 {
		t.Errorf("capsule exec exited %d, want the command's 42", code)
	}

	// A command whose first word starts with a dash still reaches the program.
	out, code = run("--", "sh", "-c", "echo dashed")
	if code != 0 || !strings.Contains(out, "dashed") {
		t.Errorf("capsule exec -- sh -c: exit %d, output %q", code, out)
	}
}
