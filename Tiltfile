# -*- mode: Python -*-
#
# Tilt dev loop for the convoy aggregated API server against a kind cluster.
#
#   make kind-up     # create the kind cluster (once)
#   tilt up          # build, deploy, and live-reload on code changes
#   tilt down        # remove the workloads (the kind cluster stays)
#   make kind-down   # delete the kind cluster
#
# On a code change Tilt recompiles the binary, syncs it into the running
# apiserver container, and restarts the process in place — etcd data and the
# APIService registrations are preserved (only the apiserver process restarts).

load('ext://restart_process', 'docker_build_with_restart')

# Safety: only ever run against the local kind dev cluster.
allow_k8s_contexts('kind-convoy-dev')

# Compile static linux binaries (both the apiserver and the controller-manager)
# matching the kind node arch on source changes.
local_resource(
    'convoy-compile',
    cmd='make build-linux',
    deps=['cmd', 'internal', 'api', 'go.mod', 'go.sum'],
    ignore=['**/*_test.go'],
)

# Build the dev images from the prebuilt binaries using dev/Dockerfile.dev (the
# root Dockerfile compiles from source and is for CI / kind-deploy). Each build
# selects its target. The flags come from the Deployment's args; the restart
# wrapper owns the ENTRYPOINT, so syncing the new binary and restarting in place
# preserves those args.
docker_build_with_restart(
    'convoy-apiserver:dev',
    'bin',
    dockerfile='dev/Dockerfile.dev',
    target='apiserver',
    entrypoint=['/convoy-apiserver'],
    only=['convoy-apiserver'],
    live_update=[
        sync('bin/convoy-apiserver', '/convoy-apiserver'),
    ],
)
docker_build_with_restart(
    'convoy-controller-manager:dev',
    'bin',
    dockerfile='dev/Dockerfile.dev',
    target='controller-manager',
    entrypoint=['/convoy-controller-manager'],
    only=['convoy-controller-manager'],
    live_update=[
        sync('bin/convoy-controller-manager', '/convoy-controller-manager'),
    ],
)

k8s_yaml(kustomize('dev/deploy'))

k8s_resource('convoy-etcd', labels=['convoy'])
k8s_resource(
    'convoy-apiserver',
    resource_deps=['convoy-compile', 'convoy-etcd'],
    labels=['convoy'],
)
k8s_resource(
    'convoy-controller-manager',
    # Needs the aggregated API served before it can list/watch Clusters.
    resource_deps=['convoy-compile', 'convoy-apiserver'],
    labels=['convoy'],
)
