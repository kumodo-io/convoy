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

# Compile static linux binaries (the apiserver, the controller-manager, and the
# observer) matching the kind node arch on source changes.
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
docker_build_with_restart(
    'convoy-observer:dev',
    'bin',
    dockerfile='dev/Dockerfile.dev',
    target='observer',
    entrypoint=['/convoy-observer'],
    only=['convoy-observer'],
    live_update=[
        sync('bin/convoy-observer', '/convoy-observer'),
    ],
)

k8s_yaml(kustomize('dev/deploy/single-replica'))

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
k8s_resource(
    'convoy-observer',
    # Needs the aggregated API served before it can list/watch Releases.
    resource_deps=['convoy-compile', 'convoy-apiserver'],
    labels=['convoy'],
)

# Default health rules are an instance of an aggregated kind (GlobalHealthRuleSet),
# so they can't be applied until the apiserver serves the extensions API. Applied
# out-of-band (not via k8s_yaml) after waiting for the APIService to be Available.
local_resource(
    'convoy-health-rules',
    cmd='bin/kubectl --context kind-convoy-dev wait --for=condition=Available --timeout=120s ' +
        'apiservice/v1alpha1.extensions.convoy.kumodo.io && ' +
        'bin/kubectl --context kind-convoy-dev apply -f dev/deploy/health-rules.yaml',
    deps=['dev/deploy/health-rules.yaml'],
    resource_deps=['convoy-apiserver'],
    labels=['convoy'],
)
