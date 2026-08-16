#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 4 ]; then
  echo "usage: $0 CONTEXT NAMESPACE JOB TIMEOUT" >&2
  exit 2
fi

context="$1"
namespace="$2"
job_name="$3"
timeout="$4"

case "${timeout}" in
  *s) timeout_seconds="${timeout%s}" ;;
  *m) timeout_seconds=$(( ${timeout%m} * 60 )) ;;
  *h) timeout_seconds=$(( ${timeout%h} * 3600 )) ;;
  *) timeout_seconds="${timeout}" ;;
esac
if [[ ! "${timeout_seconds}" =~ ^[0-9]+$ ]] || [ "${timeout_seconds}" -le 0 ]; then
  echo "invalid job timeout: ${timeout}" >&2
  exit 2
fi

deadline=$((SECONDS + timeout_seconds))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  if ! conditions="$(kubectl --context "${context}" -n "${namespace}" get "job/${job_name}" -o jsonpath='{range .status.conditions[*]}{.type}={.status}{"\n"}{end}')"; then
    echo "failed to read job/${job_name}" >&2
    exit 1
  fi
  case "${conditions}" in
    *Complete=True*)
      echo "job.batch/${job_name} condition met"
      exit 0
      ;;
    *Failed=True*)
      echo "job.batch/${job_name} failed" >&2
      exit 1
      ;;
  esac
  sleep 2
done

echo "timed out after ${timeout} waiting for job/${job_name}" >&2
exit 1
