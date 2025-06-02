#!/bin/bash
set -euxo pipefail

cd "$(dirname "$0")"

kind create cluster --config ./kind.yaml 

# Install cilium
cilium install --version 1.17.2 --set envoy.enabled=false --set ipv6.enabled=true

# Install metrics server
kubectl apply -f ./metrics-server.yaml

# Install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.17.0/cert-manager.yaml

# Install CNPG
kubectl apply --server-side -f \
  https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.25/releases/cnpg-1.25.1.yaml

# Wait for CNPG manager deployment is ready
kubectl rollout status deployment/cnpg-controller-manager -n cnpg-system --timeout=180s
