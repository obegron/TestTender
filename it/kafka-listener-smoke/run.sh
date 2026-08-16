#!/usr/bin/env bash
set -euo pipefail

context="${K8S_CONTEXT:-k3d-sidewhale-k8s}"
namespace="${K8S_NAMESPACE:-sidewhale-system}"
job_name="${K8S_KAFKA_IT_JOB_NAME:-sidewhale-kafka-listener-it}"
timeout="${K8S_KAFKA_IT_TIMEOUT:-600s}"

kubectl --context "${context}" -n "${namespace}" get svc sidewhale >/dev/null

sidewhale_host="${K8S_SIDEWHALE_DOCKER_HOST:-tcp://sidewhale.${namespace}.svc.cluster.local:23750}"

kubectl --context "${context}" -n "${namespace}" delete job "${job_name}" --ignore-not-found >/dev/null
K8S_KAFKA_IT_JOB_NAME="${job_name}" \
K8S_NAMESPACE="${namespace}" \
K8S_SIDEWHALE_DOCKER_HOST="${sidewhale_host}" \
./it/kafka-listener-smoke/generate-job.sh | kubectl --context "${context}" apply -f -

if ! ./it/wait-for-job.sh "${context}" "${namespace}" "${job_name}" "${timeout}"; then
  kubectl --context "${context}" -n "${namespace}" logs -f "job/${job_name}" || true
  exit 1
fi

kubectl --context "${context}" -n "${namespace}" logs -f "job/${job_name}"
