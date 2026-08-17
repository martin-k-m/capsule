# What crosses a capsule's boundary

The README says a capsule is "not isolation from the host". This document is
what that sentence means, measured rather than reasoned about.

Every answer below came from running a command inside a real capsule and writing
down what came back. The script is
[`bench/probe-isolation.ps1`](../bench/probe-isolation.ps1) and its raw output is
[`bench/results/isolation.txt`](../bench/results/isolation.txt). Re-run it with:

```powershell
powershell -ExecutionPolicy Bypass -File bench\probe-isolation.ps1
```

## Measured on

| | |
| :-- | :-- |
| capsule | 1.1.0, built from `harden/evidence` |
| Image | `alpine:3.20` |
| Runtime | Docker 29.6.2, server `linux/amd64` |
| Host | Windows 11 Pro build 26200, Docker Desktop on a WSL2 VM |
| Kernel seen from inside | `6.18.33.2-microsoft-standard-WSL2` |

**This matters for two answers and is called out where it does.** On Windows the
project bind mount goes through a 9p filesystem that maps all ownership to the
host user, which is not how a Linux bind mount behaves. Everything else here is
a property of the argv capsule builds and holds anywhere.

## Summary

| Question | Answer |
| :-- | :-- |
| Do the host's environment variables reach the capsule? | **No** |
| Can the capsule write to your project? | **Yes**, the whole directory, including `.git` |
| Can the capsule see any host path other than the project? | **No** |
| Is the Docker socket exposed? | **No** |
| Can the capsule reach the internet? | **Yes**, unrestricted |
| Can the capsule reach services on your machine? | **Yes**, via `host.docker.internal` |
| Does the capsule run as root? | **Yes**, uid 0 |
| Are capabilities dropped beyond the runtime's default? | **No** |
| Is a seccomp filter applied? | **Yes**, the runtime's default |
| Is `no-new-privileges` set? | **No** |
| Does the capsule share your kernel? | **Yes** |
| Does anything survive the capsule? | Only the project directory and `[persist]` volumes |

## What does not leak

**Host environment variables.** A secret exported in the shell that ran
`capsule up` does not appear inside. The probe set
`CAPSULE_PROBE_SECRET=host-secret-do-not-leak` on the host and counted it inside:

```
$ capsule up -- sh -c 'env | grep -c CAPSULE_PROBE_SECRET || echo 0'
0
```

The full environment inside a capsule with no `[env]` table is five variables,
all of them the image's or the runtime's:

```
HOME=/root
HOSTNAME=bench
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
PWD=/workspace
SHLVL=2
```

This is worth stating because it is not the default for every tool in this
space, and because it is structural: `RunArgs` emits one `-e` per key in
`[env]` and has no path that forwards the ambient environment. A capsule sees
exactly what the `capsule.toml` names.

**Host paths other than the project.** One bind mount reaches the container, and
`/proc/mounts` inside confirms it is the only one:

```
C:\134 /workspace 9p rw,noatime,aname=drvfs;path=C:\;...
```

There is no `/mnt/host`, no `/var/run/docker.sock`, no home directory, no SSH
agent socket, no cloud credentials directory. `ls /` shows the image's own root
plus `workspace`.

**The Docker socket.** Absent. This is the difference between capsule the native
binary and capsule the published image: the image needs the socket mounted to do
anything, which the README already flags as root-equivalent control of the host.
The native binary passes no socket into anything it starts.

**Host processes.** The PID namespace is the container's own. `ps -ef` inside a
capsule sees three processes: its own shell, the `ps`, and the header.

**Host loopback.** `127.0.0.1` inside a capsule is the container's loopback, not
the host's. A listener on a host port is not reachable that way.

## What crosses

**Your project, read-write.** This is the design, not a leak. The directory
holding `capsule.toml` is bind-mounted at `workdir` with no `:ro`, so anything
running in the capsule can read, modify and delete any file in your project,
including `.git`. A host file created before the capsule started was both
readable and writable from inside.

**The network, entirely.** A capsule gets the runtime's default bridge: DNS
resolves, and an outbound TCP connection to a public address succeeds.

```
$ capsule up -- sh -c 'nslookup example.com >/dev/null 2>&1 && echo RESOLVES || echo no-dns'
RESOLVES
$ capsule up -- sh -c 'nc -z -w 5 1.1.1.1 443 && echo CONNECTS || echo blocked'
CONNECTS
```

