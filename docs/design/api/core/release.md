<!-- TOC -->
  * [Spec](#spec)
  * [Source Reference](#source-reference)
    * [Source × Content compatibility](#source--content-compatibility)
  * [Content Types](#content-types)
    * [helm](#helm)
    * [kustomize](#kustomize)
    * [manifests](#manifests)
    * [jsonnet](#jsonnet)
    * [cue](#cue)
  * [Mutation Types](#mutation-types)
    * [helmValues](#helmvalues)
    * [goTemplate](#gotemplate)
    * [jsonPatch](#jsonpatch)
    * [strategicMergePatch](#strategicmergepatch)
    * [celTransform](#celtransform)
  * [Status](#status)
  * [Conditions](#conditions)
  * [Release State Model](#release-state-model)
    * [The Three Signals](#the-three-signals)
      * [Applied](#applied)
      * [InSync](#insync)
      * [Healthy](#healthy)
    * [Reading the Three Signals Together](#reading-the-three-signals-together)
    * [Why Three Signals, Not One](#why-three-signals-not-one)
    * [Why These Names](#why-these-names)
    * [Example Status Block](#example-status-block)
    * [Vocabulary Mapping for Users Coming from Other Tools](#vocabulary-mapping-for-users-coming-from-other-tools)
    * [Field Reference](#field-reference)
  * [Health Assessment](#health-assessment)
    * [Purpose](#purpose)
    * [Health States](#health-states)
    * [How Health Is Determined](#how-health-is-determined)
      * [Built-in Rules](#built-in-rules)
      * [kstatus Fallback](#kstatus-fallback)
      * [Custom Rules via `KindHealthRule` and `ResourceHealthRule`](#custom-rules-via-kindhealthrule-and-resourcehealthrule)
    * [`KindHealthRule` Resource](#kindhealthrule-resource)
    * [`ResourceHealthRule` Resource](#resourcehealthrule-resource)
    * [Aggregation: Per-Resource → Release-Level `Healthy`](#aggregation-per-resource--release-level-healthy)
    * [Status Shape](#status-shape)
      * [`healthSummary`](#healthsummary)
    * [`/health` Subresource](#health-subresource)
      * [Per-resource fields](#per-resource-fields)
    * [What's Not Covered](#whats-not-covered)
    * [Comparison with Other Tools](#comparison-with-other-tools)
  * [Preview and Diff Subresources](#preview-and-diff-subresources)
    * [/preview](#preview)
      * [Behavior](#behavior)
      * [Output](#output)
      * [Pin handling](#pin-handling)
      * [Query parameters](#query-parameters)
      * [POST with mutation overrides](#post-with-mutation-overrides)
      * [Common usage patterns](#common-usage-patterns)
      * [RBAC](#rbac)
    * [/diff](#diff)
      * [Behavior](#behavior-1)
      * [Comparison axes](#comparison-axes)
      * [Query parameters](#query-parameters-1)
      * [Common usage](#common-usage)
      * [Pin interaction](#pin-interaction)
      * [POST with mutation overrides](#post-with-mutation-overrides-1)
      * [Output format](#output-format)
      * [Managed-fields filtering](#managed-fields-filtering)
      * [Drift categorization](#drift-categorization)
      * [RBAC](#rbac-1)
    * [Summary](#summary)
  * [Lifecycle Hooks](#lifecycle-hooks)
    * [Purpose](#purpose-1)
    * [Why a Subresource, Not Annotations](#why-a-subresource-not-annotations)
    * [Phases](#phases)
    * [Hook Schema](#hook-schema)
      * [Hook fields](#hook-fields)
    * [Templating context](#templating-context)
    * [The `if` Condition (skip)](#the-if-condition-skip)
    * [The `runAfter` Trigger (wait) — postSync only](#the-runafter-trigger-wait--postsync-only)
    * [Failure Handling](#failure-handling)
    * [Cleanup](#cleanup)
      * [`cleanup` — delete after the hook completes](#cleanup--delete-after-the-hook-completes)
      * [`deleteBeforeCreation` — delete the prior workload on re-arm](#deletebeforecreation--delete-the-prior-workload-on-re-arm)
      * [The two together](#the-two-together)
    * [Ownership Tracking](#ownership-tracking)
    * [Lifecycle Retention Policy](#lifecycle-retention-policy)
    * [Helm Chart Compatibility](#helm-chart-compatibility)
    * [Status Reflection](#status-reflection)
      * [Per-hook status fields](#per-hook-status-fields)
      * [Condition and printer column](#condition-and-printer-column)
    * [Interaction with Sync Waves](#interaction-with-sync-waves)
    * [Mutation Pipeline Interaction](#mutation-pipeline-interaction)
    * [Subresource](#subresource)
    * [Summary Table](#summary-table)
    * [Validation](#validation)
  * [Sync Waves](#sync-waves)
    * [Purpose](#purpose-2)
    * [Scope](#scope)
    * [Wave Specification via Annotations](#wave-specification-via-annotations)
      * [Wave Numbering](#wave-numbering)
      * [Annotations Recognized](#annotations-recognized)
    * [Wave Transition Strategies](#wave-transition-strategies)
      * [Why Wave Strategies Matter](#why-wave-strategies-matter)
    * [Status Reflection](#status-reflection-1)
    * [Resource Selection by Wave](#resource-selection-by-wave)
    * [Mutation Pipeline Interaction](#mutation-pipeline-interaction-1)
  * [Release-within-Cluster Ordering (Axis B)](#release-within-cluster-ordering-axis-b)
    * [What's Not Addressed Here](#whats-not-addressed-here)
    * [Summary Table](#summary-table-1)
<!-- TOC -->

The deployment unit. Binds one `Source` + one `Cluster` + a content interpretation, with an ordered mutation pipeline. Self-describing via resolved status snapshots. Identity (source **kind + name**, cluster, content type) is immutable; routine config (the **source ref/version**, mutations, paths) is mutable.

`spec.source` and `spec.content` are two orthogonal axes: **source = where + which** (the endpoint object plus the coordinate to fetch), **content = how** (how to render the fetched bytes). Both are discriminated unions on `.type`, mirroring `auth`. The `Source` object (`GitRepository`/`HelmRegistry`/`OCIRegistry`) is a reusable, credentialed *endpoint* — it does not pin a version; the coordinate lives on the Release: `spec.source.gitRepository.ref` (`branch`/`tag`/`commit`/`semver`), `spec.source.helmRegistry.chart{name,version}`, or `spec.source.ociRegistry.artifact{name,version}`. So one Source is shared across Releases pulling different versions — e.g. staging on `branch: main`, prod on `tag: v1.2.3` from one `GitRepository`.

**Subresources**

- `/status` — observed reconciliation state, resolved snapshots, last successful + last attempted records
- `/mutations` — the ordered transformation pipeline, separately addressable for field-level RBAC and audit
- `/sync` — POST to trigger reconciliation
- `/diff` — GET diff between rendered state and live cluster
- `/preview` — GET the fully rendered post-mutation manifest set (without applying)
- `/pin` — pin state (GET to read, POST to set, DELETE to clear)
- `/history` — GET paginated list of past (inputs → output) records, S3-backed
- `/health` — GET per-resource health for managed resources (re-evaluated at runtime; not persisted in `status`)

```yaml
apiVersion: core.convoy.kumodo.io/v1alpha1
kind: Release
metadata:
  name: payments-api-prod-eu
  namespace: payments-team
  generation: 42
  resourceVersion: "1847263"
  uid: 8f3a2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c
  labels:
    app: payments-api
    env: prod
    region: eu-west-1
  annotations:
    convoy.kumodo.io/owner: payments-team@example.com

spec:
  # WHERE to get the artifact + WHICH version. Discriminated union on source.type,
  # mirroring auth/content. Each block references a reusable Source object (which
  # holds only URL + auth) and adds the coordinate to fetch.
  # IMMUTABLE: source.type and the referenced name. MUTABLE: the coordinate.
  source:
    # type: gitRepository | helmRegistry | ociRegistry
    type: gitRepository

    gitRepository:
      name: payments-charts-repo
      # namespace: payments-team   # defaults to the Release's namespace
      # Which revision to fetch — exactly one of {branch, tag, commit, semver}.
      ref:
        branch: main
        # tag: v1.2.3
        # commit: abc123def456789aabbccddeeff001122334455
        # semver: ">=1.2.0 <2.0.0"

    # helmRegistry:
    #   name: payments-helm-repo
    #   chart:
    #     name: payments-api
    #     version: ">=1.2.0 <2.0.0"

    # ociRegistry:
    #   name: payments-oci-repo
    #   artifact:
    #     name: payments-api
    #     version: ">=1.2.0 <2.0.0"

  # HOW to interpret the fetched artifact (rendering). Discriminated on content.type.
  # IMMUTABLE: content.type. MUTABLE: render options (paths, dependencyMode, ...).
  # Must be compatible with source.type (see "Source × Content compatibility").
  content:
    # type: helm | kustomize | manifests | jsonnet | cue
    type: helm

    # type: helm — render a Helm chart.
    helm:
      # Defaults to chart name if not set
      releaseName: payments-api-dev
      
      # Defaults to the 'default' namespace if not set
      releaseNamespace: payments-team
      
      # Path to the chart within a GitRepository tree. Omit for helmRegistry/
      # ociRegistry sources (the chart/artifact is selected in spec.source).
      path: charts/payments-api

      # How to handle subchart dependencies.
      # - vendored: chart's charts/ dir is pre-populated (default, more reproducible)
      # - resolve:  run `helm dependency update` at render time (requires network)
      dependencyMode: vendored

      # The list of sources of helm chart values to be applyed
      values:
        - type: gitRepository
          gitRepository:
            name: <git-repository-name>
            namespace: ... # optional
            ref:
              branch: main
              # tag: v1.2.3
              # commit: abc123def456789aabbccddeeff001122334455
              # semver: ">=1.2.0 <2.0.0"
            paths:
              - path/to/values/file/1
              - path/to/values/file/2
        - type: configMap
          configMap:
            name: <config-map-name>
            key: <key-containing-values>
        - type: secret
          secret:
            name: <secret-name>
            key: <key-containing-values>
        - type: inline
          inline:
            key-one: string-values
            key-two: true
            # other values
    
    # type: kustomize — for Kustomize overlays.
    # kustomize:
    #   path: overlays/prod
    
    # type: manifests — for raw YAML files.
    # manifests:
    #   paths: ["deploy/", "config/*.yaml"]
    
    # type: jsonnet — for jsonnet-rendered manifests.
    # jsonnet:
    #   path: main.jsonnet
    #   tla:
    #     - name: env
    #       value: prod
  
  # Reference to the target Cluster.
  # IMMUTABLE after creation.
  clusterRef:
    name: prod-eu
    # namespace: platform-fleet  # if cluster is in a different namespace
  
  # Sync behavior controlling how reconciliation drives the cluster toward desired state.
  syncPolicy:
    # Auto-sync vs manual (POST /sync to trigger).
    automatic: true
    
    # Remove resources from cluster that exist in cluster but not in rendered state.
    # Independent of deletion policy; this is steady-state pruning.
    prune:
      # Delete resources this Release previously applied but no longer renders.
      orphaned: true
      # Also prune orphaned CustomResourceDefinitions (dangerous — cascades to all CRs).
      includeCRDs: false
      # Also prune orphaned Namespaces (dangerous)
      includeNamespaces: false
    
    # Reconciliation interval for drift detection (compare live vs rendered).
    interval: 3m
    
    # Backoff for failed sync attempts.
    retryBackoff:
      initial: 30s
      max: 10m
      factor: 2

    # Axis B — this Release's ordering relative to other Releases on the SAME cluster.
    # (Resource ordering WITHIN a Release is Axis A, set via per-manifest
    # convoy.kumodo.io/sync-* annotations — see Sync Waves.)
    waveStrategy:
      # This Release's cluster-wave; it waits for lower-wave Releases on the cluster. Default 0.
      wave: 0
      # strategy: waitForApplied | waitForHealthy | waitForHealthyWithTimeout
      strategy: waitForApplied
      # timeout for waitForHealthyWithTimeout. If not set, the controller waits indefinitely.
      timeout: 10m
  
  # How many historical (rendered, applied) records to retain in S3.
  # Drives /history pagination depth and rollback range.
  retentionPolicy:
    keepHistory: 50

  # Per-Release deletion behaviour - what happens when the specific Release is deleted
  deletionPolicy:
    # defaults to false, if set set to true, only Release object will be deleted without
    # touching deployed resources.
    keepWorkloads: true | false
    # defaults to false, if set to true, Release object deletion will be blocked until
    # cluster reachability restored. If it's set to false controller will attempt to
    # prune deployed resources, but in case of cluster is unreachable Release object
    # will be deleted without deployed resources cleanup.
    blockOnClusterUnreachable: true | false

# Top-level field (not part of spec). Holds the ordered transformation pipeline.
# Exposed via the /mutations subresource for field-level RBAC and audit clarity.
# +listType=atomic — list is treated as a single unit; concurrent edits conflict.
mutations:
  # 1. Environment-wide values (shared across all prod releases).
  - type: helmValues
    from:
      kind: GitRepository
      name: payments-config-repo
      path: env/prod/values.yaml
  
  # 2. Cluster-specific values (per-region overrides).
  - type: helmValues
    from:
      kind: GitRepository
      name: payments-config-repo
      path: clusters/prod-eu/values.yaml
  
  # 3. Templated values using cluster metadata.
  # Default: applied AFTER Helm rendering on the rendered YAML, to avoid {{ }} collisions.
  # Template context: .Cluster, .Release, .Source.
  - type: goTemplate
    inline: |
      replicaCount: {{ .Cluster.metadata.replicas | default 3 }}
      region: {{ .Cluster.metadata.region }}
      ingress:
        host: payments-{{ .Cluster.metadata.region }}.example.com
      podLabels:
        cluster: {{ .Cluster.name }}
  
  # 4. Small inline JSON patch — direct overrides without templating.
  - type: jsonPatch
    patches:
      - op: replace
        path: /spec/template/spec/containers/0/resources/requests/memory
        value: 512Mi
      - op: add
        path: /spec/template/metadata/annotations/co.elastic.logs~1json.keys_under_root
        value: "true"
  
  # 5. Strategic merge patch from a separate source (e.g., experimental feature).
  - type: strategicMergePatch
    from:
      kind: GitRepository
      name: payments-experiments-repo
      path: features/new-tracing/patch.yaml

# Top-level field (not part of spec). Ordered lifecycle hooks, keyed by phase.
# Exposed via the /lifecycle subresource for field-level RBAC and audit clarity.
# Full schema in the "Lifecycle Hooks" section below.
lifecycle:
  preSync:
    - name: db-migration
      type: Job
      if: 'cluster.tier == "production"'
      jobTemplate: { ... }          # { metadata (partial), spec: JobSpec }
      weight: 0
      onFailure: Fail
  postSync:
    - name: smoke-test
      type: Job
      runAfter: Healthy             # postSync only: wait until the release is healthy
      jobTemplate: { ... }
      onFailure: Continue

pin:
  renderRef: sha256:abc123def456789aabbccddeeff001122334455667788990011223344556677
  pinnedAt: 2026-04-29T10:00:00Z
  pinnedBy: alice@example.com
  reason: "Production incident #4521 — payment failures after deploy"
        
status:
  observedGeneration: 42
  
  # Three core conditions are written (Applied, InSync, Healthy), plus
  # LifecycleSucceeded when the Release has hooks (see Conditions). Render-pipeline
  # progress is in lastAttempted; pin state is in status.pin (and surfaced as the
  # Applied condition's reason). A composite Ready and dedicated SourceResolved/
  # MutationsResolved/Rendered/Pinned conditions are planned, not implemented.
  conditions:
    - type: Applied
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:05Z
      reason: ApplySucceeded
      message: "Last apply at 10:00:05; 14 resources applied successfully"
    - type: InSync
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:30Z
      reason: NoDriftDetected
      message: "All managed resources match rendered state"
    - type: Healthy
      status: "False"
      lastTransitionTime: 2026-04-29T10:01:15Z
      reason: WorkloadsDegraded
      message: "1 of 14 resources degraded: Deployment payments-api has 0/3 ready replicas (image pull failed)"

    # If the user has edited spec or mutations since pinning, this condition appears
    # to warn that there's pending intent not yet applied because of the active pin.
    - type: SpecDriftFromPin
      status: "True"
      lastTransitionTime: 2026-04-29T11:30:00Z
      reason: SpecChangedWhilePinned
      message: "Spec/mutations have been edited since pin was set. Changes will apply on unpin."

  # Pin state mirror — the *fact* of being pinned, surfaced for general visibility.
  # Full details (reason, who, etc.) live behind the /pin subresource for RBAC.
  pin:
    intent: freeze | rollback
    # Snapshot of what produced the pinned render — useful for diff against current spec/mutations.
    renderInputs:
      sourceKind: GitRepository
      sourceName: payments-charts-repo
      sourceRevision: 7788990011223344556677aabbccddeeff001122
      contentType: helm
      contentPath: charts/payments-api
      mutationsHash: sha256:5566778899aabbccddeeff00112233445566778899aabbccddeeff0011223344
  
  # Retention policy resolved into operational state.
  resolvedRetentionPolicy:
    keepHistory: 50
    historyCount: 23
  
  # Last successful render — represents what's actually deployed in the cluster.
  lastSuccessful:
    # Snapshot of resolved Source spec (not just a reference).
    # Captures URL, ref, etc. as they were at render time, even if Source object changes later.
    resolvedSource:
      kind: GitRepository
      name: payments-charts-repo
      uid: 0c81fcef-4470-4bf7-87e3-24c776cb222c
      observedGeneration: 7    # Source.metadata.generation at render time
      url: https://github.com/example/payments-charts
      ref: main                       # spec.ref from Source
      revision: abc123def456789aabbccddeeff001122334455   # actual resolved Git SHA
      contentType: helm
      path: charts/payments-api
    
    # Snapshot of resolved Cluster (kubeconfig server, metadata used for templating).
    resolvedCluster:
      name: prod-eu
      uid: 7119fd26-3d58-4b18-88b9-5e0f4c9c76a0
      observedGeneration: 3
      server: https://eks-prod-eu.example.com
      metadataSnapshot:
        region: eu-west-1
        tier: production
        replicas: "5"
        cni: cilium
    
    # One entry per mutation in spec, with content hashes for reproducibility.
    resolvedMutations:
      - index: 0
        type: helmValues
        sourceRef: { kind: GitRepository, name: payments-config-repo, path: env/prod/values.yaml }
        sourceRevision: 1a2b3c4d5e6f7890abcdef1234567890abcdef12
        contentHash: sha256:7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e
      - index: 1
        type: helmValues
        sourceRef: { kind: GitRepository, name: payments-config-repo, path: clusters/prod-eu/values.yaml }
        sourceRevision: 1a2b3c4d5e6f7890abcdef1234567890abcdef12
        contentHash: sha256:9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a
      - index: 2
        type: goTemplate
        contentHash: sha256:0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b
      - index: 3
        type: jsonPatch
        contentHash: sha256:1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c
      - index: 4
        type: strategicMergePatch
        sourceRef: { kind: GitRepository, name: payments-experiments-repo, path: features/new-tracing/patch.yaml }
        sourceRevision: 2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d
        contentHash: sha256:2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d
    
    # S3 hash of the fully rendered manifest set. Lookup key for /history and /rollback.
    renderRef: sha256:abc123def456789aabbccddeeff001122334455667788990011223344556677
    
    syncedAt: 2026-04-29T10:00:05Z
    syncedResources: 14
  
  # Most recent reconciliation attempt — may be in-progress, succeeded, or failed.
  # When phase: Succeeded and renderRef matches lastSuccessful.renderRef, all is well.
  # When divergent, status describes what's being attempted and any failure.
  lastAttempted:
    resolvedSource:
      kind: GitRepository
      name: payments-charts-repo
      observedGeneration: 7
      url: https://github.com/example/payments-charts
      ref: main
      revision: abc123def456789aabbccddeeff001122334455
      contentType: helm
      path: charts/payments-api
    resolvedCluster:
      name: prod-eu
      observedGeneration: 3
      server: https://eks-prod-eu.example.com
      metadataSnapshot:
        region: eu-west-1
        tier: production
        replicas: "5"
        cni: cilium
    resolvedMutations:
      - { index: 0, type: helmValues, sourceRevision: 1a2b3c4d5e6f7890abcdef1234567890abcdef12, contentHash: "sha256:7d8e9f..." }
      - { index: 1, type: helmValues, sourceRevision: 1a2b3c4d5e6f7890abcdef1234567890abcdef12, contentHash: "sha256:9f0a1b..." }
      - { index: 2, type: goTemplate, contentHash: "sha256:0a1b2c..." }
      - { index: 3, type: jsonPatch,  contentHash: "sha256:1b2c3d..." }
      - { index: 4, type: strategicMergePatch, sourceRevision: 2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d, contentHash: "sha256:2c3d4e..." }
    renderRef: sha256:abc123def456789aabbccddeeff001122334455667788990011223344556677
    attemptedAt: 2026-04-29T10:00:00Z
    
    # phase: Rendering | Syncing | Succeeded | Failed
    phase: Succeeded
    
    # Set only when phase: Failed.
    # failure:
    #   stage: SourceResolution | ClusterResolution | MutationResolution | Rendering | Syncing | HookFailed
    #   mutationIndex: 1   # if failure during mutation pipeline
    #   message: "failed to fetch values file: 404 not found"
    #   waveNumber: 1
    #   hookName: "my-hook"   # set when stage: HookFailed
    #   hookPhase: PreSync    # set when stage: HookFailed

  # Per-wave apply progress (Axis A). listType=map keyed by number. The controller
  # owns `applied`; `healthy` is rolled up from the observer-written ReleaseInventory.
  waves:
    - number: -10
      resources: 1                      # CRD
      applied: true
      healthy: true
      state: Completed
      startedAt: 2026-04-29T10:00:00Z
      completedAt: 2026-04-29T10:00:15Z

    - number: 0
      resources: 12                     # most resources
      applied: true
      healthy: false                    # applied; health still rolling up
      state: Completed
      startedAt: 2026-04-29T10:00:15Z
      completedAt: 2026-04-29T10:00:15Z
  
  # Last reconciliation timestamp, even when nothing changed.
  # Distinct from lastAttempted (which only updates on actual change attempts).
  reconciledAt: 2026-04-29T10:03:00Z
  
  historyTruncated: false
```

## Spec

`spec` binds a source, a content interpretation, and a target cluster, plus sync/retention/deletion policy. Identity fields (`source.type` + the referenced source name, `content.type`, `cluster`) are **immutable** after creation — to re-target, create a new Release. Routine fields (the source coordinate — `ref`/`chart.version`/`artifact.version` — render options, mutations, policies) are mutable.

| Field             | Type   | Required | Default | Description                                                                                                                                                                                                         |
|-------------------|--------|----------|---------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `source`          | object | yes      | —       | WHERE + WHICH to fetch. Discriminated on `source.type` (`gitRepository`/`helmRegistry`/`ociRegistry`); each block references a Source object and carries the coordinate. See [Source Reference](#source-reference). |
| `content`         | object | yes      | —       | HOW to render the fetched bytes. Discriminated on `content.type`; see [Content Types](#content-types). Must be compatible with `source.type`. `content.type` immutable.                                             |
| `cluster`         | object | yes      | —       | Reference (`ref` [+ `namespace`]) to the target `Cluster`. Immutable.                                                                                                                                               |
| `syncPolicy`      | object | no       | —       | Reconciliation behavior: `automatic`, `pruneOrphaned`, `interval`, `retryBackoff`, `waveStrategy` (see [Sync Waves](#sync-waves)).                                                                                  |
| `retentionPolicy` | object | no       | —       | `keepHistory` — number of historical (rendered, applied) records to retain in S3; drives `/history` depth and rollback range.                                                                                       |
| `deletionPolicy`  | object | no       | —       | Per-Release deletion behavior (`workloads: prune \| leave`).                                                                                                                                                        |

`mutations` is a **top-level field** (not under `spec`), exposed via the `/mutations` subresource for field-level RBAC and audit. It is an ordered, atomic (`+listType=atomic`) transformation pipeline; see [Mutation Types](#mutation-types).

`lifecycle` is likewise a **top-level field** (not under `spec`), exposed via the `/lifecycle` subresource for field-level RBAC and audit. It holds the phase-keyed hook lists; see [Lifecycle Hooks](#lifecycle-hooks). Execution state is mirrored under `status.lifecycle`.

## Source Reference

`spec.source` is a discriminated union on `source.type` (mirroring `auth`/`content`): exactly one of the per-type blocks is set. Each references a reusable, credentialed `Source` object (by `name` [+ `namespace`]) and carries the **coordinate** that selects which version to fetch. `source.type` and the referenced `name` are immutable; the coordinate is mutable.

| `source.type`   | References      | Coordinate (mutable)                                                        |
|-----------------|-----------------|-----------------------------------------------------------------------------|
| `gitRepository` | `GitRepository` | `gitRepository.ref` — one of `branch`/`tag`/`commit`/`semver`. Most common. |
| `helmRegistry`  | `HelmRegistry`  | `helmRegistry.chart.{name, version}`.                                       |
| `ociRegistry`   | `OCIRegistry`   | `ociRegistry.artifact.{name, version}`.                                     |

For `gitRepository`, `ref` is a discriminated union — exactly one of:

| `ref` field | Resolution                                                                     |
|-------------|--------------------------------------------------------------------------------|
| `branch`    | Branch tip; re-resolves over time so new commits are picked up.                |
| `tag`       | The commit a tag points at; stable unless the tag is moved upstream.           |
| `commit`    | A pinned SHA — fully immutable.                                                |
| `semver`    | Highest tag satisfying the constraint; picks up new releases within the range. |

```yaml
spec:
  source:
    type: gitRepository          # | helmRegistry | ociRegistry
    gitRepository:
      name: payments-charts-repo
      # namespace: payments-team   # defaults to the Release's namespace
      ref:
        branch: main               # | tag: v1.2.3 | commit: <sha> | semver: ">=1.2.0 <2.0.0"
    # helmRegistry:
    #   name: payments-helm-repo
    #   chart: { name: payments-api, version: ">=1.2.0 <2.0.0" }
    # ociRegistry:
    #   name: payments-oci-repo
    #   artifact: { name: payments-api, version: ">=1.2.0 <2.0.0" }
```

### Source × Content compatibility

`source.type` (where) and `content.type` (how) are orthogonal, but not every pairing is valid:

| `source.type` ↓ / `content.type` → | `helm` | `manifests` | `kustomize` | `jsonnet` | `cue` |
|------------------------------------|:------:|:-----------:|:-----------:|:---------:|:-----:|
| `gitRepository`                    |   ✅   |     ✅      |     ✅      |    ✅     |  ✅   |
| `helmRegistry`                     |   ✅   |      —      |      —      |     —     |   —   |
| `ociRegistry`                      |   ✅   |     ✅      |     ✅      |     —     |   —   |

Incompatible pairings are rejected by validation (e.g. `helmRegistry` requires `content.type: helm`).

## Content Types

`spec.content.type` is a discriminated union describing how the fetched artifact is rendered into a manifest set. `content.type` is immutable; the render options are mutable. The "which version" coordinate is **not** here — it lives in [`spec.source`](#source-reference).

### helm

Render a Helm chart. `path` is the chart directory within a `gitRepository` tree; omit it for `helmRegistry`/`ociRegistry` sources, where the chart/artifact is selected in `spec.source`. `dependencyMode` controls subchart handling: `vendored` (chart's `charts/` is pre-populated — default, reproducible) or `resolve` (`helm dependency update` at render time — requires network).

```yaml
spec:
  content:
    type: helm
    helm:
      path: charts/payments-api      # gitRepository only; registry sources select the chart in spec.source
      dependencyMode: vendored       # | resolve
```

### kustomize

Build a Kustomize overlay at `path`.

```yaml
spec:
  content:
    type: kustomize
    kustomize:
      path: overlays/prod
```

### manifests

Use raw YAML manifests directly, selected by `paths` (files or globs).

```yaml
spec:
  content:
    type: manifests
    manifests:
      paths: ["deploy/", "config/*.yaml"]
```

### jsonnet

Render Jsonnet to manifests, optionally passing top-level arguments (`tla`).

```yaml
spec:
  content:
    type: jsonnet
    jsonnet:
      path: main.jsonnet
      tla:
        - name: env
          value: prod
```

### cue

Render CUE to manifests from an entry path. Type-specific config mirrors the other path-based content types.

```yaml
spec:
  content:
    type: cue
    cue:
      path: ./manifests
```

## Mutation Types

`mutations[]` is an ordered pipeline; each entry declares its `type`. Entries run in order, each operating on the output of the previous one. Inline content is provided directly; external content is referenced via `from` (typically a `GitRepository`).

### helmValues

Merge Helm values before chart rendering. Sourced from a values file (`from`) or inline.

```yaml
mutations:
  - type: helmValues
    from:
      kind: GitRepository
      name: payments-config-repo
      path: env/prod/values.yaml
```

### goTemplate

Apply a Go template. By default it runs **after** Helm rendering on the rendered YAML (to avoid `{{ }}` collisions). Template context: `.Cluster`, `.Release`, `.Source`. Provided `inline` or via `from`.

```yaml
mutations:
  - type: goTemplate
    inline: |
      replicaCount: {{ .Cluster.metadata.replicas | default 3 }}
      region: {{ .Cluster.metadata.region }}
```

### jsonPatch

Apply an RFC 6902 JSON Patch — direct, untemplated field overrides.

```yaml
mutations:
  - type: jsonPatch
    patches:
      - op: replace
        path: /spec/template/spec/containers/0/resources/requests/memory
        value: 512Mi
```

### strategicMergePatch

Apply a Kubernetes strategic merge patch (kind-aware list/map merging). Provided inline or via `from`.

```yaml
mutations:
  - type: strategicMergePatch
    from:
      kind: GitRepository
      name: payments-experiments-repo
      path: features/new-tracing/patch.yaml
```

### celTransform

Transform the rendered manifest set with a CEL expression, with the same binding context available to `goTemplate`. Useful for programmatic edits that are awkward to express as a patch. (Planned beyond the PoC; included here for completeness.)

```yaml
mutations:
  - type: celTransform
    inline: |
      # CEL expression operating over the rendered objects
```

## Status

| Field                     | Type        | Description                                                                                                                                                                         |
|---------------------------|-------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `observedGeneration`      | integer     | `spec` generation last reconciled.                                                                                                                                                  |
| `conditions`              | []Condition | See [Conditions](#conditions).                                                                                                                                                      |
| `healthSummary`           | object      | Aggregate health counts (`total`/`healthy`/`progressing`/`degraded`/`suspended`/`unknown`). The per-resource detail is served by [`/health`](#health-subresource), not stored here. |
| `pin`                     | object      | Mirror of the *fact* of being pinned (`intent: freeze \| rollback`, `renderInputs`); full detail behind `/pin`.                                                                     |
| `resolvedRetentionPolicy` | object      | Retention resolved into operational state (`keepHistory`, `historyCount`).                                                                                                          |
| `lastSuccessful`          | object      | Last successful render — what is actually deployed: resolved source/cluster/mutation snapshots, `renderRef`, `syncedAt`, `syncedResources`.                                         |
| `lastAttempted`           | object      | Most recent reconciliation attempt (may be in-progress/failed): same snapshots plus `phase` (`Rendering \| Syncing \| Succeeded \| Failed`) and a `failure` block when failed.      |
| `waves`                   | []object    | Per-wave apply progress; see [Sync Waves](#sync-waves).                                                                                                                             |
| `reconciledAt`            | timestamp   | Last reconciliation timestamp, even when nothing changed (distinct from `lastAttempted`).                                                                                           |
| `historyTruncated`        | bool        | Whether retained history was truncated below requested depth.                                                                                                                       |

## Conditions

`Release` reports **three** core conditions in `status.conditions`, each with a single writer (there is intentionally no composite condition — see [Release State Model](#release-state-model)). A fourth, `LifecycleSucceeded`, appears only when the Release has lifecycle hooks:

| Condition            | Writer                 | What `True` means                                                                                                                                                                               |
|----------------------|------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `Applied`            | release controller     | The most recent apply to the cluster succeeded (kube-apiserver accepted every resource). See [State Model](#applied).                                                                           |
| `InSync`             | observer (drift loop)  | The live cluster state matches the rendered manifest set (no drift). See [State Model](#insync).                                                                                                |
| `Healthy`            | observer (health loop) | Every managed resource reached its intended runtime state (for the current render). `Unknown`/`Pending` while progressing or not yet applied. See [State Model](#healthy).                      |
| `LifecycleSucceeded` | release controller     | All applicable lifecycle hooks succeeded (or were skipped). Absent when the Release has no hooks; `Unknown` while any runs; `False` when any failed. See [Lifecycle Hooks](#status-reflection). |

Progress that is *not* a condition:

- **Render pipeline stages** (source resolution, mutation resolution, rendering) are reported through `status.lastAttempted.phase` and, on failure, `status.lastAttempted.failure.stage` — not as separate conditions.
- **Pin state** is reported in `status.pin`; while a pin holds, the `Applied` condition carries `reason: Pinned`.

> **Planned (not implemented).** The following are design intent and are **not** currently written: a composite **`Ready`** condition (`Applied ∧ InSync ∧ Healthy`), and dedicated **`SourceResolved`** / **`MutationsResolved`** / **`Rendered`** / **`Pinned`** / **`SpecDriftFromPin`** conditions. Today the render stages and pin state are surfaced via `status.lastAttempted` / `status.pin` as described above.

## Release State Model

A deployed `Release` has three orthogonal state signals reflecting different aspects of the deployment. They are reported as separate conditions in `Release.status.conditions`, each with a single writer. There is **intentionally no composite `Ready` condition** — collapsing the signals would require a single writer to own all three (they are produced by different components) and would lose the diagnostic split below. Tooling watches the three signals directly.

### The Three Signals

#### Applied

**Question answered:** "Did the controller successfully apply the rendered manifests to the cluster?"

**Type:** Past-tense fact about the controller's action.

`Applied: True` means the most recent apply attempt succeeded — the kube-apiserver accepted all create/update/patch operations for every resource in the rendered manifest set. This is the boundary of the controller's responsibility: it's about whether the tool did its job, not whether the resulting deployment is working.

`Applied: False` means the most recent apply failed in whole or in part. Common causes: RBAC denial, admission webhook rejection, validation failure, network failure between controller and target cluster.

`Applied` does not imply that the cluster currently matches the manifests (drift may have occurred since the apply) or that workloads are functioning (apply success is independent of runtime state).

#### InSync

**Question answered:** "Does the cluster currently match the rendered manifests?"

**Type:** Present-tense fact about cluster state vs desired state.

`InSync: True` means drift detection compared the live state of managed resources against the most recently rendered manifest set and found them equivalent. No external modifications, no manual edits, no field-ownership conflicts.

`InSync: False` means drift was detected. The resources still exist in the cluster, but their current state diverges from what was last applied. Causes:

- A user ran `kubectl edit` on a managed resource
- Another controller (HPA, KEDA, custom operator) modified a field your Release manages
- Server-side apply field-ownership conflict where another field manager won
- A resource was deleted out-of-band

Drift may be transient (the controller will re-apply on next reconciliation) or persistent (when there's a genuine ownership conflict). The distinction is visible in repeated `InSync: False` transitions.

`InSync` does not imply healthy workloads — a Deployment can match its manifest exactly while having zero ready replicas.

#### Healthy

**Question answered:** "Are the deployed workloads functioning as intended?"

**Type:** Present-tense fact about runtime state of workloads.

`Healthy: True` means every resource managed by the Release has reached its intended runtime state per its kind-specific health rule:

- `Deployment` — `readyReplicas == replicas`, `observedGeneration` current
- `StatefulSet` — all replicas at current revision and ready
- `Job` — completed successfully (`succeeded > 0`)
- `Pod` — `Running` phase, all containers ready
- `PersistentVolumeClaim` — `Bound`
- `Service` (LoadBalancer) — `loadBalancer.ingress` populated
- Custom resources — controller-defined readiness (e.g., `Certificate.status.conditions[Ready]`)

`Healthy: False` means at least one resource is degraded — failing to come up, stuck progressing past a deadline, or in a known-bad state.

`Healthy: Unknown` is a meaningful third value here: a Pod that's still pulling its image isn't unhealthy, it's progressing. A Deployment that just started rolling out isn't unhealthy. The health assessment must distinguish "still working on it" from "broken." `Healthy` is also reported `Unknown` with reason `Pending` when the **current render** is not yet reflected on the cluster — nothing is deployed yet, or a re-render (or a gating `preSync` hook) means the assessed resources still belong to a prior render. Health is always assessed against the render the resource was actually applied under, so a prior render's healthy resources never read as the new render's `True`.

`Healthy` is independent of `Applied` and `InSync`. A Release can be applied, in-sync, and unhealthy (manifest is correct, workloads aren't working). A Release can be unapplied (apply failed) but its previously-deployed workloads are still healthy (cluster still has the prior version running). During a rolling update, `Healthy` can legitimately stay `True` — the current render's workloads never went down — while `InSync` flips `False` until convergence.

### Reading the Three Signals Together

There is no `Ready` condition; consumers combine the three signals themselves. The common combinations:

| `Applied` | `InSync` | `Healthy` | Meaning                                                               |
|-----------|----------|-----------|-----------------------------------------------------------------------|
| `True`    | `True`   | `True`    | Steady state — deployed and working                                   |
| `True`    | `True`   | `Unknown` | Progressing — workloads coming up                                     |
| `True`    | `True`   | `False`   | Deployment broken — manifests applied but workloads not working       |
| `True`    | `False`  | *         | Drift — cluster has diverged from desired                             |
| `False`   | *        | *         | Apply failed (or a wave/cluster-wave hold) — see the `Applied` reason |

> **Planned.** A composite `Ready` (`Applied ∧ InSync ∧ Healthy`) may be added later as a convenience for dashboards. It is not written today.

### Why Three Signals, Not One

Operators ask three distinct questions during deployment troubleshooting:

| Question                             | Signal    | Diagnostic path                                         |
|--------------------------------------|-----------|---------------------------------------------------------|
| Did my deploy go through?            | `Applied` | Controller logs, RBAC, admission webhooks, network      |
| Is something modifying my resources? | `InSync`  | Drift inspection, field-ownership, external controllers |
| Are my workloads working?            | `Healthy` | Pod logs, events, resource constraints, image issues    |

Different failure modes, different remediation. Collapsing into a single signal loses diagnostic clarity and forces tooling to re-derive the underlying signals to be useful.

### Why These Names

| Name      | Why this name                                                             | Names rejected and why                                                                                                                             |
|-----------|---------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------|
| `Applied` | Plain English; describes what the controller did. No CD-tool baggage.     | `Synced` — ambiguous between "manifests pushed" (ArgoCD) and "fully converged" (intuitive reading). Causes recurring user confusion.               |
| `InSync`  | Plain English; describes the cluster's current state relative to desired. | `Reconciled` — k8s vocabulary error. Reconciliation is the controller's *process*, not an outcome. Every Release is being reconciled at all times. |
| `Healthy` | Standard term across infrastructure tooling. Unambiguous.                 | `Available`, `Working`, `Functional` — all mean roughly the same thing; `Healthy` is the most established.                                         |

### Example Status Block

```yaml
status:
  conditions:
    - type: Applied
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:05Z
      reason: ApplySucceeded
      message: "Last apply at 10:00:05; 14 resources applied successfully"
    
    - type: InSync
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:30Z
      reason: NoDriftDetected
      message: "All managed resources match rendered state"
    
    - type: Healthy
      status: "False"
      lastTransitionTime: 2026-04-29T10:01:15Z
      reason: WorkloadsDegraded
      message: "1 of 14 resources degraded: Deployment payments-api has 0/3 ready replicas (image pull failed)"
```

Here the Release is applied and in-sync but not healthy — a consumer reads that directly from the three signals; there is no aggregate condition.

### Vocabulary Mapping for Users Coming from Other Tools

| Concept                        | This tool             | ArgoCD                 | Flux                                     |
|--------------------------------|-----------------------|------------------------|------------------------------------------|
| Manifests successfully applied | `Applied`             | `Synced`               | (implicit; not a separate condition)     |
| Cluster matches desired state  | `InSync`              | (implicit in `Synced`) | `Ready` (partial — also includes health) |
| Workloads running correctly    | `Healthy`             | `Healthy`              | (folded into `Ready`)                    |
| Aggregate "all good"           | — (combine the three) | `Synced + Healthy`     | `Ready`                                  |

ArgoCD users will recognize `Healthy`. The split between `Applied` and `InSync` is more granular than ArgoCD's `Synced` (which conflates "applied" with "matches" — fine in practice but loses signal during drift events). Flux users will find the three-way split more granular than Flux's combined `Ready` — convoy has no single aggregate condition; consumers combine the three.

### Field Reference

These three conditions appear in `Release.status.conditions` as standard k8s `Condition` objects:

| Field                | Description                                                                                                                             |
|----------------------|-----------------------------------------------------------------------------------------------------------------------------------------|
| `type`               | One of `Applied`, `InSync`, `Healthy`.                                                                                                  |
| `status`             | `True`, `False`, or `Unknown`. `Unknown` is meaningful for `Healthy` (progressing) and for `Applied`/`InSync` before any apply attempt. |
| `lastTransitionTime` | When the status last changed.                                                                                                           |
| `reason`             | Standardized machine-readable code (e.g., `ApplySucceeded`, `DriftDetected`, `WorkloadsDegraded`).                                      |
| `message`            | Human-readable detail. May reference specific resources for `Healthy` and `InSync`.                                                     |
| `observedGeneration` | The `metadata.generation` of the Release at the time this condition was set. Allows consumers to detect stale conditions.               |

## Health Assessment

### Purpose

Health assessment is the controller's evaluation of whether the workloads deployed by a `Release` are functioning as intended. It is distinct from `Applied` (manifests pushed) and `InSync` (cluster matches desired state). A Release can be applied and in-sync but still unhealthy — the manifest is correct, but Pods are crashlooping, PVCs are unbound, or custom resources haven't reached their `Ready` condition.

Health assessment provides:

- A trustworthy signal that a deploy actually worked, not just that it was applied
- Per-resource visibility into what's wrong when something fails
- The foundation for downstream features: auto-rollback, sync-wave gating, fleet-level health views

### Health States

Each managed resource is evaluated to one of five states:

| State         | Meaning                                         | Example                                                                                  |
|---------------|-------------------------------------------------|------------------------------------------------------------------------------------------|
| `Healthy`     | Resource has reached its intended runtime state | `Deployment` with all replicas ready, `observedGeneration` current                       |
| `Progressing` | Working toward intended state, no failure yet   | `Deployment` rolling out a new revision; `Pod` pulling its image                         |
| `Degraded`    | Failed or in a known-bad state                  | `Deployment` with `ProgressDeadlineExceeded`; `Pod` in `CrashLoopBackOff` past threshold |
| `Suspended`   | Intentionally paused                            | `CronJob` with `spec.suspend: true`                                                      |
| `Unknown`     | Health cannot be determined                     | Custom resource with no rule and no standard conditions                                  |

`Suspended` is a deliberate distinction from `Degraded` — operators shouldn't be alerted for resources that are intentionally not running.

`Unknown` is a deliberate distinction from `Healthy` — the controller is honest about what it doesn't know rather than defaulting optimistically.

### How Health Is Determined

For each managed resource, the controller resolves a health rule in this order:

1. **`ResourceHealthRule`** matching `(apiGroup, kind, namespace, name)` — highest priority; for cluster-scoped resources, `namespace` is empty
2. **`KindHealthRule`** matching `(apiGroup, kind)` — applies to all resources of a kind
3. **Built-in rule** for standard kinds — used when no user rule applies
4. **kstatus fallback** for resources following Kubernetes API conventions
5. **`Unknown`** — when none of the above can determine state

The first matching layer is used; lower layers are consulted only when higher ones don't apply.

#### Built-in Rules

The controller ships with rules for ~18 standard kinds covering 95% of typical deployments:

**Workloads:** `Deployment`, `StatefulSet`, `DaemonSet`, `ReplicaSet`, `Pod`, `Job`, `CronJob`

**Networking:** `Service`, `Ingress`

**Storage:** `PersistentVolumeClaim`, `PersistentVolume`

**Configuration:** `ConfigMap`, `Secret`

**RBAC:** `ServiceAccount`, `Role`, `RoleBinding`, `ClusterRole`, `ClusterRoleBinding`

**Autoscaling/Reliability:** `HorizontalPodAutoscaler`, `PodDisruptionBudget`

Rules incorporate kind-appropriate progress deadlines (e.g., `Deployment.spec.progressDeadlineSeconds` for Deployments; 10m default for StatefulSets) and look at child-resource events for fast failure detection (e.g., `ImagePullBackOff` on Pods triggers `Degraded` on the parent without waiting for the parent's deadline).

Built-in rule logic is documented per kind so users can predict behavior.

#### kstatus Fallback

For resources not covered by a built-in rule, the controller uses Google's `kstatus` library, which infers health from common Kubernetes API conventions:

- Presence and value of a `Ready` condition
- Generic `Failed`, `Reconciling`, `Stalled` conditions
- `observedGeneration` vs `metadata.generation`

This handles convention-following CRDs automatically with zero per-CRD configuration. Resources that don't follow conventions fall through to `Unknown`.

#### Custom Rules via `KindHealthRule` and `ResourceHealthRule`

For CRDs with non-standard status (or to override built-in behavior), users define `KindHealthRule` or `ResourceHealthRule` resources. See the resource summaries below for full schema.

### `KindHealthRule` Resource

`KindHealthRule` is **cluster-scoped**. It defines a health rule that applies to all resources of a given kind across the entire cluster, regardless of which Release manages them.

**Targeting:** Each `KindHealthRule` targets exactly one `(apiGroup, kind)`. Validating admission rejects creation of a second `KindHealthRule` with the same target. To change behavior, edit the existing rule.

**Relationship to Release:** None. The controller indexes `ResourceHealthRule` by `(apiGroup, kind, version)` and looks up the applicable rule by the resource's kind, regardless of which Release manages it. This ensures consistent health verdicts across the system.

**Expression language:** CEL, with bindings for the live resource (`object`) and current time (`now`). Returns one of `Healthy`, `Progressing`, `Degraded`, `Suspended`, `Unknown`.

```yaml
apiVersion: extensions.convoy.kumodo.io/v1alpha1
kind: KindHealthRule
metadata:
  name: certificate-health
spec:
  target:
    apiGroup: cert-manager.io
    kind: Certificate
    version: v1
  
  expression: |
    has(object.status.conditions) && object.status.conditions.exists(c, 
      c.type == "Ready" && c.status == "True"
    ) ? "Healthy" :
    has(object.status.conditions) && object.status.conditions.exists(c,
      c.type == "Ready" && c.status == "False"
    ) ? "Degraded" :
    "Progressing"
  
  progressDeadline: 5m
  
  messageExpression: |
    has(object.status.conditions) && object.status.conditions.exists(c, c.type == "Ready") 
      ? object.status.conditions.filter(c, c.type == "Ready")[0].message
      : "Waiting for Certificate to become ready"
```

### `ResourceHealthRule` Resource

`ResourceHealthRule` is **namespaced**. It defines a health rule for a specific resource instance, identified by `(apiGroup, kind, namespace, name)`. Takes precedence over `KindHealthRule` when both match. For cluster-scoped target resources, `namespace` is empty.

**Relationship to Release:** None. The controller indexes `ResourceHealthRule` by `(apiGroup, kind, version, namespace, name)` and looks up the rule at evaluation time, independently of which Release manages the resource.

**Expression language:** Same CEL bindings as `KindHealthRule`.

```yaml
apiVersion: extensions.convoy.kumodo.io/v1alpha1
kind: ResourceHealthRule
metadata:
  name: payments-db-health
  namespace: payments-team
spec:
  target:
    apiGroup: apps
    kind: StatefulSet
    version: v1
    name: payments-db
    namespace: payments-team
  
  expression: |
    object.status.readyReplicas == object.status.replicas &&
    object.status.replicas >= 3
      ? "Healthy"
      : object.status.readyReplicas > 0
        ? "Progressing"
        : "Degraded"
  
  progressDeadline: 15m
  
  messageExpression: |
    string(object.status.readyReplicas) + "/" + string(object.status.replicas) + " replicas ready"
```

Cluster-scoped target example (namespace is empty in the target):

```yaml
apiVersion: extensions.convoy.kumodo.io/v1alpha1
kind: ResourceHealthRule
metadata:
  name: some-ns-health
  namespace: platform-team
spec:
  target:
    apiGroup: ""
    kind: Namespace
    name: some-ns
    namespace: ""
  
  expression: |
    object.status.phase == "Active" ? "Healthy" : "Degraded"
```

### Aggregation: Per-Resource → Release-Level `Healthy`

Per-resource health verdicts aggregate into the Release-level `Healthy` condition:

| Resource states                                | Release `Healthy` condition |
|------------------------------------------------|-----------------------------|
| Any `Degraded`                                 | `False`                     |
| All `Healthy` (and/or `Suspended`)             | `True`                      |
| Any `Progressing`, no `Degraded`               | `Unknown`                   |
| Any `Unknown`, no `Degraded`, no `Progressing` | `Unknown`                   |

Key rules:

- **`Suspended` doesn't count against `Healthy: True`.** Intentional pause is intentional; suspended resources are a normal steady state, not a failure.
- **`Unknown` doesn't count toward `Healthy: True`.** The controller doesn't fabricate confidence; if any resource's state is unknown, the Release-level verdict is also `Unknown`.
- **`Progressing` produces `Unknown`, not `True` or `False`.** Active progress is genuine indeterminacy — the deploy is in flight. `Healthy: Unknown` during deploys is normal, not an alert condition.

`Healthy: Unknown` is meaningful and distinct from `False`. Tooling watching for failures should alert on `Healthy: False`, not on any non-`True` state.

### Status Shape

`status` carries only the **bounded aggregate**: the `Healthy` condition and a `healthSummary` of counts. The per-resource detail is **not** stored in `status` — for a large Release it could exceed the API object size limit — and is served on demand by the [`/health` subresource](#health-subresource) instead.

```yaml
status:
  conditions:
    - type: Healthy
      status: "False"
      reason: WorkloadsDegraded
      lastTransitionTime: 2026-04-29T10:01:15Z
      message: "1 of 14 resources degraded"
  
  healthSummary:
      total: 14
      healthy: 11
      progressing: 1
      degraded: 1
      suspended: 1
      unknown: 0
```

#### `healthSummary`

Always present. Aggregate counts by state across all managed resources. Lets tooling answer "is anything wrong" at a glance and produce dashboards without parsing the resource list.

| Field         | Description                                      |
|---------------|--------------------------------------------------|
| `total`       | Total number of resources managed by the Release |
| `healthy`     | Count of resources in `Healthy` state            |
| `progressing` | Count of resources in `Progressing` state        |
| `degraded`    | Count of resources in `Degraded` state           |
| `suspended`   | Count of resources in `Suspended` state          |
| `unknown`     | Count of resources in `Unknown` state            |

### `/health` Subresource

`GET /releases/<name>/health` returns the per-resource health detail for the Release's managed resources. This detail is a runtime view re-evaluated by the controller from live cluster state; it is **not** persisted in `Release.status` (only the [`healthSummary`](#healthsummary) aggregate is). Keeping the potentially large, high-churn per-resource list out of the object keeps `status` bounded regardless of Release size.

| Verb | Path                      | Purpose                                               |
|------|---------------------------|-------------------------------------------------------|
| GET  | `/releases/<name>/health` | Return per-resource health for the managed resources. |

The response contains only entries that need operator attention — the non-`Healthy`/`Progressing` buckets:

| Health state  | Included in response?                                |
|---------------|------------------------------------------------------|
| `Healthy`     | No — nothing to surface                              |
| `Progressing` | No — transient noise during deploys                  |
| `Degraded`    | Yes — needs attention                                |
| `Suspended`   | Yes — operators should know what's paused            |
| `Unknown`     | Yes — indicates missing rule or non-conventional CRD |

This makes the per-resource view directly actionable: a Release with 200 healthy resources and 1 degraded one returns 1 entry, not 201.

```yaml
# GET /releases/payments-api-prod-eu/health
resources:
  - kind: Deployment
    apiGroup: apps
    version: v1
    name: payments-api
    namespace: payments-team
    health: Degraded
    since: 2026-04-29T10:01:15Z
    message: "0/3 ready replicas; CrashLoopBackOff on container 'api' (exit code 1)"
    ruleUsed:
      type: builtin
  - kind: CronJob
    apiGroup: batch
    version: v1
    name: payments-cleanup
    namespace: payments-team
    health: Suspended
    since: 2026-04-15T08:00:00Z
    message: "spec.suspend is true"
    ruleUsed:
      type: builtin
```

**RBAC:** `GET` requires `releases/health: get`.

#### Per-resource fields

| Field                         | Description                                                                                                                |
|-------------------------------|----------------------------------------------------------------------------------------------------------------------------|
| `kind`, `apiGroup`, `version` | Resource kind identification                                                                                               |
| `name`, `namespace`           | Resource identity                                                                                                          |
| `health`                      | One of `Degraded`, `Suspended`, `Unknown`                                                                                  |
| `since`                       | When this state was first observed (transitions reset this)                                                                |
| `message`                     | Human-readable detail from the health rule                                                                                 |
| `ruleUsed.type`               | One of `resourceHealthRule`, `kindHealthRule`, `builtin`, `kstatus`, `fallbackUnknown`                                     |
| `ruleUsed.ref`                | Reference to the `KindHealthRule` or `ResourceHealthRule` (only when `type: resourceHealthRule` or `type: kindHealthRule`) |

The `ruleUsed` field makes assessment auditable. Users debugging unexpected health verdicts can immediately see which rule produced them.

### What's Not Covered

Health assessment scope is deliberately narrow:

- **Liveness/readiness probes** are kubelet-internal mechanisms; health assessment reads their resulting state (`readyReplicas`) but doesn't replace them.
- **Application-level health** (e.g., `/metrics` endpoints showing healthy business metrics) is out of scope. Health assessment evaluates whether k8s resources reached intended state, not whether application logic is correct.
- **Smoke tests / integration tests** are active probing, handled by post-sync hooks (separate feature).
- **SLO/SLA monitoring** belongs to observability tooling (Prometheus, etc.).

Health assessment is passive observation of resource state, not active testing.

### Comparison with Other Tools

| Concept                    | This tool                                                                      | ArgoCD                       | Flux                            |
|----------------------------|--------------------------------------------------------------------------------|------------------------------|---------------------------------|
| Per-resource health        | Yes, via `/health` (degraded/suspended/unknown only)                           | Yes, full list               | Limited                         |
| Custom health rules        | CEL via `KindHealthRule` (cluster-scoped) or `ResourceHealthRule` (namespaced) | Lua via ConfigMap            | None native                     |
| Five health states         | Yes                                                                            | Yes (similar set)            | Reduced (mostly Ready/NotReady) |
| `Unknown` as honest signal | Yes                                                                            | Sometimes (for unknown CRDs) | Often defaults optimistic       |
| Bounded status size        | Yes (per-resource detail kept out of the object entirely)                      | No (full list)               | Compact (less detail)           |

## Preview and Diff Subresources

`/preview` and `/diff` are read-only subresources on `Release` that answer two operational questions: "what would deploy?" and "what would change?". Both are essential for safe deployment workflows — letting operators verify intent before triggering a sync.

### /preview

Returns the rendered manifest set that would result from a sync, given the Release's current configuration (or specified overrides). The output is consumable directly: pipeable to `kubectl apply`, validators (`kubeval`, `conftest`, OPA), or human review.

#### Behavior

| Verb | Path                       | Purpose                                          |
|------|----------------------------|--------------------------------------------------|
| GET  | `/releases/<name>/preview` | Return rendered manifests                        |
| POST | `/releases/<name>/preview` | Return rendered manifests with overrides applied |

#### Output

A multi-document YAML stream (default) or JSON list. The output is the post-mutation manifest set — exactly what would be applied to the cluster on sync. Nothing else: no lifecycle hooks (accessed via `/lifecycle`), no resolved-source metadata (accessed via `/status`), no diff annotations.

#### Pin handling

`/preview` always answers "what would `/sync` apply right now?" When pinned, that's the pinned content. When not pinned, it's a fresh render from current spec.

| Release state | Default behavior             |
|---------------|------------------------------|
| Not pinned    | Render from current spec     |
| Pinned        | Return pinned render content |

The default is operationally consistent: `/preview` always shows what the next sync would deploy.

#### Query parameters

| Parameter          | Values           | Default                 | Description                                                                                                                                                                                        |
|--------------------|------------------|-------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `source`           | `spec`, `pinned` | (smart)                 | Override the default pin handling. `spec` forces fresh render from current spec, ignoring pin. `pinned` requires the Release to be pinned and returns pinned content; returns 400 when not pinned. |
| `cluster`          | cluster name     | Release's bound cluster | Override the cluster context for templating, capabilities, and cluster-specific value selection. The Release itself is not modified.                                                               |
| `clusterNamespace` | namespace        | Release's namespace     | Override the cluster reference's namespace for cross-namespace resolution.                                                                                                                         |

#### POST with mutation overrides

For "what would happen if I changed these mutations?" without committing to the Release. Lets users iterate safely — change a value file reference, an inline patch, a templated condition — and see the resulting render before persisting.

```
POST /releases/<name>/preview
Content-Type: application/json
Body: {
  "mutations": [
  {
    "type": "helmValues",
    "from": {
      "sourceRef": { "name": "experimental-values", "path": "test/values.yaml" }
    }
  },
  {
    "type": "jsonPatch",
      "inline": [
        { "op": "replace", "path": "/spec/replicas", "value": 5 }
      ]
    }
  ]
}
```

The provided mutations replace the Release's mutations for this preview only. Query parameters (`source`, `cluster`, `clusterNamespace`) compose with POST overrides:

```
POST /releases/<name>/preview?cluster=prod-staging
Body: { 
  "mutations": [...]
}
```

Returns the render that would result if the Release used these mutations and was deployed to the specified cluster. Useful for "is my mutation change correct, and does it work right in staging?" without two separate operations.

#### Common usage patterns

```bash
# What would deploy if I synced right now?
kubectl get release/payments-prod-eu --raw "/.../preview"

# What would deploy if I synced this Release to staging instead?
kubectl get ... --raw "/.../preview?cluster=staging-eu"

# What would deploy if I unpinned?
kubectl get ... --raw "/.../preview?source=spec"

# What if I changed my mutations?
curl -X POST "/.../preview" -d '{"mutations": [...]}'

# Validate against policies
kubectl get ... --raw "/.../preview" | conftest test -
```

#### RBAC

| Verb | Permission                 |
|------|----------------------------|
| GET  | `releases/preview: get`    |
| POST | `releases/preview: create` |

When `?cluster=<name>` is used, additionally requires `clusters: get` on the referenced cluster (standard k8s reference-resolution semantics).

### /diff

Returns the differences between two states. The "what would change?" complement to `/preview`'s "what would deploy?". Several comparison axes are supported via the `against` parameter.

#### Behavior

| Verb | Path                    | Purpose                                     |
|------|-------------------------|---------------------------------------------|
| GET  | `/releases/<name>/diff` | Return diff between specified states        |
| POST | `/releases/<name>/diff` | Return diff with mutation overrides applied |

#### Comparison axes

The `against` parameter controls what's being compared.

| `against`        | Compares                                                                            | Common use case                      |
|------------------|-------------------------------------------------------------------------------------|--------------------------------------|
| `live` (default) | Current rendered output vs live cluster state                                       | "What would change if I synced now?" |
| `pin`            | Current rendered output vs pinned content                                           | "What would change if I unpinned?"   |
| `render`         | Current rendered output vs a specific historical render (or two historical renders) | "What changed between deploys?"      |

#### Query parameters

| Parameter          | Used with        | Description                                                                                                                                    |
|--------------------|------------------|------------------------------------------------------------------------------------------------------------------------------------------------|
| `against`          | always           | Comparison axis: `live`, `pin`, or `render`. Default: `live`.                                                                                  |
| `cluster`          | `against=live`   | Compare against the live state of a specified cluster instead of the Release's bound cluster. Useful for cross-cluster comparison.             |
| `clusterNamespace` | `against=live`   | Override cluster reference's namespace.                                                                                                        |
| `renderRef`        | `against=render` | Hash of the render to compare against. Required when `against=render`.                                                                         |
| `compareTo`        | `against=render` | Optional second render hash. When provided, compares the two specified historical renders directly (Release's current render is not involved). |
| `format`           | always           | Output format: `structural` (default), `text` (unified diff).                                                                                  |

#### Common usage

```
GET /diff                                                       # against bound cluster's live state
GET /diff?against=live&cluster=staging-eu                       # against staging's live state
GET /diff?against=pin                                           # current render vs pinned content
GET /diff?against=render&renderRef=sha256:abc...                # current render vs historical
GET /diff?against=render&renderRef=sha256:abc...&compareTo=sha256:def...   # two historicals
```

#### Pin interaction

When the Release is pinned, "current rendered output" in the comparison defaults to the **pinned content** (matching `/preview`'s default). To diff against the would-be-rendered content from current spec, combine with `/preview?source=spec` reasoning — implementation-side, the user requests `against=pin`, which gives "what would change if I unpinned and synced."

#### POST with mutation overrides

Same pattern as `/preview`. Lets users see what diff their mutation changes would produce before committing.

```
POST /releases/<name>/diff?against=live
Content-Type: application/json
Body: {
  "mutations": [...]   // overrides Release's mutations for this diff
}
```

Returns the diff between the specified comparison target and a render produced with the override mutations. Combinable with all GET query parameters.

#### Output format

**Structural diff (default):** A JSON document describing changes per resource. Format:

```json
{
  "resources": [
    {
      "kind": "Deployment",
      "apiGroup": "apps",
      "version": "v1",
      "name": "payments-api",
      "namespace": "payments-team",
      "changeType": "Modified",
      "fieldChanges": [
        { "path": "/spec/replicas", "before": 3, "after": 5 },
        { "path": "/spec/template/spec/containers/0/image", "before": "...:v1.2.0", "after": "...:v1.3.0" }
      ]
    },
    {
      "kind": "ConfigMap",
      "apiGroup": "",
      "version": "v1",
      "name": "payments-config",
      "namespace": "payments-team",
      "changeType": "Added",
      "manifest": { ... }
    }
  ],
  "summary": {
    "added": 1,
    "modified": 1,
    "deleted": 0,
    "unchanged": 12
  }
}
```

`changeType` values: `Added`, `Modified`, `Deleted`, `Unchanged` (omitted from output by default; included if requested via `?include=unchanged`).

**Text diff (`?format=text`):** Unified diff of YAML representations, one block per modified resource. Easier for human consumption; pipeable to standard diff-viewer tools.

#### Managed-fields filtering

The diff filters out fields that aren't owned by the Release, to avoid showing noise that the user didn't cause. Specifically:

- Fields populated by other field managers (HPA scaling `replicas`, defaulted by admission, set by other controllers) are excluded
- Fields the apiserver sets at apply time (`creationTimestamp`, `resourceVersion`, `uid`) are excluded
- Status fields are excluded (status is observed state, not desired state)

This makes the diff output reflect "differences caused by this Release's intent," not "all bytes that differ between rendered and live."

#### Drift categorization

When `against=live` and field-level drift exists, the diff categorizes:

- **Drift** — field is managed by this Release but the live value differs (someone manually edited the resource, or a controller fighting with field ownership)
- **External** — field is not managed by this Release but exists in the live state (HPA-managed replicas, etc.) — informational only, not flagged as drift

Categorization appears in the structural output as `changeReason` per field change.

#### RBAC

| Verb | Permission              |
|------|-------------------------|
| GET  | `releases/diff: get`    |
| POST | `releases/diff: create` |

When `?cluster=<name>` is used (with `against=live`), additionally requires `clusters: get` on the referenced cluster.

### Summary

| Aspect            | `/preview`                         | `/diff`                                                     |
|-------------------|------------------------------------|-------------------------------------------------------------|
| Purpose           | "What would deploy?"               | "What would change?"                                        |
| Verbs             | GET, POST                          | GET, POST                                                   |
| Default behavior  | Render with current config         | Diff current render vs live cluster                         |
| Pin handling      | Default returns pinned when pinned | Default uses pinned content as "current render" when pinned |
| Cluster override  | `?cluster=<name>`                  | `?cluster=<name>` (with `against=live`)                     |
| Mutation override | POST with `mutations` body         | POST with `mutations` body                                  |
| Lifecycle hooks   | Not included                       | Not included                                                |
| Output            | YAML stream / JSON list            | Structural JSON (default) / text diff                       |
| Field filtering   | n/a                                | Managed-fields filtering; drift categorization              |
| Format options    | YAML (default), JSON               | structural (default), text                                  |

Both subresources share the same parameter conventions (`?cluster=`, mutation override via POST), making them ergonomically consistent. `/preview` is for content; `/diff` is for differences. They compose: render via `/preview`, validate via `/diff` against live or pin, then `/sync` when satisfied.



## Lifecycle Hooks

### Purpose

Lifecycle hooks extend the Release sync pipeline with operations that aren't part of the deployed manifest set. They execute at well-defined points in the deploy lifecycle and handle side-effect work: database migrations, smoke tests, cache warming, deploy notifications, backups, alerts on failure.

Hook configuration is stored in the Release's top-level `lifecycle` field (visible in a plain `kubectl get release -o yaml`) and is written through the `/lifecycle` subresource, which provides the dedicated write path with its own RBAC, audit, and atomic-PUT semantics. Execution state is mirrored under `status.lifecycle` for visibility.

### Why a Subresource, Not Annotations

Hooks could be expressed as annotations on individual manifests (the ArgoCD/Helm pattern). The subresource model was chosen instead because:

- **Separates concerns.** Manifests describe what to deploy; lifecycle describes what to do at deploy time. Mixing them in annotations conflates two different things.
- **Decouples hooks from charts.** With annotations, hooks live inside the chart. To add a smoke test for a vendored chart, users would have to fork it. Subresource-defined hooks are owned by the deploying team.
- **Templating with cluster metadata is natural.** Hooks in the subresource use the same template engine as mutations and have direct access to per-cluster context.
- **Audit and RBAC.** Subresource means standard k8s RBAC; distinct audit events for hook configuration changes.
- **Helm chart compatibility can happen at translation time.** When sources carry `helm.sh/hook` annotations, the renderer can translate them into lifecycle entries (planned — see [Helm Chart Compatibility](#helm-chart-compatibility)).

### Phases

| Phase        | When it runs                                                 | Common uses                                              |
|--------------|--------------------------------------------------------------|----------------------------------------------------------|
| `PreSync`    | Before any wave applies                                      | Database migrations; pre-deploy checks; resource cleanup |
| `PostSync`   | After all waves complete (optionally after healthy — see `runAfter`) | Smoke tests; cache warming; deploy notifications |
| `SyncFailed` | After a sync failure at the apply stage (render succeeded)   | Alerts; partial-state cleanup; rollback triggers         |
| `PreDelete`  | Before workloads are pruned on Release deletion              | Backups; graceful shutdown; external cleanup             |
| `PostDelete` | After workloads are pruned                                   | Final notifications; resource accounting                 |

Hooks run at the boundaries of the **apply** and **delete** flows. There are no pre-/post-*render* phases: a hook is a workload created on the target cluster, so it cannot feed the in-memory render (that is the mutation pipeline's job) nor gate anything the mutation pipeline doesn't already gate. `SyncFailed` fires only when the sync itself failed — the render succeeded (so a target cluster and render exist) but an apply/wave/prune step failed; it does not fire on a source/cluster/render failure, and it is best-effort (fire-and-forget).

### Hook Schema

Hooks are typed units — each declares its kind via `type` and provides a Kubernetes-native template for that kind.

```yaml
# /releases/payments-api-prod-eu/lifecycle subresource

preSync:
  - name: db-migration
    type: Job                       # Job | Pod (v1)

    # Optional CEL condition. If absent, the hook always runs.
    # If present and evaluates to false (or errors), the hook is skipped.
    # Context is lowercase {cluster, release} (see "Templating context" below).
    if: 'cluster.tier == "production"'

    # Custom template with PARTIAL metadata (labels/annotations/namespace only —
    # the controller owns the workload name). `spec` is a k8s-native JobSpec.
    jobTemplate:
      metadata:
        labels: { audit: "true" }
      spec:
        template:
          spec:
            serviceAccountName: payments-migrator
            restartPolicy: Never
            containers:
              - name: migrate
                image: registry.example.com/payments-migration:v1.2.3
                command: ["./migrate.sh"]
                env:
                  - name: TARGET_REGION
                    # Templated against the hook context at sync time
                    value: "{{ .cluster.region }}"
        backoffLimit: 0

    # Ordering within the phase (parallel within the same weight)
    weight: 0

    # Hook timeout. If unset, the controller waits indefinitely.
    timeout: 10m

    # What happens if the hook fails: Fail (abort) | Continue (best-effort).
    onFailure: Fail

    # Cleanup behavior. Empty behaves like Never (kept).
    deleteBeforeCreation: true
    cleanup: OnSuccess  # | OnCompletion | Never

postSync:
  - name: smoke-test
    type: Job
    # postSync-only: hold until the release condition is reached before firing.
    # Applied (default) fires right after apply; Healthy waits until the whole
    # release is healthy for the current render.
    runAfter: Healthy
    jobTemplate: { spec: { template: { spec: { restartPolicy: Never, containers: [ ... ] } } } }
    onFailure: Continue
    cleanup: OnCompletion

syncFailed:
  - name: alert-oncall
    type: Job
    jobTemplate:
      spec:
        template:
          spec:
            restartPolicy: Never
            containers:
              - name: alert
                image: alerting-tool:v1
                env:
                  - name: RELEASE_NAME
                    value: "{{ .release.name }}"
    cleanup: OnCompletion

preDelete:
  - name: backup-database
    type: Job
    jobTemplate: { spec: { template: { spec: { restartPolicy: Never, containers: [ ... ] } } } }
    timeout: 30m
    onFailure: Fail                 # blocks deletion until it succeeds
    cleanup: Never                  # retain for compliance audit
```

#### Hook fields

| Field                  | Type     | Required      | Default        | Description                                                                                     |
|------------------------|----------|---------------|----------------|-------------------------------------------------------------------------------------------------|
| `name`                 | string   | yes           | —              | Unique within its phase. Identifies the hook for status reporting and updates.                  |
| `type`                 | enum     | yes           | —              | One of `Job`, `Pod`. Determines which template field is used.                                   |
| `if`                   | string   | no            | —              | CEL expression. The hook runs only when it evaluates to `true`. False/error → skipped.          |
| `runAfter`             | enum     | no (postSync) | `Applied`      | **postSync only.** `Applied` fires right after apply; `Healthy` holds until the release is healthy for the current render. Forbidden on other phases. |
| `jobTemplate`          | object   | when type=Job | —              | `{ metadata (partial), spec (k8s JobSpec) }`. Templated against the hook context.               |
| `podTemplate`          | object   | when type=Pod | —              | `{ metadata (partial), spec (k8s PodSpec) }`.                                                    |
| `weight`               | integer  | no            | `0`            | Ordering within the phase. Lower weight runs (and must complete) before higher; equal weight runs in parallel. |
| `timeout`              | duration | no            | wait forever   | Max time to wait for completion before marking `TimedOut`. Unset = no timeout.                  |
| `onFailure`            | enum     | no            | `Continue`     | `Fail` (abort) or `Continue` (best-effort). Empty behaves as `Continue`; only `Fail` blocks.    |
| `deleteBeforeCreation` | bool     | no            | `false`        | Delete this hook's prior workloads (by label) before creating the new one.                      |
| `cleanup`              | enum     | no            | `Never`        | `OnSuccess`, `OnCompletion`, or `Never`. Empty behaves as `Never` (kept).                        |

There is no per-phase defaulting: `timeout`, `onFailure`, and `cleanup` take the values above regardless of phase.

### Templating context

Hook workload templates (Go templates) and the `if` CEL expression share one context — the same lowercase, credential-free projection the mutation pipeline uses. It is the **same for every phase**; there are no phase-specific bindings:

| Binding    | Contents                                                                                          |
|------------|---------------------------------------------------------------------------------------------------|
| `cluster`  | The target `Cluster`'s `spec.metadata` keys **flattened directly** (`cluster.<key>`), plus `cluster.name`. Credentials are never exposed. |
| `release`  | The `Release`'s `name`, `namespace`, `labels`, `annotations`.                                      |

So a metadata key `region` is `{{ .cluster.region }}` (Go template) / `cluster.region` (CEL) — there is **no** `.metadata` segment. There are no `.source`, `.render`, or `.sync` bindings; in particular, `SyncFailed` hooks do **not** currently have the failure stage/message in their template context (the failure is recorded in `status`, not passed to the hook).

### The `if` Condition (skip)

The optional `if` field makes a hook conditional. Absent → the hook always runs. Present → it must evaluate to a bool; the hook runs only when `true`. A `false` result — or a compile/evaluation error — **skips** the hook.

```yaml
if: 'cluster.tier == "production"'                          # production-only
if: 'cluster.region in ["eu-west-1", "eu-central-1"]'       # region-specific
```

Evaluation semantics:

- The `if` expression is compiled and evaluated **at run time** by the controller (not at admission). Structural validation — name uniqueness within a phase, `type`↔template consistency, enum values, `runAfter` being postSync-only — is enforced on `/lifecycle` write; the CEL itself is not.
- A false result or an evaluation error → the hook is **skipped**: no workload is created and no entry is recorded, so a skipped hook does not appear in `status.lifecycle`.

### The `runAfter` Trigger (wait) — postSync only

`if` decides *whether* a hook runs; `runAfter` decides *when* a postSync hook fires. They are orthogonal and compose.

| `runAfter`          | Behavior                                                                                             |
|---------------------|------------------------------------------------------------------------------------------------------|
| `Applied` (default) | Fire right after the apply completes.                                                                |
| `Healthy`           | Hold until the **whole release** (every applied resource, all waves) is **Healthy for the current render**, then fire. |

`runAfter` is only allowed on `postSync` hooks (it is `Forbidden` elsewhere — the other phases have no post-apply health milestone to wait on). With no health assessment configured, `Healthy` is trivially satisfied and behaves like `Applied`.

**Choosing `runAfter`, and the one trap.** `runAfter: Healthy` is right when the hook is *downstream* of health — smoke tests, backfills, "deployment healthy" notifications, cache warming — things the app does not block on. It is the **wrong** choice for a schema migration the app itself needs to become healthy: the app stays unhealthy waiting for the migration → the release never becomes healthy → the migration never fires (a deadlock). Migrations the app depends on belong in **`preSync`** (which gates the whole apply before the app is deployed), or in a separate DB release the app release orders behind. This is inherent to any run-after-healthy hook model (Argo's `PostSync` has the same trap), so it is guidance, not a bug.

### Failure Handling

Each hook's `onFailure` controls what happens when the hook reaches `Failed` or `TimedOut`. There is no per-phase defaulting; empty behaves as `Continue`.

| Value      | Behavior                                                                                                                         |
|------------|--------------------------------------------------------------------------------------------------------------------------------|
| `Fail`     | Abort the flow. In `preSync` the sync is aborted before anything is applied; in `preDelete` the finalizer is held, so **deletion blocks** until the hook succeeds (or the finalizer is removed manually). |
| `Continue` | Record the failure in `status` and proceed. `postSync`/`syncFailed`/`postDelete` are best-effort, so a failure there never un-applies or un-deletes anything. |

Independently of `onFailure`, a failed hook is always reflected in `status.lifecycle` and flips the `LifecycleSucceeded` condition to `False` (see [Status Reflection](#status-reflection)) — `onFailure` governs only whether the flow proceeds, not whether the failure is surfaced.

### Cleanup

Two orthogonal fields control a hook workload's lifecycle. They act at **different moments**: `cleanup` acts when the hook *completes*; `deleteBeforeCreation` acts when the hook is *re-armed* on a later render.

#### `cleanup` — delete after the hook completes

| Value          | Behavior                                                                                             |
|----------------|------------------------------------------------------------------------------------------------------|
| `OnSuccess`    | Delete on success; retain on failure (for debugging).                                                |
| `OnCompletion` | Delete after completion regardless of success/failure.                                               |
| `Never`        | Retain. The user is responsible for manual cleanup (compliance use case: audit jobs, backups).       |

Empty `cleanup` behaves like `Never` (the workload is kept). The default is therefore *keep* — see the caveat below.

#### `deleteBeforeCreation` — delete the prior workload on re-arm

Hook workload names embed the render, so a new render creates a new workload and the prior render's workload can linger. `deleteBeforeCreation` (default `false`) controls that at arm time, by **label selector** (all of this hook's prior workloads, across renders):

| Value            | Behavior                                                                              |
|------------------|---------------------------------------------------------------------------------------|
| `true`           | Delete this hook's prior workloads before creating the new one.                       |
| `false` (default)| Leave prior-render workloads in place.                                                |

#### The two together

The fields don't overlap — `cleanup` handles this run, `deleteBeforeCreation` handles the next:

| `cleanup`               | `deleteBeforeCreation: true` | `deleteBeforeCreation: false` |
|-------------------------|------------------------------|-------------------------------|
| `OnSuccess`/`OnCompletion` | keep none                 | keep none (cleanup already removed it) |
| `Never` (or empty)      | **keep latest** (replaced on re-arm) | **keep all** (accumulate) |

> **Caveat — the defaults accumulate.** With both fields unset (`cleanup` empty → `Never`, `deleteBeforeCreation: false`), every render leaves its hook workload behind. Set `cleanup: OnCompletion` (or `deleteBeforeCreation: true`) for a hook that runs repeatedly, and reserve `Never` for workloads you intend to inspect. Bounded retention/GC of accumulated or orphaned workloads is not yet implemented — see [Lifecycle Retention Policy](#lifecycle-retention-policy).

### Ownership Tracking

Hook resources live in target clusters; the Release object lives in the control plane cluster. Cross-cluster `ownerReferences` don't work (the target cluster's GC can't resolve references to resources in another cluster), so hooks use **labels-only ownership tracking**.

Every hook workload created by the controller carries:

```yaml
metadata:
  # The controller owns the name: {phase}-{hook-name}-{8-char hash(release|hook|renderRef)}.
  # The render is encoded in the name (not an annotation), so a new render yields a new
  # workload while re-reconciles of the same render are idempotent.
  name: presync-db-migration-6f43257d
  labels:
    convoy.kumodo.io/release-name: payments-api-prod-eu
    convoy.kumodo.io/release-namespace: payments-team
    convoy.kumodo.io/release-uid: 8f3a2b1c-...
    convoy.kumodo.io/hook-phase: PreSync
    convoy.kumodo.io/hook-name: db-migration
    convoy.kumodo.io/managed-by: convoy
```

Ownership labels are stamped by the controller and win over any user-supplied label of the same key. The controller queries by the `(release-uid, hook-phase, hook-name)` label selector for `deleteBeforeCreation` and `cleanup`. The namespace defaults to the Release's target namespace when the template omits one.

If the controller is permanently lost or disconnected, hook workloads in target clusters become orphaned. Operators discover this via target-cluster inspection (`kubectl get jobs -l convoy.kumodo.io/managed-by=convoy`) and clean up manually. A periodic GC sweep is not yet implemented (see [Lifecycle Retention Policy](#lifecycle-retention-policy)).

### Lifecycle Retention Policy

_Status: planned; not implemented._

There is currently no automatic garbage collection of hook workloads beyond the per-hook `cleanup` and `deleteBeforeCreation` above. That leaves three sources of accumulation unbounded: `cleanup: Never` workloads, `deleteBeforeCreation: false` accumulation across renders, and workloads orphaned after their Release is deleted.

A retention backstop (`maxAge`, `maxRetainedPerHook`, prune-on-hook-removed) is planned, but **not** as a `Release.spec` field: once a Release is deleted, the release controller no longer knows about it (finalizer removed, inventory deleted), and hook workloads carry no cross-cluster owner references, so k8s GC can't reclaim them either. The reclaimer therefore belongs at **cluster scope** — a component that connects to a target cluster, lists `convoy.kumodo.io/managed-by=convoy` workloads, and prunes those whose owning Release no longer exists. The retention-policy shape, its owner (cluster controller vs. a dedicated sweeper), and RBAC are open design questions.

### Helm Chart Compatibility

_Status: planned; not implemented._

Helm charts mark hook resources with `helm.sh/hook` annotations. The intent is for the renderer to translate these into lifecycle entries automatically (`pre-install`/`pre-upgrade` → `PreSync`, `post-*` → `PostSync`, `pre-delete`/`post-delete` → the delete phases, `helm.sh/hook-weight` → `weight`, `helm.sh/hook-delete-policy` → `cleanup`/`deleteBeforeCreation`), so standard charts with hooks work unmodified and chart-translated hooks coexist with user hooks (distinguished in status by `source: chart` vs `source: lifecycle`).

Today none of this translation runs: the renderer only **excludes** Helm test hooks (`helm.sh/hook: test*`) from the applied set. The `source` field exists in the status schema for when translation lands.

### Status Reflection

Hook execution state is reported per-phase in `Release.status.lifecycle`. It is a controller-owned rollup of the durable per-hook records (parity with `status.waves`), and it is **render-aware**: after a re-render, a prior render's completed hooks are filtered out of the summary/column until they re-arm, so the status reflects the current render.

```yaml
status:
  lifecycle:
    preSync:
      - name: db-migration
        source: lifecycle              # | chart (planned)
        kind: Job
        apiVersion: batch/v1
        resourceNamespace: payments-team
        resourceName: presync-db-migration-6f43257d
        state: Succeeded
        weight: 0
        renderRef: sha256:ab12…        # the render this hook ran for
        startedAt: 2026-04-29T09:59:30Z
        completedAt: 2026-04-29T10:00:00Z
        message: "Job completed successfully (1/1 succeeded)."

    postSync:
      - name: smoke-test
        source: lifecycle
        state: Failed
        renderRef: sha256:ab12…
        startedAt: 2026-04-29T10:00:50Z
        completedAt: 2026-04-29T10:01:30Z
        message: "Job failed (backoff limit exceeded; container exit 1)"
        followUpAction: Continue       # the hook's onFailure, recorded on failure
```

Skipped hooks (those whose `if` was false) create no workload and no record, so they do **not** appear here.

#### Per-hook status fields

| Field                               | Description                                                                                           |
|-------------------------------------|-------------------------------------------------------------------------------------------------------|
| `name`                              | Matches the hook's name in the subresource.                                                           |
| `source`                            | `lifecycle` (user-defined). `chart` is reserved for translated Helm hooks (planned).                  |
| `kind`, `apiVersion`                | Kind/apiVersion of the created workload (`Job`/`batch/v1` or `Pod`/`v1`).                             |
| `resourceNamespace`, `resourceName` | Identity of the actual created workload (the controller-derived name).                                |
| `state`                             | `Running`, `Succeeded`, `Failed`, `TimedOut` (`Skipped` is not recorded — skipped hooks are absent).  |
| `weight`                            | Weight of the hook.                                                                                   |
| `renderRef`                         | The render this hook was armed for. Distinguishes a current run from a prior render's stale record.   |
| `startedAt`                         | When the hook workload was created (its `createdAt`).                                                 |
| `completedAt`                       | When the hook reached a terminal state (success/failure/timeout).                                     |
| `cleanedUpAt`                       | Reserved for the cleanup timestamp; **not currently populated** by the controller.                    |
| `message`                           | Human-readable detail.                                                                                |
| `followUpAction`                    | The hook's `onFailure` (`Continue`/`Fail`), recorded when it failed.                                  |

#### Condition and printer column

Two aggregate signals summarize the rollup:

- **`LifecycleSucceeded` condition** (controller-owned, alongside `Applied`): absent when the Release has no hooks; `True`/`AllSucceeded` when all applicable hooks succeeded or were skipped; `Unknown`/`Running` while any is in progress; `False`/`HookFailed` when any hook failed — **regardless of its `onFailure`** (the condition reports the fact; `onFailure` only governs whether the flow proceeded).
- **`Lifecycle` printer column** (`kubectl get releases`): `N/A` (no hooks), `Running:<phase>`, `Failed:<phase>`, or `Completed:<phase>` — computed from the current render's hooks only.

### Interaction with Sync Waves

Hooks and waves operate at different points in the sync pipeline and don't mix at the resource level:

- `PreSync` hooks run once, before any wave starts (they gate the whole apply)
- All waves apply through their own ordering and transition strategies
- `PostSync` hooks run once, after all waves complete — optionally after the release is healthy (`runAfter: Healthy`)

Within a phase, hooks order by `weight` (not by `sync-wave`). Ordering mechanisms are scoped: waves order regular manifests; hook weights order hooks within a phase. They don't interact, and there is no "run a hook between two specific waves" — the phases bracket the entire apply.

### Mutation Pipeline Interaction

Hook templates are part of the rendered configuration and flow through mutations naturally — `goTemplate` and `celTransform` mutations can modify hook templates the same way they modify any other resource. Cluster metadata, Release context, and other bindings are available in hook templates per the phase binding rules above.

This keeps the rendering pipeline uniform; hooks are not a special case at render time.

### Subresource

| Verb   | Path                         | Purpose                                        |
|--------|------------------------------|------------------------------------------------|
| GET    | `/releases/<name>/lifecycle` | Read full lifecycle config                     |
| PUT    | `/releases/<name>/lifecycle` | Replace entire lifecycle config                |
| PATCH  | `/releases/<name>/lifecycle` | Partial update                                 |
| DELETE | `/releases/<name>/lifecycle` | Remove all lifecycle config (clear all phases) |

Per-phase or per-hook addressing is not supported in v1; the entire lifecycle config is small enough to PUT atomically.

### Summary Table

| Aspect                | Decision                                                                                |
|-----------------------|-----------------------------------------------------------------------------------------|
| Expression            | `/lifecycle` subresource with phase-keyed hook lists                                    |
| Phases                | `PreSync`, `PostSync`, `SyncFailed`, `PreDelete`, `PostDelete`                           |
| Hook types            | `Job` and `Pod` for v1                                                                  |
| Hook template         | Custom template per type: partial `metadata` + k8s-native `spec` (`JobSpec`/`PodSpec`)  |
| Templating context    | One context for all phases: lowercase `{cluster, release}` (same as mutations)          |
| Conditional execution | Optional CEL `if` per hook (skip); `runAfter` on postSync (wait for `Applied`/`Healthy`) |
| Completion criteria   | Job conditions / Pod phase, plus per-hook `timeout` → `TimedOut`                        |
| Failure handling      | Per-hook `onFailure` (`Fail`/`Continue`); no per-phase defaults                         |
| Ordering              | Per-hook `weight` within phase; parallel within same weight                             |
| Cleanup               | Per-hook `cleanup` (on completion) and `deleteBeforeCreation` (on re-arm)               |
| Retention backstop    | Planned (cluster-scope); not implemented                                                |
| Ownership tracking    | Labels-only (cross-cluster `ownerReferences` don't work); render encoded in the name    |
| Helm compatibility    | Planned; today only Helm test hooks are excluded from the applied set                   |
| Status                | Per-phase records in `status.lifecycle` + `LifecycleSucceeded` condition + `Lifecycle` column |

### Validation

`/lifecycle` writes are **structurally** validated at admission:

- Hook `name` is unique within its phase.
- `type` matches the template (`Job`↔`jobTemplate`, `Pod`↔`podTemplate`; exactly one).
- `onFailure`, `cleanup`, and `runAfter` are recognized enum values (empty allowed).
- `runAfter` is present only on `postSync` hooks.

The CEL `if` expression and the Go-template bodies are **not** validated at admission — they are compiled and evaluated at run time (see [The `if` Condition](#the-if-condition-skip)). A bad `if` skips the hook; a bad template surfaces as a hook/render failure in `status`.

## Sync Waves

### Purpose

Sync waves provide ordered application of resources within a single Release. They solve real-world deployment dependencies where some resources must exist or be ready before others can be applied successfully.

Common use cases:

- **CRDs before CRs.** Custom resource definitions must be registered with the API server before custom resources of those kinds can be applied.
- **Namespaces before namespaced resources.** A `Namespace` must exist before resources are placed in it.
- **ConfigMaps and Secrets before workloads.** Avoids transient failure states when Pods reference configs that don't yet exist.
- **PVC and StorageClass relationships.** Storage must be bound before workloads consume it.
- **Operators before their CRs.** An operator's controller must be running before its custom resources can be reconciled.
- **Service before Ingress.** Some ingress controllers require the referenced Service to exist.

Without ordering, users hit these problems repeatedly and resort to splitting deployments across multiple Releases, manual coordination, or reliance on eventual consistency through retries — all of which introduce operational friction.

### Scope

Convoy orders on **two independent axes**:

- **Axis A — resources within a single Release.** Ordering of the rendered manifests of one Release, driven by per-manifest annotations. This is the primary subject of this section.
- **Axis B — Releases within a cluster.** Ordering of whole Releases that target the same cluster (e.g., "the infra Release must be healthy before the app Release applies"), driven by `spec.syncPolicy.waveStrategy` on each Release. See [Release-within-Cluster Ordering](#release-within-cluster-ordering-axis-b) below.

Broader fleet-level ordering across many clusters remains a separate concern for a future `ReleaseSet`/fleet design.

### Wave Specification via Annotations

Wave assignment is expressed as annotations on individual manifests. The annotation appears in the rendered output (after mutations are applied) and is read by the sync engine.

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: certificates.cert-manager.io
  annotations:
    convoy.kumodo.io/sync-wave: "-10"     # apply early
spec: ...
```

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: payments-tls
  annotations:
    convoy.kumodo.io/sync-wave: "10"      # apply late
spec: ...
```

A second annotation sets the resource's **readiness strategy**, and a third an optional timeout:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payments-operator
  annotations:
    convoy.kumodo.io/sync-wave: "0"
    convoy.kumodo.io/sync-strategy: "waitForHealthyWithTimeout"  # bar the NEXT wave must clear
    convoy.kumodo.io/sync-timeout: "5m"                          # only for waitForHealthyWithTimeout
spec: ...
```

#### Wave Numbering

- Wave numbers are signed integers
- Lower numbers apply earlier; higher numbers apply later
- Negative numbers are valid and commonly used for "bootstrap" resources
- Default wave (no annotation) is `0`
- Resources within the same wave are applied **sequentially** (in render order) as a group; the group's readiness bar is then evaluated before the next wave begins (see Wave Transition Strategies below)

#### Annotations Recognized

Only the convoy-native annotations are recognized. There is no ArgoCD (`argocd.argoproj.io/sync-wave`) or Helm-hook (`helm.sh/hook`, `helm.sh/hook-weight`) translation — those are not implemented.

| Annotation                       | Purpose                                                                                                        |
|----------------------------------|----------------------------------------------------------------------------------------------------------------|
| `convoy.kumodo.io/sync-wave`     | Wave number (signed integer). Absent → wave `0`.                                                               |
| `convoy.kumodo.io/sync-strategy` | Readiness strategy: `waitForApplied` (default), `waitForHealthy`, `waitForHealthyWithTimeout`.                 |
| `convoy.kumodo.io/sync-timeout`  | Health-wait deadline (duration) for `waitForHealthyWithTimeout`. Absent → `30s`. Ignored for other strategies. |

These annotations can be written directly in source, or produced by a `syncWave` [mutation](#mutations) (validated authoring that stamps them onto matched resources). By apply time they are just annotations on the rendered manifests, read by the sync engine.

### Wave Transition Strategies

The readiness strategy is a **per-resource** annotation (`convoy.kumodo.io/sync-strategy`), not a per-Release setting. It is the **bar a resource's wave imposes on the wave that follows it**: a wave whose resources declare a health strategy holds the *next* wave until this wave is healthy. A wave aggregates its resources' strategies — if any resource in the wave requires health, the wave carries a health bar; if any requires `waitForHealthyWithTimeout`, the wave's wait is bounded (timeout = the largest `sync-timeout` among them).

| Strategy                    | The following wave may start when …                                                                                                                                                       |
|-----------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `waitForApplied`            | Every resource in this wave has been accepted by the kube-apiserver. Default. Fast; appropriate when the next wave has no runtime dependency on this one.                                  |
| `waitForHealthy`            | Every resource in this wave has reached `Healthy` per the health-assessment rules. Waits indefinitely. Necessary when the next wave depends on runtime state (operator Running before its CRs; CRD established before its CRs). |
| `waitForHealthyWithTimeout` | Same as `waitForHealthy`, but if `sync-timeout` elapses first, the **next wave fails** (`reason: WaveFailed`) rather than proceeding — the rollout stops with the failure recorded in status. |

**Which wave a health bar gates.** A wave that carries a health bar waits on the **immediately-preceding wave only** — the wave directly before it in ascending order — not transitively on every lower wave. For example, with wave 0 (a degraded Deployment), wave 1 (a healthy Secret), and wave 2 (`waitForHealthy`, depending on the Secret), wave 2 proceeds once wave 1 is healthy; a still-degraded wave 0 does not block it. Health itself is assessed by the observer, which persists per-resource health on the `ReleaseInventory`; the controller rolls that up over the preceding wave's resources.

#### Why Wave Strategies Matter

`waitForApplied` is fast but insufficient for ordering that depends on runtime readiness. A common failure: applying a CRD then immediately applying a CR of that kind — the CR application can fail because the API server hasn't yet finished registering the CRD's API. `waitForHealthy` is necessary for these cases.

`waitForHealthyWithTimeout` bounds the wait for slow-starting workloads. Note the convoy semantics: on timeout the rollout **fails** (it does not silently skip ahead), so a stuck dependency surfaces as a failure rather than a partially-applied deploy.

### Status Reflection

Per-wave progress is reported in `Release.status.waves` (a `listType=map` keyed by `number`). The controller owns `applied` (and the descriptive fields); `healthy` is rolled up from the observer-written per-resource health:

```yaml
status:
  waves:
    - number: -10                        # CRD, applied and healthy
      resources: 1
      applied: true
      healthy: true
      state: Completed
      startedAt: 2026-04-29T10:00:00Z
      completedAt: 2026-04-29T10:00:15Z

    - number: 0                          # applied; health still rolling up
      resources: 12
      applied: true
      healthy: false
      state: Completed
      startedAt: 2026-04-29T10:00:15Z
      completedAt: 2026-04-29T10:00:15Z

  conditions:
    # While a health-gated wave holds, the controller-owned Applied condition is
    # False with reason WaitingForWaveHealth (not Unknown). A bounded-wait timeout
    # sets reason WaveFailed.
    - type: Applied
      status: "False"
      reason: WaitingForWaveHealth
      message: "waiting for wave -10 to become healthy before applying wave 0"
```

| Field         | Description                                                                                                  |
|---------------|--------------------------------------------------------------------------------------------------------------|
| `number`      | Wave number from the manifest annotation (the list-map key).                                                 |
| `applied`     | Whether every resource in the wave has been applied (controller-owned).                                      |
| `healthy`     | Whether every resource in the wave is Healthy, rolled up from the `ReleaseInventory`. `null` until assessed. |
| `resources`   | Count of manifests in this wave.                                                                             |
| `state`       | Enum `Pending`/`InProgress`/`Completed`/`Failed`/`TimedOut`. In the current implementation applied waves are recorded as `Completed`; the other values are reserved. |
| `startedAt`   | When the wave was first applied (preserved across reconciles).                                               |
| `completedAt` | When the wave was applied (preserved across reconciles).                                                     |
| `failedAt`    | Reserved for a failed wave.                                                                                  |
| `message`     | Human-readable detail.                                                                                       |

The `Applied` condition is controller-owned; `Healthy` is observer-owned (`Unknown` while resources are still progressing). There is no per-wave `Unknown`/`WaveInProgress` on `Applied` — a hold is `Applied: False` with reason `WaitingForWaveHealth`.

### Resource Selection by Wave

Resources without an explicit wave annotation default to wave `0`. A Release with no annotated resources runs as a single wave (effectively no ordering), behaving identically to a non-wave deploy.

A Release with annotated resources runs through waves in order:

1. Sort waves ascending by number
2. For each wave: apply its resources sequentially, then (if the wave carries a health bar) hold until it is healthy before the next wave
3. Move to next wave; repeat
4. After last wave: prune orphans and report final sync state

If any resource in a wave fails to apply, the sync fails — subsequent waves don't run, nothing is pruned, `lastSuccessful` is not advanced, and status reflects the failure. Configurable `continueOnWaveFailure` is a possible future enhancement.

### Mutation Pipeline Interaction

Wave annotations are part of the rendered manifest set. They flow through the mutation pipeline naturally — mutations can add, modify, or remove wave annotations:

- A `goTemplate` mutation could add wave annotations based on cluster metadata
- A `jsonPatch` mutation could override a chart's default wave for a specific resource
- A `strategicMergePatch` mutation could add wave annotations to resources that don't have them
- The dedicated `syncWave` mutation is validated authoring for exactly this — it stamps the three `convoy.kumodo.io/sync-*` annotations onto matched resources

This means waves are not a separate concept layered on top of the rendering pipeline — they're a property of rendered manifests, set by whatever combination of source content and mutations produced the final output. The sync engine reads them at the end, after all rendering is complete.

## Release-within-Cluster Ordering (Axis B)

Axis A (above) orders resources inside one Release. **Axis B** orders whole Releases that target the **same cluster**, so an infrastructure Release can gate an application Release without a fleet-level object. It is configured per Release under `spec.syncPolicy.waveStrategy`:

```yaml
spec:
  syncPolicy:
    waveStrategy:
      wave: 1                              # this Release's cluster-wave (default 0)
      strategy: waitForHealthy             # waitForApplied | waitForHealthy | waitForHealthyWithTimeout
      timeout: 10m                         # only waitForHealthyWithTimeout; unset ⇒ wait indefinitely
```

Semantics mirror Axis A — the strategy is the bar a Release's **predecessors** must clear:

- A Release at cluster-wave *N* holds its apply until every **existing** Release targeting the same cluster with a **lower** wave clears the bar: `waitForApplied` → the predecessor's `Applied` condition is `True`; `waitForHealthy*` → its `Healthy` condition is `True`.
- Predecessors may live in **other namespaces** (a shared cluster is ordered cluster-wide). Same-wave Releases do not gate each other, so ordering is acyclic.
- While holding, the Release reports `Applied: False` with reason `WaitingForClusterWave`. A bounded (`waitForHealthyWithTimeout` with an explicit `timeout`) wait that elapses fails with reason `ClusterWaveFailed`; the deadline is measured from the predecessors' apply time.
- When a predecessor's `Applied`/`Healthy` flips, its higher-wave successors on the same cluster are re-reconciled promptly (rather than waiting for the next poll); the periodic reconcile is the fallback.

Only Releases that already exist are considered — a wave cannot wait for a Release that has not been created.

### What's Not Addressed Here

- **Pre-sync, post-sync, sync-fail hooks** — actions (typically Jobs) at specific points in the sync lifecycle, configured via the `/lifecycle` subresource. Planned; not implemented. There is no Helm-hook (`helm.sh/hook`) translation.
- **Fleet-level ordering across clusters** — deployment dependencies spanning many clusters. Belongs to a future `ReleaseSet`/fleet design; Axis B covers only Releases on one shared cluster.
- **Conditional waves** — running a wave only if a condition is met (e.g., "run migration wave only on first deploy"). Future enhancement.

### Summary Table

| Aspect                    | Axis A — resources within a Release                                           | Axis B — Releases within a cluster                        |
|---------------------------|-------------------------------------------------------------------------------|-----------------------------------------------------------|
| Specification             | Per-manifest `convoy.kumodo.io/sync-{wave,strategy,timeout}` annotations       | Per-Release `spec.syncPolicy.waveStrategy{wave,strategy,timeout}` |
| Aliases                   | None (no ArgoCD / Helm-hook translation)                                       | —                                                         |
| Default wave              | `0`                                                                            | `0`                                                       |
| Within-group behavior     | Resources applied sequentially                                                 | Each Release reconciles independently                     |
| Gating                    | A health-gated wave waits on the **immediately-preceding** wave                | A Release waits on **lower-wave** Releases on the cluster |
| Strategies                | `waitForApplied` (default) / `waitForHealthy` / `waitForHealthyWithTimeout`    | same                                                      |
| Timeout                   | `sync-timeout` annotation, default `30s`                                       | `waveStrategy.timeout`; unset ⇒ indefinite                |
| Failure handling          | `waitForHealthyWithTimeout` elapsed → sync fails (`WaveFailed`)                | bounded wait elapsed → `ClusterWaveFailed`                |
| Status                    | `status.waves[]`                                                               | `Applied` condition reason (`WaitingForClusterWave`)      |
