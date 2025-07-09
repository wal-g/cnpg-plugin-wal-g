#!/bin/bash
set -euxo pipefail

# Deleting kind cluster
kind delete cluster -n cnpg-wal-g

# ==== Unmount and remove loop devices for persistent volumes ==== #
VOLUME_DIR="/tmp/kind-cnpg-wal-g/kind-volumes" # Should be same as in ./bootstrap_kind_with_cnpg.sh
MOUNT_BASE="/run/kind-cnpg-wal-g-volumes-mounts-"  # Should be same as in ./bootstrap_kind_with_cnpg.sh
VOLUME_COUNT=4  # Should be same as in ./bootstrap_kind_with_cnpg.sh


# 1. Create and mount volume files
for i in $(seq 1 "$VOLUME_COUNT"); do
  VOL_FILE="${VOLUME_DIR}/vol${i}.img"
  MOUNT_POINT="${MOUNT_BASE}${i}"

  sudo umount $MOUNT_POINT
  sudo rm -f $VOL_FILE
done
sudo rm -rf $VOLUME_DIR