**Services on your machine.** A capsule reaches the host through the runtime's
gateway name. The probe started a real TCP listener on the host and connected to
it from inside:

```
$ capsule up -- sh -c 'nc -z -w 3 host.docker.internal 57259 && echo REACHES-HOST || echo blocked'
REACHES-HOST
```

So a database on your laptop, a metadata service, a local admin interface bound
to all interfaces, all of them are addressable from inside a capsule. Nothing
capsule does opens this and nothing capsule does closes it. It is the runtime's
default and capsule passes no `--network` for a capsule with no `[services]`.

**Your kernel.** `uname -a` inside reports the host's kernel, as the README says.
A kernel vulnerability is not contained by a capsule.

## Where a capsule is genuinely weak

These are real, they are capsule's own defaults rather than the runtime's
inevitabilities, and each one is a flag capsule chooses not to pass.

**It runs as root.** `id` inside a capsule is `uid=0(root) gid=0(root)`. capsule
emits no `--user`, so the container's own default user applies and for most base
images that is root. Two consequences:

1. Anything that escapes the container's confinement does so from uid 0.
2. On a **Linux** host, files a capsule creates in your project arrive owned by
   root, and you need `sudo` to delete your own build output. This is the
   classic bind-mount papercut and capsule does nothing to avoid it.

   The measurement here did **not** reproduce that, because it ran on Windows,
   where the project reaches the container over 9p with `uid=0;gid=0` mapping and
   everything lands back on the host owned by the host user:

   ```
   made-inside.txt  owner=MARTIN_LAPTOP\comma
   ```

   That result is a property of Docker Desktop's filesystem, not of capsule, and
   it should not be read as evidence that capsule handles uid mapping. It does
   not.

   The Linux half is no longer a prediction. The nightly workflow asserts it and
   had never executed, so on 2026-08-17 the same probe was run by hand inside a
   `docker:27-dind` container, which is a Linux host with a real bind mount and
   its own daemon:

   ```
   uid=0(root) gid=0(root)
   -rw-r--r--  1 0  0  0  /tmp/probe/made-inside
   ```

   Root-owned, as stated. The other three probes in that workflow, the host
   environment, the docker socket and the leftover check, passed there too.

**Capabilities are not dropped.** The measured bounding set is
`00000000a80425fb`, which is exactly the runtime's default fourteen: `CHOWN`,
`DAC_OVERRIDE`, `FOWNER`, `FSETID`, `KILL`, `SETGID`, `SETUID`, `SETPCAP`,
`NET_BIND_SERVICE`, `NET_RAW`, `SYS_CHROOT`, `MKNOD`, `AUDIT_WRITE`, `SETFCAP`.
`CAP_SYS_ADMIN`, `CAP_SYS_PTRACE`, `CAP_SYS_MODULE` and `CAP_NET_ADMIN` are all
absent, which is the important part. But capsule passes no `--cap-drop`, so a
capsule keeps everything the runtime hands it. `mknod` inside a capsule
succeeds.

**`no-new-privileges` is not set.** `NoNewPrivs: 0`. A setuid binary in the base
image can still raise privileges. Since a capsule already runs as root this
changes little in the default case, but it would matter the moment a `--user`
option existed, and it is a one-flag hardening capsule does not take.

**A seccomp filter is applied**, `Seccomp: 2`, but it is the runtime's default
profile rather than anything capsule chose. Run under a runtime configured with
`--security-opt seccomp=unconfined` and capsule will not notice or object.

## The one promise held

After twenty-five capsules had run in the same project, started shells, written
files, and in one case created a device node, the host was left with exactly the
project directory and nothing else:

```
capsule.toml     owner=MARTIN_LAPTOP\comma
host-file.txt    owner=MARTIN_LAPTOP\comma
made-inside.txt  owner=MARTIN_LAPTOP\comma
```

`made-inside.txt` is there because a probe wrote it into `/workspace`, which is
the project and is supposed to survive. No capsule container remained:

```
containers capsule left running:
  (none)
```

## What this adds up to

A capsule is a clean, disposable environment. It does not carry your shell's
secrets, it does not mount your home directory, and it does not hand out the
Docker socket. It is not a boundary you should put hostile code behind: that code
would run as root, on your kernel, with your project writable and your network
reachable.

The README's sentence is accurate and this document exists so it does not have to
be taken on faith.
