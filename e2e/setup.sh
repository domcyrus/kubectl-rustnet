#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${KIND_CLUSTER_NAME:-rustnet-e2e}"
KIND_CONFIG="$(dirname "$0")/kind-config.yaml"
RUSTNET_IMAGE="${RUSTNET_IMAGE:-ghcr.io/domcyrus/rustnet:latest}"

case "${1:-}" in
  create)
    echo "Creating kind cluster '${CLUSTER_NAME}'..."
    kind create cluster --name "${CLUSTER_NAME}" --config "${KIND_CONFIG}" --wait 60s

    echo "Pulling and loading rustnet image into kind..."
    docker pull "${RUSTNET_IMAGE}" || true
    kind load docker-image "${RUSTNET_IMAGE}" --name "${CLUSTER_NAME}" || true

    echo "Cluster ready."
    ;;
  delete)
    echo "Deleting kind cluster '${CLUSTER_NAME}'..."
    kind delete cluster --name "${CLUSTER_NAME}" || true
    ;;
  *)
    echo "Usage: $0 {create|delete}"
    exit 1
    ;;
esac
