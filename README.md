![sidewhale logo](assets/sidewhale-logo.png)

`sidewhale` is a small Docker API shim for Testcontainers execution in restricted environments.

Primary target: **Docker-on-Kubernetes style execution** via the `k8s` runtime backend (Sidewhale service creates worker Pods through the Kubernetes API).

It also supports a `host` runtime backend for sidecar-style usage where `proot` runs workloads locally in the Sidewhale process.

It is not a container runtime and does not try to be Docker-compatible beyond what Testcontainers needs.

## Status

Early project. No compatibility or stability guarantees.

Current focus:

- Kubernetes service runtime usage (`k8s` backend)
- Sidecar fallback usage (`host` backend)
- Testcontainers integration tests
- Simple and deterministic behavior

## What Works Right Now

- Basic Testcontainers lifecycle (`create`, `start`, `inspect`, `logs`, `stop`, `delete`)
- Unix socket listener (default: `<state-dir>/docker.sock`) for in-container Docker clients
- Image pulling and rootfs extraction
- Port publishing through TCP proxying (the k8s manifest uses a headless Service so mapped ports share the API hostname)
- Health and readiness probes (`/healthz`, `/readyz`)
- Optional Docker-compatible TLS/mTLS with certificate-scoped container, network, and exec ownership
- Per-owner worker Pod labels and generated ingress NetworkPolicies for same-owner communication
- In-cluster sidecar run in k3d for PostgreSQL test (`DatabaseTest`) passed

## Known Gaps / Limitations

- mTLS isolates Docker API resources and generated NetworkPolicies isolate worker ingress by owner when the CNI enforces them, but images and aggregate metrics remain shared
- No registry auth management beyond pass-through headers from clients
- Insecure registry configuration is not implemented as a runtime flag yet
- Oracle image currently fails under `host` backend (proot) due missing syscall behavior; fully supported under `k8s` backend with automatic resource/probe injection.
- Some clients may log noisy `Socket closed` traces when log-follow streams are closed
- No support for many Docker APIs (networks, volumes, build, exec/attach parity, etc.)
- Host backend only: no per-container network namespace or embedded DNS
- Cross-container name resolution (for example `broker-1` in multi-node stacks) is not supported
- Privileged internal bind ports (`<1024`) can fail for images that insist on binding as non-root
- Port publishing is best-effort TCP proxying, not Docker bridge/NAT semantics

## Docker API Support Matrix

Backend modes:

- `--runtime-backend=host` (proot execution in Sidewhale process)
- `--runtime-backend=k8s` (worker Pod execution via Kubernetes API)
- Most endpoints are shared, but runtime behavior differs at `create/start/stop/kill/delete/inspect/logs/wait`.

Implemented:

- `GET /_ping`
- `GET /version`
- `GET /info`
- `POST /images/create`
- `GET /images/json`
- `GET /images/{name}/json` (returns 404 when not present)
- `POST /containers/create`
- `POST /containers/{id}/start`
- `POST /containers/{id}/stop`
- `POST /containers/{id}/kill`
- `DELETE /containers/{id}`
- `GET /containers/{id}/json`
- `GET /containers/{id}/logs`
- `GET /containers/{id}/stats`
- `POST /containers/{id}/wait`
- `GET /containers/{id}/archive`
- `PUT /containers/{id}/archive`

Partially implemented / best-effort:

- `POST /exec/{id}/start`
- `GET /exec/{id}/json`

Not implemented:

- Most other Docker endpoints return `404`.

## Local Smoke Run

```bash
docker build -t sidewhale:dev .
docker run --rm --network host sidewhale:dev --listen :23750 --listen-unix /tmp/sidewhale/docker.sock
```

Runtime backend flag:

- `--runtime-backend=host` (default, current implementation)
- `--runtime-backend=k8s` (in-cluster Pod execution backend, early implementation)
- `--k8s-runtime-namespace=<ns>` (optional worker Pod namespace override for k8s backend)
- `--k8s-image-pull-secrets=<name1,name2>` (optional imagePullSecrets for k8s worker Pods)
- `--k8s-cleanup-orphans=true|false` (default `true`, deletes labeled worker Pods not present in persisted state)
- `--max-request-body-bytes=<bytes>` (default `4194304`, set `0` for unlimited control requests)
- `--max-archive-bytes=<bytes>` (default `536870912`, set `0` for unlimited archive uploads)
- `--tls-cert=<path>` and `--tls-key=<path>` enable TLS on the TCP listener
- `--tls-client-ca=<path>` requires a verified client certificate and enables certificate-scoped ownership; `--listen-unix=-` is required so plaintext Unix access cannot bypass it

