# Trusted-Team POC Readiness

Last updated: 2026-08-16

TestTender is ready for a deliberately narrow proof of concept, not for a
shared or production service. The acceptable POC topology is one TestTender
Deployment and its test runner Jobs in one tenant's existing dev/test
namespace. The central CI system may submit those Jobs, but the Docker API
Service remains cluster-local.

## Required boundaries

- one trusted tenant team and one controlled pipeline identity
- no mutually untrusted or overlapping runs until ownership enforcement exists
- a deny-by-default image policy that rewrites to the internal registry
- a pinned internally scanned TestTender image, not `latest`
- exact `--allowed-secrets` entries managed with the deployment configuration
- namespace ResourceQuota and LimitRange sized for the selected test modules
- the supplied worker service account with token automount disabled
- TestTender's control Role must retain no Secret permissions
- cleanup after every pipeline and a short `--reapmax` safety window
- no ingress for the Docker API; clients use `tcp://testtender:2475` in-cluster
- runner Jobs carry `testtender.io/client=true`; Tekton PipelineRun Pods are
  admitted by the supplied NetworkPolicy

The current local k3d cluster accepted the NetworkPolicy resource but did not
block an unlabeled probe. This is evidence that manifest installation alone is
not an access-control claim. The target tenant CNI must prove the negative case
before the policy is treated as a boundary.

## POC acceptance checklist

- [ ] Install TestTender through the tenant's normal Argo CD path.
- [ ] Pin the TestTender image by digest.
- [ ] Configure and verify internal image rewrites with public egress disabled.
- [ ] Configure only the namespace-local Secret names required by the tests.
- [ ] Confirm the control service account cannot read Secrets.
- [ ] Run PostgreSQL or Redis through the real pipeline and protocol client.
- [ ] Run one representative multi-container test using a network alias.
- [ ] Verify a denied image fails before a worker Pod is created.
- [ ] Verify a non-allowlisted Secret reference fails before a worker Pod is created.
- [ ] Prove an unlabeled Pod cannot reach port 2475 on the target tenant CNI.
- [ ] Verify success, test failure and cancelled-pipeline cleanup.
- [ ] Record peak CPU, memory and ephemeral storage for quota sizing.

Run the supplied positive and negative NetworkPolicy probe with an internally
mirrored image that contains `curl` and `sleep`:

```bash
K8S_CONTEXT=tenant-cluster \
K8S_NAMESPACE=tenant-dev \
K8S_NETWORK_PROBE_IMAGE=registry.internal/tools/curl:approved \
./it/network-policy-smoke/run.sh
```

The check is successful only when the labeled Pod reaches `/_ping` and the
otherwise identical unlabeled Pod cannot connect. It deletes only the two
temporary probe Pods that it creates.

## Explicitly deferred

OIDC caller authentication, per-run ownership, concurrent-run alias isolation,
application-enforced resource ceilings and restart reconciliation are not
complete. Until they are, this POC must not expose one
TestTender instance to mutually untrusted callers or treat it as a durable
shared service.
