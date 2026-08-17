package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/martin-k-m/capsule/internal/config"
	cruntime "github.com/martin-k-m/capsule/internal/runtime"
)

// The tests below drive a stack against a fake container runtime rather than
// docker. The fake is this test binary re-executed: TestMain hands over to
// fakeRuntime when the environment says so, so a stack can be told to fail one
// exact command without a container, a daemon or a shell script.
const (
	envFake  = "CAPSULE_FAKE_RT"
	envLog   = "CAPSULE_FAKE_LOG"
	envFail  = "CAPSULE_FAKE_FAIL"
	envSleep = "CAPSULE_FAKE_SLEEP"
	envTrap  = "CAPSULE_FAKE_TRAP"
)

func TestMain(m *testing.M) {
	switch {
	case os.Getenv(envFake) != "":
		fakeRuntime()
	case os.Getenv(envTrap) != "":
		trapChild()
	}
	os.Exit(m.Run())
}

// fakeRuntime stands in for `docker`. It records the command line it was given
// and fails the ones named in CAPSULE_FAKE_FAIL, which is how a cleanup step
// that fails partway is produced on demand.
func fakeRuntime() {
	line := strings.Join(os.Args[1:], " ")
	if path := os.Getenv(envLog); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Fprintln(f, line)
		_ = f.Close()
	}
	if d := os.Getenv(envSleep); d != "" {
		if wait, err := time.ParseDuration(d); err == nil {
			time.Sleep(wait)
		}
	}
	for _, want := range strings.Split(os.Getenv(envFail), ",") {
		if want != "" && strings.Contains(line, want) {
			fmt.Fprintf(os.Stderr, "fake: refusing %q\n", line)
			os.Exit(1)
		}
	}
	fmt.Println("running")
	os.Exit(0)
}

// fake returns a stack wired to the fake runtime, plus a reader for everything
// the fake was asked to run.
func fake(t *testing.T, fail string) (*stack, func() []string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv(envFake, "1")
	t.Setenv(envLog, log)
	t.Setenv(envFail, fail)

	s := &stack{
		rt: &cruntime.Runtime{Bin: os.Args[0]},
		c:  &config.Capsule{Name: "demo", Image: "alpine:3.20"},
		id: "abcd1234",
	}
	return s, func() []string {
		t.Helper()
		b, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if l != "" {
				lines = append(lines, l)
			}
		}
		return lines
	}
}

// withServices fills in what a started stack would have recorded.
func withServices(s *stack, names ...string) {
	s.network = cruntime.NetworkName(s.c.Name, s.id)
	for _, n := range names {
		s.started = append(s.started, cruntime.ServiceContainerName(s.c.Name, s.id, n))
	}
}

// captureStderr collects what f writes to os.Stderr.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()
	f()
	os.Stderr = saved
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// captureErr runs f, discarding what it printed and keeping what it returned.
func captureErr(t *testing.T, f func() error) error {
	t.Helper()
	var err error
	captureStderr(t, func() { err = f() })
	return err
}

func TestTeardownWithNothingStartedRunsNothing(t *testing.T) {
	// A capsule with no services creates no network and no sidecars, so its
	// teardown must not reach for the runtime at all: the deferred call in
	// runUp happens on every path, including ones where nothing was started.
	s, calls := fake(t, "")
	s.teardown()
	if got := calls(); len(got) != 0 {
		t.Fatalf("teardown ran %v, want nothing", got)
	}
}

