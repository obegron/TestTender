#!/usr/bin/env bash
set -euo pipefail

context="${K8S_CONTEXT:-k3d-testtender-k8s}"
namespace="${K8S_NAMESPACE:-testtender-system}"
job_name="${K8S_MTLS_JOB_NAME:-testtender-mtls-isolation}"
timeout="${K8S_MTLS_TIMEOUT:-180s}"

kubectl --context "${context}" -n "${namespace}" delete job "${job_name}" --ignore-not-found >/dev/null
./it/mtls-smoke/generate-isolation-job.sh | kubectl --context "${context}" apply -f -

if ! ./it/wait-for-job.sh "${context}" "${namespace}" "${job_name}" "${timeout}"; then
  kubectl --context "${context}" -n "${namespace}" logs "job/${job_name}" || true
  exit 1
fi

kubectl --context "${context}" -n "${namespace}" logs "job/${job_name}"
