#!/bin/bash
set -euxo pipefail

cd "$(dirname "$0")"

# ==== Prepare loop devices for persistent volumes with limited size ==== #
# Configuration for bind-mount volumes (to restrict space used by local-path-provisioner on each docker node)
VOLUME_DIR="/tmp/kind-cnpg-wal-g/kind-volumes"
MOUNT_BASE="/run/kind-cnpg-wal-g-volumes-mounts-"
VOLUME_COUNT=4  # Should be same as workers in kind.yaml file
VOLUME_SIZE=3G  # 3 GB size limit for each loop device

mkdir -p "$VOLUME_DIR"

# 1. Create and mount volume files
for i in $(seq 1 "$VOLUME_COUNT"); do
  VOL_FILE="${VOLUME_DIR}/vol${i}.img"
  MOUNT_POINT="${MOUNT_BASE}${i}"

  if [ ! -f "$VOL_FILE" ]; then
    echo "Creating sparse file: $VOL_FILE"
    fallocate -l "$VOLUME_SIZE" "$VOL_FILE"
    mkfs.ext4 -F "$VOL_FILE"
  fi

  mkdir -p "$MOUNT_POINT"

  # Only mount if not already mounted
  if ! mountpoint -q "$MOUNT_POINT"; then
    echo "Mounting $VOL_FILE to $MOUNT_POINT"
    sudo mount -o loop "$VOL_FILE" "$MOUNT_POINT"
  fi
done

# Create kind cluster
kind create cluster --config ./kind.yaml 

# Install cilium
helm repo add cilium https://helm.cilium.io/
helm -n kube-system upgrade --install cilium cilium/cilium --version 1.17.4 --namespace kube-system --set envoy.enabled=false --set ipv6.enabled=true

# Install metrics server
kubectl apply -f ./metrics-server.yaml

# Install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.17.0/cert-manager.yaml

# Install CNPG
kubectl apply --server-side -f \
  https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.25/releases/cnpg-1.25.1.yaml

# Wait for CNPG manager deployment is ready
kubectl rollout status deployment/cnpg-controller-manager -n cnpg-system --timeout=180s
