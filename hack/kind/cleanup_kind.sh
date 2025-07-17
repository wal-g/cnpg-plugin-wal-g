#!/bin/bash
set -euxo pipefail

# Deleting kind cluster
kind delete cluster -n cnpg-wal-g