For the host backend, point Testcontainers (or any Docker API client) to:

```bash
DOCKER_HOST=tcp://127.0.0.1:23750

# Ryuk/inner clients can use:
# DOCKER_HOST=unix:///var/run/docker.sock
```

For the k8s backend, label the client Pod with `sidewhale.io/client=true` and use:

```bash
DOCKER_HOST=tcp://sidewhale.sidewhale-system.svc.cluster.local:23750
```

For the optional mTLS profile, also set the standard Docker variables. The
certificate directory must contain `ca.pem`, `cert.pem`, and `key.pem`:

```bash
DOCKER_HOST=tcp://sidewhale.sidewhale-system.svc.cluster.local:23750
DOCKER_TLS_VERIFY=1
DOCKER_CERT_PATH=/var/run/sidewhale-client-tls
```

See `docs/deployment-profiles.md` for certificate requirements and rollout
commands. A client identity is derived from its certificate public key. Keep
the key across certificate renewal, or delete its resources before rotating it.

## Proxy + TLS Interception

Image pulls happen in the `sidewhale` process. Configure proxy and trust settings on the `sidewhale` container itself.

- Proxy env vars: `HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY`
- Optional insecure mode for intercepted TLS: `--trust-insecure`

Quick smoke test (build image first):

```bash
make image VERSION=dev IMAGE_NAME=sidewhale
make smoke-pull IMAGE_NAME=sidewhale VERSION=dev \
  SIDEWHALE_RUN_ARGS="--trust-insecure" \
  SMOKE_IMAGE=redis:7-alpine
```

Example with proxy env vars:

```bash
HTTPS_PROXY=http://proxy.corp:8080 \
HTTP_PROXY=http://proxy.corp:8080 \
NO_PROXY=127.0.0.1,localhost \
make smoke-pull IMAGE_NAME=sidewhale VERSION=dev \
  SIDEWHALE_RUN_ARGS="--trust-insecure"
```

## In-Repo Testcontainers Smoke Tests

Run the Java smoke tests in `it/testcontainers-smoke` against a running Sidewhale endpoint:

```bash
export DOCKER_HOST=tcp://127.0.0.1:23750
make integration-test
```

By default this target sets `TESTCONTAINERS_RYUK_DISABLED=true`. Override if needed.

Run the same Redis and PostgreSQL Java tests as a labeled in-cluster client
through the documented headless Service endpoint:

```bash
make integration-test-java-smoke-k8s-image
make integration-test-java-smoke-k8s
```

The first target builds and imports the reusable Maven runner image into the
configured k3d cluster. The second target creates a fail-fast Kubernetes Job.

Against an mTLS-patched deployment, run the same suite with a client Secret and
then prove a second certificate cannot list or inspect its resources:

```bash
make integration-test-java-smoke-k8s \
  K8S_SIDEWHALE_TLS_SECRET=sidewhale-client-a-tls
make integration-test-mtls-k8s
```

`it/mtls-smoke/generate-certs.sh` creates short-lived integration certificates;
use your existing PKI or certificate controller for real deployments.

## Upstream Testcontainers In-Cluster Runner

To avoid local `kubectl port-forward` instability, you can run upstream Gradle tests entirely inside Kubernetes.

Build/import runner image:

```bash
make integration-test-upstream-k8s-image
```

Run a Job in `sidewhale-system` (default baseline is `ContainerStateTest`):

```bash
make integration-test-upstream-k8s
```

Useful overrides:

```bash
make integration-test-upstream-k8s \
  K8S_UPSTREAM_TASK=":testcontainers:test" \
  K8S_UPSTREAM_TEST_ARGS="--tests org.testcontainers.dockerclient.ImagePullTest"
```

Follow logs later:

```bash
make integration-test-upstream-k8s-logs
```

Delete job:

```bash
make integration-test-upstream-k8s-clean
```

## Development

Run the unit tests with `make test`. Run formatting validation, `go vet`, unit tests,
and the race detector with `make check`.

## Compatibility Matrix

For current host-backend compatibility status across Testcontainers modules, see:

- `docs/compatibility-matrix.md`
- `docs/codebase-layout.md` (source map for where new code should go)

## Deployment Profiles

- `docs/deployment-profiles.md`
- `deploy/sidewhale-host-sidecar.yaml`
- `deploy/sidewhale-k8s-runtime.yaml`
- `deploy/sidewhale-k8s-runtime-mtls-patch.yaml` (optional strict mTLS patch)
- `deploy/sidewhale-k8s-runtime-nodeport.yaml` (dev only)
