# TestTender

Policy-controlled Testcontainers on Kubernetes.

TestTender is a policy-focused Docker API compatibility service for running
short-lived Testcontainers workloads as Pods in Kubernetes. It is intended for
restricted, on-premises environments where tests may use approved container
images but cannot access a Docker daemon or public registries.

The implementation is a fork of
[Kubedock](https://github.com/joyrex2001/kubedock), currently based on upstream
commit [`9e43ee7`](https://github.com/joyrex2001/kubedock/commit/9e43ee7cd9009aaef70dcc4871c86f1f13911fd4).
See [UPSTREAM.md](UPSTREAM.md) for provenance and update guidance.

## Project status

The Kubedock-derived baseline is now the active implementation. It retains
upstream Docker and libpod compatibility while TestTender adds restricted-
environment controls. Exact image authorization and mirror rewriting,
namespace-local Secret references, strict OIDC caller authentication, and a
loopback-only token-injecting client proxy are implemented. Per-run ownership
remains under development.

This is not production ready. The current scope and release gates are tracked
in the [fork plan](docs/kubedock-fork-plan.md) and
[compatibility matrix](docs/compatibility-matrix.md).

## Product boundary

TestTender runs ephemeral integration-test dependencies in a tenant's existing
dev/test namespace. It intentionally does not provide image build, load or push,
privileged containers, host namespace access, arbitrary target namespaces, or
general production container hosting.

Docker-in-Docker is not included. Requests that mount `/var/run/docker.sock`
are rejected; all test dependencies are created as Kubernetes Pods.

## Build and test

The project currently requires Go 1.26 or newer.

```bash
make test
go test -race ./...
```

To run locally against the namespace in the current kubeconfig context:

```bash
go run . server --port-forward

export DOCKER_HOST=tcp://127.0.0.1:2475
export TESTCONTAINERS_RYUK_DISABLED=true
mvn test
```

The API service needs namespace-scoped permissions to manage Pods, Services and
ConfigMaps. See [config.md](config.md) for inherited runtime options.

The supplied manifest also restricts Docker API ingress to same-namespace Pods
labeled `testtender.io/client=true` and Tekton PipelineRun Pods. This requires a
CNI that enforces Kubernetes NetworkPolicy. Argo CD can consume `deploy/` as a
Kustomize base; use the commit-SHA image tag produced by CI for a POC rather
than `latest`. Before relying on that restriction, run
`it/network-policy-smoke/run.sh` against the target cluster as described in
`docs/poc-readiness.md`.

## Caller authentication

The supplied deployment fails closed until OIDC is configured. TestTender
validates one exact issuer, a dedicated audience, token lifetime, signature and
an exact allowlist of Kubernetes service-account subjects or CI/CD namespaces.
For the namespace shortcut, both the signed Kubernetes namespace claim and the
`system:serviceaccount:<namespace>:<name>` subject must agree. It grants no
administrator role.

The discovery base URL is configured separately from the issuer, allowing an
internal helper to expose a Kubernetes cluster's discovery and JWKS endpoints.
See `deploy/oidc-config.example.yaml` and [config.md](config.md); keep actual
issuer URLs and CI/CD namespace names in the private Argo CD configuration.

Because Docker clients do not share one portable custom-header mechanism,
`testtender client-proxy` can run beside an unmodified Testcontainers process.
It rereads a projected token for every request, injects the Bearer header and
forwards only to an HTTPS TestTender endpoint. Point `DOCKER_HOST` at its
loopback listener; see [config.md](config.md) for the command. When Traefik is
the upstream, the private Argo CD overlay must patch the NetworkPolicy with the
real ingress-controller namespace and Pod labels; the example patch is
`deploy/traefik-network-policy-patch.example.yaml`.

## Image policy

Pass `--image-policy-file` (or `IMAGE_POLICY_FILE`) to enable exact image
authorization and rewriting. A configured policy is loaded at startup and
invalid policy fails startup. With `defaultAction: deny`, unlisted images are
rejected before a Pod is created.

```json
{
  "version": "v1",
  "defaultAction": "deny",
  "rules": [
    {
      "source": "postgres:16",
      "target": "registry.internal/testcontainers/postgres:16"
    },
    {
      "source": "redis:7-alpine",
      "target": "registry.internal/testcontainers/redis:7-alpine"
    }
  ]
}
```

Familiar Docker names are normalized before matching, so `postgres:16` and
`docker.io/library/postgres:16` are the same source. Omitting the policy file
temporarily preserves upstream allow-all behavior and produces a startup
warning. The supplied Kubernetes manifest starts with a deny-all policy;
restricted deployments should replace it with their Argo-managed allowlist.

## Tenant Secret references

TestTender can inject keys from explicitly allowlisted, namespace-local Secrets
without reading their values. Configure exact names with `--allowed-secrets`
(or `ALLOWED_SECRETS`), then add a label to the Testcontainers request:

```java
withLabel(
    "testtender.io/secret-env.DB_PASSWORD",
    "integration-database:password"
)
```

The worker Pod receives a Kubernetes `secretKeyRef`; TestTender's service
account does not need `get`, `list` or `watch` access to Secrets. An empty
allowlist rejects every Secret reference.

Docker-secret style files use the same allowlist:

```java
withLabel(
    "testtender.io/secret-file.db_password",
    "integration-database:password"
)
```

The kubelet mounts that key read-only at `/run/secrets/db_password`. Clients
cannot select another directory or reference a Secret in another namespace.

## License

TestTender is licensed under the MIT License and retains Kubedock's original
copyright notice. The pre-fork prototype remains available through Git history
under its original Apache-2.0 terms.
