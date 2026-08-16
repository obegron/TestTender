#!/usr/bin/env bash
set -euo pipefail

context="${K8S_CONTEXT:-k3d-testtender-k8s}"
namespace="${K8S_NAMESPACE:-testtender-system}"
job_name="${K8S_JAVA_SMOKE_JOB_NAME:-testtender-java-smoke}"
timeout="${K8S_JAVA_SMOKE_TIMEOUT:-600s}"
tls_secret="${K8S_TESTTENDER_TLS_SECRET:-}"

kubectl --context "${context}" -n "${namespace}" get svc testtender >/dev/null
kubectl --context "${context}" -n "${namespace}" delete job "${job_name}" --ignore-not-found >/dev/null
K8S_TESTTENDER_TLS_SECRET="${tls_secret}" ./it/testcontainers-smoke/generate-job.sh | kubectl --context "${context}" apply -f -

if ! ./it/wait-for-job.sh "${context}" "${namespace}" "${job_name}" "${timeout}"; then
  kubectl --context "${context}" -n "${namespace}" logs "job/${job_name}" || true
  exit 1
fi

kubectl --context "${context}" -n "${namespace}" logs "job/${job_name}"
