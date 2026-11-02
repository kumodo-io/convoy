#!/usr/bin/env bash
# Cross-compiles kubectl-convoy and writes one archive per platform plus
# checksums.txt. Archive names follow the krew convention, so a krew manifest
# can point at the release assets without renames.
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <version> <outdir>" >&2
  exit 2
fi
version="$1"
out="$2"

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
platforms=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64)

mkdir -p "$out"
out="$(cd "$out" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

for platform in "${platforms[@]}"; do
  os="${platform%/*}"
  arch="${platform#*/}"
  name="kubectl-convoy_${version}_${os}_${arch}"
  bin="kubectl-convoy"
  [[ "$os" == windows ]] && bin="kubectl-convoy.exe"

  mkdir -p "$work/$name"
  (cd "$repo" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X github.com/kumodo-io/convoy/internal/cli.version=$version" \
    -o "$work/$name/$bin" ./cmd/kubectl-convoy)
  cp "$repo/LICENSE" "$work/$name/"

  if [[ "$os" == windows ]]; then
    (cd "$work/$name" && zip -q "$out/$name.zip" "$bin" LICENSE)
  else
    tar -czf "$out/$name.tar.gz" -C "$work/$name" "$bin" LICENSE
  fi
  echo "built $name"
done

(cd "$out" && sha256sum kubectl-convoy_"${version}"_* >checksums.txt)
