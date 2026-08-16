# TestTender Compatibility Matrix

Last updated: 2026-08-16

> This remains the canonical compatibility matrix for the Kubedock-derived
> implementation. The first fork baseline is recorded in
> [`dev/testcontainers-baseline-2026-08-16.md`](dev/testcontainers-baseline-2026-08-16.md).
> Evidence that has not been rerun against the fork is explicitly marked
> unverified even when the Apache-2.0 prototype previously passed it.

TestTender's product target is the Kubernetes backend. PRoot results are useful
fallback evidence but do not establish Kubernetes compatibility.

Status definitions:

- `Confirmed`: a pinned upstream module/test or equivalent end-to-end k3d test passed.
- `Partial`: important paths pass, but the complete upstream module or behavior has gaps.
- `Unverified`: host/PRoot evidence exists, but current Kubernetes evidence is insufficient.
- `Out of scope`: intentionally excluded from the MVP.

## Kubernetes Docker API Behavior

| Area | Status | Current evidence / gap |
|---|---|---|
| Ping, version, info | Confirmed | Used by Java Testcontainers and all in-cluster jobs. |
| Container create/start/inspect/stop/delete | Confirmed | Long-running flows and immediately completed one-shot Pods pass the selected upstream lifecycle coverage. Terminated Pods are retained for inspect, wait, logs, and supported archive operations. |
| Image pull and inspect | Confirmed | Used successfully by the pinned upstream suite and PostgreSQL, Redis, and Oracle module smokes. Mirror/offline behavior is a separate acceptance target. |
| Port publishing | Confirmed | All six upstream `ContainerStateTest` cases and three database smokes pass through the headless Service. Two nominal port tests fail before their assertions because they require the out-of-scope build API. TCP is confirmed; UDP remains unsupported. |
| Logs and follow | Partial | Long-running and short-lived combined/stdout retrieval pass (3 pass, 1 fail, 1 skip). Kubernetes exposes merged Pod logs, so Docker's separate stderr selection remains open. |
| Wait and exit state | Confirmed | Immediate zero/non-zero completion, timestamps, OOM state, and real wait exit codes are represented from kubelet status. |
| Archive HEAD/GET/PUT | Partial | Pre-start copy is 4/4 and file operations are 7/8, including live upload/download, directories and a 1 GiB upload. Copy from an exited container and bounded download buffering remain open. |
| Exec | Partial | All four pinned non-interactive upstream cases pass: ordinary command, user, workdir, and environment. User switching requires a supported in-image helper and fails closed when unavailable; stdin/TTY/hijack parity is not implemented. |
| Networks API | Confirmed | All five pinned network cases pass. Requested driver/options are compatibility metadata only; Kubernetes CNI selection remains cluster-managed. Concurrent-run alias collision/isolation is a separate acceptance target. |
| TLS and client authentication | Unverified | Strict mTLS evidence belongs to the old prototype; authentication has not yet been reintroduced and rerun against the fork. |
| Per-client resource ownership | Unverified | Required by the fork plan but not yet implemented in the imported baseline. |
| Cross-container alias DNS | Confirmed | Both pinned two-container network cases pass through portless headless Services, including aliases for undeclared peer ports. |
| Volumes and bind mounts | Out of scope | Host paths are deliberately not exposed. tmpfs/emptyDir and archive copy cover the MVP. |
| Image build / ImageFromDockerfile | Out of scope | No Docker build API in the MVP. |
| Docker Compose | Out of scope | Requires a substantially larger networking and orchestration surface. |

## Kubernetes Testcontainers Modules

| Module / area | Status | Current evidence / next step |
|---|---|---|
| Core client behavior | Partial | Pinned Testcontainers Java `1.21.3`: 39 passed, 4 failed, 3 skipped across 46 selected upstream core tests after lifecycle, exec, and network slices. Two failures require the intentionally unsupported build API. |
| PostgreSQL | Confirmed | Testcontainers `1.21.3` `PostgreSQLContainer` passed a real `SELECT 1` through the headless Service. |
| Redis | Confirmed | Testcontainers `1.21.3` `GenericContainer` passed its mapped-port wait strategy. |
| MySQL / MariaDB | Unverified | Host module evidence exists; k8s module evidence needs refreshing. |
| MockServer | Unverified | Prototype evidence exists; rerun the pinned module against the fork. |
| LDAP (LLDAP) | Unverified | Prototype evidence exists; rerun the pinned module against the fork. |
| Kafka (single node) | Unverified | Prototype evidence exists; rerun after one-shot/log and network behavior is better defined. |
| Kafka clusters | Unverified | Blocked on reliable per-network alias DNS and isolation. |
| Oracle Free | Confirmed | Testcontainers `2.0.5` and `gvenzl/oracle-free:23-slim-faststart` passed a real JDBC query in 22.536 s. |
| Cassandra | Unverified | PRoot runs were flaky; do not add more compatibility code until a k8s module run identifies real gaps. |
| MSSQL, Solr, Vault | Unverified | Existing confirmation is primarily host-backend evidence. |
| DB2 | Out of scope | Startup cost and resource use are poor fits for the current ephemeral model. |

## Next MVP Acceptance Targets

1. Decide and document Kubernetes-appropriate behavior for separate stderr logs
   and archive access after workload termination.
2. Prove two simultaneous runs can reuse the same network aliases without
   collision or cross-run communication.
3. Bound archive downloads and logs, then confirm MySQL/MariaDB, MongoDB,
   RabbitMQ, and MockServer on pinned Kubernetes runs.
4. Add a CNI-capable cross-owner deny test before treating one instance as a
   multi-tenant boundary.

## PRoot Fallback

The host backend remains useful where Docker and cgroups are unavailable. It has
strong single-container coverage across several modules, but it is not a security
boundary or the basis for Kubernetes compatibility claims. Image-specific PRoot
rewrites should stay isolated from the k8s execution path.
