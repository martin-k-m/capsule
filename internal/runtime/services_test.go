package runtime

import (
	"strings"
	"testing"

	"github.com/martin-k-m/capsule/internal/config"
)

const withServices = `
image = "golang:1-alpine"

[services.db]
image   = "postgres:16"
ready   = "pg_isready -U postgres"
timeout = "90s"

[services.db.env]
POSTGRES_PASSWORD = "dev"

[services.cache]
image = "redis:7"
`

func TestServicesGetTheirOwnNetwork(t *testing.T) {
	c := parse(t, withServices)
	o := opts(c)

	capsule := joined(RunArgs(o))
	network := NetworkName(c.Name, o.ID)
	if !strings.Contains(capsule, "--network "+network) {
		t.Errorf("the capsule must join the network its services are on:\n%s", capsule)
	}
	for _, s := range c.Services {
		service := joined(ServiceRunArgs(c, s, o.ID))
		if !strings.Contains(service, "--network "+network) {
			t.Errorf("service %s is not on the capsule's network:\n%s", s.Name, service)
		}
		// The bare service name is the whole point: the capsule reaches
		// postgres at `db`, not at some generated container name.
		if !strings.Contains(service, "--network-alias "+s.Name) {
			t.Errorf("service %s must answer to its own name:\n%s", s.Name, service)
		}
	}
}

func TestACapsuleWithoutServicesGetsNoNetwork(t *testing.T) {
	// A plain capsule keeps the runtime's default networking. Creating a
	// network for it would be one more object to clean up for no benefit.
	if got := joined(RunArgs(opts(parse(t, `image = "alpine"`)))); strings.Contains(got, "--network") {
		t.Errorf("a capsule with no services should not be put on its own network:\n%s", got)
	}
	if steps := Plan(opts(parse(t, `image = "alpine"`))); len(steps) != 1 {
		t.Errorf("a capsule with no services should plan one command, got %d", len(steps))
	}
}

func TestTwoCapsulesOfOneProjectAreIsolated(t *testing.T) {
	c := parse(t, withServices)
	mine, theirs := opts(c), opts(c)
	mine.ID, theirs.ID = "aaaa1111", "bbbb2222"

	if NetworkName(c.Name, mine.ID) == NetworkName(c.Name, theirs.ID) {
		t.Fatal("two capsules of the same project must not share a network")
	}
	for _, s := range c.Services {
		if ServiceContainerName(c.Name, mine.ID, s.Name) == ServiceContainerName(c.Name, theirs.ID, s.Name) {
			t.Fatalf("service %s collides between two capsules of the same project", s.Name)
		}
	}
	// Same alias on both networks, which is what makes isolation the thing
	// keeping them apart rather than the naming.
	first := joined(ServiceRunArgs(c, c.Services[0], mine.ID))
	second := joined(ServiceRunArgs(c, c.Services[0], theirs.ID))
	if !strings.Contains(first, "--network-alias cache") || !strings.Contains(second, "--network-alias cache") {
		t.Error("both capsules should reach their own service under the same name")
	}
}

