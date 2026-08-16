# Testcontainers Kubernetes Baseline — 2026-08-16

This is the first retained compatibility baseline for the Kubedock-derived
TestTender implementation. It measures the Kubernetes-only product path; it is
not a claim of complete Docker Engine compatibility.

## Test environment

- TestTender image ID:
  `sha256:5b7c540b269d1f57a34975b84b7808c2596fb3c7df0f70e9ea118776050f3e4b`
- Workspace HEAD: `912ede6b0de6673858d8c580779d63fd5e78c018`
  with an uncommitted fork import based on Kubedock
  `9e43ee7c8fd013f79765060d6f02a55bb2ed96f7`
- Testcontainers Java: tag `1.21.3`, commit
  `bebbb2c373e15e41e2faaa78632c85dc0f87b899`
- Oracle runner: Testcontainers Java `2.0.5`
- k3d `v5.8.3`, k3s `v1.31.5+k3s1`, two nodes
- Docker `29.7.2` was used only to build/export images into k3d containerd.
  No Docker-in-Docker daemon or Docker runtime backend was used.
- Test runners and all test containers ran as Kubernetes Pods in
  `testtender-it`. Runners used
  `DOCKER_HOST=tcp://testtender.testtender-it.svc.cluster.local:2475`.

The TestTender control Pod ran non-root with a read-only root filesystem,
RuntimeDefault seccomp, all capabilities dropped, and a 512 MiB memory limit.
Workers used RuntimeDefault seccomp, `allowPrivilegeEscalation: false`, and no
mounted service-account token. The test deployment enabled pre-start archives,
the reverse TCP proxy, and a headless Service. The image policy was deliberately
set to allow all images for compatibility measurement; this is not the intended
restricted-environment policy.

## Pinned upstream core result

The runner executed these unmodified upstream Testcontainers Java classes via
`:testcontainers:test --rerun-tasks --max-workers=1 --no-daemon`:

| Class | Passed | Failed | Skipped | Total |
|---|---:|---:|---:|---:|
| `ContainerStateTest` | 6 | 0 | 0 | 6 |
| `GenericContainerTest` | 6 | 6 | 2 | 14 |
| `NetworkTest` | 1 | 4 | 0 | 5 |
| `ContainerLogsTest` | 1 | 3 | 1 | 5 |
| `CopyFileToContainerTest` | 4 | 0 | 0 | 4 |
| `ExecInContainerTest` | 1 | 3 | 0 | 4 |
| `FileOperationsTest` | 7 | 1 | 0 | 8 |
| **Total** | **26** | **17** | **3** | **46** |

Confirmed paths include all six port-binding cases, long-running container
logs, ordinary exec, pre-start classpath files, directory copies, live archive
upload/download, missing-file behavior, and a 1.0 GiB live file upload.

The failures group into a small set of implementation gaps:

- Pods that run and exit immediately are not represented as successful
  one-shot Docker containers. This affects exit-code diagnostics, short-lived
  log tests, and copy-from-exited-container.
- Docker network driver/options are modeled as the Kubernetes `bridge`
  abstraction, and two-container alias communication does not yet work.
- Exec accepts the base command but does not honor per-exec user, working
  directory, or environment overrides.
- Two `GenericContainerTest` port-publication/wait expectations still need
  focused diagnosis even though all `ContainerStateTest` port cases and the
  database mapped-port smokes pass.

## Module smokes

| Module | Version / image | Result |
|---|---|---|
| PostgreSQL | Testcontainers `1.21.3`, `postgres:14.5-alpine` (`aac01494762a1`) | Passed a real `SELECT 1` over the mapped port. |
| Redis | Testcontainers `1.21.3`, `redis:7-alpine` (`2a51817f79255`) | Passed the mapped-port wait strategy. |
| Oracle Free | Testcontainers `2.0.5`, `gvenzl/oracle-free:23-slim-faststart` (`33afa4329053f`) | Passed JDBC `SELECT 40 + 2 FROM DUAL`; lifecycle 22.536 s. |

## Reliability finding fixed during the run

The initial combined run exposed an OOM kill when upstream copied a roughly
1 GiB file. Raw request/response logging middleware buffered every payload even
with verbose logging disabled, and the archive route buffered live uploads a
second time. The baseline image above includes these fixes:

- request/response logging records bounded metadata only and never raw headers,
  bodies, container logs, archives, credentials, or test secrets;
- live archive uploads stream into Kubernetes exec rather than being retained
  in control-plane memory;
- Kubernetes exec prefers WebSocket streaming and falls back to legacy SPDY;
- the namespace Role permits the narrowly required `get` and `create` verbs on
  `pods/exec`.

The 1 GiB upstream case then passed. TestTender stayed near 9 MiB in the observed
under-load sample and did not restart. Archive downloads still use a complete
in-memory buffer and therefore remain a resource-governance follow-up.

## Interpretation

The baseline is strong enough to preserve the fork and iterate: databases,
mapped TCP ports, ordinary exec, and most archive workflows are already useful.
It is not yet a release gate. One-shot lifecycle state, network aliases, exec
overrides, archive/download bounds, and broader module coverage are the next
compatibility priorities.

## Lifecycle iteration result

