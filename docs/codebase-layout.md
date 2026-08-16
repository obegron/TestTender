# Codebase layout

TestTender follows its Kubedock upstream closely so updates remain reviewable.

- `cmd/`: CLI commands and runtime flag/environment bindings.
- `internal/backend/`: Kubernetes Pod, Service, exec, archive and log operations.
- `internal/model/`: in-memory Docker-compatible container, image, exec and
  network state.
- `internal/server/`: HTTP server and Docker/libpod compatibility routes.
- `internal/policy/`: TestTender authorization and mutation policies. Image
  normalization, allowlisting and mirror rewriting begin here.
- `internal/reaper/`: abandoned resource cleanup.
- `internal/util/`: focused Kubernetes, archive, port-forward and proxy helpers.
- `deploy/`: example namespace-scoped manifests and policy data.
- `examples/`: inherited upstream client and CI examples; these are useful
  compatibility fixtures, not TestTender's recommended multi-tenant topology.
- `it/`: TestTender integration and compatibility tests retained across the
  implementation migration.
- `docs/compatibility-matrix.md`: canonical compatibility evidence ledger.
- `UPSTREAM.md`: imported revision and upstream synchronization notes.

New restricted-environment features should enter through a small policy or
authentication package and be injected into the compatibility routes. Avoid
copying the old Apache-2.0 runtime back into this baseline.
