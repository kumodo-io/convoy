#!/usr/bin/env bash
# Lints the convoy chart, runs its unit tests, and validates the rendered
# manifests of each supported values combination against the Kubernetes schemas.
set -euo pipefail

chart="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/deploy/chart/convoy"
k8s_version="${KUBERNETES_VERSION:-1.34.0}"

helm lint --strict "$chart"
untt --strict "$chart"

render() {
  local name="$1"
  shift
  echo "--- $name"
  # cert-manager CRDs have no schema in the default catalog.
  helm template convoy "$chart" --namespace convoy-system "$@" |
    kubeconform -strict -summary -ignore-missing-schemas -kubernetes-version "$k8s_version"
}

render defaults
render cert-manager --set tls.insecure=false
render existing-secret --set tls.insecure=false --set tls.certManager.enabled=false \
  --set tls.existingSecret=convoy-tls --set tls.caBundle=Y2E=
render external-etcd --set etcd.enabled=false \
  --set 'etcd.external.servers={https://etcd-0:2379,https://etcd-1:2379}' \
  --set etcd.external.clientSecret=etcd-client
render render-store --set renderStore.url=s3://convoy --set renderStore.existingSecret=creds
render no-cluster-admin --set rbac.clusterAdminApply=false --set healthRules.enabled=false
render other-namespace --namespace convoy
