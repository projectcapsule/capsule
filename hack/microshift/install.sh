#!/usr/bin/env bash

# Installs an upstream (OKD based) MicroShift release from
# https://github.com/microshift-io/microshift on an Ubuntu host (DEB packages,
# Kindnet CNI) and waits until it is ready. Requires root.
#
# e.g.: sudo ./install.sh 4.21.0_g29f429c21_4.21.0_okd_scos.ec.15 4.22.0_202610071017_g0a516b4eb_4.22.0_okd_scos.ec.16

set -euo pipefail

TAG="${1:?usage: $0 <microshift-io release tag> [installer release tag]}"
# The installer of older releases still uses the discontinued pkgs.k8s.io CRI-O repository
INSTALLER_TAG="${2:-${TAG}}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-/var/lib/microshift/resources/kubeadmin/kubeconfig}"
READY_TIMEOUT="${READY_TIMEOUT:-10m}"

case "$(uname -m)" in
  x86_64|aarch64) ARCH="$(uname -m)" ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

if [ "$(id -u)" -ne 0 ]; then
  echo "this script must be run as root" >&2
  exit 1
fi

WORKDIR="$(mktemp -d)"
trap 'rm -rf "${WORKDIR}"' EXIT

BASE_URL="https://github.com/microshift-io/microshift"

mkdir -p "${WORKDIR}/debs"
curl -fsSL "${BASE_URL}/releases/download/${TAG}/microshift-debs-${ARCH}.tgz" | tar -xz -C "${WORKDIR}/debs"
curl -fsSL "https://raw.githubusercontent.com/microshift-io/microshift/${INSTALLER_TAG}/src/deb/install.sh" -o "${WORKDIR}/install.sh"

# podman is used to load locally built images into the CRI-O image storage
export DEBIAN_FRONTEND=noninteractive
apt-get update -y -q
apt-get install -y -q podman

bash "${WORKDIR}/install.sh" "${WORKDIR}/debs"

# A slow first start can hit the systemd start timeout; the service is restarted automatically and readiness is checked below
systemctl start microshift || echo "microshift.service reported a start failure, waiting for its restart" >&2

echo "waiting for the MicroShift kubeconfig"
timeout 5m bash -c "until [ -s '${KUBECONFIG_PATH}' ]; do sleep 5; done"

export KUBECONFIG="${KUBECONFIG_PATH}"

# kubectl wait fails on an empty selection, hence wait for the objects to be created first
echo "waiting for the node and the core components to be created"
timeout "${READY_TIMEOUT}" bash -c 'until kubectl get node -o name | grep -q . && kubectl -n openshift-service-ca get deployment service-ca >/dev/null 2>&1; do sleep 5; done'

kubectl wait --for=condition=Ready node --all --timeout="${READY_TIMEOUT}"
kubectl wait --for=condition=Available deployment --all --all-namespaces --timeout="${READY_TIMEOUT}"
kubectl get pods --all-namespaces
