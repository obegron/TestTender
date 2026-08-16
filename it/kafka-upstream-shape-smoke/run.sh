#!/usr/bin/env bash
set -euo pipefail

context="${K8S_CONTEXT:-k3d-testtender-k8s}"
namespace="${K8S_NAMESPACE:-testtender-system}"
job_name="${K8S_KAFKA_UPSTREAM_IT_JOB_NAME:-testtender-kafka-upstream-shape-it}"
timeout="${K8S_KAFKA_UPSTREAM_IT_TIMEOUT:-900s}"

kubectl --context "${context}" -n "${namespace}" get svc testtender >/dev/null

testtender_host="${K8S_TESTTENDER_DOCKER_HOST:-tcp://testtender.${namespace}.svc.cluster.local:23750}"

kubectl --context "${context}" -n "${namespace}" delete job "${job_name}" --ignore-not-found >/dev/null
K8S_KAFKA_UPSTREAM_IT_JOB_NAME="${job_name}" \
K8S_NAMESPACE="${namespace}" \
K8S_TESTTENDER_DOCKER_HOST="${testtender_host}" \
./it/kafka-upstream-shape-smoke/generate-job.sh | kubectl --context "${context}" apply -f -

if ! ./it/wait-for-job.sh "${context}" "${namespace}" "${job_name}" "${timeout}"; then
  kubectl --context "${context}" -n "${namespace}" logs -f "job/${job_name}" || true
  exit 1
fi

kubectl --context "${context}" -n "${namespace}" logs -f "job/${job_name}"
