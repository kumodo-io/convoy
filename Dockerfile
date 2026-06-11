# syntax=docker/dockerfile:1
#
# Self-contained multi-stage build for the convoy aggregated API server.
# `docker build .` compiles the binary and produces the runtime image — no
# prebuilt binary, `make`, or Go toolchain on the host required. Used by CI and
# `make kind-deploy`.

FROM golang:1.26-bookworm AS build
WORKDIR /src

# Download modules in a separate layer so source edits don't re-fetch deps.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o /convoy-apiserver ./cmd/convoy-apiserver

# Runtime image. debian-slim (not distroless) so Tilt's restart wrapper — which
# needs basic coreutils (touch/chmod) — still works if this image is used for
# the live-reload dev loop.
FROM debian:stable-slim
COPY --from=build /convoy-apiserver /convoy-apiserver
ENTRYPOINT ["/convoy-apiserver"]
