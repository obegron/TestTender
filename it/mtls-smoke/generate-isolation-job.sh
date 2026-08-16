#!/usr/bin/env bash
set -euo pipefail

namespace="${K8S_NAMESPACE:-testtender-system}"
job_name="${K8S_MTLS_JOB_NAME:-testtender-mtls-isolation}"
image="${K8S_JAVA_SMOKE_IMAGE:-testtender-testcontainers-smoke}:${K8S_JAVA_SMOKE_TAG:-dev}"
client_a_secret="${K8S_MTLS_CLIENT_A_SECRET:-testtender-client-a-tls}"
client_b_secret="${K8S_MTLS_CLIENT_B_SECRET:-testtender-client-b-tls}"
api="https://testtender.${namespace}.svc.cluster.local:23750"
hold_seconds="${K8S_MTLS_HOLD_SECONDS:-0}"

cat <<YAML
apiVersion: batch/v1
kind: Job
metadata:
  name: ${job_name}
  namespace: ${namespace}
spec:
  backoffLimit: 0
  ttlSecondsAfterFinished: 86400
  template:
    metadata:
      labels:
        testtender.io/client: "true"
    spec:
      restartPolicy: Never
      automountServiceAccountToken: false
      containers:
        - name: probe
          image: ${image}
          imagePullPolicy: IfNotPresent
          command: ["sh", "-ec"]
          args:
            - |
              api='${api}'
              curl_a() {
                curl --silent --show-error --fail \
                  --cacert /tls-a/ca.pem --cert /tls-a/cert.pem --key /tls-a/key.pem "\$@"
              }
              curl_b() {
                curl --silent --show-error --fail \
                  --cacert /tls-b/ca.pem --cert /tls-b/cert.pem --key /tls-b/key.pem "\$@"
              }
              created="\$(curl_a -H 'Content-Type: application/json' \
                -d '{"Image":"${image}","Entrypoint":["sh","-c"],"Cmd":["sleep 120"]}' \
                "\${api}/v1.41/containers/create?name=mtls-owner-a")"
              id="\$(printf '%s' "\${created}" | sed -n 's/.*"Id":"\([^"]*\)".*/\1/p')"
              test -n "\${id}"
              cleanup() {
                curl_a -X DELETE "\${api}/v1.41/containers/\${id}?force=true" >/dev/null 2>&1 || true
              }
              trap cleanup EXIT
              curl_a -X POST "\${api}/v1.41/containers/\${id}/start"
              curl_a "\${api}/v1.41/containers/json" | grep -q "\${id}"
              if curl_b "\${api}/v1.41/containers/json" | grep -q "\${id}"; then
                echo 'client B listed client A resource' >&2
                exit 1
              fi
              status="\$(curl --silent --output /dev/null --write-out '%{http_code}' \
                --cacert /tls-b/ca.pem --cert /tls-b/cert.pem --key /tls-b/key.pem \
                "\${api}/v1.41/containers/\${id}/json")"
              test "\${status}" = 404
              echo "mTLS isolation ok: client B cannot list or inspect \${id}"
              if [ '${hold_seconds}' -gt 0 ]; then
                sleep '${hold_seconds}'
              fi
          volumeMounts:
            - name: client-a
              mountPath: /tls-a
              readOnly: true
            - name: client-b
              mountPath: /tls-b
              readOnly: true
      volumes:
        - name: client-a
          secret:
            secretName: ${client_a_secret}
            defaultMode: 0400
        - name: client-b
          secret:
            secretName: ${client_b_secret}
            defaultMode: 0400
YAML
