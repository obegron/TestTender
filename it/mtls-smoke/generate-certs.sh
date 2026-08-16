#!/usr/bin/env bash
set -euo pipefail

out_dir="${1:?usage: generate-certs.sh OUTPUT_DIR [SERVICE_DNS]}"
service_dns="${2:-testtender.testtender-system.svc.cluster.local}"

mkdir -p "${out_dir}/client-a" "${out_dir}/client-b"

openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "${out_dir}/ca-key.pem" >/dev/null 2>&1
openssl req -x509 -new -key "${out_dir}/ca-key.pem" -sha256 -days 2 \
  -subj "/CN=testtender-integration-ca" -out "${out_dir}/ca.pem" >/dev/null 2>&1

openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "${out_dir}/server.key" >/dev/null 2>&1
openssl req -new -key "${out_dir}/server.key" -subj "/CN=${service_dns}" \
  -addext "subjectAltName=DNS:${service_dns}" \
  -addext "extendedKeyUsage=serverAuth" \
  -out "${out_dir}/server.csr" >/dev/null 2>&1
openssl x509 -req -in "${out_dir}/server.csr" -CA "${out_dir}/ca.pem" \
  -CAkey "${out_dir}/ca-key.pem" -CAcreateserial -days 2 -sha256 \
  -copy_extensions copy -out "${out_dir}/server.crt" >/dev/null 2>&1

for client in client-a client-b; do
  openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 \
    -out "${out_dir}/${client}/key.pem" >/dev/null 2>&1
  openssl req -new -key "${out_dir}/${client}/key.pem" -subj "/CN=${client}" \
    -addext "extendedKeyUsage=clientAuth" \
    -out "${out_dir}/${client}.csr" >/dev/null 2>&1
  openssl x509 -req -in "${out_dir}/${client}.csr" -CA "${out_dir}/ca.pem" \
    -CAkey "${out_dir}/ca-key.pem" -CAcreateserial -days 2 -sha256 \
    -copy_extensions copy -out "${out_dir}/${client}/cert.pem" >/dev/null 2>&1
  cp "${out_dir}/ca.pem" "${out_dir}/${client}/ca.pem"
done

chmod 0600 "${out_dir}/ca-key.pem" "${out_dir}/server.key" \
  "${out_dir}/client-a/key.pem" "${out_dir}/client-b/key.pem"
