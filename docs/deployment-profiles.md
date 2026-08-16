# Deployment Profiles

Sidewhale has one primary deployment model and one best-effort fallback.

## Kubernetes Service (Primary)

The `k8s` backend runs Sidewhale as a namespace-scoped control plane. Docker API
container operations are translated into worker Pods managed through the
Kubernetes API.

- Manifest: `deploy/sidewhale-k8s-runtime.yaml`
- Docker endpoint: `tcp://sidewhale.sidewhale-system.svc.cluster.local:23750`
- Runtime namespace: `sidewhale-system` by default
- State: persistent volume `sidewhale-state`
- Scale: one Sidewhale replica per state volume and trust boundary

The `sidewhale` Service is headless. Its DNS name resolves directly to the
Sidewhale Pod, which is necessary because Testcontainers connects to dynamic
mapped ports in addition to the Docker API port. A normal ClusterIP Service
that declares only port 23750 cannot forward those dynamic ports.

Client Pods in `sidewhale-system` must carry this label:

```yaml
metadata:
  labels:
    sidewhale.io/client: "true"
```

The included NetworkPolicies allow labeled clients to reach the Sidewhale API
and mapped ports. The baseline worker policy allows ingress only from the
Sidewhale control-plane Pod. Before creating a worker, Sidewhale creates a
second policy selecting its opaque `sidewhale.owner-id` and permitting traffic
from workers with that same owner ID. Cross-owner worker ingress is therefore
not allowed by the supplied policy set. Worker Pods cannot call the Sidewhale
API unless they are separately granted the client label.

NetworkPolicy objects provide isolation only when the cluster CNI enforces
them. Treat an unlabeled-client deny test as a deployment acceptance check. A
cluster where the policy objects exist but traffic is still allowed is not a
supported shared-service environment.

Generated worker Pods use these baseline controls:

- service-account token automount disabled
- service-link environment injection disabled
- `RuntimeDefault` seccomp profile
- privilege escalation disabled
- no privileged, host-network, or host-path translation
- an opaque, public-key-derived owner label when mTLS is enabled

The base manifest also disables image mutation endpoints. Pulled images remain
a shared cache, so tag/push/delete is not appropriate for a shared service.

### Optional strict mTLS

Sidewhale supports the Docker client's normal TLS settings. When
`--tls-client-ca` is configured, the TCP listener requires a certificate signed
by that CA. Containers, custom networks, and exec instances are owned by a
stable SHA-256 identity derived from the client's public key. Lists are filtered
and cross-owner resource operations return `404`. Legacy resources without an
owner belong only to plaintext/anonymous clients.

The server certificate must include this Service DNS name in its SANs:

```text
sidewhale.sidewhale-system.svc.cluster.local
```

Create the Secret, apply the base manifest, and apply the strategic merge
patch:

```bash
kubectl -n sidewhale-system create secret generic sidewhale-tls \
  --from-file=server.crt=server.crt \
  --from-file=server.key=server.key \
  --from-file=client-ca.crt=client-ca.crt
kubectl apply -f deploy/sidewhale-k8s-runtime.yaml
kubectl -n sidewhale-system patch deployment sidewhale \
  --type strategic \
  --patch-file deploy/sidewhale-k8s-runtime-mtls-patch.yaml
```

The patch disables the Unix listener, mounts the Secret, and changes the HTTP
health probes to TCP because kubelet does not present a client certificate.
Give each CI trust boundary its own client private key and mount Docker's
expected `ca.pem`, `cert.pem`, and `key.pem` files into the client Pod:

```yaml
env:
  - name: DOCKER_HOST
    value: tcp://sidewhale.sidewhale-system.svc.cluster.local:23750
  - name: DOCKER_TLS_VERIFY
    value: "1"
  - name: DOCKER_CERT_PATH
    value: /var/run/sidewhale-client-tls
```

Sidewhale also creates one ingress NetworkPolicy per certificate owner before
creating that owner's first worker Pod. The policy admits Sidewhale's port proxy
and same-owner workers, but not workers belonging to another certificate. It is
removed when the owner's final Kubernetes container is deleted and reconciled
after restarts.

This is still not a complete multi-tenant boundary. Aggregate metrics and the
image cache are global, egress is not owner-isolated, and NetworkPolicy
enforcement depends on the CNI. Pod labels are part of the security decision,
so untrusted users must not have Kubernetes RBAC allowing them to create or
patch arbitrary Pods or NetworkPolicies in the runtime namespace. Treat both a
cross-owner worker deny test and a label-spoofing RBAC audit as deployment
acceptance checks.

Optional settings:

- `--k8s-runtime-namespace=<ns>` places worker Pods in another namespace. The
  RBAC and NetworkPolicies must also be installed there.
- `--k8s-image-pull-secrets=<secret1,secret2>` configures private registry pulls.
- `--k8s-cleanup-orphans=false` disables startup orphan cleanup.

The NodePort manifest exposes only the Docker API port. It does not by itself
make dynamic mapped ports reachable outside the cluster. External clients need
a dedicated tunnel/data-plane design and are not part of the current MVP.

## PRoot Sidecar (Experimental Fallback)

The `host` backend runs workloads through PRoot in the test runner Pod without
Kubernetes workload RBAC or cgroups.

- Manifest: `deploy/sidewhale-host-sidecar.yaml`
- Docker endpoint: normally `unix:///var/run/docker.sock`
- Isolation and Docker compatibility: best effort

This mode is useful on systems that cannot provide Docker or cgroups, but PRoot
cannot reproduce Docker namespaces, privileges, networking, mounts, or every
image entrypoint. It is not the production architecture and compatibility fixes
for it must not weaken the Kubernetes backend.
