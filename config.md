# Configuration reference

The testtender binary has the following commands available:
* `server` Start the testtender api server
* `readme` Display project readme
* `version`  Display testtender version details

The `server` command is the actual testtender server, and is the command to start testtender. The table below shows all possible commands and possible arguments. Some commands are also configurable via environment variables, as shown in the environment variable column.

|command|argument|default|environment variable|description|
|---|---|---|---|---|
|server|--listen-addr|:2475|SERVER_LISTEN_ADDR|Webserver listen address|
|server|--unix-socket|||Unix socket to listen to (instead of port)|
|server|--tls-enable|false|SERVER_TLS_ENABLE|Enable TLS on api server|
|server|--tls-key-file||SERVER_TLS_CERT_FILE|TLS keyfile|
|server|--tls-cert-file||SERVER_TLS_CERT_FILE|TLS certificate file|
|server|--namespace / -n|<current namespace>|NAMESPACE|Namespace in which containers should be orchestrated|
|server|--initimage|ghcr.io/obegron/testtender:version|INIT_IMAGE|Image to use as initcontainer for volume setup|
|server|--pull-policy|ifnotpresent|PULL_POLICY|Pull policy that should be applied (ifnotpresent,never,always)|
|server|--service-account|default|SERVICE_ACCOUNT|Service account that should be used for deployed pods|
|server|--image-pull-secrets||IMAGE_PULL_SECRETS|Comma separated list of image pull secrets that should be used|
|server|--allowed-secrets||ALLOWED_SECRETS|Comma separated exact Secret names that workloads may reference|
|server|--pod-template||POD_TEMPLATE|Pod file that should be used as the base for creating pods|
|server|--pod-name-prefix||POD_NAME_PREFIX|The prefix of the name to be used in the created pods|
|server|--inspector / -i|false||Enable image inspect to fetch container port config from a registry|
|server|--image-policy-file|||JSON policy file used to authorize and rewrite container images (`IMAGE_POLICY_FILE`)|
|server|--timeout / -t|1m|TIME_OUT|Container creating/deletion timeout|
|server|--reapmax / -r|60m|REAPER_REAPMAX|Reap all resources older than this time (0 disables container reaping)|
|server|--request-cpu||K8S_REQUEST_CPU|Default k8s cpu resource request (optionally add ,limit)|
|server|--request-memory||K8S_REQUEST_MEMORY|Default k8s memory resource request (optionally add ,limit)|
|server|--node-selector||K8S_NODE_SELECTOR|Default k8s node selector in the form of key1=value1[,key2=value2]|
|server|--runas-user||K8S_RUNAS_USER|Numeric UID to run pods as (defaults to UID in image)|
|server|--lock|false||Lock namespace for this instance|
|server|--lock-timeout|15m||Max time trying to acquire namespace lock|
|server|--verbosity / -v|1|VERBOSITY|Log verbosity level|
|server|--prune-start / -P|false||Prune all existing testtender resources before starting|
|server|--port-forward|false||Open port-forwards for all services|
|server|--reverse-proxy|false||Reverse proxy all services via 0.0.0.0 on the testtender host as well|
|server|--pre-archive|false||Enable support for copying single files to containers without starting them|
|server|--annotation||K8S_ANNOTATION_annotation|annotation that need to be added to every k8s resource (key=value)|
|server|--label||K8S_LABEL_label|label that need to be added to every k8s resource (key=value)|
|server|--active-deadline-seconds|-1|K8S_ACTIVE_DEADLINE_SECONDS|Default value for pod deadline, in seconds (a negative value means no deadline)|
|server|--ignore-container-memory|false||Ignore container memory setting and use requests/limits from gobal settings or container labels|
|server|--kube-api-qps|0|K8S_QPS|Maximum QPS for requests to the Kubernetes API (0 uses client default)|
|server|--kube-api-burst|0|K8S_BURST|Maximum burst for requests to the Kubernetes API (0 uses client default)|
|server|--poll-rate|0|POLL_RATE|Maximum polling requests per second towards the backend (0 uses default of 1)|
|server|--poll-burst|0|POLL_BURST|Maximum burst of poll requests towards the backend (0 uses default of 3)|
|readme||||Display project readme|
|readme|config|||Display configuration reference|
|readme|licence|||Display project licence|
|version||||Display testtender version details|

## Labels and annotations

Labels added to container images are added as annotations and labels to the created kubernetes pods. Additional labels and annotations can be added with the `--annotation` and `--label` cli argument. Environment variables that start with `K8S_ANNOTATION_` and `K8S_LABEL_` will be added as a kubernetes annotation or label as well. For example `K8S_ANNOTATION_FOO` will create an annotation `foo` with the value of the environment variable. Note that annotations and labels added via environment variables or cli will not be processed by testtender if they have a specific control function. For these occasions specific environment variables and cli arguments are present.

### Namespace-local Secret environment variables

An allowlisted namespace-local Secret key can be exposed to a workload through
a Docker label. For example:

```text
testtender.io/secret-env.DB_PASSWORD=integration-database:password
```

`integration-database` must be listed in `--allowed-secrets` or
`ALLOWED_SECRETS`. TestTender puts a `secretKeyRef` in the worker Pod and never
reads the Secret object or value. The allowlist is exact, the namespace is
always the configured worker namespace, ordinary environment variables cannot
be overwritten, and each container is limited to 32 Secret references.

The same key can be exposed with Docker-secret file semantics:

```text
testtender.io/secret-file.db_password=integration-database:password
```

This creates the read-only file `/run/secrets/db_password` from a projected
Secret volume. The filename is taken from the label suffix and cannot contain a
path separator; clients cannot choose another mount directory.
