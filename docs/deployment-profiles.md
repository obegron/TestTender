# Deployment profile

TestTender has one supported architecture: a namespace-scoped control service in
each tenant's existing dev/test namespace. Test runners and the worker Pods
created for Testcontainers run in that same namespace.

The repository intentionally does not create a Namespace. Apply the resources
to the namespace already managed for the tenant:

```bash
kubectl -n tenant-dev create configmap testtender-image-policy \
  --from-file=policy.json=deploy/image-policy.example.json \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n tenant-dev apply -f deploy/testtender.yaml
```

The example policy contains placeholder internal registry names and must be
replaced with the tenant's Argo CD-managed policy before use. TestTender fails
startup if the mounted policy is missing or invalid.

Test runners use:

```text
DOCKER_HOST=tcp://testtender:2475
TESTCONTAINERS_RYUK_DISABLED=true
```

The Docker API Service is headless. Testcontainers uses the Docker host name for
mapped ports as well as for API calls, so TestTender's `--reverse-proxy` listeners
must be reachable through the same DNS name. A normal ClusterIP exposing only
2475 would hide those dynamic ports.

The control service creates Pods, Services and ConfigMaps only in its own
namespace. Worker Pods use the `testtender-worker` ServiceAccount with token
automount disabled and inherit a baseline Pod template with RuntimeDefault
seccomp and no privilege escalation. A blanket `drop: ["ALL"]` is not used:
common database images need a small set of capabilities to initialize files and
switch to their runtime user. Image-bound capability profiles remain a security
release requirement and must be enforced from approved policy rather than from
Docker client labels.

This baseline does not yet implement OIDC authentication or per-run ownership.
Until those controls land, do not expose the Docker API through ingress and do
not share one TestTender deployment between mutually untrusted concurrent runs.
The intended central-CI control API is separate from this in-namespace Docker
API and remains on the fork plan.

The old PRoot sidecar, cross-namespace runtime, NodePort and prototype mTLS
profiles were removed when the Kubedock-derived implementation became active.
The inherited Docker-in-Docker proxy and worker sidecar path were also removed;
the runtime is Kubernetes-only.