func TestTeardownRemovesTheCapsuleThenServicesThenTheNetwork(t *testing.T) {
	s, calls := fake(t, "")
	withServices(s, "db", "cache")
	s.teardown()

	want := []string{
		"rm -f capsule-demo-abcd1234",
		"rm -f capsule-demo-abcd1234-db",
		"rm -f capsule-demo-abcd1234-cache",
		"network rm capsule-demo-abcd1234-net",
	}
	got := calls()
	if len(got) != len(want) {
		t.Fatalf("teardown ran %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTeardownFinishesAfterAServiceRemovalFails(t *testing.T) {
	// The README says network cleanup "is a step capsule runs, and a step can
	// fail". The property that matters is that one failing step does not
	// abandon the rest: a service capsule could not remove must not stop it
	// removing the other one, or trying the network.
	s, calls := fake(t, "rm -f capsule-demo-abcd1234-db")
	withServices(s, "db", "cache")

	out := captureStderr(t, s.teardown)

	got := strings.Join(calls(), "\n")
	for _, want := range []string{
		"rm -f capsule-demo-abcd1234-db",
		"rm -f capsule-demo-abcd1234-cache",
		"network rm capsule-demo-abcd1234-net",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("teardown never ran %q:\n%s", want, got)
		}
	}
	// A removal that fails is not reported on its own. The network is the
	// check on it, and here the network came away, so capsule stays quiet.
	if strings.TrimSpace(out) != "" {
		t.Errorf("teardown reported %q, want nothing while the network came away", out)
	}
}

func TestTeardownReportsALeakWhenTheNetworkWillNotGo(t *testing.T) {
	// A runtime refuses to remove a network something is still attached to, so
	// this failing is the only evidence capsule has that a container outlived
	// its removal. It has to name the fix, and it must not be silent.
	s, calls := fake(t, "network rm")
	withServices(s, "db")

	out := captureStderr(t, s.teardown)

	if !strings.Contains(out, "`capsule down` will clear it") {
		t.Errorf("a leaked network printed %q, want it to name `capsule down`", out)
	}
	if !strings.Contains(out, "network rm") {
		t.Errorf("a leaked network printed %q, want the runtime's own complaint", out)
	}
	if n := len(calls()); n != 3 {
		t.Errorf("teardown ran %d commands, want 3", n)
	}
}

func TestTeardownWithOnlyANetworkStillRemovesIt(t *testing.T) {
	// start creates the network before any service, so a failure in between
	// leaves a stack with a network and no started containers. That is the
	// shape that leaks a network if teardown short-circuits on `started`.
	s, calls := fake(t, "")
	s.network = cruntime.NetworkName(s.c.Name, s.id)
	s.teardown()

	got := calls()
	if len(got) != 2 || got[1] != "network rm capsule-demo-abcd1234-net" {
		t.Fatalf("teardown ran %v, want the capsule removed and the network removed", got)
	}
}

func TestTeardownRunsOnlyOnce(t *testing.T) {
	// Both the deferred call and the signal handler call teardown without
	// checking whether the other one did, so the second call has to be inert.
	s, calls := fake(t, "")
	withServices(s, "db")
	s.teardown()
	first := len(calls())
	s.teardown()
	if got := len(calls()); got != first {
		t.Fatalf("a second teardown ran %d more commands, want 0", got-first)
	}
}

func TestSignalStatusIsTheConventional128PlusN(t *testing.T) {
	cases := []struct {
		sig  os.Signal
		want int
	}{
		{syscall.SIGTERM, 143},
		{os.Interrupt, 130},
		// Not a signal trap listens for. signalStatus still has to answer,
		// and 130 is what it answers.
		{syscall.SIGHUP, 130},
	}
	for _, tc := range cases {
		if got := signalStatus(tc.sig); got != tc.want {
			t.Errorf("signalStatus(%v) = %d, want %d", tc.sig, got, tc.want)
		}
	}
}

func TestTrapWithoutASignalTearsNothingDown(t *testing.T) {
	// The handler itself calls os.Exit, so what is checked in-process is the
	// quiet path: installing the trap and stopping it again, which is what a
	// `capsule up` that ends normally does, must touch nothing.
	s, calls := fake(t, "")
	withServices(s, "db")

	stop := trap(s)
	stop()

	if got := calls(); len(got) != 0 {
		t.Fatalf("trap tore down %v without a signal", got)
	}
}

// trapChild is one `capsule up`-shaped process for the signal tests: a stack
// with a service, a trap installed, then a wait for the signal to arrive. It
// only exists on the child side of TestTrapTearsDownOnASignal.
func trapChild() {
	// Anything this process starts is the fake runtime, not another trapChild.
	os.Setenv(envFake, "1")
	s := &stack{
		rt: &cruntime.Runtime{Bin: os.Args[0]},
		c:  &config.Capsule{Name: "demo", Image: "alpine:3.20"},
		id: "abcd1234",
	}
	s.network = cruntime.NetworkName(s.c.Name, s.id)
	s.started = []string{cruntime.ServiceContainerName(s.c.Name, s.id, "db")}

	trap(s)
	fmt.Println("ready")
	time.Sleep(30 * time.Second)
	os.Exit(9) // never reached when the signal is handled
}

// signalChild starts trapChild and returns it once it says it is listening.
func signalChild(t *testing.T, sleep string) (*exec.Cmd, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "calls.log")
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		envTrap+"=1",
		envLog+"="+log,
		envSleep+"="+sleep,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("ready\n"))
	if _, err := stdout.Read(buf); err != nil {
		t.Fatal(err)
	}
	return cmd, log
}

func TestTrapTearsDownOnASignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX signal to send to another process")
	}
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
		want int
	}{
		{"interrupt", syscall.SIGINT, 130},
		{"terminate", syscall.SIGTERM, 143},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, log := signalChild(t, "")
			if err := cmd.Process.Signal(tc.sig); err != nil {
				t.Fatal(err)
			}
			err := cmd.Wait()
			if got := cruntime.ExitCode(err); got != tc.want {
				t.Fatalf("a signalled capsule exited %d (%v), want %d", got, err, tc.want)
			}
			b, readErr := os.ReadFile(log)
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, want := range []string{
				"rm -f capsule-demo-abcd1234\n",
				"rm -f capsule-demo-abcd1234-db\n",
				"network rm capsule-demo-abcd1234-net\n",
			} {
				if !strings.Contains(string(b), want) {
					t.Errorf("a signalled capsule never ran %q:\n%s", want, b)
				}
			}
		})
	}
}

func TestASecondSignalIsIgnoredWhileTearingDown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX signal to send to another process")
	}
	// The handler runs once and then exits, and the notify channel is still
	// installed while it works, so a second Ctrl-C is buffered and never read.
	// Impatience does not abandon the cleanup half-done: teardown finishes and
	// the status is still the first signal's.
	cmd, log := signalChild(t, "300ms")
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	if got := cruntime.ExitCode(err); got != 130 {
		t.Fatalf("a twice-signalled capsule exited %d (%v), want 130", got, err)
	}
	b, readErr := os.ReadFile(log)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(b), "network rm capsule-demo-abcd1234-net") {
		t.Errorf("a second signal cut the teardown short:\n%s", b)
	}
}

func TestStartCreatesTheNetworkThenEveryService(t *testing.T) {
	s, calls := fake(t, "")
	s.c.Services = []config.Service{
		{Name: "db", Image: "postgres:14", Timeout: time.Minute},
		{Name: "cache", Image: "redis:7", Timeout: time.Minute},
	}

	out := captureStderr(t, func() {
		if err := s.start(); err != nil {
			t.Errorf("start: %v", err)
		}
	})

	got := calls()
	if len(got) < 3 || !strings.HasPrefix(got[0], "network create") {
		t.Fatalf("start ran %v, want the network created first", got)
	}
	if s.network == "" || len(s.started) != 2 {
		t.Errorf("start recorded network %q and %v, want both services and a network", s.network, s.started)
	}
	// Neither service declares a `ready` check, so capsule says what it did
	// not check rather than calling a started container ready.
	if !strings.Contains(out, "declares no `ready` check") {
		t.Errorf("start printed %q, want it to admit the unchecked service", out)
	}
}

func TestStartRecordsAServiceBeforeItCouldFail(t *testing.T) {
	// A `run` that fails can still have left a container behind, so the name
	// has to be recorded first. This is what stops a half-started stack
	// leaking: teardown can only remove what start told it about.
	s, calls := fake(t, "run -d --rm --name capsule-demo-abcd1234-db")
	s.c.Services = []config.Service{{Name: "db", Image: "postgres:14", Timeout: time.Minute}}

	err := captureErr(t, s.start)
	if err == nil {
		t.Fatal("start succeeded with a service that would not run")
	}
	if len(s.started) != 1 {
		t.Fatalf("start recorded %v, want the service it tried to run", s.started)
	}
	s.teardown()
	if !strings.Contains(strings.Join(calls(), "\n"), "rm -f capsule-demo-abcd1234-db") {
		t.Errorf("teardown never removed the service start failed on:\n%v", calls())
	}
}

func TestWaitSaysWhenAServiceDiedBeforeItWasReady(t *testing.T) {
	// An inspect that fails means the runtime has no record of the container:
	// it ran with --rm and took its logs with it. capsule has to say that and
	// hand back a command that shows why, rather than waiting out the timeout.
	s, _ := fake(t, "inspect,logs")
	s.c.Services = []config.Service{{Name: "db", Image: "postgres:14", Ready: "pg_isready", Timeout: time.Minute}}

	err := captureErr(t, s.start)
	if err == nil {
		t.Fatal("start succeeded with a service that was gone")
	}
	for _, want := range []string{"stopped before it was ready", "already gone", "postgres:14"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("start error %q, want it to mention %q", err, want)
		}
	}
}

func TestWaitTimesOutNamingTheProbeAndQuotingTheLogs(t *testing.T) {
	s, _ := fake(t, "exec capsule-demo-abcd1234-db")
	s.c.Services = []config.Service{{Name: "db", Image: "postgres:14", Ready: "pg_isready -U app"}}

	err := captureErr(t, s.start)
	if err == nil {
		t.Fatal("start succeeded with a probe that never passed")
	}
	for _, want := range []string{"was not ready within", "pg_isready -U app", "Last output from db"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("start error %q, want it to mention %q", err, want)
		}
	}
}
