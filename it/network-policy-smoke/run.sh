#!/usr/bin/env bash
set -euo pipefail

context="${K8S_CONTEXT:-}"
namespace="${K8S_NAMESPACE:-default}"
service="${K8S_TESTTENDER_SERVICE:-testtender}"
port="${K8S_TESTTENDER_PORT:-2475}"
probe_image="${K8S_NETWORK_PROBE_IMAGE:-}"
settle_seconds="${K8S_NETWORK_POLICY_SETTLE_SECONDS:-5}"

if [[ -z "${probe_image}" ]]; then
  echo "K8S_NETWORK_PROBE_IMAGE must name an internally available image containing curl and sleep" >&2
  exit 2
fi

kubectl_args=()
if [[ -n "${context}" ]]; then
  kubectl_args+=(--context "${context}")
fi
kubectl_args+=(-n "${namespace}")

suffix="$$"
allowed_pod="testtender-netpol-allowed-${suffix}"
denied_pod="testtender-netpol-denied-${suffix}"
endpoint="http://${service}:${port}/_ping"

cleanup() {
  kubectl "${kubectl_args[@]}" delete pod \
    "${allowed_pod}" "${denied_pod}" \
    --ignore-not-found --wait=false >/dev/null 2>&1 || true
}
trap cleanup EXIT

kubectl "${kubectl_args[@]}" get service "${service}" >/dev/null
kubectl "${kubectl_args[@]}" get networkpolicy testtender-api >/dev/null

kubectl "${kubectl_args[@]}" run "${allowed_pod}" \
  --image="${probe_image}" \
  --labels='testtender.io/client=true' \
  --restart=Never \
  --command -- sleep 3600 >/dev/null

kubectl "${kubectl_args[@]}" run "${denied_pod}" \
  --image="${probe_image}" \
  --restart=Never \
  --command -- sleep 3600 >/dev/null

kubectl "${kubectl_args[@]}" wait \
  --for=condition=Ready "pod/${allowed_pod}" "pod/${denied_pod}" \
  --timeout=120s >/dev/null

sleep "${settle_seconds}"

allowed_status="$(kubectl "${kubectl_args[@]}" exec "${allowed_pod}" -- \
  curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
  --max-time 5 "${endpoint}")"
if [[ "${allowed_status}" == "000" ]]; then
  echo "FAIL: labeled Pod did not receive an HTTP response from TestTender" >&2
  exit 1
fi

if kubectl "${kubectl_args[@]}" exec "${denied_pod}" -- \
  curl --fail --silent --show-error --max-time 5 "${endpoint}" >/dev/null 2>&1; then
  echo "FAIL: unlabeled Pod reached the TestTender Docker API" >&2
  exit 1
fi

echo "PASS: labeled Pod reached TestTender (HTTP ${allowed_status}) and unlabeled Pod was denied"
