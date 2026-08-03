package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/martin-k-m/capsule/internal/config"
	"github.com/martin-k-m/capsule/internal/runtime"
)

const (
	// readyPoll is how often a service's `ready` check is retried. Short enough
	// that a fast service does not visibly wait on capsule, long enough that a
	// slow one is not asked a hundred times a second.
	readyPoll = 500 * time.Millisecond

	// logTail is how much of a failed service's output capsule quotes back.
	logTail = 20
)

// stack is everything one `capsule up` creates besides the capsule itself: a
// network of its own, and one container per [services] entry.
//
// It exists because those are the only things capsule starts that a runtime
// will not clean up on its own. The capsule's container is removed by --rm
// whatever ends it, but a detached service keeps running until something
// removes it, so that something has to survive a service that never came up, a
// capsule that failed to start, and capsule being signalled halfway through.
type stack struct {
	rt *runtime.Runtime
	c  *config.Capsule
	id string

	// network is empty until it exists, so teardown never tries to remove a
	// network that was never created.
	network string

	// started names the service containers to remove. A name is recorded before
	// its container is started, because a `run` that fails partway can still
	// leave one behind, and teardown has to know about the containers capsule
	// only tried to start.
	started []string

	once sync.Once
}

// start creates the network and brings every service up, returning once they
// are all ready. Whatever it returns, teardown is what undoes it.
func (s *stack) start() error {
	if len(s.c.Services) == 0 {
		return nil
	}

	if _, err := s.rt.Output(runtime.NetworkCreateArgs(s.c, s.id)...); err != nil {
		return err
	}
	s.network = runtime.NetworkName(s.c.Name, s.id)

	for _, svc := range s.c.Services {
		s.started = append(s.started, runtime.ServiceContainerName(s.c.Name, s.id, svc.Name))
		fmt.Fprintf(os.Stderr, "capsule: starting %s (%s)\n", svc.Name, svc.Image)
		if _, err := s.rt.Output(runtime.ServiceRunArgs(s.c, svc, s.id)...); err != nil {
			return err
		}
	}

	// Every service is started before any of them is waited on, so two slow
	// images initialise at the same time rather than one after the other.
	for _, svc := range s.c.Services {
		if err := s.wait(svc); err != nil {
			return err
		}
	}
	return nil
}

// wait blocks until one service is ready, its container stops, or its timeout
// passes.
func (s *stack) wait(svc config.Service) error {
	container := runtime.ServiceContainerName(s.c.Name, s.id, svc.Name)

	if svc.Ready == "" {
		// Honest degradation: capsule will not call a service ready when all it
		// watched was a container start.
		fmt.Fprintf(os.Stderr, "capsule: %s declares no `ready` check, so capsule waits only for its container to start\n", svc.Name)
	} else {
		fmt.Fprintf(os.Stderr, "capsule: waiting for %s\n", svc.Name)
	}

	began := time.Now()
	deadline := began.Add(svc.Timeout)
	for {
		switch s.state(container) {
		case "running":
			if svc.Ready == "" {
				return nil
			}
			if s.rt.Has(runtime.ReadyArgs(container, svc.Ready)...) {
				fmt.Fprintf(os.Stderr, "capsule: %s ready in %s\n",
					svc.Name, time.Since(began).Round(100*time.Millisecond))
				return nil
			}
		case "created", "restarting", "paused":
			// Still on its way up.
		default:
			// Stopped, or removed by its own --rm. Either way it is not coming
			// back, and waiting out the timeout would only delay saying so.
			return fmt.Errorf("service %s stopped before it was ready. %s",
				svc.Name, s.explain(svc, container))
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("service %s was not ready within %s: `%s` never succeeded. %s",
				svc.Name, svc.Timeout, svc.Ready, s.explain(svc, container))
		}
		time.Sleep(readyPoll)
	}
}

// state is a service container's status word, or "gone" when the runtime has no
// record of it. A service runs with --rm like everything else capsule starts,
// so a crash removes the container as well as stopping it.
func (s *stack) state(container string) string {
	out, err := s.rt.Output(runtime.StateArgs(container)...)
	if err != nil {
		return "gone"
	}
	return strings.TrimSpace(out)
}

// explain says what capsule can still find out about a service that failed.
//
// One that is up but not answering still has its logs. One that crashed was
// removed with its logs inside it, so capsule hands back the command to watch
// it start by hand instead, which is the same command it just ran without the
// capsule-specific flags.
func (s *stack) explain(svc config.Service, container string) string {
	if out := strings.TrimSpace(s.rt.Combined(runtime.LogsArgs(container, logTail)...)); out != "" {
		return "Last output from " + svc.Name + ":\n" + indent(out)
	}
	return "Its container is already gone, and its logs with it. Start it by hand to see why:\n  " +
		runtime.Display(s.rt.Name(), runtime.ServiceDebugArgs(svc))
}

// teardown removes everything the stack created. It is safe to call more than
// once, because both the ordinary path and the signal handler have to be able
// to call it without checking whether the other one already did.
func (s *stack) teardown() {
	s.once.Do(func() {
		if s.network == "" && len(s.started) == 0 {
			return
		}

		// The capsule's own container carries --rm, so an ordinary exit has
		// already removed it by now. This is for the other ending: a signal,
		// where it is still running and still attached to the network below.
		s.rt.Has(runtime.RmArgs(runtime.ContainerName(s.c.Name, s.id))...)

		for _, name := range s.started {
			// A container that is not there is the outcome capsule wants, and
			// docker and podman disagree about whether removing one is an
			// error, so the status of this says nothing worth reporting.
			s.rt.Has(runtime.RmArgs(name)...)
		}

		if s.network == "" {
			return
		}
		// The network is the check on all of that. A runtime refuses to remove
		// one while a container is still attached to it, so this failing is how
		// capsule finds out that a removal above did not take.
		if _, err := s.rt.Output(runtime.NetworkRmArgs(s.network)...); err != nil {
			fmt.Fprintf(os.Stderr, "capsule: %v\n", err)
			fmt.Fprintf(os.Stderr, "capsule: something capsule started is still running, `capsule down` will clear it\n")
		}
	})
}

// trap tears the stack down if capsule is signalled, and returns the function
// that stops listening.
//
// It is installed only for a capsule with services. Without them there is
// nothing for a handler to do: --rm removes the container whatever happens to
// capsule, and catching a signal capsule has no use for would only change how a
// plain capsule behaves under Ctrl-C.
//
// A signal capsule cannot catch, or a machine that loses power, still leaves
// the containers running. That is what `capsule down` is for, and why the
// network and the services carry the same labels the capsule does.
func trap(s *stack) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		select {
		case sig := <-ch:
			fmt.Fprintf(os.Stderr, "\ncapsule: %s, tearing down\n", sig)
			s.teardown()
			// Exit rather than unwind: removing the capsule's container above
			// is what ends the run, and unwinding would mean waiting on the
			// very process the signal was meant to stop.
			os.Exit(signalStatus(sig))
		case <-done:
		}
	}()

	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// signalStatus is the conventional 128+N exit status for dying of a signal, so
// a `capsule up -- <cmd>` in a pipeline reports the interrupt the way any other
// command would.
func signalStatus(sig os.Signal) int {
	if sig == syscall.SIGTERM {
		return 143
	}
	return 130
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}
