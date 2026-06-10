# syntax=docker/dockerfile:1
#
# Dev image for the convoy aggregated API server.
#
# It packages a pre-built static linux binary (see `make build-linux`) rather
# than compiling inside the image, so it pairs with Tilt live-update: Tilt syncs
# a freshly compiled bin/convoy-apiserver into the running container (overwriting
# /convoy-apiserver) and restarts the process in place.
#
# Base is debian-slim (not distroless) on purpose: Tilt's restart wrapper adds
# build steps that need basic coreutils (touch/chmod), which distroless lacks.
# The binary is CGO_ENABLED=0 static, so it runs on debian unchanged. Runs as
# root with a writable root fs so live-update can replace the binary — this is a
# dev-only image, not for production.
FROM debian:stable-slim

COPY bin/convoy-apiserver /convoy-apiserver

ENTRYPOINT ["/convoy-apiserver"]
