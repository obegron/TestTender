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
environment controls. The first implemented control is exact image
authorization and mirror rewriting. OIDC run authentication, per-run ownership
and constrained tenant Secret references remain under development.

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

## License

TestTender is licensed under the MIT License and retains Kubedock's original
copyright notice. The pre-fork prototype remains available through Git history
under its original Apache-2.0 terms.
