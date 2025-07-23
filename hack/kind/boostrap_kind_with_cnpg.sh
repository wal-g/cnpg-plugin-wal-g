#!/bin/bash
set -euxo pipefail

cd "$(dirname "$0")"

# Create kind cluster
kind create cluster --config ./kind.yaml 

# Install cilium
helm repo add cilium https://helm.cilium.io/
helm -n kube-system upgrade --install cilium cilium/cilium --version 1.17.4 --namespace kube-system --set envoy.enabled=false --set ipv6.enabled=true

# Install metrics server
kubectl apply -f ./metrics-server.yaml

# Install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.18.2/cert-manager.yaml


# Install CNPG
kubectl apply --server-side -f \
  https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.25/releases/cnpg-1.25.1.yaml

# Wait for CNPG manager deployment is ready
kubectl rollout status deployment/cnpg-controller-manager -n cnpg-system --timeout=180s
