#!/usr/bin/env bash
set -euo pipefail

job_name="${K8S_JAVA_SMOKE_JOB_NAME:-testtender-java-smoke}"
namespace="${K8S_NAMESPACE:-testtender-system}"
image="${K8S_JAVA_SMOKE_IMAGE:-testtender-testcontainers-smoke}:${K8S_JAVA_SMOKE_TAG:-dev}"
docker_host="${K8S_TESTTENDER_DOCKER_HOST:-tcp://testtender.${namespace}.svc.cluster.local:2475}"
tls_secret="${K8S_TESTTENDER_TLS_SECRET:-}"

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
        - name: runner
          image: ${image}
          imagePullPolicy: IfNotPresent
          env:
            - name: DOCKER_HOST
              value: "${docker_host}"
            - name: TESTCONTAINERS_RYUK_DISABLED
              value: "true"
            - name: TESTCONTAINERS_CHECKS_DISABLE
              value: "true"
YAML

if [[ -n "${tls_secret}" ]]; then
  cat <<YAML
            - name: DOCKER_TLS_VERIFY
              value: "1"
            - name: DOCKER_CERT_PATH
              value: /var/run/testtender-client-tls
          volumeMounts:
            - name: client-tls
              mountPath: /var/run/testtender-client-tls
              readOnly: true
      volumes:
        - name: client-tls
          secret:
            secretName: ${tls_secret}
            defaultMode: 0400
YAML
fi
