# Kubedock Fork Plan

Last updated: 2026-08-17

## Decision

TestTender is being rebuilt as a full fork of
[Kubedock](https://github.com/joyrex2001/kubedock). Kubedock becomes the Docker
API and Kubernetes worker implementation; TestTender's differentiating work is
restricted-environment policy, tenant authentication and isolation, controlled
secret references, internal image resolution, and remote run orchestration.

The repository now contains the Kubedock-derived implementation. The previous
Apache-2.0 runtime is retained only in history and a local pre-fork snapshot.
Reintroduce a legacy feature only when it is required by this plan and has a
regression or acceptance test.

## Current baseline

The initial Kubernetes-only fork baseline is retained in
[`dev/testcontainers-baseline-2026-08-16.md`](dev/testcontainers-baseline-2026-08-16.md).
The imported upstream unit and race suites pass. The selected upstream
Testcontainers Java core baseline is 39 passed, 4 failed and 3 skipped after
correcting completed-Pod lifecycle state, exec context handling, and network
alias DNS; the
PostgreSQL, Redis and Oracle real-protocol smokes pass.

Low-level deployment, image policy, license/upstream attribution and the
Kubernetes-only product boundary are established. The two nominal remaining
port cases require the intentionally unsupported build API and are not port
regressions. The remaining selected failures are two build-API exclusions,
separate stderr selection, and archive access after workload termination.

## Trusted-Team POC Gate

The current tree is suitable for a controlled, non-production POC with one
trusted tenant team in its existing dev/test namespace. It now has
deny-by-default image rewriting, namespace-scoped worker RBAC, allowlisted
Secret env/file references resolved only by the kubelet, a Docker API
NetworkPolicy manifest, commit-SHA container tags, and retained PostgreSQL,
Redis, Oracle and multi-container network evidence. NetworkPolicy enforcement
is a target-CNI acceptance gate, not a portable property of the manifest. OIDC
now validates the central CI/CD Kubernetes issuer, a dedicated audience and
exact allowed CI/CD service-account namespaces without administrator roles. A
loopback-only client proxy supplies Bearer tokens to unmodified Testcontainers
clients; live pipeline transport remains an acceptance gate.

This is not approval for a shared service. Before the work POC starts, complete
the site-specific items in [`poc-readiness.md`](poc-readiness.md): pin an image,
configure internal mirrors and exact Secret names, apply namespace quotas, and
run representative success/failure/cancellation cleanup through the real
pipeline. Live bearer-token transport acceptance, ownership and concurrent-run
isolation remain the next gate before mutually untrusted or overlapping
pipelines use one instance.

## Product Boundary

The service exists to run short-lived Testcontainers workloads in a tenant's
existing Kubernetes dev/test namespace. It is not a general Docker daemon or a
container build service.

Intentionally unsupported:

- image build, load and push
- privileged containers, host paths, host networking and host namespaces
- arbitrary Kubernetes namespaces or service accounts supplied by clients
- arbitrary public registries or unapproved image sources
- production workloads and long-lived container hosting
- Docker Compose as a compatibility target

Not implementing image build and push is a security feature. Test runner and
fixture images must enter through the organization's existing build, scanning,
signing and registry process.

## Deployment Shape

The preferred topology has one namespace-scoped TestTender deployment in each
tenant's existing dev/test namespace. The integration-test runner also executes
in that namespace. A central CI control plane submits and observes a run, but it
does not carry the tenant's database, broker or test-runner compute.

```text
central CI control plane
        |
        | authenticated run request
        v
tenant dev/test namespace
        +-- TestTender control service (Kubedock fork)
        +-- ephemeral integration-test runner Job
        +-- Testcontainers worker Pods and Services
        +-- tenant-managed Secrets
```

Keeping the Testcontainers client beside its workers avoids exposing dynamic
database and broker ports across an ingress controller. The externally exposed
surface can remain a small authenticated control, status, log and result API.

This repository deliberately does not document organization-specific CI
namespaces, issuers, hostnames or secret backends.

## Authentication and Ownership

- Validate short-lived OIDC tokens with explicit issuer and audience checks.
- Authorize exact central CI/CD service-account namespaces or exact subjects;
  never infer an administrator role from a token.
- Bind run submission to an allowed tenant, namespace and CI service identity.
- Give each runner Job a projected service-account token with a TestTender-only
  audience for Docker API access inside the tenant cluster.
- Derive an immutable owner from tenant, run UID and runner identity.
- Inject the owner into every Pod, Service, ConfigMap, exec and network record.
- Filter list operations and reject inspect, logs, exec, archive, stop, kill and
  delete operations for resources owned by another run.
- Persist or reconstruct ownership well enough that a control-plane restart
  cannot make running resources unmanageable or cross-visible.

One deployment per tenant prevents cross-tenant access, but owner enforcement
is still required for concurrent runs belonging to the same tenant.

## Tenant Secret References

Tenants continue materializing the Secrets needed by tests in their existing
dev/test namespace. TestTender should translate a constrained Docker label or
placeholder into Kubernetes `secretKeyRef` or Secret volume references.

Example client intent:

```text
testtender.io/secret-env.DB_PASSWORD=integration-database:password
```

Required controls:

- the namespace is implicit and cannot be selected by the Docker client
- only Secrets explicitly marked for integration-test use may be referenced
- names, keys, mount locations and total count are validated and bounded
- secret values are never read, stored, returned or logged by TestTender
- the worker kubelet resolves the reference when starting the Pod
- worker service-account token automount remains disabled

This avoids introducing a separate service that reads and redistributes tenant
secret values.

## Image Policy and Mirror Resolution

All image names are normalized and authorized before a worker Pod is created.
An Argo CD-managed policy maps common Testcontainers names to approved internal
mirrors, for example:

```text
postgres:16 -> registry.internal/testcontainers/postgres:16
redis:7-alpine -> registry.internal/testcontainers/redis:7-alpine
```

The implementation must:

- normalize implicit Docker Hub and `library/` names before policy evaluation
- rewrite approved aliases to internal repositories
- reject unknown registries, direct registry IPs and disallowed tags
- support digest-pinned policy entries and record the resolved digest
- attach only configured namespace-local image pull secrets
- bound image pull time and return actionable policy or registry errors
- emit an audit event containing requested image, resolved image and outcome

Admission policy, node registry configuration and egress controls must enforce
the same boundary. The Docker API policy is not the only security layer.

## Kubernetes Security and Resource Governance

- namespace-scoped control-plane RBAC
- worker service account with no workload permissions and no mounted token
- RuntimeDefault seccomp, no privilege escalation and dropped capabilities
- deny hostPath, privileged mode, host networking, host PID/IPC and devices
- configurable CPU, memory and ephemeral-storage defaults and ceilings
- per-run and per-tenant concurrent worker limits
- active deadlines, bounded archive/log sizes and abandoned-run cleanup
- NetworkPolicy enforcement verified on the supported CNI
- no client-controlled policy labels, service accounts or node placement

Image-specific exceptions must be keyed to approved immutable image identities,
not accepted directly from Docker `HostConfig`.

## Compatibility and Acceptance

The imported Kubedock baseline must retain its upstream unit suite. TestTender's
acceptance suite additionally tracks:

- [x] Redis and PostgreSQL with real protocol/SQL operations
- [x] Oracle Free with a real JDBC query
- [ ] network aliases with two concurrent containers
- [ ] complete exec exit status and output semantics
- [x] archive upload and download, including a 1 GiB generated file
- [x] pre-start files and directory-copy behavior
- [ ] cleanup after success, test failure, client disappearance and restart
- [ ] two simultaneous runs using the same network aliases without collision
- [ ] positive and negative owner-authorization tests
- [ ] internal mirror operation with public internet egress disabled
- [x] allowlisted tenant Secret env references without TestTender reading values
- [x] allowlisted tenant Secret files mounted below `/run/secrets` without
      TestTender reading values

The Go race suite is a release gate. The inherited reverse-proxy data race must
be fixed before the first TestTender fork release.

## Upstream Relationship

Keep the Git history and configure the original repository as the `upstream`
remote. TestTender-specific changes should be separated into focused commits and
packages so upstream updates can be merged or rebased regularly.

Submit generally useful fixes upstream when practical, including correctness,
race, filtering and test improvements. Authentication, organization-specific
policy and remote-run orchestration can remain fork features unless upstream is
interested in generic extension points.

## Licensing Migration

Kubedock is distributed under the MIT License. A derivative must retain its
original copyright and MIT permission notice in copies or substantial portions.

The fork is MIT licensed because that is the clearest model for a complete
derivative and makes later upstream contributions straightforward:

- preserve Kubedock's original `LICENSE` text and copyright
- add the appropriate TestTender contributor copyright without removing the
  original notice
- identify Kubedock as the upstream project in the README and release metadata
- retain third-party notices and dependency license material from upstream
- mark materially changed files or releases through source history and release
  notes even though MIT does not require per-file change notices

The current TestTender tree uses the MIT Kubedock baseline and retains the
upstream notice. If any Apache-2.0 prototype code is copied into the fork, its
existing license and notices must remain unless every relevant copyright owner
has authorized relicensing. Prefer implementing required behavior anew against
the MIT baseline so the active tree keeps one clear license.

This section records the project's intended compliance approach, not legal
advice. Confirm the final copyright holder names and release packaging before
the first redistributed fork release.

## Migration Sequence

1. [x] Archive the final Apache-2.0 prototype state in Git history.
2. [x] Create the TestTender fork from Kubedock while preserving provenance.
3. [x] Keep Kubedock's MIT license and add explicit upstream attribution.
4. [x] Import the upstream unit suite and run the race suite.
5. [x] Establish the namespace-scoped deployment and in-namespace runner shape.
6. [x] Add image normalization, allowlisting and mirror rewriting.
7. [ ] Add projected-token authentication and per-run ownership enforcement.
8. [ ] Add constrained Kubernetes Secret references.
9. [ ] Add concurrency, network-isolation, restart and offline acceptance tests.
10. [ ] Publish the first fork release after the security and race gates pass.
