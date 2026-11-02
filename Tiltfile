# -*- mode: Python -*-
#
# Tilt dev loop for the convoy aggregated API server against a kind cluster.
#
#   mise run kind-convoy-up     # create the kind cluster (once)
#   mise run tilt-up            # build, deploy, and live-reload on code changes
#   mise run tilt-down          # remove the workloads (the kind cluster stays)
#   mise run kind-convoy-down   # delete the kind cluster
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
    cmd='mise build-binaries',
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

# Render-store backend is selected at `tilt up` time:
#
#   tilt up                     # mem://  — in-process store, ephemeral (default)
#   tilt up -- --storage=file   # file:// on a PVC — durable across restarts
#   tilt up -- --storage=minio  # in-cluster MinIO (S3) — durable + shared
#
# Each is a kustomize overlay under dev/deploy/storage-<mode> on the single-replica
# base. (The single-replica / multi-replica overlays remain for the sharding demo.)
config.define_string('storage')
_storage = config.parse().get('storage', 'mem')
if _storage not in ('mem', 'file', 'minio'):
    fail('unknown --storage %r; expected one of: mem, file, minio' % _storage)
k8s_yaml(kustomize('dev/deploy/storage-' + _storage))

# MinIO backing store (only present in the minio overlay).
if _storage == 'minio':
    k8s_resource('minio', labels=['convoy'])
    k8s_resource('minio-mkbucket', resource_deps=['minio'], labels=['convoy'])

k8s_resource('convoy-etcd', labels=['convoy'])
k8s_resource(
    'convoy-apiserver',
    resource_deps=['convoy-compile', 'convoy-etcd'],
    labels=['convoy'],
)
# Needs the aggregated API served before it can list/watch Clusters; in minio mode
# also wait for the bucket so the first render persist doesn't retry against a
# missing bucket.
_cm_deps = ['convoy-compile', 'convoy-apiserver']
if _storage == 'minio':
    _cm_deps.append('minio-mkbucket')
k8s_resource(
    'convoy-controller-manager',
    resource_deps=_cm_deps,
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
    cmd='kubectl --context kind-convoy-dev wait --for=condition=Available --timeout=120s ' +
        'apiservice/v1alpha1.extensions.convoy.kumodo.io && ' +
        'kubectl --context kind-convoy-dev apply -f dev/deploy/health-rules.yaml',
    deps=['dev/deploy/health-rules.yaml'],
    resource_deps=['convoy-apiserver'],
    labels=['convoy'],
)
