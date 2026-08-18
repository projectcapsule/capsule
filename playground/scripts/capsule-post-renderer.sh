#!/usr/bin/env bash

set -euo pipefail

for required_variable in PLAYGROUND_CA_BUNDLE CAPSULE_TLS_CERT_BUNDLE CAPSULE_TLS_KEY_BUNDLE; do
  if [[ -z "${!required_variable:-}" ]]; then
    echo "Missing required environment variable: ${required_variable}" >&2
    exit 1
  fi
done

render_dir="$(mktemp -d)"
trap 'rm -rf "${render_dir}"' EXIT

tee "${render_dir}/rendered.yaml" >/dev/null

{
  printf '%s\n' \
    'apiVersion: kustomize.config.k8s.io/v1beta1' \
    'kind: Kustomization' \
    'resources:' \
    '  - rendered.yaml' \
    'patches:' \
    '  - target:' \
    '      version: v1' \
    '      kind: Secret' \
    '      name: capsule-tls' \
    '    patch: |' \
    '      - op: add' \
    '        path: /data' \
    '        value:'
  printf '          ca.crt: %s\n' "${PLAYGROUND_CA_BUNDLE}"
  printf '          tls.crt: %s\n' "${CAPSULE_TLS_CERT_BUNDLE}"
  printf '          tls.key: %s\n' "${CAPSULE_TLS_KEY_BUNDLE}"
} > "${render_dir}/kustomization.yaml"

kubectl kustomize "${render_dir}"