func TestServicesAreEphemeralAndLabelled(t *testing.T) {
	c := parse(t, withServices)
	args := ServiceRunArgs(c, c.Services[1], "id")

	if args[0] != "run" || args[1] != "-d" || args[2] != "--rm" {
		t.Fatalf("a service must be detached and ephemeral, got %v", args[:3])
	}
	got := joined(args)
	for _, want := range []string{
		"--label " + Label + "=1",
		"--label " + LabelName + "=proj",
		"--label " + LabelRole + "=" + RoleService,
		"--label " + LabelService + "=db",
		"-e POSTGRES_PASSWORD=dev",
		"postgres:16",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// A service is a dependency, not a second view of the project.
	if strings.Contains(got, "-v ") || strings.Contains(got, "-w ") {
		t.Errorf("nothing from the project should be mounted into a service:\n%s", got)
	}
	if !strings.Contains(joined(RunArgs(opts(c))), "--label "+LabelRole+"="+RoleCapsule) {
		t.Error("the capsule's own container must say that is what it is")
	}
}

func TestReadyCheckRunsInsideTheService(t *testing.T) {
	c := parse(t, withServices)
	got := joined(ReadyArgs(ServiceContainerName(c.Name, "id", "db"), c.Services[1].Ready))
	want := "exec capsule-proj-id-db " + config.DefaultShell + " -c pg_isready -U postgres"
	if got != want {
		t.Errorf("ready check = %q, want %q", got, want)
	}
}

func TestPlanIsTheWholeLifecycleInOrder(t *testing.T) {
	c := parse(t, withServices)
	o := opts(c)

	var lines []string
	for _, step := range Plan(o) {
		lines = append(lines, DisplayStep("docker", step))
	}
	got := strings.Join(lines, "\n")

	// The order is the feature: a network before the services on it, every
	// service before any wait, the capsule after they are ready, and teardown
	// of both after the capsule.
	want := []string{
		"docker network create",
		"docker run -d --rm --name capsule-proj-id-cache",
		"docker run -d --rm --name capsule-proj-id-db",
		"docker exec capsule-proj-id-db",
		"docker run --rm --name capsule-proj-id ",
		"docker rm -f capsule-proj-id-cache",
		"docker rm -f capsule-proj-id-db",
		"docker network rm capsule-proj-id-net",
	}
	at := -1
	for _, w := range want {
		i := strings.Index(got, w)
		if i < 0 {
			t.Fatalf("plan is missing %q:\n%s", w, got)
		}
		if i < at {
			t.Errorf("plan has %q out of order:\n%s", w, got)
		}
		at = i
	}
	// Every line has to survive being pasted into a shell, comments included.
	for _, l := range lines {
		if strings.Contains(l, "\n") {
			t.Errorf("a planned command spans lines: %q", l)
		}
	}
	if !strings.Contains(got, "giving up after 1m30s") {
		t.Errorf("the plan should say how long a wait is bounded by:\n%s", got)
	}
}

func TestPlanIsDeterministic(t *testing.T) {
	o := opts(parse(t, withServices))
	first := planText(o)
	for i := 0; i < 50; i++ {
		if got := planText(o); got != first {
			t.Fatalf("plan differs between calls:\n%s\n%s", first, got)
		}
	}
}

func planText(o RunOptions) string {
	var b strings.Builder
	for _, step := range Plan(o) {
		b.WriteString(DisplayStep("docker", step))
		b.WriteString("\n")
	}
	return b.String()
}

func TestServiceDebugArgsAreWhatAPersonWouldType(t *testing.T) {
	c := parse(t, withServices)
	// The command capsule prints when a service died and took its logs with it
	// has to be runnable, which means none of capsule's own bookkeeping.
	got := Display("docker", ServiceDebugArgs(c.Services[1]))
	if got != "docker run --rm -e POSTGRES_PASSWORD=dev postgres:16" {
		t.Errorf("debug command = %q", got)
	}
}

func TestNetworkIsLabelledForTheSweep(t *testing.T) {
	c := parse(t, withServices)
	create := joined(NetworkCreateArgs(c, "id"))
	for _, want := range []string{"label " + Label + "=1", "label " + LabelName + "=proj", "capsule-proj-id-net"} {
		if !strings.Contains(create, want) {
			t.Errorf("missing %q in %q", want, create)
		}
	}
	// `capsule down` finds a leaked network the same way it finds a leaked
	// container: by label, with no state file in between.
	ls := joined(NetworkLsArgs("proj"))
	if !strings.Contains(ls, "label="+Label+"=1") || !strings.Contains(ls, "label="+LabelName+"=proj") {
		t.Errorf("network sweep must filter by capsule's labels: %s", ls)
	}
}
