<!-- TOC -->
  * [Spec](#spec)
  * [Source Reference](#source-reference)
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
    * [Composite Condition](#composite-condition)
      * [Ready](#ready)
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
      * [`health.summary`](#healthsummary)
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
    * [CEL `when` Conditions](#cel-when-conditions)
      * [Phase-specific bindings](#phase-specific-bindings)
      * [Common patterns](#common-patterns)
      * [Evaluation semantics](#evaluation-semantics)
    * [Failure Handling](#failure-handling)
    * [Cleanup](#cleanup)
      * [`deleteBeforeCreation`](#deletebeforecreation)
      * [`cleanupOnCompletion`](#cleanuponcompletion)
    * [Ownership Tracking](#ownership-tracking)
    * [Lifecycle Retention Policy](#lifecycle-retention-policy)
      * [Interaction with per-hook policy](#interaction-with-per-hook-policy)
      * [When GC runs](#when-gc-runs)
    * [Helm Chart Compatibility](#helm-chart-compatibility)
    * [Status Reflection](#status-reflection)
      * [Per-hook status fields](#per-hook-status-fields)
    * [Interaction with Sync Waves](#interaction-with-sync-waves)
    * [Mutation Pipeline Interaction](#mutation-pipeline-interaction)
    * [Subresource](#subresource)
    * [Summary Table](#summary-table)
    * [Binding Model](#binding-model)
    * [`.Release` Bindings](#release-bindings)
      * [Release aliases](#release-aliases)
    * [`.Cluster` Bindings](#cluster-bindings)
      * [Cluster aliases](#cluster-aliases)
    * [`.Source` Bindings](#source-bindings)
      * [Source aliases](#source-aliases)
    * [`.Sync` Bindings](#sync-bindings)
      * [`.Sync.failure` (SyncFail only)](#syncfailure-syncfail-only)
    * [Phase Availability Summary](#phase-availability-summary)
    * [Common Patterns](#common-patterns-1)
      * [First-deploy vs. update](#first-deploy-vs-update)
      * [Skip on no-op syncs](#skip-on-no-op-syncs)
      * [Environment-conditional](#environment-conditional)
      * [Region-conditional](#region-conditional)
      * [Failure-stage-conditional](#failure-stage-conditional)
      * [Combining conditions](#combining-conditions)
      * [Reference render hash in template](#reference-render-hash-in-template)
    * [Evaluation Semantics](#evaluation-semantics-1)
    * [Validation](#validation)
  * [Sync Waves](#sync-waves)
    * [Purpose](#purpose-2)
    * [Scope](#scope)
    * [Wave Specification via Annotations](#wave-specification-via-annotations)
      * [Wave Numbering](#wave-numbering)
      * [Annotations Recognized](#annotations-recognized)
      * [Helm Hook Compatibility](#helm-hook-compatibility)
    * [Wave Transition Strategies](#wave-transition-strategies)
      * [Why Wave Strategies Matter](#why-wave-strategies-matter)
    * [Status Reflection](#status-reflection-1)
    * [Resource Selection by Wave](#resource-selection-by-wave)
    * [Interaction with `/preview` and `/diff`](#interaction-with-preview-and-diff)
    * [Mutation Pipeline Interaction](#mutation-pipeline-interaction-1)
    * [What's Not Addressed Here](#whats-not-addressed-here)
    * [Summary Table](#summary-table-1)
<!-- TOC -->

The deployment unit. Binds one `Source` + one `Cluster` + a content interpretation, with an ordered mutation pipeline. Self-describing via resolved status snapshots. Identity (source, cluster, content type) is immutable; routine config (mutations, paths, versions) is mutable.

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
  # Reference to a Source object. Polymorphic — kind can be GitRepository,
  # HelmRegistry, or OCIRegistry.
  # IMMUTABLE after creation. To re-target, create a new Release.
  source:
    kind: GitRepository
    name: payments-charts-repo
    # namespace defaults to Release's namespace if omitted; cross-namespace requires explicit ref.
  
  # How to interpret the fetched source content.
  # IMMUTABLE: content.type. MUTABLE: path, version, type-specific config (for routine ops).
  content:
    # type: helm | kustomize | manifests | jsonnet | cue
    type: helm
    
    # type: helm — for Helm charts (in Git, OCI, or HelmRegistry sources).
    helm:
      # Path within the source. For GitRepository, the chart directory.
      # Ignored for HelmRegistry (registry implies path).
      path: charts/payments-api
      
      # For HelmRegistry/OCIRegistry sources: chart name and version selector.
      # chart: payments-api
      # version: ">=1.2.0 <2.0.0"
      
      # How to handle subchart dependencies.
      # - vendored: chart's charts/ dir is pre-populated (default, more reproducible)
      # - resolve:  run `helm dependency update` at render time (requires network)
      dependencyMode: vendored
    
    # type: kustomize — for Kustomize overlays.
    # kustomize:
    #   path: overlays/prod
    
    # type: manifests — for raw YAML files.
    # manifests:
    #   paths: ["deploy/", "config/*.yaml"]
    
    # type: jsonnet — for jsonnet-rendered manifests.
    # jsonnet:
    #   path: main.jsonnet
    #   tlaStr:
    #     - name:
    #.      value:
  
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
    pruneOrphaned: true
    
    # Reconciliation interval for drift detection (compare live vs rendered).
    interval: 3m
    
    # Backoff for failed sync attempts.
    retryBackoff:
      initial: 30s
      max: 10m
      factor: 2

    # Defines the order in which resources are applied and their health monitoring strategy.
    waveStrategy:
      # strategy: waitForApplied | waitForHealthy
      strategy: waitForApplied
      # the timeout for the wave strategy. If not set, controller will wait indefinitely.
      timeout: 10m
  
  # How many historical (rendered, applied) records to retain in S3.
  # Drives /history pagination depth and rollback range.
  retentionPolicy:
    keepHistory: 50
  
  # Per-Release deletion behavior — what happens when this specific Release is deleted.
  # Overrides Cluster.spec.deletionPolicy (which governs cluster-cascade deletion).
  deletionPolicyOverride:
    # - prune: remove deployed manifests from the cluster (graceful shutdown)
    # - leave: leave workloads running (rare; for migration to another tool)
    workloads: prune

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
      when: '.Cluster.metadata.tier == "production"'
      jobTemplate: { ... }          # JobTemplateSpec
      weight: 0
      onFailure: AbortSync
  postSync:
    - name: smoke-test
      type: Job
      jobTemplate: { ... }
      onFailure: Continue

pin:
  renderRef: sha256:abc123def456789aabbccddeeff001122334455667788990011223344556677
  pinnedAt: 2026-04-29T10:00:00Z
  pinnedBy: alice@example.com
  reason: "Production incident #4521 — payment failures after deploy"
        
status:
  observedGeneration: 42
  
  conditions:
    - type: Ready
      status: "False"
      lastTransitionTime: 2026-04-29T10:01:15Z
      reason: NotHealthy
      message: "Release is applied and in-sync but workloads are not healthy"
    - type: SourceResolved
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:01Z
    - type: MutationsResolved
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:02Z
    - type: Rendered
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:03Z
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

    # Dedicated condition making the pinned state loudly visible.
    - type: Pinned
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:00Z
      reason: Frozen | Rollback
      message: "Release is pinned to render sha256:abc123… since 2026-04-29T10:00:00Z. Spec/mutation changes will not be applied until unpin (DELETE /pin)."

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

  # Resolved deletion policy snapshot — combines Release.spec.deletionPolicy
  # and Cluster.spec.deletionPolicy. Top-level (current state, not per-render).
  resolvedDeletionPolicy:
      workloadsPolicySource: release | cluster
      policy:
        releases: block
        workloads: prune
        onUnreachable: abort
      sourceClusterGeneration: 3
  
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
      observedSourceGeneration: 7    # Source.metadata.generation at render time
      url: https://github.com/example/payments-charts
      ref: main                       # spec.ref from Source
      revision: abc123def456789aabbccddeeff001122334455   # actual resolved Git SHA
      contentType: helm
      path: charts/payments-api
    
    # Snapshot of resolved Cluster (kubeconfig server, metadata used for templating).
    resolvedCluster:
      name: prod-eu
      uid: 7119fd26-3d58-4b18-88b9-5e0f4c9c76a0
      observedClusterGeneration: 3
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
      observedSourceGeneration: 7
      url: https://github.com/example/payments-charts
      ref: main
      revision: abc123def456789aabbccddeeff001122334455
      contentType: helm
      path: charts/payments-api
    resolvedCluster:
      name: prod-eu
      observedClusterGeneration: 3
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
    #   stage: SourceResolution | MutationResolution | Rendering | Syncing
    #   mutationIndex: 1   # if failure during mutation pipeline
    #   message: "failed to fetch values file: 404 not found"

  # Waves reflects the order in which resources were applied and their health status.
  # Each wave entry represents a group of resources applied together, with a state indicating
  # whether they have been fully applied and healthy, are in progress, or have failed.
  waves:
    - number: -10
      resources: 1                      # CRD
      state: Completed
      startedAt: 2026-04-29T10:00:00Z
      completedAt: 2026-04-29T10:00:15Z
      transitionReason: AppliedAndHealthy

    - number: 0
      resources: 12                     # most resources
      state: Completed
      startedAt: 2026-04-29T10:00:15Z
      completedAt: 2026-04-29T10:00:45Z
      transitionReason: AppliedAndHealthy

    - number: 10
      resources: 1                      # Certificate (CR of the CRD above)
      state: InProgress
      startedAt: 2026-04-29T10:00:45Z
      transitionReason: ""
  
  # Last reconciliation timestamp, even when nothing changed.
  # Distinct from lastAttempted (which only updates on actual change attempts).
  reconciledAt: 2026-04-29T10:03:00Z
  
  historyTruncated: false
```

## Spec

`spec` binds a source, a content interpretation, and a target cluster, plus sync/retention/deletion policy. Identity fields (`source`, `content.type`, `cluster`) are **immutable** after creation — to re-target, create a new Release. Routine fields (paths, versions, mutations, policies) are mutable.

| Field                    | Type   | Required | Default | Description                                                                                                                              |
|--------------------------|--------|----------|---------|------------------------------------------------------------------------------------------------------------------------------------------|
| `source`                 | object | yes      | —       | Reference (`kind` + `name` [+ `namespace`]) to a `Source` object. Polymorphic; see [Source Reference](#source-reference). Immutable.     |
| `content`                | object | yes      | —       | How to interpret fetched source content. Discriminated on `content.type`; see [Content Types](#content-types). `content.type` immutable. |
| `cluster`                | object | yes      | —       | Reference (`ref` [+ `namespace`]) to the target `Cluster`. Immutable.                                                                    |
| `syncPolicy`             | object | no       | —       | Reconciliation behavior: `automatic`, `pruneOrphaned`, `interval`, `retryBackoff`, `waveStrategy` (see [Sync Waves](#sync-waves)).       |
| `retentionPolicy`        | object | no       | —       | `keepHistory` — number of historical (rendered, applied) records to retain in S3; drives `/history` depth and rollback range.            |
| `deletionPolicyOverride` | object | no       | —       | Per-Release deletion behavior (`workloads: prune \| leave`); overrides `Cluster.spec.deletionPolicy`.                                    |

`mutations` is a **top-level field** (not under `spec`), exposed via the `/mutations` subresource for field-level RBAC and audit. It is an ordered, atomic (`+listType=atomic`) transformation pipeline; see [Mutation Types](#mutation-types).

`lifecycle` is likewise a **top-level field** (not under `spec`), exposed via the `/lifecycle` subresource for field-level RBAC and audit. It holds the phase-keyed hook lists; see [Lifecycle Hooks](#lifecycle-hooks). Execution state is mirrored under `status.lifecycle`.

## Source Reference

`spec.source.kind` selects which `Source` kind the Release pulls content from. The reference is immutable.

| `kind`          | Content pulled from                                                                                          |
|-----------------|--------------------------------------------------------------------------------------------------------------|
| `GitRepository` | A tracked Git ref (branch/tag/commit/semver). Most common — the source of charts, manifests, kustomizations. |
| `HelmRegistry`  | An HTTP Helm chart repository, selected by chart name + version constraint.                                  |
| `OCIRegistry`   | An OCI registry artifact (Helm chart, manifest bundle, or kustomize bundle).                                 |

```yaml
spec:
  source:
    kind: GitRepository        # | HelmRegistry | OCIRegistry
    name: payments-charts-repo
    # namespace: payments-team   # defaults to the Release's namespace
```

## Content Types

`spec.content.type` is a discriminated union describing how fetched source content is rendered into a manifest set. `content.type` is immutable; the type-specific config (paths, versions) is mutable.

### helm

Render a Helm chart. `path` is the chart directory (for `GitRepository`) or ignored for registry sources, which select via `chart` + `version`. `dependencyMode` controls subchart handling: `vendored` (chart's `charts/` is pre-populated — default, reproducible) or `resolve` (`helm dependency update` at render time — requires network).

```yaml
spec:
  content:
    type: helm
    helm:
      path: charts/payments-api
      # chart: payments-api          # for registry sources
      # version: ">=1.2.0 <2.0.0"
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

Render Jsonnet to manifests, optionally passing top-level arguments (`tlaStr`).

```yaml
spec:
  content:
    type: jsonnet
    jsonnet:
      path: main.jsonnet
      tlaStr:
        env: prod
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

`mutations[]` is an ordered pipeline; each entry declares its `type`. Entries run in order, each operating on the output of the previous one. Inline content is provided directly; external content is referenced via `from.sourceRef` (typically a `GitRepository`).

### helmValues

Merge Helm values before chart rendering. Sourced from a values file (`from.sourceRef`) or inline.

```yaml
mutations:
  - type: helmValues
    from:
      sourceRef: { kind: GitRepository, name: payments-config-repo, path: env/prod/values.yaml }
```

### goTemplate

Apply a Go template. By default it runs **after** Helm rendering on the rendered YAML (to avoid `{{ }}` collisions). Template context: `.Cluster`, `.Release`, `.Source`. Provided `inline` or via `from.sourceRef`.

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

Apply a Kubernetes strategic merge patch (kind-aware list/map merging). Provided inline or via `from.sourceRef`.

```yaml
mutations:
  - type: strategicMergePatch
    from:
      sourceRef: { kind: GitRepository, name: payments-experiments-repo, path: features/new-tracing/patch.yaml }
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
| `health.summary`          | object      | Aggregate health counts (`total`/`healthy`/`progressing`/`degraded`/`suspended`/`unknown`). The per-resource detail is served by [`/health`](#health-subresource), not stored here. |
| `pin`                     | object      | Mirror of the *fact* of being pinned (`intent: freeze \| rollback`, `renderInputs`); full detail behind `/pin`.                                                                     |
| `resolvedDeletionPolicy`  | object      | Effective deletion policy, combining `Release.spec.deletionPolicyOverride` and `Cluster.spec.deletionPolicy`.                                                                       |
| `resolvedRetentionPolicy` | object      | Retention resolved into operational state (`keepHistory`, `historyCount`).                                                                                                          |
| `lastSuccessful`          | object      | Last successful render — what is actually deployed: resolved source/cluster/mutation snapshots, `renderRef`, `syncedAt`, `syncedResources`.                                         |
| `lastAttempted`           | object      | Most recent reconciliation attempt (may be in-progress/failed): same snapshots plus `phase` (`Rendering \| Syncing \| Succeeded \| Failed`) and a `failure` block when failed.      |
| `waves`                   | []object    | Per-wave apply progress; see [Sync Waves](#sync-waves).                                                                                                                             |
| `reconciledAt`            | timestamp   | Last reconciliation timestamp, even when nothing changed (distinct from `lastAttempted`).                                                                                           |
| `historyTruncated`        | bool        | Whether retained history was truncated below requested depth.                                                                                                                       |

## Conditions

`Release` reports the following conditions in `status.conditions`. The three core signals (`Applied`, `InSync`, `Healthy`) and the composite `Ready` are detailed in [Release State Model](#release-state-model) below.

| Condition           | What `True` means                                                                                                                         |
|---------------------|-------------------------------------------------------------------------------------------------------------------------------------------|
| `SourceResolved`    | The referenced `Source` resolved to a concrete revision and its content was fetched.                                                      |
| `MutationsResolved` | All mutation inputs (values files, patches, templates) were resolved/fetched.                                                             |
| `Rendered`          | The post-mutation manifest set rendered successfully.                                                                                     |
| `Applied`           | The most recent apply to the cluster succeeded (kube-apiserver accepted every resource). See [State Model](#applied).                     |
| `InSync`            | The live cluster state matches the rendered manifest set (no drift). See [State Model](#insync).                                          |
| `Healthy`           | Every managed resource reached its intended runtime state. `Unknown` while progressing. See [State Model](#healthy).                      |
| `Ready`             | Composite: `Applied` ∧ `InSync` ∧ `Healthy`. The signal most tooling should watch. See [State Model](#ready).                             |
| `Pinned`            | The Release is pinned to a fixed render (`reason: Frozen \| Rollback`); spec/mutation changes will not apply until unpin (`DELETE /pin`). |
| `SpecDriftFromPin`  | Spec or mutations were edited since the pin was set; the pending intent will apply on unpin.                                              |

## Release State Model

A deployed `Release` has three orthogonal state signals reflecting different aspects of the deployment. They are reported as separate conditions in `Release.status.conditions`, and aggregated into a composite `Ready` condition.

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

`Healthy: Unknown` is a meaningful third value here: a Pod that's still pulling its image isn't unhealthy, it's progressing. A Deployment that just started rolling out isn't unhealthy. The health assessment must distinguish "still working on it" from "broken."

`Healthy` is independent of `Applied` and `InSync`. A Release can be applied, in-sync, and unhealthy (manifest is correct, workloads aren't working). A Release can be unapplied (apply failed) but its previously-deployed workloads are still healthy (cluster still has the prior version running).

### Composite Condition

#### Ready

`Ready: True` requires all three:

| `Applied` | `InSync` | `Healthy` | `Ready` | Meaning                                                         |
|-----------|----------|-----------|---------|-----------------------------------------------------------------|
| `True`    | `True`   | `True`    | `True`  | Steady state — deployed and working                             |
| `True`    | `True`   | `Unknown` | `False` | Progressing — workloads coming up                               |
| `True`    | `True`   | `False`   | `False` | Deployment broken — manifests applied but workloads not working |
| `True`    | `False`  | *         | `False` | Drift — cluster has diverged from desired                       |
| `False`   | *        | *         | `False` | Apply failed — manifests not in cluster                         |

`Ready` is the signal most tooling and dashboards should watch. The three underlying conditions provide diagnostic detail when `Ready: False`.

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
    
    - type: Ready
      status: "False"
      lastTransitionTime: 2026-04-29T10:01:15Z
      reason: NotHealthy
      message: "Release is applied and in-sync but workloads are not healthy"
```

### Vocabulary Mapping for Users Coming from Other Tools

| Concept                        | This tool | ArgoCD                 | Flux                                     |
|--------------------------------|-----------|------------------------|------------------------------------------|
| Manifests successfully applied | `Applied` | `Synced`               | (implicit; not a separate condition)     |
| Cluster matches desired state  | `InSync`  | (implicit in `Synced`) | `Ready` (partial — also includes health) |
| Workloads running correctly    | `Healthy` | `Healthy`              | (folded into `Ready`)                    |
| Aggregate "all good"           | `Ready`   | `Synced + Healthy`     | `Ready`                                  |

ArgoCD users will recognize `Healthy`. The split between `Applied` and `InSync` is more granular than ArgoCD's `Synced` (which conflates "applied" with "matches" — fine in practice but loses signal during drift events). Flux users will find the three-way split more granular than Flux's combined `Ready`.

### Field Reference

These three conditions appear in `Release.status.conditions` as standard k8s `Condition` objects:

| Field                | Description                                                                                                                             |
|----------------------|-----------------------------------------------------------------------------------------------------------------------------------------|
| `type`               | One of `Applied`, `InSync`, `Healthy`, `Ready`.                                                                                         |
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

`status` carries only the **bounded aggregate**: the `Healthy` condition and a `health.summary` of counts. The per-resource detail is **not** stored in `status` — for a large Release it could exceed the API object size limit — and is served on demand by the [`/health` subresource](#health-subresource) instead.

```yaml
status:
  conditions:
    - type: Healthy
      status: "False"
      reason: WorkloadsDegraded
      lastTransitionTime: 2026-04-29T10:01:15Z
      message: "1 of 14 resources degraded"
  
  health:
    summary:
      total: 14
      healthy: 11
      progressing: 1
      degraded: 1
      suspended: 1
      unknown: 0
```

#### `health.summary`

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

`GET /releases/<name>/health` returns the per-resource health detail for the Release's managed resources. This detail is a runtime view re-evaluated by the controller from live cluster state; it is **not** persisted in `Release.status` (only the [`health.summary`](#healthsummary) aggregate is). Keeping the potentially large, high-churn per-resource list out of the object keeps `status` bounded regardless of Release size.

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
- **Phase vocabulary is unconstrained.** Not bound to ArgoCD or Helm phase names; can include phases (like `PreRender` and `PostRender`) that don't exist in either ecosystem.
- **Audit and RBAC.** Subresource means standard k8s RBAC; distinct audit events for hook configuration changes.
- **Helm chart compatibility happens at translation time.** When sources have `helm.sh/hook` annotations, the renderer translates them into lifecycle entries automatically. Existing Helm charts with hooks work without modification.

### Phases

| Phase        | When it runs                                      | Common uses                                              |
|--------------|---------------------------------------------------|----------------------------------------------------------|
| `PreRender`  | Before mutations are applied                      | Validation of source content; environment checks         |
| `PostRender` | After rendering, before applying                  | Manifest validation; security scanning; signing          |
| `PreSync`    | Before any wave applies                           | Database migrations; pre-deploy checks; resource cleanup |
| `PostSync`   | After all waves complete (and optionally healthy) | Smoke tests; cache warming; deploy notifications         |
| `SyncFail`   | When sync fails at any point                      | Alerts; partial-state cleanup; rollback triggers         |
| `PreDelete`  | Before workloads are pruned on Release deletion   | Backups; graceful shutdown; external cleanup             |
| `PostDelete` | After workloads are pruned                        | Final notifications; resource accounting                 |

### Hook Schema

Hooks are typed units — each declares its kind via `type` and provides a Kubernetes-native template for that kind.

```yaml
# /releases/payments-api-prod-eu/lifecycle subresource

preSync:
  - name: db-migration
    type: Job                       # Job | Pod (v1)
    
    # Optional CEL condition. If absent, hook always runs.
    # If present and evaluates to false (or errors), hook is skipped.
    when: '.Cluster.metadata.tier == "production"'
    
    # k8s-native template (here: JobTemplateSpec, similar to CronJob.spec.jobTemplate)
    jobTemplate:
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
                    # Templated against per-Release context at sync time
                    value: "{{ .Cluster.metadata.region }}"
        backoffLimit: 0
    
    # Ordering within the phase (signed integer, parallel within same weight)
    weight: 0
    
    # Hook timeout — overrides any phase-level default
    timeout: 10m
    
    # What happens if the hook fails
    onFailure: AbortSync            # | Continue | AbortDelete (PreDelete only)
    
    # Cleanup behavior
    deleteBeforeCreation: true
    cleanup: OnSuccess  # | OnCompletion | Never

postSync:
  - name: smoke-test
    type: Job
    jobTemplate: { ... }
    onFailure: Continue
    cleanup: OnCompletion

syncFail:
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
                    value: "{{ .Release.name }}"
                  - name: FAILURE_STAGE
                    value: "{{ .Sync.failure.stage }}"
    cleanup: OnCompletion

preDelete:
  - name: backup-database
    type: Job
    jobTemplate: { ... }
    timeout: 30m
    onFailure: AbortDelete
    cleanupOnCompletion: Never      # retain for compliance audit
```

#### Hook fields

| Field                  | Type     | Required      | Default       | Description                                                                        |
|------------------------|----------|---------------|---------------|------------------------------------------------------------------------------------|
| `name`                 | string   | yes           | —             | Unique within the Release. Identifies the hook for status reporting and updates.   |
| `type`                 | enum     | yes           | —             | One of `Job`, `Pod`. Determines which template field is used.                      |
| `when`                 | string   | no            | —             | CEL expression. Hook runs only when expression is `true`. Errors treat as `false`. |
| `jobTemplate`          | object   | when type=Job | —             | Standard k8s `JobTemplateSpec`. Templated against phase-specific context.          |
| `podTemplate`          | object   | when type=Pod | —             | Standard k8s `PodTemplateSpec`.                                                    |
| `weight`               | integer  | no            | `0`           | Ordering within phase. Lower runs first; same weight runs in parallel.             |
| `timeout`              | duration | no            | phase default | Maximum time to wait for hook completion before marking it `TimedOut`.             |
| `onFailure`            | enum     | no            | phase default | `AbortSync`, `AbortDelete`, or `Continue`.                                         |
| `deleteBeforeCreation` | bool     | no            | `true`        | Delete any existing hook resource of the same name before creating a new one.      |
| `cleanupOnCompletion`  | enum     | no            | phase default | `OnSuccess`, `OnCompletion`, or `Never`.                                           |

### CEL `when` Conditions

The optional `when` field on each hook makes hook execution conditional. When absent, the hook always runs. When present, the expression evaluates against phase-specific bindings; the hook runs only if the expression is `true`.

#### Phase-specific bindings

| Phase        | Bindings available                                                                              |
|--------------|-------------------------------------------------------------------------------------------------|
| `PreRender`  | `.Release`, `.Cluster`, `.Source`                                                               |
| `PostRender` | `.Release`, `.Cluster`, `.Source`, `.Render.manifestsRef`                                       |
| `PreSync`    | `.Release`, `.Cluster`, `.Render.manifestsRef`, `.Release.previousRender`                       |
| `PostSync`   | `.Release`, `.Cluster`, `.Render.manifestsRef`, `.Sync.completedAt`                             |
| `SyncFail`   | `.Release`, `.Cluster`, `.Render.manifestsRef`, `.Sync.failure.{stage, message, mutationIndex}` |
| `PreDelete`  | `.Release`, `.Cluster`                                                                          |
| `PostDelete` | `.Release`, `.Cluster`                                                                          |

`.Release.previousRender` is `null` on first deploy and populated otherwise — enables "first-deploy vs update" conditional logic without separate phases.

#### Common patterns

```yaml
# Production-only
when: '.Cluster.metadata.tier == "production"'
```

```yaml
# First deploy only
when: '.Release.firstDeploy'
```

```yaml
# Region-specific
when: '.Cluster.metadata.region in ["eu-west-1", "eu-central-1"]'
```

```yaml
# Specific failure stage
when: '.Sync.failure.stage == "Rendering"'
```

```yaml
# Combination
when: '.Cluster.metadata.tier == "production" && .Release.firstDeploy'
```

#### Evaluation semantics

- Validation at admission time: each `when` expression is compiled and field references are validated; invalid expressions are rejected on `/lifecycle` update
- Errors at runtime treat the condition as `false`; hook is skipped with a warning recorded in status
- Skipped hooks appear in status with `state: Skipped` and a reason

### Failure Handling

Each hook's `onFailure` controls behavior when the hook fails. Defaults vary by phase based on stake:

| Phase        | Default `onFailure` |
|--------------|---------------------|
| `PreRender`  | `AbortSync`         |
| `PostRender` | `AbortSync`         |
| `PreSync`    | `AbortSync`         |
| `PostSync`   | `Continue`          |
| `SyncFail`   | `Continue`          |
| `PreDelete`  | `AbortDelete`       |
| `PostDelete` | `Continue`          |

| Value         | Behavior                                                                                                                            |
|---------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `AbortSync`   | Stop the sync. Set `Applied: False` with reason `HookFailed`. Run `SyncFail` hooks. Don't apply remaining waves or post-sync hooks. |
| `AbortDelete` | Stop the deletion. Preserve workloads. Release stays in deleting state until policy or hook is changed.                             |
| `Continue`    | Log the failure, continue with the rest of the sync. Failure is recorded in status but doesn't halt progress.                       |

### Cleanup

Two orthogonal axes control hook resource lifecycle.

#### `deleteBeforeCreation`

Whether to delete any existing same-named hook resource before creating a new one for the current sync.

| Value            | Behavior                                                                                                                                  |
|------------------|-------------------------------------------------------------------------------------------------------------------------------------------|
| `true` (default) | Delete leftover hook resource before creating new one. Required for re-runnable hooks (migrations re-running on each sync).               |
| `false`          | Allow accumulation. Each sync creates a new hook resource (typically with a generated suffix). Subject to `maxRetainedPerHook` retention. |

#### `cleanupOnCompletion`

When to delete the hook resource after it completes.

| Value          | Behavior                                                                                             |
|----------------|------------------------------------------------------------------------------------------------------|
| `OnSuccess`    | Delete on success; retain on failure (for debugging).                                                |
| `OnCompletion` | Delete after completion regardless of success/failure.                                               |
| `Never`        | Retain indefinitely. User responsible for manual cleanup. Compliance use case (audit jobs, backups). |

Defaults per phase:

| Phase                     | Default `cleanupOnCompletion` |
|---------------------------|-------------------------------|
| `PreRender`, `PostRender` | `OnSuccess`                   |
| `PreSync`, `PostSync`     | `OnSuccess`                   |
| `SyncFail`                | `OnCompletion`                |
| `PreDelete`               | `Never`                       |
| `PostDelete`              | `OnCompletion`                |

`deleteBeforeCreation: true` is the default for all phases.

### Ownership Tracking

Hook resources live in target clusters; the Release object lives in the control plane cluster. Cross-cluster `ownerReferences` don't work (the target cluster's GC can't resolve references to resources in another cluster), so hooks use **labels-only ownership tracking**.

Every hook resource created by the controller carries:

```yaml
metadata:
  labels:
    convoy.kumodo.io/release-name: payments-api-prod-eu
    convoy.kumodo.io/release-namespace: payments-team
    convoy.kumodo.io/release-uid: 8f3a2b1c-...
    convoy.kumodo.io/hook-phase: PreSync
    convoy.kumodo.io/hook-name: db-migration
    convoy.kumodo.io/managed-by: convoy
  annotations:
    convoy.kumodo.io/created-at: "2026-04-29T09:59:30Z"
    convoy.kumodo.io/sync-id: <sync-uuid>
```

The controller queries by label selector to discover owned resources. All cleanup is controller-driven (sync-time and periodic GC sweep).

If the controller is permanently lost or disconnected, hook resources in target clusters become orphaned. Operators discover this via target-cluster inspection (`kubectl get jobs -l convoy.kumodo.io/managed-by=convoy`) and clean up manually. This is a property of any cross-cluster managed system; the cure is operational practice (monitoring controller health), not a technical mechanism.

### Lifecycle Retention Policy

`Release.spec.lifecycleRetentionPolicy` provides a backstop for stale hook resources beyond per-hook cleanup behavior. Catches edge cases: orphans from removed hooks, leftovers from controller crashes, accumulation under `cleanupOnCompletion: Never`.

```yaml
spec:
  lifecycleRetentionPolicy:
    maxAge: 30d
    onHookRemoved: prune
    overrideNeverCleanup: false
    maxRetainedPerHook: 5
```

| Field                  | Type     | Default | Description                                                                                                                                       |
|------------------------|----------|---------|---------------------------------------------------------------------------------------------------------------------------------------------------|
| `maxAge`               | duration | `30d`   | Maximum age for any retained hook resource. Older resources are deleted by GC sweep. Set to `0` to disable.                                       |
| `onHookRemoved`        | enum     | `prune` | When a hook is removed from `/lifecycle`, what to do with existing resources. `prune` deletes them; `retain` leaves them to age out via `maxAge`. |
| `overrideNeverCleanup` | bool     | `false` | Whether `maxAge` applies even to hooks with `cleanupOnCompletion: Never`. Default `false` respects compliance intent.                             |
| `maxRetainedPerHook`   | integer  | `5`     | Maximum retained resources per hook name when `deleteBeforeCreation: false`.                                                                      |

#### Interaction with per-hook policy

| Per-hook config                                             | `maxAge` behavior                              |
|-------------------------------------------------------------|------------------------------------------------|
| `cleanupOnCompletion: OnSuccess`, succeeded                 | Already cleaned; nothing to do                 |
| `cleanupOnCompletion: OnSuccess`, failed                    | Retained; deleted when older than `maxAge`     |
| `cleanupOnCompletion: OnCompletion`                         | Already cleaned; nothing to do                 |
| `cleanupOnCompletion: Never`, `overrideNeverCleanup: false` | Retained indefinitely; `maxAge` does not apply |
| `cleanupOnCompletion: Never`, `overrideNeverCleanup: true`  | Deleted when older than `maxAge`               |

#### When GC runs

- **Sync-time:** cleanup runs as part of each sync, after applying manifests and running hooks
- **Periodic background sweep:** controller scans all Releases periodically (e.g., hourly) for stale resources, regardless of sync activity
- Both modes apply the same retention policy

### Helm Chart Compatibility

Helm charts use `helm.sh/hook` annotations to mark resources as hooks. The renderer recognizes these and translates them into lifecycle entries automatically:

| Helm hook                                          | Translated to                                               | Notes                              |
|----------------------------------------------------|-------------------------------------------------------------|------------------------------------|
| `pre-install`, `pre-upgrade`                       | `PreSync` entry                                             | Treated as a regular PreSync hook  |
| `post-install`, `post-upgrade`                     | `PostSync` entry                                            |                                    |
| `pre-delete`                                       | `PreDelete` entry                                           |                                    |
| `post-delete`                                      | `PostDelete` entry                                          |                                    |
| `helm.sh/hook-weight`                              | `weight` field                                              |                                    |
| `helm.sh/hook-delete-policy: hook-succeeded`       | `cleanupOnCompletion: OnSuccess`                            |                                    |
| `helm.sh/hook-delete-policy: hook-failed`          | `cleanupOnCompletion: Never` (retain on failure equivalent) |                                    |
| `helm.sh/hook-delete-policy: before-hook-creation` | `deleteBeforeCreation: true`                                |                                    |
| `test`, `test-success`, `test-failure`             | Currently ignored                                           | Helm test framework not integrated |

User-defined hooks (in `/lifecycle`) and chart-translated hooks coexist. Chart hooks appear in status marked with `source: chart`; user hooks with `source: lifecycle`. Within a phase, ordering is by `weight` regardless of source.

This means standard Helm charts with hook annotations work without modification when deployed via this tool — chart authors don't have to do anything special, and deploying teams don't have to translate hooks manually.

### Status Reflection

Hook execution state is reported per-phase in `Release.status.lifecycle`:

```yaml
status:
  lifecycle:
    preSync:
      - name: db-migration
        source: lifecycle              # | chart
        kind: Job
        apiVersion: batch/v1
        resourceNamespace: payments-team
        resourceName: db-migration-1745928000
        state: Succeeded
        weight: 0
        startedAt: 2026-04-29T09:59:30Z
        completedAt: 2026-04-29T10:00:00Z
        cleanedUpAt: 2026-04-29T10:00:05Z
        message: "Job completed successfully (1/1 succeeded). Resource cleaned up."
    
    postSync:
      - name: smoke-test
        source: lifecycle
        state: Failed
        startedAt: 2026-04-29T10:00:50Z
        completedAt: 2026-04-29T10:01:30Z
        message: "Job failed (backoff limit exceeded; container exit 1)"
        followUpAction: Continue
      
      - name: payments-helm-post-install
        source: chart                   # came from helm.sh/hook annotation
        state: Skipped
        message: "when condition evaluated to false"
  
  lifecycleRetention:
    lastSweepAt: 2026-04-29T10:00:00Z
    resourcesDeleted: 3
    resourcesRetained: 5
  
  resolvedLifecycleRetentionPolicy:
    maxAge: 30d
    onHookRemoved: prune
    overrideNeverCleanup: false
    maxRetainedPerHook: 5
```

#### Per-hook status fields

| Field                               | Description                                                                                           |
|-------------------------------------|-------------------------------------------------------------------------------------------------------|
| `name`                              | Matches the hook's name in the subresource.                                                           |
| `source`                            | `lifecycle` (user-defined) or `chart` (translated from Helm hook annotation).                         |
| `kind`, `apiVersion`                | Resource kind of the created hook.                                                                    |
| `resourceNamespace`, `resourceName` | Identity of the actual created resource.                                                              |
| `state`                             | `Running`, `Succeeded`, `Failed`, `TimedOut`, `Skipped`.                                              |
| `weight`                            | Weight of the hook.                                                                                   |
| `skipReason`                        | When `state: Skipped`: `Condition` (when=false) or `EvaluationError`.                                 |
| `startedAt`                         | When the hook resource was created and began running.                                                 |
| `completedAt`                       | When the hook reached terminal state (success/failure/timeout).                                       |
| `cleanedUpAt`                       | When the hook resource was deleted by cleanup logic. Absent when retained.                            |
| `message`                           | Human-readable detail.                                                                                |
| `followUpAction`                    | What the controller did after the hook completed: `None`, `Continue`, `AbortedSync`, `AbortedDelete`. |

### Interaction with Sync Waves

Hooks and waves operate at different points in the sync pipeline and don't mix at the resource level:

- `PreSync` hooks run before any wave starts
- All waves apply through their own ordering and transition strategies
- `PostSync` hooks run after all waves complete

Within a phase, hooks order by `weight` (not by `sync-wave`). Ordering mechanisms are scoped: waves order regular manifests; hook weights order hooks within a phase. They don't interact.

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
| Phases                | `PreRender`, `PostRender`, `PreSync`, `PostSync`, `SyncFail`, `PreDelete`, `PostDelete` |
| Hook types            | `Job` and `Pod` for v1                                                                  |
| Hook template         | k8s-native template per type (e.g., `JobTemplateSpec`)                                  |
| Templating context    | Phase-specific bindings (`.Release`, `.Cluster`, `.Render`, `.Sync`, etc.)              |
| Conditional execution | Optional CEL `when` field per hook                                                      |
| Completion criteria   | Reuses `HealthRule` machinery — hook is done when its resource is `Healthy`             |
| Failure handling      | Per-hook `onFailure`; phase-appropriate defaults                                        |
| Ordering              | Per-hook `weight` within phase; parallel within same weight                             |
| Cleanup               | Per-hook `deleteBeforeCreation` and `cleanupOnCompletion`                               |
| Retention backstop    | Release-level `lifecycleRetentionPolicy`                                                |
| Ownership tracking    | Labels-only (cross-cluster `ownerReferences` don't work)                                |
| Helm compatibility    | `helm.sh/hook` annotations translated automatically; coexist with user hooks            |
| Status                | Per-phase, per-hook execution state in `status.lifecycle`                               |

### Binding Model

The context exposes three top-level objects, each a projection of an underlying API resource, plus a small set of computed aliases for common patterns.

| Object     | Backing resource                                            | Available in phases                                              |
|------------|-------------------------------------------------------------|------------------------------------------------------------------|
| `.Release` | The Release object itself                                   | All phases                                                       |
| `.Cluster` | The Cluster object referenced by the Release                | All phases                                                       |
| `.Source`  | The Source object referenced by the Release                 | `PreRender`, `PostRender` only                                   |
| `.Sync`    | Computed projection of the in-flight or just-completed sync | `PostRender` onward (with phase-specific availability per field) |

Direct access to underlying resource fields (`.Release.status.lastAttempted.renderRef`, etc.) is fully supported. Aliases exist purely for ergonomic access to commonly-referenced information; they are read-only projections that present the right value for the hook's phase.

When the underlying status structure evolves between API versions, aliases preserve their documented semantics. Users referencing aliases are insulated from internal status reorganization; users using direct status access bear the responsibility of tracking schema changes.

### `.Release` Bindings

Direct projection of the Release object. Available in all phases.

| Path                   | Backing                | Notes                                                               |
|------------------------|------------------------|---------------------------------------------------------------------|
| `.Release.name`        | `metadata.name`        | Release object name                                                 |
| `.Release.namespace`   | `metadata.namespace`   | Release object namespace                                            |
| `.Release.uid`         | `metadata.uid`         | Stable identity across renames                                      |
| `.Release.generation`  | `metadata.generation`  | Spec generation at hook execution time                              |
| `.Release.labels`      | `metadata.labels`      | Release labels                                                      |
| `.Release.annotations` | `metadata.annotations` | Release annotations                                                 |
| `.Release.spec.*`      | `spec.*`               | Full Release spec — source, cluster, content, syncPolicy, etc.      |
| `.Release.status.*`    | `status.*`             | Full Release status — for direct access when an alias doesn't exist |

#### Release aliases

| Alias                              | Backing                                          | Notes                                                                   |
|------------------------------------|--------------------------------------------------|-------------------------------------------------------------------------|
| `.Release.firstDeploy`             | computed: `!has(.Release.status.lastSuccessful)` | `true` on the first deploy of this Release; `false` thereafter.         |
| `.Release.lastSuccessfulRenderRef` | `.Release.status.lastSuccessful.renderRef`       | Hash of currently deployed render. `null` when `firstDeploy` is `true`. |
| `.Release.lastSuccessfulSyncedAt`  | `.Release.status.lastSuccessful.syncedAt`        | When the previously deployed render was synced. `null` on first deploy. |

### `.Cluster` Bindings

Direct projection of the Cluster object. Available in all phases.

| Path                 | Backing              | Notes                                     |
|----------------------|----------------------|-------------------------------------------|
| `.Cluster.name`      | `metadata.name`      | Cluster object name                       |
| `.Cluster.namespace` | `metadata.namespace` | Cluster object namespace                  |
| `.Cluster.uid`       | `metadata.uid`       | Stable cluster identity                   |
| `.Cluster.labels`    | `metadata.labels`    | Cluster labels (used for fleet selection) |
| `.Cluster.spec.*`    | `spec.*`             | Full Cluster spec                         |
| `.Cluster.status.*`  | `status.*`           | Full Cluster status                       |

#### Cluster aliases

| Alias                        | Backing                             | Notes                                                                                                                                                                                |
|------------------------------|-------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `.Cluster.metadata`          | `.Cluster.spec.metadata`            | Per-cluster free-form metadata. **Note:** this aliases the spec field, not the k8s `metadata` block. The k8s metadata fields are exposed as `.Cluster.name`, `.Cluster.labels`, etc. |
| `.Cluster.discoveredVersion` | `.Cluster.status.discoveredVersion` | Discovered Kubernetes version (e.g., `v1.29.4`).                                                                                                                                     |

The `.Cluster.metadata` alias intentionally surfaces the most commonly-referenced field (per-cluster templating metadata) at a convenient path. Users referencing the k8s metadata block directly use `.Cluster.name`, `.Cluster.labels`, etc.

### `.Source` Bindings

Direct projection of the Source object referenced by `Release.spec.source`. Available only in `PreRender` and `PostRender` phases — by `PreSync` onward, the rendered output is what matters and source-specific information is no longer relevant.

| Path                | Backing                     | Notes                                                 |
|---------------------|-----------------------------|-------------------------------------------------------|
| `.Source.kind`      | source kind                 | One of `GitRepository`, `HelmRegistry`, `OCIRegistry` |
| `.Source.name`      | source `metadata.name`      |                                                       |
| `.Source.namespace` | source `metadata.namespace` |                                                       |
| `.Source.spec.*`    | source `spec.*`             | Full source spec                                      |
| `.Source.status.*`  | source `status.*`           | Full source status                                    |

#### Source aliases

| Alias                         | Backing                               | Notes                                                    |
|-------------------------------|---------------------------------------|----------------------------------------------------------|
| `.Source.lastFetchedRevision` | `.Source.status.lastFetched.revision` | Resolved revision (e.g., Git SHA, chart version digest). |

### `.Sync` Bindings

Computed projection of the sync attempt the hook is part of. Aliases consolidate render and sync information that would otherwise require navigating `lastAttempted` and `lastSuccessful` separately.

`.Sync.*` is **phase-dependent**: the underlying field that backs each alias varies by phase, so the alias presents the value meaningful for that phase without users needing to know which status field to read.

| Alias                       | Backing (phase-dependent)                                                                                                                  | Available in                            | Notes                                                                                                                        |
|-----------------------------|--------------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------|------------------------------------------------------------------------------------------------------------------------------|
| `.Sync.renderRef`           | `.Release.status.lastAttempted.renderRef` (PostRender, PreSync, PostSync, SyncFail); equals `lastSuccessful.renderRef` once sync completes | PostRender, PreSync, PostSync, SyncFail | Hash of the render being applied (or just applied). May be `null` in `SyncFail` if failure occurred before render completed. |
| `.Sync.renderCompletedAt`   | `.Release.status.lastAttempted.renderCompletedAt`                                                                                          | PostRender, PreSync, PostSync           | When rendering finished for the current attempt.                                                                             |
| `.Sync.attemptedAt`         | `.Release.status.lastAttempted.attemptedAt`                                                                                                | PreSync, PostSync, SyncFail             | When the current sync attempt started.                                                                                       |
| `.Sync.completedAt`         | `.Release.status.lastSuccessful.syncedAt`                                                                                                  | PostSync                                | When sync finished.                                                                                                          |
| `.Sync.syncedResources`     | `.Release.status.lastSuccessful.syncedResources`                                                                                           | PostSync                                | Count of resources applied in the just-completed sync.                                                                       |
| `.Sync.changedFromPrevious` | computed: `lastAttempted.renderRef != lastSuccessful.renderRef` (or `true` if `lastSuccessful` is `null`)                                  | PostSync                                | `true` if this sync changed the deployed render hash; `false` if the same content was reapplied.                             |

#### `.Sync.failure` (SyncFail only)

| Alias                         | Backing                                               | Notes                                                                                  |
|-------------------------------|-------------------------------------------------------|----------------------------------------------------------------------------------------|
| `.Sync.failure.stage`         | `.Release.status.lastAttempted.failure.stage`         | One of `SourceResolution`, `MutationResolution`, `Rendering`, `Syncing`, `HookFailed`  |
| `.Sync.failure.message`       | `.Release.status.lastAttempted.failure.message`       | Human-readable failure detail                                                          |
| `.Sync.failure.mutationIndex` | `.Release.status.lastAttempted.failure.mutationIndex` | When `stage: MutationResolution`, the index of the failing mutation. `null` otherwise. |
| `.Sync.failure.waveNumber`    | `.Release.status.lastAttempted.failure.waveNumber`    | When `stage: Syncing`, the wave number where failure occurred. `null` otherwise.       |
| `.Sync.failure.hookName`      | `.Release.status.lastAttempted.failure.hookName`      | When `stage: HookFailed`, the name of the failing hook. `null` otherwise.              |
| `.Sync.failure.hookPhase`     | `.Release.status.lastAttempted.failure.hookPhase`     | When `stage: HookFailed`, the phase of the failing hook. `null` otherwise.             |

### Phase Availability Summary

| Binding                     | PreRender | PostRender | PreSync | PostSync | SyncFail        | PreDelete | PostDelete |
|-----------------------------|-----------|------------|---------|----------|-----------------|-----------|------------|
| `.Release.*`                | ✓         | ✓          | ✓       | ✓        | ✓               | ✓         | ✓          |
| `.Cluster.*`                | ✓         | ✓          | ✓       | ✓        | ✓               | ✓         | ✓          |
| `.Source.*`                 | ✓         | ✓          | —       | —        | —               | —         | —          |
| `.Sync.renderRef`           | —         | ✓          | ✓       | ✓        | ✓ (may be null) | —         | —          |
| `.Sync.renderCompletedAt`   | —         | ✓          | ✓       | ✓        | —               | —         | —          |
| `.Sync.attemptedAt`         | —         | —          | ✓       | ✓        | ✓               | —         | —          |
| `.Sync.completedAt`         | —         | —          | —       | ✓        | —               | —         | —          |
| `.Sync.syncedResources`     | —         | —          | —       | ✓        | —               | —         | —          |
| `.Sync.changedFromPrevious` | —         | —          | —       | ✓        | —               | —         | —          |
| `.Sync.failure.*`           | —         | —          | —       | —        | ✓               | —         | —          |

### Common Patterns

#### First-deploy vs. update

```yaml
when: '.Release.firstDeploy'
```

Runs only on the first successful deploy of this Release. Useful for one-time initialization (database seeds, namespace setup, initial configuration).

#### Skip on no-op syncs

```yaml
when: '.Sync.changedFromPrevious'
```

In `PostSync`, runs the hook only when the sync actually changed something. Avoids re-running smoke tests when reconciliation reapplies an unchanged manifest set.

#### Environment-conditional

```yaml
when: '.Cluster.metadata.tier == "production"'
```

Production-only operations. The user populates `Cluster.spec.metadata.tier` during cluster registration; the alias makes templating concise.

#### Region-conditional

```yaml
when: '.Cluster.metadata.region in ["eu-west-1", "eu-central-1"]'
```

Region-specific operations (e.g., GDPR-related cleanup in EU regions).

#### Failure-stage-conditional

```yaml
when: '.Sync.failure.stage == "Rendering"'
```

In `SyncFail`, route different alerts depending on what stage failed. Render failures are config issues; sync failures are runtime issues; they typically need different escalation paths.

#### Combining conditions

```yaml
when: '.Cluster.metadata.tier == "production" && .Release.firstDeploy'
```

Production, first deploy only. Common pattern for one-time production setup that should never run in dev or staging.

#### Reference render hash in template

```yaml
env:
  - name: RENDER_HASH
    value: "{{ .Sync.renderRef }}"
  - name: REGION
    value: "{{ .Cluster.metadata.region }}"
```

### Evaluation Semantics

- Bindings are evaluated at hook execution time, immediately before the controller decides whether to run the hook (for `when`) or templates the hook resource (for template bodies).
- Values reflect the Release's state at that exact moment. They are not snapshotted earlier in the sync.
- Aliases are resolved against their backing fields at evaluation time. If an aliased field is `null` (e.g., `.Release.lastSuccessfulRenderRef` on first deploy), the alias resolves to `null`.
- Expressions referencing potentially-null bindings must handle `null` explicitly using CEL's `has()` operator or null comparisons. Errors during evaluation treat the condition as `false` — the hook is skipped with a warning recorded in status.

### Validation

When `/lifecycle` is updated:

- Each `when` expression is compiled as CEL and field references are validated against the bindings available for the hook's phase.
- Each template body's go-template syntax is parsed; field references against the bindings are checked.
- Invalid expressions or out-of-phase binding references are rejected at admission time with a message identifying the offending hook and expression.

This catches typos and binding-availability mistakes before they cause runtime failures or skipped hooks.

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

Sync waves are **resource-level ordering within a single Release**. Cross-Release ordering (e.g., "deploy infra Release before app Release") is a separate concern handled at the fleet level (`ReleaseSet`) and is not addressed here.

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

#### Wave Numbering

- Wave numbers are signed integers
- Lower numbers apply earlier; higher numbers apply later
- Negative numbers are valid and commonly used for "bootstrap" resources
- Default wave (no annotation) is `0`
- Resources within the same wave apply in parallel
- Between waves: the next wave begins only after the current wave's transition condition is met (see Wave Transition Strategies below)

#### Annotations Recognized

| Annotation                     | Purpose                                                                                                                                                                      |
|--------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `convoy.kumodo.io/sync-wave`   | Native annotation. Always preferred when set.                                                                                                                                |
| `argocd.argoproj.io/sync-wave` | Recognized for ecosystem compatibility. Used when the native annotation is absent. Allows Helm charts and bundles already annotated for ArgoCD to work without modification. |
| `helm.sh/hook-weight`          | Recognized **only** for resources also marked with `helm.sh/hook` (see Helm Hook Compatibility below). Maps Helm's hook ordering into the wave system.                       |

When multiple annotations are present, precedence is: `convoy.kumodo.io/sync-wave` > `argocd.argoproj.io/sync-wave`. Helm hook annotations apply only to Helm-marked hook resources and don't conflict with the wave annotations on regular resources.

#### Helm Hook Compatibility

Helm charts use `helm.sh/hook` annotations to mark resources as hooks (e.g., `pre-install`, `post-install`, `pre-upgrade`). The sync engine recognizes these and translates them into wave behavior:

| Helm hook                              | Treated as                           | Notes                                                             |
|----------------------------------------|--------------------------------------|-------------------------------------------------------------------|
| `pre-install`, `pre-upgrade`           | Pre-sync hook (negative early waves) | Runs before regular manifests                                     |
| `post-install`, `post-upgrade`         | Post-sync hook (positive late waves) | Runs after regular manifests                                      |
| `pre-delete`, `post-delete`            | Currently ignored                    | Helm-specific; not yet integrated with Release deletion semantics |
| `test`, `test-success`, `test-failure` | Currently ignored                    | Helm test framework; not part of sync semantics                   |

Resources with `helm.sh/hook` annotations are processed as hooks; their `helm.sh/hook-weight` controls ordering within the hook phase. Resources without `helm.sh/hook` go through the regular wave pipeline.

This means standard Helm charts using hook annotations work without modification when deployed via this tool.

### Wave Transition Strategies

When a wave finishes, the sync engine decides when to start the next wave. The strategy is configured per-Release:

```yaml
spec:
  syncPolicy:
    waveStrategy:
      strategy: waitForApplied      # | waitForHealthy | waitForHealthyWithTimeout
      waveTimeout: 10m              # used only with waitForHealthyWithTimeout
```

| Strategy                    | Wave transition condition                                                                                                                                                                                                                                                                  |
|-----------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `waitForApplied`            | All resources in the wave have been accepted by the kube-apiserver. Default. Fast; appropriate when resources don't have meaningful runtime dependencies between waves.                                                                                                                    |
| `waitForHealthy`            | All resources in the wave have reached `Healthy` (or `Suspended`) per the health assessment rules. Necessary when subsequent waves depend on runtime state — e.g., CRD must be fully registered before CRs of that kind can apply, operator must be Running before its CRs are reconciled. |
| `waitForHealthyWithTimeout` | Same as `waitForHealthy`, but if `waveTimeout` elapses without the wave reaching healthy, sync proceeds to the next wave with a warning recorded in status. Fallback for slow-starting workloads where waiting indefinitely would block deploys.                                           |

#### Why Wave Strategies Matter

`waitForApplied` is fast but insufficient for ordering that depends on runtime readiness. A common failure: applying a CRD then immediately applying a CR of that kind — the CR application can fail because the API server hasn't yet finished registering the CRD's API. `waitForHealthy` is necessary for these cases.

`waitForHealthyWithTimeout` is the practical compromise for production deploys where some workloads (databases, slow init containers) might legitimately take 10+ minutes to be healthy but blocking deploys indefinitely is unacceptable.

### Status Reflection

Per-wave progress is reported in `Release.status.waves`:

```yaml
status:
  waves:
    - number: -10
      resources: 1                      # CRD
      state: Completed
      startedAt: 2026-04-29T10:00:00Z
      completedAt: 2026-04-29T10:00:15Z
    
    - number: 0
      resources: 12                     # most resources
      state: Completed
      startedAt: 2026-04-29T10:00:15Z
      completedAt: 2026-04-29T10:00:45Z
    
    - number: 10
      resources: 1                      # Certificate (CR of the CRD above)
      state: Failed
      startedAt: 2026-04-29T10:00:45Z
      failedAt: 2026-04-29T10:00:55Z
      message: "failed to apply"
  
  conditions:
    - type: Applied
      status: "Unknown"
      reason: WaveInProgress
      message: "Wave 10 of 10 in progress (1 resource)"
```

| Field         | Description                                                        |
|---------------|--------------------------------------------------------------------|
| `number`      | Wave number from manifest annotation.                              |
| `resources`   | Count of manifests in this wave.                                   |
| `state`       | `Pending`, `InProgress`, `Completed`, `Failed`, `TimedOut`.        |
| `startedAt`   | When the wave's first resource was applied.                        |
| `completedAt` | When the wave's resources are successfully synced and healthy.     |
| `failedAt`    | When the wave's resources are not successfully synced and healthy. |
| `message`     | A human-readable message describing the failure reason.            |

While a sync is in progress, the relevant `Applied` and `Healthy` conditions reflect intermediate state — `status: "Unknown"` rather than `True` or `False` — making clear that the verdict is pending wave completion.

### Resource Selection by Wave

Resources without an explicit wave annotation default to wave `0`. A Release with no annotated resources runs as a single wave (effectively no ordering), behaving identically to a non-wave deploy.

A Release with annotated resources runs through waves in order:

1. Sort waves ascending by number
2. For each wave: apply all resources in parallel; wait for transition condition
3. Move to next wave; repeat
4. After last wave: report final sync state

If any resource in a wave fails to apply, the wave fails. By default, this fails the whole sync — subsequent waves don't run, status reflects the failure. Future enhancement: configurable `continueOnWaveFailure` — not in v1.

### Interaction with `/preview` and `/diff`

The `/preview` subresource (when implemented) shows the rendered manifest set with wave annotations preserved. Tooling can group resources by wave for display.

The `/diff` subresource shows wave-by-wave diff when both sides have wave annotations, allowing operators to see "what's in wave 0 that's changing" rather than a single flat diff.

### Mutation Pipeline Interaction

Wave annotations are part of the rendered manifest set. They flow through the mutation pipeline naturally — mutations can add, modify, or remove wave annotations:

- A `goTemplate` mutation could add wave annotations based on cluster metadata
- A `jsonPatch` mutation could override a chart's default wave for a specific resource
- A `strategicMergePatch` mutation could add wave annotations to resources that don't have them

This means waves are not a separate concept layered on top of the rendering pipeline — they're a property of rendered manifests, set by whatever combination of source content and mutations produced the final output. The sync engine reads them at the end, after all rendering is complete.

### What's Not Addressed Here

Three related concepts are deliberately out of scope for sync waves and will be designed separately:

- **Pre-sync, post-sync, sync-fail hooks** — actions (typically Jobs) that run at specific points in the sync lifecycle. Helm's `pre-install`/`post-install` hooks map to the wave system as described above; the broader hook concept (with explicit hook resource definitions, hook success criteria, hook timeout) is a future enhancement.
- **Cross-Release ordering** — fleet-level deployment dependencies. Belongs to the `ReleaseSet` design.
- **Conditional waves** — running a wave only if a condition is met (e.g., "run migration wave only on first deploy, not on updates"). Future enhancement.

### Summary Table

| Aspect                | Decision                                                                              |
|-----------------------|---------------------------------------------------------------------------------------|
| Ordering scope        | Within a single Release (resource-level)                                              |
| Specification         | Annotation on individual manifests                                                    |
| Native annotation     | `convoy.kumodo.io/sync-wave`                                                          |
| Recognized aliases    | `argocd.argoproj.io/sync-wave` (compatibility)                                        |
| Helm compatibility    | `helm.sh/hook` annotations recognized; `helm.sh/hook-weight` orders within hook phase |
| Default wave          | `0`                                                                                   |
| Within-wave behavior  | Parallel application                                                                  |
| Between-wave behavior | Strategy-controlled: `waitForApplied`, `waitForHealthy`, `waitForHealthyWithTimeout`  |
| Default strategy      | `waitForApplied`                                                                      |
| Status                | Per-wave progress in `status.waves[]`                                                 |
| Failure handling      | Wave failure fails the sync (no subsequent waves)                                     |
