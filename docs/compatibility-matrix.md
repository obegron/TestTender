# Sidewhale Compatibility Matrix

Last updated: 2026-08-16

Sidewhale's product target is the Kubernetes backend. PRoot results are useful
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
| Container create/start/inspect/stop/delete | Confirmed | Repeated PostgreSQL/Kafka/MockServer/LDAP and custom k3d flows. |
| Image pull and inspect | Confirmed | Registry, digest, mirror, and local-import paths exercised. |
| Port publishing | Confirmed | Dynamic TCP proxying works through the headless Sidewhale Service. UDP is rejected. |
| Logs and follow | Confirmed | Kafka lifecycle and log-follow integration jobs pass. |
| Wait and exit state | Confirmed | Core lifecycle and failed/completed state paths exercised. |
| Archive HEAD/GET/PUT | Partial | Upload/copy paths and Testcontainers startup-script flow pass; broader upstream coverage remains desirable. |
| Exec | Partial | Non-interactive exec and exit status work; complete stdin/TTY/hijack parity is not implemented. |
| Networks API | Partial | API/state/aliases exist, but they do not provide dynamic Docker-network DNS semantics. |
| TLS and client authentication | Confirmed | Java Testcontainers Redis/PostgreSQL passed through strict mTLS in k3d, and an independent client certificate without access was rejected (2026-08-16). |
| Per-client resource ownership | Partial | API resources and peer discovery are certificate-scoped. Worker Pods receive opaque owner labels and same-owner ingress policies; CNI enforcement, shared images/metrics, and egress remain deployment concerns. |
| Cross-container alias DNS | Unverified | Static host aliases help known peers; peers added after Pod creation are not discoverable reliably. |
| Volumes and bind mounts | Out of scope | Host paths are deliberately not exposed. tmpfs/emptyDir and archive copy cover the MVP. |
| Image build / ImageFromDockerfile | Out of scope | No Docker build API in the MVP. |
| Docker Compose | Out of scope | Requires a substantially larger networking and orchestration surface. |

## Kubernetes Testcontainers Modules

| Module / area | Status | Current evidence / next step |
|---|---|---|
| Core client behavior | Partial | ContainerState-style port behavior and custom lifecycle flows pass; run the complete pinned k8s core suite regularly. |
| PostgreSQL | Confirmed | Java `PostgreSQLContainer` smoke passed in k3d through the headless Service and completed a real SQL query. |
| Redis | Confirmed | Java `GenericContainer` smoke passed in k3d through the headless Service and its mapped-port wait strategy. |
| MySQL / MariaDB | Unverified | Host module evidence exists; k8s module evidence needs refreshing. |
| MockServer | Confirmed | Upstream k8s module covered standard, TLS, mTLS, and wait-strategy paths. |
| LDAP (LLDAP) | Confirmed | Upstream k8s module covered default/custom bind and base-DN paths. |
| Kafka (single node) | Partial | Listener, log-stream, and upstream-shaped k3d flows pass; full pinned k8s module remains the acceptance target. |
| Kafka clusters | Unverified | Blocked on reliable per-network alias DNS and isolation. |
| Oracle Free | Confirmed | K8s-specific memory, user, and startup-probe handling has passed. |
| Cassandra | Unverified | PRoot runs were flaky; do not add more compatibility code until a k8s module run identifies real gaps. |
| MSSQL, Solr, Vault | Unverified | Existing confirmation is primarily host-backend evidence. |
| DB2 | Out of scope | Startup cost and resource use are poor fits for the current ephemeral model. |

## Next MVP Acceptance Targets

1. Confirm MySQL/MariaDB, MongoDB, RabbitMQ, and MockServer on pinned k8s runs.
2. Implement two-container communication through a Testcontainers network alias.
3. Add an automated CNI-capable cross-owner worker deny test before treating one instance as a multi-tenant boundary.
4. Complete non-interactive exec semantics before expanding image-specific compatibility code.
5. Record the Testcontainers version, image digest/tag, test task, security profile, and date for every `Confirmed` row.

## PRoot Fallback

The host backend remains useful where Docker and cgroups are unavailable. It has
strong single-container coverage across several modules, but it is not a security
boundary or the basis for Kubernetes compatibility claims. Image-specific PRoot
rewrites should stay isolated from the k8s execution path.
