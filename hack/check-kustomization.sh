#!/usr/bin/env bash
# Builds every supported kustomization and validates it against the Kubernetes
# schemas. Components cannot build alone, so each one is checked inside a
# generated overlay that combines it with the base.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/deploy/kustomization"
k8s_version="${KUBERNETES_VERSION:-1.34.0}"

# Kustomize rejects absolute resource paths, so the generated overlays live
# next to the base and refer to it relatively.
tmp="$(mktemp -d "$root/.check.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

validate() {
  local name="$1" dir="$2"
  echo "--- $name"
  kustomize build "$dir" >"$tmp/$name.yaml"
  # cert-manager CRDs have no schema in the default catalog.
  kubeconform -strict -summary -ignore-missing-schemas \
    -kubernetes-version "$k8s_version" "$tmp/$name.yaml"
}

combine() {
  local name="$1"
  shift
  mkdir -p "$tmp/$name"
  {
    echo "apiVersion: kustomize.config.k8s.io/v1beta1"
    echo "kind: Kustomization"
    echo "resources:"
    echo "  - ../../base"
    echo "components:"
    for c in "$@"; do
      echo "  - ../../components/$c"
    done
  } >"$tmp/$name/kustomization.yaml"
  validate "$name" "$tmp/$name"
}

validate default "$root/default"
validate health-rules "$root/health-rules"
combine bundled-etcd-insecure etcd tls-insecure
combine bundled-etcd-cert-manager etcd tls-cert-manager
combine external-etcd-full external-etcd tls-cert-manager render-store cluster-admin-apply
