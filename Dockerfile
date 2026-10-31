# syntax=docker/dockerfile:1
#
# Self-contained multi-stage build for the convoy aggregated API server.
# `docker build .` compiles the binary and produces the runtime image — no
# prebuilt binary, `make`, or Go toolchain on the host required. Used by CI and
# `make kind-deploy`.

FROM golang:1.27-bookworm AS base
WORKDIR /src

# Download modules in a separate layer so source edits don't re-fetch deps.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

# Per-binary build stages. Selecting a final target only pulls in that binary's
# build stage (BuildKit prunes the rest), so the apiserver image doesn't compile
# the controller-manager and vice versa.
FROM base AS build-apiserver
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o /apiserver ./cmd/convoy-apiserver

FROM base AS build-controller-manager
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o /controller-manager ./cmd/convoy-controller-manager

FROM base AS build-observer
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o /observer ./cmd/convoy-observer

# Runtime images. debian-slim (not distroless) so Tilt's restart wrapper — which
# needs basic coreutils (touch/chmod) — still works if these are used for the
# live-reload dev loop. Build with `docker build --target controller-manager`.
FROM debian:stable-slim AS controller-manager
COPY --from=build-controller-manager /controller-manager /convoy-controller-manager
ENTRYPOINT ["/convoy-controller-manager"]

FROM debian:stable-slim AS observer
COPY --from=build-observer /observer /convoy-observer
ENTRYPOINT ["/convoy-observer"]

# apiserver is the last stage, so a plain `docker build` (CI) builds it by default.
FROM debian:stable-slim AS apiserver
COPY --from=build-apiserver /apiserver /convoy-apiserver
ENTRYPOINT ["/convoy-apiserver"]