The first implementation slice after the import corrected completed-Pod state:
terminated workloads are retained, non-zero exits are no longer mistaken for
Kubernetes deployment failures, and kubelet exit code, OOM state, message, and
start/finish timestamps are returned through inspect and wait. The Go unit and
race suites passed before the Kubernetes rerun.

The same pinned selection was then rerun on a fresh two-node k3d cluster using
TestTender image `sha256:bdd5b1b0351069f97e3891ccc8e030ba111b2e44c6aa41394d5a283ed6b9b4a7`.
The first command accidentally used the wrong package for
`ExecInContainerTest`; the correctly qualified four tests were run immediately
afterward against the same deployment. Combined results are:

| Class | Passed | Failed | Skipped | Total |
|---|---:|---:|---:|---:|
| `ContainerStateTest` | 6 | 0 | 0 | 6 |
| `GenericContainerTest` | 10 | 2 | 2 | 14 |
| `NetworkTest` | 1 | 4 | 0 | 5 |
| `ContainerLogsTest` | 3 | 1 | 1 | 5 |
| `CopyFileToContainerTest` | 4 | 0 | 0 | 4 |
| `ExecInContainerTest` | 1 | 3 | 0 | 4 |
| `FileOperationsTest` | 7 | 1 | 0 | 8 |
| **Total** | **32** | **11** | **3** | **46** |

This is six additional passes with six fewer failures. The remaining eleven
failures are now sharply classified:

- two `GenericContainerTest` cases construct an image with
  `ImageFromDockerfile` and fail at the intentionally unsupported build API;
  their port assertions never execute;
- four network cases cover unsupported Docker driver/options fidelity and
  two-container alias communication;
- three exec cases cover per-exec user, working directory, and environment;
- one log case requests stderr separately, but the Kubernetes Pod log API
  exposes a merged stream;
- one archive case copies from the root filesystem of a terminated container,
  which Kubernetes no longer exposes through `pods/exec`.

The runner now accepts `UPSTREAM_TC_EXPECTED_TESTS` so retained baselines can
fail if Gradle reports an unexpected selected-test count. Its bundled offline
cache is still missing `slf4j-api:1.7.30` and `commons-compress:1.21`; the rerun
therefore used networked dependency resolution and the offline runner remains
an explicit restricted-environment follow-up.

## Exec-context iteration result

The next slice translated Docker exec environment and working-directory
requests into validated Kubernetes exec commands. Per-exec user selection uses
an unprivileged helper already present in the approved image (`gosu`, `su-exec`,
`setpriv`, or `runuser`) and fails with exit 126 when an image cannot safely
switch identity. The pinned `redis:6-alpine` test image uses `setpriv`.

After the Go unit and race suites passed, the complete, correctly qualified
46-test selection was rerun on image
`sha256:b6acfb95233325a4f77ad285866cd1a3f16cd609ee9d9382dc08a9110e673579`:

| Class | Passed | Failed | Skipped | Total |
|---|---:|---:|---:|---:|
| `ContainerStateTest` | 6 | 0 | 0 | 6 |
| `GenericContainerTest` | 10 | 2 | 2 | 14 |
| `NetworkTest` | 1 | 4 | 0 | 5 |
| `ContainerLogsTest` | 3 | 1 | 1 | 5 |
| `CopyFileToContainerTest` | 4 | 0 | 0 | 4 |
| `ExecInContainerTest` | 4 | 0 | 0 | 4 |
| `FileOperationsTest` | 7 | 1 | 0 | 8 |
| **Total** | **35** | **8** | **3** | **46** |

The current result is nine additional passes and nine fewer failures than the
26/17/3 import baseline. The remaining eight failures are exactly the two
out-of-scope build-dependent cases, four network cases, one separate-stderr
case, and one copy-from-terminated-root-filesystem case described above.

## Network iteration result

The third slice creates a portless headless Kubernetes Service for a network
alias when the client has not declared any exposed ports. Kubernetes DNS then
resolves the alias directly to the selected Pod, allowing arbitrary
container-to-container ports without inventing Docker port metadata. Requested
Docker network drivers/options are retained as API compatibility metadata but
do not select or reconfigure the cluster CNI.

All five pinned `NetworkTest` cases passed. After another clean Go unit/race
run, the exact 46-test selection was rerun on image
`sha256:4f13235fca70d3b0a5a411cb83001c9b771e550edabfe2367af70be18638482d`:

| Class | Passed | Failed | Skipped | Total |
|---|---:|---:|---:|---:|
| `ContainerStateTest` | 6 | 0 | 0 | 6 |
| `GenericContainerTest` | 10 | 2 | 2 | 14 |
| `NetworkTest` | 5 | 0 | 0 | 5 |
| `ContainerLogsTest` | 3 | 1 | 1 | 5 |
| `CopyFileToContainerTest` | 4 | 0 | 0 | 4 |
| `ExecInContainerTest` | 4 | 0 | 0 | 4 |
| `FileOperationsTest` | 7 | 1 | 0 | 8 |
| **Total** | **39** | **4** | **3** | **46** |

The four remaining failures are two intentional build-API exclusions, one
separate-stderr request that Kubernetes Pod logs cannot represent, and one
copy from a terminated container root filesystem. Alias Service names are
namespace-global; two simultaneous runs reusing an alias remain an ownership
and isolation acceptance target rather than a completed multi-run guarantee.
