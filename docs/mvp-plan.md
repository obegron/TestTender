# TestTender MVP Plan

Last updated: 2026-08-16

> **Legacy plan:** The implementation direction in this document is superseded
> by [`kubedock-fork-plan.md`](kubedock-fork-plan.md). Retain the acceptance and
> security requirements as evidence; do not continue expanding the custom
> Docker API/runtime implementation.

This plan takes TestTender from an early Kubernetes-focused Docker API shim to a
usable on-premises Testcontainers service. Work is ordered by release risk, not
by implementation convenience.

## Current Baseline

The repository already has:

- a Kubernetes worker-Pod backend and experimental PRoot fallback
- core create/start/inspect/logs/wait/stop/delete behavior
- dynamic TCP port forwarding through a headless Service
- non-root worker security defaults and namespace-scoped RBAC
- optional strict mTLS and certificate-scoped API resource ownership
- generated per-owner worker ingress NetworkPolicies
- Redis, PostgreSQL, Kafka, MockServer, LDAP and Oracle Kubernetes evidence
- unit, race, Java smoke and reusable k3d integration tooling

Confirmed compatibility and security claims remain limited to the evidence in
`docs/compatibility-matrix.md`.

## Ongoing Code Health

These changes support the MVP phases but do not replace acceptance evidence.

- [x] Remove unused store compatibility aliases left by the naming migration.
- [x] Separate generic HTTP helpers from filesystem utilities.
- [x] Separate Kubernetes Pod-volume construction and runtime-state translation
      from API transport.
- [x] Move stop, kill and delete behavior into a focused container-control file.
- [ ] Split the remaining container lifecycle handling into create and start
      files without changing route behavior.
- [x] Replace repeated start-failure/runtime-slot bookkeeping with one tested
      reservation lifecycle.
- [ ] Split archive handling into HTTP orchestration, local tar/filesystem logic
      and Kubernetes synchronization.
- [ ] Move image-specific Cassandra and Kafka archive rewrites behind explicit
      compatibility helpers.
- [ ] Reduce untyped Kubernetes Pod-spec maps where typed internal structures
      would make invalid combinations harder to construct.

Code-health work should remain behavior-preserving unless its plan item includes
a regression test for the behavior being changed. Run the full unit and race
suite after each slice rather than accumulating a large refactor.

## Phase 1 — Kubernetes Runtime Reliability

- [ ] Reproduce the in-cluster Go Kubernetes API timeout on a healthy cluster.
- [ ] Determine whether the timeout is application transport, CNI, kube-proxy or
      local k3d behavior and add a regression test for the result.
- [ ] Run a clean installation with `--k8s-cleanup-orphans=true`.
- [ ] Verify startup remains bounded when the Kubernetes API is slow or absent.
- [ ] Define readiness behavior when TestTender cannot create worker Pods.
- [ ] Test restart recovery with running, completed, stopped and missing workers.
- [ ] Test orphan Pod and orphan owner-NetworkPolicy cleanup.
- [ ] Verify state recovery after an interrupted Docker daemon or k3d restart.

Exit criteria:

- TestTender reaches readiness predictably on a healthy cluster.
- API failure produces a bounded, actionable error rather than a hang.
- Restart reconciliation preserves correct container state and removes orphans.

## Phase 2 — Worker Network-Isolation Acceptance

- [ ] Select a CI test cluster whose CNI enforces standard NetworkPolicy.
- [ ] Verify a worker receives the expected opaque `testtender.owner-id` label.
- [ ] Verify the corresponding owner NetworkPolicy is created before the worker.
- [ ] Prove same-owner workers can communicate over a Testcontainers network.
- [ ] Prove cross-owner worker ingress is denied.
- [ ] Prove TestTender's mapped-port proxy can still reach every owned worker.
- [ ] Prove the final-container delete removes the owner policy.
- [ ] Prove restart reconciliation restores missing labels and policies.
- [ ] Audit namespace RBAC and admission controls against owner-label,
      control-plane-label and NetworkPolicy spoofing.

Exit criteria:

- A repeatable positive and negative connectivity suite passes on the supported
  CNI, and an untrusted client cannot assign itself a privileged label.

## Phase 3 — Testcontainers Compatibility MVP

- [ ] Implement reliable two-container communication using Testcontainers
      network aliases.
- [ ] Define behavior when a peer is added after another worker Pod has started.
- [ ] Complete non-interactive exec exit status, error and stream semantics.
- [x] Expand archive upload/download and startup-script coverage; the fork
      baseline passes pre-start files, directories, live upload/download and a
      1 GiB upload, with exited-container copy still tied to lifecycle work.
- [x] Run the pinned upstream core lifecycle suite and retain the measured
      39-pass/4-fail/3-skip post-lifecycle/exec/network baseline (the original
      26-pass/17-fail/3-skip import baseline remains in the evidence record).
- [ ] Confirm Kubernetes module runs for MySQL and MariaDB.
- [ ] Confirm Kubernetes module runs for MongoDB.
- [ ] Confirm Kubernetes module runs for RabbitMQ.
- [ ] Run the complete pinned Kafka module suite.
- [x] Revalidate PostgreSQL, Redis and Oracle with real protocol/JDBC checks.
- [ ] Revalidate MockServer and LDAP against the fork.
- [ ] Record Testcontainers version, image tag/digest, task, security profile and
      date for every confirmed result.

Exit criteria:

- The core suite and the selected MVP modules pass from ordinary in-cluster
  Testcontainers clients through the documented Service DNS endpoint.

## Phase 4 — Restricted and Offline Environment Support

- [ ] Define private-registry credential ownership and storage.
- [ ] Test worker `imagePullSecrets` end to end.
- [ ] Test an internal registry mirror with all internet egress disabled.
- [ ] Test custom corporate CA bundles for registry and proxy interception.
- [ ] Make every acceptance-test runner offline-complete.
- [ ] Add a diagnostic command covering DNS, Kubernetes API, registry, proxy and
      certificate failures.
- [ ] Define retry and error behavior for temporary DNS, registry and API loss.

Exit criteria:

- A fresh installation and the complete MVP suite pass without public internet
  access using only documented internal dependencies.

## Phase 5 — Resource Governance and Lifecycle Limits

- [ ] Add configurable default and maximum CPU requests/limits for workers.
- [ ] Add configurable memory requests/limits for ordinary workers.
- [ ] Add ephemeral-storage requests/limits and validate disk exhaustion.
- [ ] Add per-owner concurrent-container quotas.
- [ ] Add per-owner API rate limiting.
- [ ] Verify maximum runtime, log, archive and disk limits under failure.
- [ ] Define cleanup behavior for expired jobs and abandoned client identities.
- [ ] Ensure limits cannot be bypassed through concurrent create/start requests.

Exit criteria:

- One client cannot exhaust the runtime namespace or prevent another allowed
  client from cleaning up its resources.

## Phase 6 — Shared-Service Security Decision

- [ ] Decide whether image lists and cached image metadata are global or owned.
- [ ] Decide whether aggregate metrics may be visible to every authenticated
      client.
- [ ] Decide whether worker egress requires per-owner policy.
- [ ] Add audit events with owner ID, operation, resource ID and outcome.
- [ ] Document client certificate renewal, private-key rotation and CA rotation.
- [ ] Define handling for resources owned by a retired client key.
- [ ] Test certificate expiry and the chosen revocation mechanism.
- [ ] Run dependency, container-image and manifest security scans in CI.
- [ ] Complete a threat model for the Docker API, Kubernetes RBAC, state volume,
      image cache, port proxy and worker-to-worker traffic.

Exit criteria:

- Shared information is explicitly accepted or isolated, the threat model is
  reviewed, and two mutually untrusted CI identities pass all isolation tests.

## Phase 7 — Packaging, Upgrades and Operations

- [ ] Provide a supported Helm chart or Kustomize package.
- [ ] Pin release images instead of deploying `:dev`.
- [ ] Add TestTender control-plane resource requests and limits.
- [ ] Define a safe strategy for the single-replica ReadWriteOnce state volume.
- [ ] Document PVC backup, restore and corruption recovery.
- [ ] Test upgrades with persisted containers, networks and owner identities.
- [ ] Add dashboards and alerts for API errors, worker failures, reconciliation,
      quota rejection and policy failures.
- [ ] Publish SBOM, provenance, signatures and release notes.
- [ ] Define supported Kubernetes, CNI and Testcontainers version ranges.
- [ ] Write an operator installation and acceptance checklist.

Exit criteria:

- A pinned release can be installed, upgraded, monitored, backed up and rolled
  back using documented procedures.

## Release Gates

### Trusted-Team MVP

A single TestTender instance may be used by one trusted team when:

- [ ] Phase 1 is complete.
- [ ] Phase 3 core and selected module acceptance tests pass.
- [ ] Phase 4 passes in the target restricted environment.
- [ ] Worker resource limits from Phase 5 are enforced.
- [ ] A pinned deployment and basic recovery procedure exist.

This gate does not claim isolation between mutually untrusted teams.

### Shared-Service MVP

A TestTender instance may serve mutually untrusted CI identities only when:

- [ ] The Trusted-Team MVP gate is complete.
- [ ] Phase 2 is complete on the production CNI.
- [ ] Phase 5 per-owner quotas are complete.
- [ ] Phase 6 is complete and reviewed.
- [ ] Cross-owner API and worker-network negative tests run continuously in CI.

## Product Decisions to Record

- [ ] Confirm that multi-replica/high availability remains out of scope until
      the state model changes.
- [ ] Publish the supported Docker API contract and stable error behavior.
- [ ] List Docker features that are intentionally unsupported.
- [ ] Decide whether UDP publishing, Docker build, bind mounts, Compose and
      privileged containers remain out of scope.
- [ ] Define the compatibility and deprecation policy.

## Evidence Required for Completion

Every checked compatibility or security item should record:

- TestTender commit or release
- Kubernetes and CNI versions
- Testcontainers version and language
- container image tag and digest
- exact test target or command
- relevant security profile and feature flags
- pass/fail result and date
- links to retained logs or CI output
