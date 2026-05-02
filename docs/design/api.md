# API Design

This document contains the description and examples of the API types and their usage within the project. It serves as a reference for developers working with the API, providing clear guidelines on how to interact with the system effectively.

## Cluster

Represents a target Kubernetes cluster: connection info, authentication, free-form metadata for templating, and declarative deletion policy. Supports both agentless (push) and agent (pull) connection modes.

**Subresources**

- `/status` - observed reconciliation state
- `/sync` — POST to force re-run health/discovery probe

```yaml
apiVersion: cd.yourproject.io/v1alpha1
kind: Cluster
metadata:
  name: prod-eu
  namespace: platform-fleet
  
  # Standard k8s labels — used for fleet-level selection
  # (e.g., ReleaseSets selecting clusters by env=prod, region=eu-west-1)
  labels:
    env: prod
    region: eu-west-1
    cloud: aws
  
  annotations:
    cd.yourproject.io/owner: platform-team@example.com

spec:
  # Discriminated connection mode.
  # - agentless: control plane pushes manifests via the cluster's kube-apiserver
  # - agent:     a lightweight in-cluster agent pulls desired state and applies locally
  connectionMode: agentless
  
  # connectionMode: agentless
  # Configuration for direct API server access from the control plane.
  agentless:
    # Target cluster's kube-apiserver URL.
    server: https://eks-prod-eu.example.com
    
    # CA certificate for verifying the API server's TLS cert.
    # Either inline (base64-PEM) or via reference. Use reference for rotation-friendliness.
    caBundleRef:
      configMapRef:
        name: prod-eu-ca
        key: ca.crt
    
    # Authentication method — discriminated union.
    auth:
      # type: kubeconfig | token | clientCert | execPlugin | workloadIdentity | oidcClientCredentials
      type: execPlugin
      
      # type: execPlugin — for cloud-provider auth (EKS, GKE, AKS).
      # Token rotation is delegated to the plugin (e.g., aws eks get-token).
      execPlugin:
        command: aws
        args:
          - eks
          - get-token
          - --cluster-name
          - prod-eu
          - --region
          - eu-west-1
        # Optional env vars passed to the plugin
        env:
          - name: AWS_PROFILE
            value: cd-controller
        # Optional reference to credentials the plugin needs (e.g., AWS IAM role)
        credentialsRef:
          secretRef:
            name: aws-cd-creds

      # --------------------------------
      # type: kubeconfig — full kubeconfig blob in a Secret.
      # Convenient but coarse-grained.
      # kubeconfig:
      #   secretRef: { name: prod-eu-kubeconfig, key: config }

      # --------------------------------
      # type: token — bearer token (e.g., service account token).
      # token:
      #   secretRef: { name: prod-eu-sa-token, key: token }

      # --------------------------------
      # type: clientCert — mTLS auth for clusters using cert-based auth.
      # clientCert:
      #   certRef: { secretRef: { name: prod-eu-client-cert, key: tls.crt } }
      #   keyRef:  { secretRef: { name: prod-eu-client-cert, key: tls.key } }

      # --------------------------------
      # type: workloadIdentity — controller pod has cloud-native identity
      # (IRSA on EKS, Workload Identity on GKE). No static creds needed.
      # workloadIdentity: {}

      # --------------------------------
      # oidcClientCredentials:
      # OIDC issuer URL — the controller does discovery at /.well-known/openid-configuration
      # to find the actual token endpoint. Standard OIDC.
      # issuerURL: https://auth.example.com/realms/internal

      # Client ID is non-sensitive; typically OK in spec
      # clientId: cd-controller

      # Client authentication method (RFC 6749 + OIDC core).
      # - clientSecret:    standard shared secret (most common)
      # - privateKeyJWT:   client signs a JWT assertion (more secure, used in regulated envs)
      # - tlsClientAuth:   mTLS-based client auth (rare)
      # clientAuth:
      #   type: clientSecret      # | privateKeyJWT | tlsClientAuth
      #   clientSecret:
      #   secretRef:
      #     name: oidc-client
      #     key: client-secret
      #   Alternative methods:
      #   privateKeyJWT:
      #     privateKeyRef: { secretRef: { name: oidc-client-key, key: key.pem } }
      #   tlsClientAuth:
      #     certRef: { secretRef: { name: oidc-mtls, key: tls.crt } }
      #     keyRef:  { secretRef: { name: oidc-mtls, key: tls.key } }

    # Optional scopes requested in the token. Whether these are honored depends on IdP config.
    scopes:
      - openid
      - read:charts

    # Optional audience claim. Many IdPs require this to scope tokens to specific resources.
    audience: helm-registry

    # Optional: TLS config for the IdP itself, useful for self-signed corporate IdPs.
    tls:
      caBundleRef:
        configMapRef: { name: corporate-ca, key: ca.crt }
      insecureSkipVerify: false
    
    # Connection tuning (sane defaults; override for high-volume or rate-limited clusters).
    qps: 50
    burst: 100
    timeout: 30s
  
  # connectionMode: agent
  # Configuration for agent-based pull mode.
  # Only one of {agentless, agent} should be set, matching connectionMode.
  # agent:
  #   # Token used by the in-cluster agent to register and authenticate with the control plane.
  #   registrationTokenRef:
  #     secretRef: { name: prod-eu-agent-registration, key: token }
  #   
  #   # Optional: pin expected agent identity. If set, control plane rejects connections
  #   # from agents not matching this ID. Useful for preventing agent spoofing.
  #   expectedAgentID: prod-eu-agent
  
  # Free-form metadata used in mutation templates (goTemplate / celTransform).
  # Available in templates as {{ .Cluster.metadata.<key> }}.
  # Keep types as strings (k8s label-style); templates can convert as needed.
  metadata:
    region: eu-west-1
    cloud: aws
    tier: production
    cni: cilium
    k8sVersion: "1.29"
    replicas: "5"
    ingressClass: nginx
  
  # Capabilities the cluster supports — informational, used by the renderer.
  # If omitted, controller discovers these on first successful connection.
  capabilities:
    serverSideApply: true
    # Specific API versions the renderer can rely on (Helm Capabilities.APIVersions).
    apiVersions:
      - apps/v1
      - networking.k8s.io/v1
      - autoscaling/v2
  
  # Declarative deletion behavior. See discussion for full semantics.
  deletionPolicy:
    # What happens to Release objects targeting this Cluster when this Cluster is deleted.
    # - block:  finalizer prevents deletion until no Releases reference it (default, safest)
    # - orphan: Releases kept but enter "cluster missing" error state
    # - delete: Releases are also deleted (cascading)
    releases: block
    
    # What happens to deployed workloads in the target cluster before Releases are removed.
    # Only meaningful when releases: delete and the cluster is reachable.
    # - prune: remove deployed manifests from the target cluster (graceful shutdown)
    # - leave: leave deployed workloads in place (handover scenario)
    workloads: prune
    
    # Behavior when the target cluster is unreachable during deletion.
    # - abort:                   block deletion until reachability restored or policy changed
    # - proceedWithoutPruning:   skip prune, just clean up Release objects (cluster is gone)
    onUnreachable: abort

status:
  # Observed reality from the controller.
  
  observedGeneration: 3
  
  # Connection health, updated by background probes.
  reachable: true
  lastSeen: 2026-04-29T10:00:30Z
  
  # Discovered cluster info, populated on successful connection.
  # Used by the renderer for Helm Capabilities and API-version checks.
  discoveredVersion: v1.29.4
  discoveredAPIVersions:
    - apps/v1
    - networking.k8s.io/v1
    - autoscaling/v2
    - cilium.io/v2
  
  # Number of Releases currently referencing this Cluster.
  # Useful for understanding deletion impact at a glance.
  releaseCount: 23
  
  # For agent mode — populated when connectionMode: agent.
  # agentStatus:
  #   agentVersion: v1.0.3
  #   registeredAt: 2026-03-15T08:00:00Z
  #   lastHeartbeat: 2026-04-29T10:00:30Z
  
  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:00Z
      reason: ConnectionEstablished
      message: "Cluster reachable, capabilities discovered"
    - type: ConfigValid
      status: "True"
      lastTransitionTime: 2026-04-29T09:55:00Z
    - type: Reachable
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:00Z
```

## Release

The deployment unit. Binds one `Source` + one `Cluster` + a content interpretation, with an ordered mutation pipeline. Self-describing via resolved status snapshots. Identity (source, cluster, content type) is immutable; routine config (mutations, paths, versions) is mutable.

**Subresources**

- `/status` — observed reconciliation state, resolved snapshots, last successful + last attempted records
- `/mutations` — the ordered transformation pipeline, separately addressable for field-level RBAC and audit
- `/sync` — POST to trigger reconciliation
- `/diff` — GET diff between rendered state and live cluster
- `/preview` — GET the fully rendered post-mutation manifest set (without applying)
- `/pin` — pin state (GET to read, POST to set, DELETE to clear)
- `/history` — GET paginated list of past (inputs → output) records, S3-backed

```yaml
apiVersion: cd.yourproject.io/v1alpha1
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
    cd.yourproject.io/owner: payments-team@example.com

spec:
  # Reference to a Source object. Polymorphic — kind can be GitSource,
  # HelmRegistry, or OCIRegistry.
  # IMMUTABLE after creation. To re-target, create a new Release.
  source:
    kind: GitSource
    name: payments-charts-repo
    # namespace defaults to Release's namespace if omitted; cross-namespace requires explicit ref.
  
  # How to interpret the fetched source content.
  # IMMUTABLE: content.type. MUTABLE: path, version, type-specific config (for routine ops).
  content:
    # type: helm | kustomize | manifests | jsonnet | cue
    type: helm
    
    # type: helm — for Helm charts (in Git, OCI, or HelmRegistry sources).
    helm:
      # Path within the source. For GitSource, the chart directory.
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
    #     env: prod
  
  # Reference to the target Cluster.
  # IMMUTABLE after creation.
  cluster:
    ref: prod-eu
    # namespace: platform-fleet  # if cluster is in a different namespace
  
  # Sync behavior controlling how reconciliation drives the cluster toward desired state.
  syncPolicy:
    # Auto-sync vs manual (POST /sync to trigger).
    automated: true
    
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
  
  # How many historical (rendered, applied) records to retain in S3.
  # Drives /history pagination depth and rollback range.
  retentionPolicy:
    keepHistory: 50
  
  # Per-Release deletion behavior — what happens when this specific Release is deleted.
  # Overrides Cluster.spec.deletionPolicy (which governs cluster-cascade deletion).
  deletionPolicyOverrides:
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
      sourceRef:
        kind: GitSource
        name: payments-config-repo
        path: env/prod/values.yaml
  
  # 2. Cluster-specific values (per-region overrides).
  - type: helmValues
    from:
      sourceRef:
        kind: GitSource
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
    inline:
      - op: replace
        path: /spec/template/spec/containers/0/resources/requests/memory
        value: 512Mi
      - op: add
        path: /spec/template/metadata/annotations/co.elastic.logs~1json.keys_under_root
        value: "true"
  
  # 5. Strategic merge patch from a separate source (e.g., experimental feature).
  - type: strategicMergePatch
    from:
      sourceRef:
        kind: GitSource
        name: payments-experiments-repo
        path: features/new-tracing/patch.yaml

pin:
  intent: freeze | rollback
  renderRef: sha256:abc123def456789aabbccddeeff001122334455667788990011223344556677
  pinnedAt: 2026-04-29T10:00:00Z
  pinnedBy: alice@example.com
  reason: "Production incident #4521 — payment failures after deploy"

  # Snapshot of what produced the pinned render — useful for diff against current spec/mutations.
  renderInputs:
    sourceKind: GitSource
    sourceName: payments-charts-repo
    sourceRevision: 7788990011223344556677aabbccddeeff001122
    contentType: helm
    contentPath: charts/payments-api
    mutationsHash: sha256:5566778899aabbccddeeff00112233445566778899aabbccddeeff0011223344
        
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
    currentHistoryCount: 23
  
  # Last successful render — represents what's actually deployed in the cluster.
  lastSuccessful:
    # Snapshot of resolved Source spec (not just a reference).
    # Captures URL, ref, etc. as they were at render time, even if Source object changes later.
    resolvedSource:
      kind: GitSource
      name: payments-charts-repo
      observedSourceGeneration: 7    # Source.metadata.generation at render time
      url: https://github.com/example/payments-charts
      ref: main                       # spec.ref from Source
      revision: abc123def456789aabbccddeeff001122334455   # actual resolved Git SHA
      contentType: helm
      path: charts/payments-api
    
    # Snapshot of resolved Cluster (kubeconfig server, metadata used for templating).
    resolvedCluster:
      name: prod-eu
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
        sourceRef: { kind: GitSource, name: payments-config-repo, path: env/prod/values.yaml }
        sourceRevision: 1a2b3c4d5e6f7890abcdef1234567890abcdef12
        contentHash: sha256:7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e
      - index: 1
        type: helmValues
        sourceRef: { kind: GitSource, name: payments-config-repo, path: clusters/prod-eu/values.yaml }
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
        sourceRef: { kind: GitSource, name: payments-experiments-repo, path: features/new-tracing/patch.yaml }
        sourceRevision: 2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d
        contentHash: sha256:2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d
    
    # S3 hash of the fully rendered manifest set. Lookup key for /history and /rollback.
    renderedRef: sha256:abc123def456789aabbccddeeff001122334455667788990011223344556677
    
    syncedAt: 2026-04-29T10:00:05Z
    appliedResources: 14
  
  # Most recent reconciliation attempt — may be in-progress, succeeded, or failed.
  # When phase: Succeeded and renderedRef matches lastSuccessful.renderedRef, all is well.
  # When divergent, status describes what's being attempted and any failure.
  lastAttempted:
    resolvedSource:
      kind: GitSource
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
    renderedRef: sha256:abc123def456789aabbccddeeff001122334455667788990011223344556677
    attemptedAt: 2026-04-29T10:00:00Z
    
    # phase: Rendering | Syncing | Succeeded | Failed
    phase: Succeeded
    
    # Set only when phase: Failed.
    # failure:
    #   stage: SourceResolution | MutationResolution | Rendering | Syncing
    #   mutationIndex: 1   # if failure during mutation pipeline
    #   message: "failed to fetch values file: 404 not found"

  # Pin state mirror — the *fact* of being pinned, surfaced for general visibility.
  # Full details (reason, who, etc.) live behind the /rollback subresource for RBAC.
  pin:
    intent: freeze | rollback
    renderRef: sha256:abc123def456789aabbccddeeff001122334455667788990011223344556677
    pinnedAt: 2026-04-29T10:00:00Z
  
  # Last reconciliation timestamp, even when nothing changed.
  # Distinct from lastAttempted (which only updates on actual change attempts).
  lastReconciledAt: 2026-04-29T10:03:00Z
  
  historyTruncated: false
```

### Release State Model

A deployed `Release` has three orthogonal state signals reflecting different aspects of the deployment. They are reported as separate conditions in `Release.status.conditions`, and aggregated into a composite `Ready` condition.

#### The Three Signals

##### Applied

**Question answered:** "Did the controller successfully apply the rendered manifests to the cluster?"

**Type:** Past-tense fact about the controller's action.

`Applied: True` means the most recent apply attempt succeeded — the kube-apiserver accepted all create/update/patch operations for every resource in the rendered manifest set. This is the boundary of the controller's responsibility: it's about whether the tool did its job, not whether the resulting deployment is working.

`Applied: False` means the most recent apply failed in whole or in part. Common causes: RBAC denial, admission webhook rejection, validation failure, network failure between controller and target cluster.

`Applied` does not imply that the cluster currently matches the manifests (drift may have occurred since the apply) or that workloads are functioning (apply success is independent of runtime state).

##### InSync

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

##### Healthy

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

#### Composite Condition

##### Ready

`Ready: True` requires all three:

| `Applied` | `InSync` | `Healthy` | `Ready` | Meaning                                                         |
|-----------|----------|-----------|---------|-----------------------------------------------------------------|
| `True`    | `True`   | `True`    | `True`  | Steady state — deployed and working                             |
| `True`    | `True`   | `Unknown` | `False` | Progressing — workloads coming up                               |
| `True`    | `True`   | `False`   | `False` | Deployment broken — manifests applied but workloads not working |
| `True`    | `False`  | *         | `False` | Drift — cluster has diverged from desired                       |
| `False`   | *        | *         | `False` | Apply failed — manifests not in cluster                         |

`Ready` is the signal most tooling and dashboards should watch. The three underlying conditions provide diagnostic detail when `Ready: False`.

#### Why Three Signals, Not One

Operators ask three distinct questions during deployment troubleshooting:

| Question                             | Signal    | Diagnostic path                                         |
|--------------------------------------|-----------|---------------------------------------------------------|
| Did my deploy go through?            | `Applied` | Controller logs, RBAC, admission webhooks, network      |
| Is something modifying my resources? | `InSync`  | Drift inspection, field-ownership, external controllers |
| Are my workloads working?            | `Healthy` | Pod logs, events, resource constraints, image issues    |

Different failure modes, different remediation. Collapsing into a single signal loses diagnostic clarity and forces tooling to re-derive the underlying signals to be useful.

#### Why These Names

| Name      | Why this name                                                             | Names rejected and why                                                                                                                             |
|-----------|---------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------|
| `Applied` | Plain English; describes what the controller did. No CD-tool baggage.     | `Synced` — ambiguous between "manifests pushed" (ArgoCD) and "fully converged" (intuitive reading). Causes recurring user confusion.               |
| `InSync`  | Plain English; describes the cluster's current state relative to desired. | `Reconciled` — k8s vocabulary error. Reconciliation is the controller's *process*, not an outcome. Every Release is being reconciled at all times. |
| `Healthy` | Standard term across infrastructure tooling. Unambiguous.                 | `Available`, `Working`, `Functional` — all mean roughly the same thing; `Healthy` is the most established.                                         |

#### Example Status Block

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

#### Vocabulary Mapping for Users Coming from Other Tools

| Concept                        | This tool | ArgoCD                 | Flux                                     |
|--------------------------------|-----------|------------------------|------------------------------------------|
| Manifests successfully applied | `Applied` | `Synced`               | (implicit; not a separate condition)     |
| Cluster matches desired state  | `InSync`  | (implicit in `Synced`) | `Ready` (partial — also includes health) |
| Workloads running correctly    | `Healthy` | `Healthy`              | (folded into `Ready`)                    |
| Aggregate "all good"           | `Ready`   | `Synced + Healthy`     | `Ready`                                  |

ArgoCD users will recognize `Healthy`. The split between `Applied` and `InSync` is more granular than ArgoCD's `Synced` (which conflates "applied" with "matches" — fine in practice but loses signal during drift events). Flux users will find the three-way split more granular than Flux's combined `Ready`.

#### Field Reference

These three conditions appear in `Release.status.conditions` as standard k8s `Condition` objects:

| Field                | Description                                                                                                                             |
|----------------------|-----------------------------------------------------------------------------------------------------------------------------------------|
| `type`               | One of `Applied`, `InSync`, `Healthy`, `Ready`.                                                                                         |
| `status`             | `True`, `False`, or `Unknown`. `Unknown` is meaningful for `Healthy` (progressing) and for `Applied`/`InSync` before any apply attempt. |
| `lastTransitionTime` | When the status last changed.                                                                                                           |
| `reason`             | Standardized machine-readable code (e.g., `ApplySucceeded`, `DriftDetected`, `WorkloadsDegraded`).                                      |
| `message`            | Human-readable detail. May reference specific resources for `Healthy` and `InSync`.                                                     |
| `observedGeneration` | The `metadata.generation` of the Release at the time this condition was set. Allows consumers to detect stale conditions.               |

## Sources

### GitSource

#### Purpose

`GitSource` represents a Git repository as a content source for the continuous delivery pipeline. It tracks a specified ref (branch, tag, commit, or semver constraint) at a configurable interval, fetching content for use by `Release` objects (as the source of charts, manifests, kustomizations, etc.) or as mutation content (values files, patches).

`GitSource` is the most common source kind. Authentication supports a wide range of methods including SSH keys, HTTP basic auth, tokens, GitHub App installation, and cloud workload identity.

#### Example

```yaml
apiVersion: cd.yourproject.io/v1alpha1
kind: GitSource
metadata:
  name: payments-charts-repo
  namespace: payments-team
  labels:
    team: payments

spec:
  # Repository URL. Supports https://, git://, ssh:// schemes.
  url: https://github.com/example/payments-charts
  
  # What to track — exactly one of {branch, tag, commit, semver} must be specified.
  ref:
    # Track a branch (resolves to its tip on each fetch).
    branch: main
    
    # tag: v1.2.3                # exact tag
    # commit: abc123def456...    # pinned commit (immutable resolution)
    # semver: ">=1.2.0 <2.0.0"   # for tag-based semver tracking; resolves to highest matching tag
  
  # Authentication for private repos.
  # Optional — omit for public repos.
  auth:
    # type: basic | sshKey | token | githubApp
    type: sshKey
    
    # type: sshKey — SSH private key in a Secret.
    sshKey:
      secretRef:
        name: payments-git-ssh
        key: id_rsa
      # Optional known_hosts for strict host key checking (recommended for production).
      knownHostsRef:
        configMapRef:
          name: github-known-hosts
          key: known_hosts
    
    # type: basic — HTTP basic auth (username + password/PAT).
    # basic:
    #   usernameRef: { secretRef: { name: payments-git-basic, key: username } }
    #   passwordRef: { secretRef: { name: payments-git-basic, key: password } }
    
    # type: token — bearer token (e.g., GitHub PAT, GitLab token).
    # token:
    #   secretRef: { name: payments-git-token, key: token }
    
    # type: githubApp — GitHub App authentication (JWT-based, finer-grained permissions).
    # githubApp:
    #   appID: "123456"
    #   installationID: "7890123"
    #   privateKeyRef: { secretRef: { name: github-app-key, key: key.pem } }
  
  # Polling interval for ref resolution and content fetching.
  # Lower = faster updates, higher git load. Webhooks can supplement.
  interval: 1m
  
  # Optional: path prefix filter — only consider changes under these paths
  # for triggering re-render. Reduces noise from unrelated commits.
  includePaths:
    - charts/
    - templates/
  
  # Optional: ignore changes matching these paths (regex or glob, depending on impl).
  # ignorePaths:
  #   - "**/*.md"
  #   - docs/

webhook:
  - name: github-primary
    provider: github
    secretRef:
      name: payments-github-webhook
      key: secret
    events:
      - push
      - tag-created
    filters:
      refs:
        - main
        - releases/*
      paths:
        - charts/payments/**
  - name: gitea-mirror
    provider: gitea
    secretRef:
      name: payments-gitea-webhook
      key: secret
    events:
      - push

status:
  observedGeneration: 7
  
  # Last successful fetch info — drives Release.status.resolvedSource.
  lastFetched:
    # Resolved revision (Git SHA) at fetch time.
    revision: abc123def456789aabbccddeeff001122334455
    # Tree hash for content-addressed dedup.
    treeHash: sha256:7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e
    fetchedAt: 2026-04-29T09:59:00Z
    # Resolved ref details (e.g., for semver: which tag matched).
    resolvedRef: main
  
  # Number of Releases referencing this Source.
  releaseCount: 4

  # Webhook status
  webhooks:
    - name: github-primary
      url: https://kumo-cd.example.com/webhooks/8f3a2b1c...
      lastReceived: 2026-04-29T10:00:00Z
      lastTriggered: 2026-04-29T10:00:00Z
      receivedCount: 142
      triggeredCount: 87
      rejectedCount: 3
      filteredCount: 52
      lastRejection:
        at: 2026-04-29T08:30:00Z
        reason: SignatureMismatch
        message: "HMAC signature did not match expected value"
    - name: gitea-mirror
      url: https://kumo-cd.example.com/webhooks/2a4b6c8d...
      lastReceived: 2026-04-29T09:50:00Z
      lastTriggered: 2026-04-29T09:50:00Z
      receivedCount: 18
      triggeredCount: 18
      rejectedCount: 0
      filteredCount: 0

  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: 2026-04-29T09:59:00Z
      reason: FetchSucceeded
    - type: Reachable
      status: "True"
    - type: AuthValid
      status: "True"
      reason: CredentialsAccepted
    - type: WebhookConfigured
      status: "True"
      reason: WebhookActive
```

#### Field Reference

##### `spec`

| Field                   | Type     | Required            | Default | Description                                                                                    |
|-------------------------|----------|---------------------|---------|------------------------------------------------------------------------------------------------|
| `url`                   | string   | yes                 | —       | Repository URL. Supports `https://`, `git://`, `ssh://` schemes.                               |
| `ref`                   | object   | yes                 | —       | What to track. Exactly one sub-field must be set.                                              |
| `ref.branch`            | string   | one-of              | —       | Track a branch; resolves to its tip on each fetch.                                             |
| `ref.tag`               | string   | one-of              | —       | Track a fixed tag.                                                                             |
| `ref.commit`            | string   | one-of              | —       | Pin to a specific commit SHA (immutable resolution).                                           |
| `ref.semver`            | string   | one-of              | —       | Semver constraint resolved against tags; selects highest matching.                             |
| `auth`                  | object   | no                  | —       | Authentication configuration. Omit for public repos.                                           |
| `auth.type`             | enum     | yes (if `auth` set) | —       | One of `basic`, `sshKey`, `token`, `githubApp`, `workloadIdentity`.                            |
| `auth.sshKey`           | object   | one-of              | —       | SSH private key auth (when `type: sshKey`).                                                    |
| `auth.basic`            | object   | one-of              | —       | HTTP basic auth (when `type: basic`).                                                          |
| `auth.token`            | object   | one-of              | —       | Bearer token auth (when `type: token`).                                                        |
| `auth.githubApp`        | object   | one-of              | —       | GitHub App installation auth (when `type: githubApp`).                                         |
| `auth.workloadIdentity` | object   | one-of              | —       | Cloud workload identity (when `type: workloadIdentity`); for cloud-hosted Git like CodeCommit. |
| `interval`              | duration | no                  | `1m`    | Polling interval for ref resolution and content fetching.                                      |
| `minSyncInterval`       | duration | no                  | `10s`   | Minimum interval between manual/webhook-triggered syncs. Rate-limits the `/sync` subresource.  |
| `includePaths`          | []string | no                  | —       | Path patterns; changes outside these paths don't trigger downstream reconciliation.            |
| `ignorePaths`           | []string | no                  | —       | Path patterns; changes matching these are ignored even if inside `includePaths`.               |

#### `/webhooks`

See reference at [Webhooks](#webhooks)

##### `status`

| Field                     | Type        | Description                                                                                           |
|---------------------------|-------------|-------------------------------------------------------------------------------------------------------|
| `observedGeneration`      | integer     | Generation of `spec` last reconciled.                                                                 |
| `lastFetched`             | object      | Last successful fetch result.                                                                         |
| `lastFetched.revision`    | string      | Resolved Git SHA.                                                                                     |
| `lastFetched.treeHash`    | string      | Content-addressed tree hash.                                                                          |
| `lastFetched.fetchedAt`   | timestamp   | When the fetch completed.                                                                             |
| `lastFetched.resolvedRef` | string      | Resolved ref details (e.g., for semver, which tag matched).                                           |
| `releaseCount`            | integer     | Number of `Release` objects referencing this source.                                                  |
| `webhooks`                | object      | Webhook operational state, mirrored from `/webhooks` subresource. Details under [Webhooks](#webhooks) |
| `conditions`              | []Condition | Standard k8s conditions: `Ready`, `Reachable`, `AuthValid`, `WebhookConfigured`.                      |

#### Subresources

| Subresource | Verbs               | Purpose                                                                                                                                                    |
|-------------|---------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `/status`   | controller-writes   | Observed reconciliation state.                                                                                                                             |
| `/sync`     | POST                | Force re-fetch from upstream. Body: `{ force?: bool, cascade?: bool }`. Subject to `minSyncInterval`.                                                      |
| `/webhooks` | GET / POST / DELETE | Webhook configuration (provider, secret reference, event filters, payload filters). Generated webhook URL is returned in responses and surfaced in status. |

### HelmRegistry

#### Purpose

`HelmRegistry` represents a Helm HTTP chart repository — a server providing a chart index (`index.yaml`) and chart artifacts. The controller refreshes the index periodically so that `Release` objects can pull charts by name and version constraint.

`HelmRegistry` is intended for HTTP-based Helm repositories (the original Helm registry format). For OCI-based Helm chart distribution, use `OCIRegistry` instead.

#### Example

```yaml
apiVersion: cd.yourproject.io/v1alpha1
kind: HelmRegistry
metadata:
  name: bitnami
  namespace: payments-team

spec:
  # Helm HTTP repository URL (the index.yaml location).
  url: https://charts.bitnami.com/bitnami
  
  # Authentication — optional, for private repos.
  auth:
    # type: basic | token
    type: basic
    
    basic:
      usernameRef: { secretRef: { name: bitnami-creds, key: username } }
      passwordRef: { secretRef: { name: bitnami-creds, key: password } }

    # --------------------------------
    # type: token — bearer token auth, less common for HelmRegistry but supported.
    # token:
    #   secretRef: { name: bitnami-token, key: token }

    # --------------------------------
    # oidcClientCredentials:
    # OIDC issuer URL — the controller does discovery at /.well-known/openid-configuration
    # to find the actual token endpoint. Standard OIDC.
    # issuerURL: https://auth.example.com/realms/internal

    # Client ID is non-sensitive; typically OK in spec
    # clientId: cd-controller

    # Client authentication method (RFC 6749 + OIDC core).
    # - clientSecret:    standard shared secret (most common)
    # - privateKeyJWT:   client signs a JWT assertion (more secure, used in regulated envs)
    # - tlsClientAuth:   mTLS-based client auth (rare)
    # clientAuth:
    #   type: clientSecret      # | privateKeyJWT | tlsClientAuth
    #   clientSecret:
    #   secretRef:
    #     name: oidc-client
    #     key: client-secret
    #   Alternative methods:
    #   privateKeyJWT:
    #     privateKeyRef: { secretRef: { name: oidc-client-key, key: key.pem } }
    #   tlsClientAuth:
    #     certRef: { secretRef: { name: oidc-mtls, key: tls.crt } }
    #     keyRef:  { secretRef: { name: oidc-mtls, key: tls.key } }
  
  # How often to refresh the chart index.
  # Helm registries change less frequently than Git; longer intervals are fine.
  interval: 5m
  
  # Optional: TLS config for self-signed or custom CA registries.
  # tls:
  #   caBundleRef: { configMapRef: { name: corporate-ca, key: ca.crt } }
  #   insecureSkipVerify: false

webhook:
  - name: harbor
    provider: harbor
    secretRef:
      name: harbor-webhook-secret
      key: secret
    events:
      - chart-uploaded
    filters:
      charts:
        - payments-api
        - payments-worker

status:
  observedGeneration: 2
  
  # Last successful index fetch.
  lastFetched:
    fetchedAt: 2026-04-29T09:55:00Z
    # Number of charts available in the index (informational).
    chartCount: 87
  
  releaseCount: 12

  # Webhook status
  webhooks:
    - name: harbor
      url: https://kumo-cd.example.com/webhooks/8f3a2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c
      lastReceived: 2026-04-29T10:00:00Z
      lastTriggered: 2026-04-29T10:00:00Z
      receivedCount: 142
      triggeredCount: 87
      rejectedCount: 3
      filteredCount: 52
      lastRejection:
        at: 2026-04-29T08:30:00Z
        reason: SignatureMismatch
        message: "HMAC signature did not match expected value"

  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: 2026-04-29T09:55:00Z
      reason: IndexFetched
    - type: Reachable
      status: "True"
    - type: AuthValid
      status: "True"
      reason: CredentialsAccepted
    - type: WebhookConfigured
      status: "True"
      reason: WebhookActive
```

#### Field Reference

##### `spec`

| Field                        | Type     | Required            | Default | Description                                                                                                                 |
|------------------------------|----------|---------------------|---------|-----------------------------------------------------------------------------------------------------------------------------|
| `url`                        | string   | yes                 | —       | Helm HTTP repository URL serving `index.yaml`.                                                                              |
| `auth`                       | object   | no                  | —       | Authentication configuration. Omit for public repos.                                                                        |
| `auth.type`                  | enum     | yes (if `auth` set) | —       | One of `basic`, `token`, `oidcClientCredentials`, `workloadIdentity`.                                                       |
| `auth.basic`                 | object   | one-of              | —       | HTTP basic auth (when `type: basic`).                                                                                       |
| `auth.token`                 | object   | one-of              | —       | Bearer token auth (when `type: token`).                                                                                     |
| `auth.oidcClientCredentials` | object   | one-of              | —       | OIDC client credentials flow (when `type: oidcClientCredentials`).                                                          |
| `auth.workloadIdentity`      | object   | one-of              | —       | Cloud workload identity (when `type: workloadIdentity`); for cloud-hosted Helm registries (e.g., Google Artifact Registry). |
| `interval`                   | duration | no                  | `5m`    | Polling interval for index refresh.                                                                                         |
| `minSyncInterval`            | duration | no                  | `10s`   | Minimum interval between manual/webhook-triggered syncs.                                                                    |
| `tls`                        | object   | no                  | —       | TLS config for the registry.                                                                                                |
| `tls.caBundleRef`            | object   | no                  | —       | Reference to a ConfigMap containing the CA bundle.                                                                          |
| `tls.insecureSkipVerify`     | bool     | no                  | `false` | Skip TLS verification (not recommended).                                                                                    |

#### `/webhooks`

See reference at [Webhooks](#webhooks)

##### `status`

| Field                    | Type        | Description                                                                                           |
|--------------------------|-------------|-------------------------------------------------------------------------------------------------------|
| `observedGeneration`     | integer     | Generation of `spec` last reconciled.                                                                 |
| `lastFetched`            | object      | Last successful index fetch.                                                                          |
| `lastFetched.fetchedAt`  | timestamp   | When the index was fetched.                                                                           |
| `lastFetched.chartCount` | integer     | Number of charts in the index (informational).                                                        |
| `releaseCount`           | integer     | Number of `Release` objects referencing this registry.                                                |
| `webhooks`               | object      | Webhook operational state, mirrored from `/webhooks` subresource. Details under [Webhooks](#webhooks) |
| `conditions`             | []Condition | Standard k8s conditions: `Ready`, `Reachable`, `AuthValid`, `WebhookConfigured`.                      |

#### Subresources

| Subresource | Verbs               | Purpose                                                                                                                                                                                                    |
|-------------|---------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `/status`   | controller-writes   | Observed reconciliation state.                                                                                                                                                                             |
| `/sync`     | POST                | Force re-fetch of the chart index. Subject to `minSyncInterval`.                                                                                                                                           |
| `/webhooks` | GET / POST / DELETE | Webhook configuration. Provider list adapted to Helm-registry products that support webhooks (Harbor, Artifactory, Nexus, generic). Generated webhook URL is returned in responses and surfaced in status. |

### OCIRegistry

#### Purpose

`OCIRegistry` represents an OCI registry that may hold Helm charts (OCI-packaged), raw manifest bundles, or Kustomize bundles. OCI is a generic artifact format; the registry doesn't dictate content type, so a `defaultContentType` is configured on the registry and may be overridden per-`Release`.

OCI registries are increasingly common for chart distribution (Harbor, ghcr.io, GitLab, ECR, ACR, GAR) and are the modern alternative to HTTP Helm registries.

#### Example

```yaml
apiVersion: cd.yourproject.io/v1alpha1
kind: OCIRegistry
metadata:
  name: ghcr-payments
  namespace: payments-team

spec:
  # OCI registry URL. Use the oci:// scheme.
  # Can point to a registry root or a specific repository.
  url: oci://ghcr.io/example/charts
  
  # Authentication — most OCI registries require auth.
  auth:
    # type: basic | token | dockerConfig
    type: dockerConfig
    
    # type: dockerConfig — standard kubelet-style image pull secret.
    # Most flexible; handles registry-specific auth flows.
    dockerConfig:
      secretRef:
        name: ghcr-pull-secret
        key: .dockerconfigjson

    # --------------------------------
    # type: basic
    # basic:
    #   usernameRef: { secretRef: { name: ghcr-creds, key: username } }
    #   passwordRef: { secretRef: { name: ghcr-creds, key: password } }

    # --------------------------------
    # type: token — bearer token (e.g., GitHub PAT for ghcr.io).
    # token:
    #   secretRef: { name: ghcr-token, key: token }

    # --------------------------------
    # oidcClientCredentials:
    # OIDC issuer URL — the controller does discovery at /.well-known/openid-configuration
    # to find the actual token endpoint. Standard OIDC.
    # issuerURL: https://auth.example.com/realms/internal

    # Client ID is non-sensitive; typically OK in spec
    # clientId: cd-controller

    # Client authentication method (RFC 6749 + OIDC core).
    # - clientSecret:    standard shared secret (most common)
    # - privateKeyJWT:   client signs a JWT assertion (more secure, used in regulated envs)
    # - tlsClientAuth:   mTLS-based client auth (rare)
    # clientAuth:
    #   type: clientSecret      # | privateKeyJWT | tlsClientAuth
    #   clientSecret:
    #   secretRef:
    #     name: oidc-client
    #     key: client-secret
    #   Alternative methods:
    #   privateKeyJWT:
    #     privateKeyRef: { secretRef: { name: oidc-client-key, key: key.pem } }
    #   tlsClientAuth:
    #     certRef: { secretRef: { name: oidc-mtls, key: tls.crt } }
    #     keyRef:  { secretRef: { name: oidc-mtls, key: tls.key } }
  
  # How often to check for new tags / artifact updates.
  interval: 5m
  
  # Optional: TLS config for self-signed registries.
  # tls:
  #   caBundleRef: { configMapRef: { name: corporate-ca, key: ca.crt } }
  #   insecureSkipVerify: false
  
  # Default content type for artifacts in this registry.
  # OCI is a generic artifact format — could be Helm charts, Kustomize bundles,
  # or arbitrary manifest archives.
  # - helm:      OCI-packaged Helm charts (mediaType: application/vnd.cncf.helm.config.v1+json)
  # - manifests: tarballs of raw YAML
  # - kustomize: Kustomize bundles
  # Can be overridden per-Release in spec.content.type.
  defaultContentType: helm

webhooks:
  - name: ghcr
    provider: ghcr
    secretRef:
      name: ghcr-webhook-secret
      key: secret
    events:
      - package-published
    filters:
      repositories:
        - payments-api
        - payments-worker
      tags:
        - "v*"

status:
  observedGeneration: 2
  
  # Last successful registry probe.
  lastChecked:
    checkedAt: 2026-04-29T09:55:00Z
  
  releaseCount: 8

  # Webhook status
  webhooks:
    - name: harbor
      url: https://kumo-cd.example.com/webhooks/8f3a2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c
      lastReceived: 2026-04-29T10:00:00Z
      lastTriggered: 2026-04-29T10:00:00Z
      receivedCount: 142
      triggeredCount: 87
      rejectedCount: 3
      filteredCount: 52
      lastRejection:
        at: 2026-04-29T08:30:00Z
        reason: SignatureMismatch
        message: "HMAC signature did not match expected value"

  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: 2026-04-29T09:55:00Z
      reason: RegistryReachable
    - type: Reachable
      status: "True"
    - type: AuthValid
      status: "True"
      reason: CredentialsAccepted
    - type: WebhookConfigured
      status: "True"
      reason: WebhookActive
```

#### Field Reference

##### `spec`

| Field                        | Type     | Required            | Default | Description                                                                                                |
|------------------------------|----------|---------------------|---------|------------------------------------------------------------------------------------------------------------|
| `url`                        | string   | yes                 | —       | OCI registry URL with `oci://` scheme.                                                                     |
| `auth`                       | object   | no                  | —       | Authentication configuration.                                                                              |
| `auth.type`                  | enum     | yes (if `auth` set) | —       | One of `dockerConfig`, `basic`, `token`, `oidcClientCredentials`, `workloadIdentity`.                      |
| `auth.dockerConfig`          | object   | one-of              | —       | Standard kubelet-style image pull secret (when `type: dockerConfig`).                                      |
| `auth.basic`                 | object   | one-of              | —       | HTTP basic auth (when `type: basic`).                                                                      |
| `auth.token`                 | object   | one-of              | —       | Bearer token auth (when `type: token`).                                                                    |
| `auth.oidcClientCredentials` | object   | one-of              | —       | OIDC client credentials flow (when `type: oidcClientCredentials`).                                         |
| `auth.workloadIdentity`      | object   | one-of              | —       | Cloud workload identity (when `type: workloadIdentity`); for ECR (IRSA), GAR (GCP Workload Identity), ACR. |
| `interval`                   | duration | no                  | `5m`    | Polling interval for tag/digest checks.                                                                    |
| `minSyncInterval`            | duration | no                  | `10s`   | Minimum interval between manual/webhook-triggered syncs.                                                   |
| `defaultContentType`         | enum     | no                  | `helm`  | Default interpretation of artifacts: `helm`, `manifests`, `kustomize`. Overridable per Release.            |
| `tls`                        | object   | no                  | —       | TLS config for the registry.                                                                               |
| `tls.caBundleRef`            | object   | no                  | —       | Reference to a ConfigMap containing the CA bundle.                                                         |
| `tls.insecureSkipVerify`     | bool     | no                  | `false` | Skip TLS verification (not recommended).                                                                   |

#### `/webhooks`

See reference at [Webhooks](#webhooks)

##### `status`

| Field                        | Type        | Description                                                                                           |
|------------------------------|-------------|-------------------------------------------------------------------------------------------------------|
| `observedGeneration`         | integer     | Generation of `spec` last reconciled.                                                                 |
| `lastChecked`                | object      | Last successful registry probe.                                                                       |
| `lastChecked.checkedAt`      | timestamp   | When the check completed.                                                                             |
| `releaseCount`               | integer     | Number of `Release` objects referencing this registry.                                                |
| `webhooks`                   | object      | Webhook operational state, mirrored from `/webhooks` subresource. Details under [Webhooks](#webhooks) |
| `conditions`                 | []Condition | Standard k8s conditions: `Ready`, `Reachable`, `AuthValid`, `WebhookConfigured`.                      |

#### Subresources

| Subresource | Verbs               | Purpose                                                                                                                                                                                                                                                                 |
|-------------|---------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `/status`   | controller-writes   | Observed reconciliation state.                                                                                                                                                                                                                                          |
| `/sync`     | POST                | Force re-probe the registry. Subject to `minSyncInterval`.                                                                                                                                                                                                              |
| `/webhooks` | GET / POST / DELETE | Webhook configuration. Provider list includes Harbor, ACR, Docker Hub, Quay, ghcr, GitLab, Artifactory, Nexus, generic. ECR/GAR served via optional bridge component (using `generic` provider). Generated webhook URL is returned in responses and surfaced in status. |

## Cross-references at a glance

- `Release.spec.source` → references one of `GitSource`, `HelmRegistry`, `OCIRegistry`
- `Release.spec.cluster` → references a `Cluster`
- `Release.mutations[].from.sourceRef` → typically references a `GitSource` for content (values files, patches)
- `Release.status.resolvedSource` → snapshot derived from the referenced Source's spec at render time
- `Release.status.resolvedCluster` → snapshot derived from the referenced `Cluster`'s spec at render time
- `Release.status.resolvedDeletionPolicy` → snapshot of `Cluster.spec.deletionPolicy` at render time with overrides from `Release.spec.deletionPolicyOverrides`

# AuthValid Condition

A standardized condition reflecting authentication state, present on all resources that authenticate to remote systems.

## Resources

`AuthValid` appears on:

- `GitSource`
- `HelmRegistry`
- `OCIRegistry`
- `Cluster`

The condition is always present (never omitted), even on resources without configured authentication. This makes consumer logic simpler — no need to handle a missing-condition case.

## Status Semantics

| Status    | Meaning                                                                                                     |
|-----------|-------------------------------------------------------------------------------------------------------------|
| `True`    | Most recent authentication attempt succeeded. Credentials are working.                                      |
| `False`   | Most recent authentication attempt failed for an auth-specific reason. Actionable.                          |
| `Unknown` | Authentication has not been attempted yet, OR a non-auth failure (network, timeout) prevented testing auth. |

The `Unknown` state is meaningful and should not be conflated with `False`. Distinguishing "credentials are bad" from "we couldn't test the credentials" prevents operators from chasing the wrong problem.

## Standardized Reason Codes

All resources use the same vocabulary for the `reason` field. Resource-specific detail goes in `message`.

### Success reasons (`status: True`)

| Reason                | Meaning                                                                           |
|-----------------------|-----------------------------------------------------------------------------------|
| `TokenAcquired`       | Authentication succeeded via explicit token acquisition (OIDC, workload identity) |
| `CredentialsAccepted` | Authentication succeeded with simple credentials (basic auth, SSH key, etc.)      |
| `NoAuthRequired`      | No authentication is configured; resource is public                               |

### Failure reasons (`status: False`)

| Reason                   | Meaning                                                                           |
|--------------------------|-----------------------------------------------------------------------------------|
| `InvalidCredentials`     | Credentials were rejected by the remote system (401-class)                        |
| `TokenAcquisitionFailed` | OIDC or workload identity token exchange failed                                   |
| `SecretNotFound`         | Referenced Secret or ConfigMap does not exist                                     |
| `SecretMalformed`        | Secret exists but content is invalid (bad base64, wrong format, missing keys)     |
| `PermissionDenied`       | Credentials are valid but lack required permissions (403-class)                   |
| `TokenExpired`           | Known-expired token; refresh attempt failed                                       |
| `AuthMisconfigured`      | Configuration error detected without contacting remote (e.g., invalid issuer URL) |

### Unknown reasons (`status: Unknown`)

| Reason              | Meaning                                                        |
|---------------------|----------------------------------------------------------------|
| `NotYetAttempted`   | Resource is new; auth has not yet been tested                  |
| `RemoteUnreachable` | Could not contact remote to test auth (network, DNS, firewall) |

## Relationship to Other Conditions

`AuthValid` is one of three connection-related conditions that together provide diagnostic clarity:

- **`Reachable`** — network-level reachability of the remote endpoint
- **`AuthValid`** — authentication state
- **`Ready`** — composite: resource is fully functional

### Triage flow

| Reachable | AuthValid | Action                                                                           |
|-----------|-----------|----------------------------------------------------------------------------------|
| `False`   | `Unknown` | Fix network, DNS, firewall, or remote endpoint availability                      |
| `True`    | `False`   | Fix credentials or auth configuration                                            |
| `True`    | `True`    | If `Ready: False`, look beyond auth (rate limits, IdP misconfig, content errors) |

### Aggregation

- `Ready: True` requires `AuthValid: True` (and `Reachable: True`).
- `AuthValid: False` should always cause `Ready: False`, with `Ready.reason: AuthFailed` pointing at the underlying auth issue.

## Examples

### GitSource — successful SSH key auth

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: FetchSucceeded
    - type: Reachable
      status: "True"
    - type: AuthValid
      status: "True"
      reason: CredentialsAccepted
      message: "SSH key authentication successful"
```

### HelmRegistry — OIDC client credentials failure

```yaml
status:
  conditions:
    - type: Ready
      status: "False"
      reason: AuthFailed
    - type: Reachable
      status: "True"
    - type: AuthValid
      status: "False"
      reason: TokenAcquisitionFailed
      message: "Failed to acquire OIDC token from https://auth.example.com/realms/internal: invalid_client (client authentication failed)"
```

### OCIRegistry — public registry, no auth

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: RegistryReachable
    - type: Reachable
      status: "True"
    - type: AuthValid
      status: "True"
      reason: NoAuthRequired
      message: "Registry is public; no authentication configured"
```

### Cluster — workload identity success

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: ConnectionEstablished
    - type: Reachable
      status: "True"
    - type: AuthValid
      status: "True"
      reason: TokenAcquired
      message: "EKS GetToken succeeded via IRSA role arn:aws:iam::123456789012:role/cd-eks-deployer"
```

### GitSource — secret missing

```yaml
status:
  conditions:
    - type: Ready
      status: "False"
      reason: AuthFailed
    - type: Reachable
      status: "Unknown"
      reason: AuthRequired
    - type: AuthValid
      status: "False"
      reason: SecretNotFound
      message: "Secret 'payments-git-ssh' not found in namespace 'payments-team'"
```

## Implementation Notes

### Controller code

Factor a shared auth-result type so all controllers report consistently:

```go
type AuthResult struct {
    Status  ConditionStatus  // True / False / Unknown
    Reason  string           // from the standardized vocabulary above
    Message string           // free-form detail for humans
}
```

Each resource's auth handler returns `AuthResult`; the reconciler maps it to a condition update uniformly. This prevents drift in reason codes over time — adding a new code happens in one place, every controller stays consistent.

### Token caching

For auth methods that produce short-lived tokens (OIDC, workload identity):

- Cache tokens in memory; refresh proactively before expiry
- Never persist tokens to etcd or S3
- Token refresh failures should update `AuthValid` to `False` with `reason: TokenExpired`
- Use exponential backoff with jitter for refresh retries to avoid thundering-herd against the IdP

### Avoiding misclassification

A common mistake is reporting any failure as `AuthValid: False`. Be precise:

- Network errors → `Reachable: False`, `AuthValid: Unknown`
- 401 from remote → `AuthValid: False, reason: InvalidCredentials`
- 403 from remote → `AuthValid: False, reason: PermissionDenied`
- Local config errors (bad issuer URL, missing secret) → `AuthValid: False` with the appropriate config reason; `Reachable` may still be `Unknown` if the remote was never contacted

# Source Webhooks (`/webhooks` subresource)

## Purpose

The `/webhooks` subresource manages webhook configurations for a Source, enabling push-based notifications from upstream systems (Git providers, registries) that trigger source refreshes. Multiple webhooks can be configured per Source, supporting scenarios like multi-mirror Git setups, provider migrations, and chained CI/CD pipelines.

The subresource is available on all Source kinds: `GitSource`, `HelmRegistry`, and `OCIRegistry`. Schema and behavior are identical across kinds; only the `provider` enum and applicable filter fields differ.

Webhook configuration lives behind a subresource (rather than inline in `spec`) for several reasons:
- Different RBAC than the rest of the Source (platform team manages source definitions; app teams configure their webhook integrations)
- Different lifecycle (webhooks added/removed independently of Source changes)
- Different sensitivity (webhook secrets are credentials)
- Audit clarity (webhook config changes distinguishable from Source spec changes)

## Schema

The subresource holds a list of named webhook entries under `items`.

```yaml
# GET /apis/cd.kumodo.io/v1alpha1/namespaces/payments-team/gitsources/payments-charts-repo/webhooks
items:
  - name: github-primary
    provider: github
    secretRef:
      name: payments-github-webhook
      key: secret
    events:
      - push
      - tag-created
    filters:
      refs:
        - main
        - releases/*
      paths:
        - charts/payments/**
    url: https://kumo-cd.example.com/webhooks/8f3a2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c
  
  - name: gitea-mirror
    provider: gitea
    secretRef:
      name: payments-gitea-webhook
      key: secret
    events:
      - push
    url: https://kumo-cd.example.com/webhooks/2a4b6c8d-1e3f-5a7b-9c0d-2e4f6a8b0c1d
```

Each entry's `url` is server-generated and returned by GET/POST; it is not accepted as input. Users copy the URL into the upstream provider's webhook configuration.

## Verbs

| Path               | Verb   | Purpose                                                                 |
|--------------------|--------|-------------------------------------------------------------------------|
| `/webhooks`        | GET    | List all webhook entries for the Source.                                |
| `/webhooks`        | POST   | Create a new webhook entry. Server may auto-generate a name if omitted. |
| `/webhooks`        | DELETE | Remove all webhook entries (rare but convenient).                       |
| `/webhooks/<name>` | GET    | Read a specific webhook entry.                                          |
| `/webhooks/<name>` | PUT    | Update a specific webhook entry.                                        |
| `/webhooks/<name>` | DELETE | Remove a specific webhook entry.                                        |

## Common Fields

These fields apply to webhook entries on all Source kinds.

| Field            | Type     | Required             | Default                             | Description                                                                                                                                         |
|------------------|----------|----------------------|-------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------|
| `name`           | string   | yes                  | server-generated on POST if omitted | Unique within the Source. Used for addressability via `/webhooks/<name>`. Stable across edits; does not change once assigned.                       |
| `provider`       | enum     | yes                  | —                                   | Provider type. Drives signature verification scheme and payload parser. Per-kind enum values listed below.                                          |
| `secretRef`      | object   | yes (most providers) | —                                   | Reference to a Secret containing the shared secret used for HMAC verification (or bearer token for `generic` provider).                             |
| `secretRef.name` | string   | yes                  | —                                   | Secret name in the same namespace as the Source.                                                                                                    |
| `secretRef.key`  | string   | yes                  | —                                   | Key within the Secret holding the secret value.                                                                                                     |
| `events`         | []string | no                   | provider-specific                   | Provider event types that trigger sync. Defaults vary per provider.                                                                                 |
| `filters`        | object   | no                   | —                                   | Optional payload-based filtering. Specific fields vary by Source kind.                                                                              |
| `url`            | string   | server-generated     | —                                   | Endpoint URL for the upstream provider to call. Returned in GET/POST responses; mirrored in `status.webhooks[].url`. Each entry has a distinct URL. |

## Per-Kind Variations

### `GitSource` webhooks

**Provider enum values:**

| Value             | Description                                                                              |
|-------------------|------------------------------------------------------------------------------------------|
| `github`          | GitHub / GitHub Enterprise. HMAC-SHA256 via `X-Hub-Signature-256`.                       |
| `gitlab`          | GitLab. Token via `X-Gitlab-Token` (HMAC opt-in).                                        |
| `bitbucket`       | Bitbucket Cloud. HMAC-SHA256 via `X-Hub-Signature`.                                      |
| `bitbucketServer` | Bitbucket Data Center / Server. Provider-specific headers.                               |
| `gitea`           | Gitea / Forgejo. HMAC via `X-Gitea-Signature`.                                           |
| `azure`           | Azure DevOps Service Hooks.                                                              |
| `generic`         | Bearer token via `Authorization: Bearer <secret>`, or no auth if `secretRef` is omitted. |

**Filter fields:**

| Field           | Type     | Description                                                                                                                                   |
|-----------------|----------|-----------------------------------------------------------------------------------------------------------------------------------------------|
| `filters.refs`  | []string | Only trigger sync when the push affects matching refs. Supports glob patterns (e.g., `releases/*`).                                           |
| `filters.paths` | []string | Only trigger sync when changed paths intersect these patterns. Composes with `spec.includePaths` and `spec.ignorePaths` on the Source itself. |

**Default events:** `["push"]`

### `HelmRegistry` webhooks

**Provider enum values:**

| Value         | Description                                           |
|---------------|-------------------------------------------------------|
| `harbor`      | Harbor with Helm support. CloudEvents-style payloads. |
| `artifactory` | JFrog Artifactory. HMAC-based auth.                   |
| `nexus`       | Sonatype Nexus 3 with Helm support.                   |
| `generic`     | Any system that can POST with bearer token or HMAC.   |

Public Helm registries (e.g., `charts.bitnami.com`) typically don't support webhooks; this subresource is primarily for self-hosted enterprise registries.

**Filter fields:**

| Field            | Type     | Description                                             |
|------------------|----------|---------------------------------------------------------|
| `filters.charts` | []string | Only trigger sync when matching chart names are pushed. |

### `OCIRegistry` webhooks

**Provider enum values:**

| Value         | Description                                                                                            |
|---------------|--------------------------------------------------------------------------------------------------------|
| `harbor`      | Harbor. CloudEvents-style payloads.                                                                    |
| `acr`         | Azure Container Registry. HTTP webhook support.                                                        |
| `dockerHub`   | Docker Hub. Per-repository webhook config.                                                             |
| `quay`        | Quay.io. Notification system supporting webhooks.                                                      |
| `ghcr`        | GitHub Container Registry (via GitHub `package` events).                                               |
| `gitlab`      | GitLab Container Registry (via GitLab generic webhook system).                                         |
| `artifactory` | JFrog Artifactory with OCI support.                                                                    |
| `nexus`       | Sonatype Nexus 3 with Docker/OCI support.                                                              |
| `generic`     | Any system that can POST. ECR and GAR are typically served via a bridge component using this provider. |

**Filter fields:**

| Field                  | Type     | Description                                                    |
|------------------------|----------|----------------------------------------------------------------|
| `filters.repositories` | []string | Only trigger sync when matching repository names are affected. |
| `filters.tags`         | []string | Only trigger sync for matching tag patterns (supports globs).  |

**Note on ECR / GAR:** AWS ECR uses EventBridge; Google Artifact Registry uses Pub/Sub. Neither sends direct HTTP webhooks. A small bridge component (Lambda or Cloud Function) consumes events from these systems and POSTs to the `generic` webhook endpoint. Bridge implementation is provided as an optional, separately-installable helper.

## Status Reflection

Each Source's `status.webhooks` mirrors operational state per webhook entry. Sensitive config (secret reference, filters) stays behind the subresource; status surfaces only non-sensitive operational facts.

```yaml
status:
  webhooks:
    - name: github-primary
      url: https://kumo-cd.example.com/webhooks/8f3a2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c
      lastReceived: 2026-04-29T10:00:00Z
      lastTriggered: 2026-04-29T10:00:00Z
      receivedCount: 142
      triggeredCount: 87
      rejectedCount: 3
      filteredCount: 52
      lastRejection:
        at: 2026-04-29T08:30:00Z
        reason: SignatureMismatch
        message: "HMAC signature did not match expected value"
    
    - name: gitea-mirror
      url: https://kumo-cd.example.com/webhooks/2a4b6c8d-1e3f-5a7b-9c0d-2e4f6a8b0c1d
      lastReceived: 2026-04-29T09:50:00Z
      lastTriggered: 2026-04-29T09:50:00Z
      receivedCount: 18
      triggeredCount: 18
      rejectedCount: 0
      filteredCount: 0
  
  conditions:
    - type: WebhookConfigured
      status: "True"
      lastTransitionTime: 2026-04-29T08:00:00Z
      reason: WebhooksActive
      message: "2 webhooks configured; all receiving events"
```

The `WebhookConfigured` condition is `True` if at least one webhook is configured. Per-entry health is in `status.webhooks[].rejectedCount` and `lastRejection`; aggregate failures across entries trigger condition transitions to `False` with appropriate reasons.

## Status Fields per Entry

| Field                   | Type      | Description                                                                                      |
|-------------------------|-----------|--------------------------------------------------------------------------------------------------|
| `name`                  | string    | Matches the entry's name in the subresource.                                                     |
| `url`                   | string    | Endpoint URL (mirrored from subresource for visibility).                                         |
| `lastReceived`          | timestamp | Most recent webhook delivery received (regardless of outcome).                                   |
| `lastTriggered`         | timestamp | Most recent delivery that passed verification and filters and triggered sync.                    |
| `receivedCount`         | integer   | Total deliveries received since last status reset.                                               |
| `triggeredCount`        | integer   | Deliveries that resulted in a sync.                                                              |
| `rejectedCount`         | integer   | Deliveries rejected (signature failures, malformed payloads).                                    |
| `filteredCount`         | integer   | Deliveries that passed verification but were filtered out (events/refs/paths/tags didn't match). |
| `lastRejection`         | object    | Details of the most recent rejection, if any.                                                    |
| `lastRejection.at`      | timestamp | When the rejection occurred.                                                                     |
| `lastRejection.reason`  | string    | Standardized reason: `SignatureMismatch`, `MalformedPayload`, `UnknownEvent`, `SecretNotFound`.  |
| `lastRejection.message` | string    | Human-readable detail.                                                                           |

## RBAC

Standard Kubernetes RBAC operates at the subresource level — `gitsources/webhooks`, `helmregistries/webhooks`, `ociregistries/webhooks` — not at individual entry granularity. Granting webhook-edit permissions on a Source allows managing all webhook entries on that Source.

```yaml
# Platform team — manages source definitions but not webhook config
- apiGroups: ["cd.kumodo.io"]
  resources: ["gitsources", "helmregistries", "ociregistries"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

# App team — manages webhook integrations only
- apiGroups: ["cd.kumodo.io"]
  resources:
    - "gitsources/webhooks"
    - "helmregistries/webhooks"
    - "ociregistries/webhooks"
  verbs: ["get", "create", "update", "delete"]
```

For per-webhook access control (rare), users can:
- Use separate Sources (recommended for cleanly isolated webhooks).
- Apply `ValidatingAdmissionPolicy` with CEL expressions on the subresource (Kubernetes 1.30+).

## Defaults and Auto-Generation

To avoid penalizing the common case of one webhook per Source:

- POST without a `name` auto-generates a name based on `provider` (e.g., `github-1`, `harbor-1`). Subsequent POSTs of the same provider increment the suffix.
- Single-webhook users can ignore names entirely and treat the subresource as effectively singular by addressing the auto-generated entry.

## URL Generation

Webhook URLs use opaque, server-generated identifiers: `https://<control-plane-host>/webhooks/<webhook-uuid>`

The UUID is generated when the webhook entry is created and remains stable for the lifetime of that entry. Deletion and recreation of the same-named entry produces a new UUID and new URL — users must reconfigure the upstream provider with the new URL.

This pattern is preferred over Source-identifying URLs (e.g., `/webhooks/sources/<namespace>/<source-name>/<webhook-name>`) because:
- It avoids leaking namespace/Source naming structure to the public webhook endpoint.
- Webhook URL rotation (regenerating the UUID) doesn't require changing Source or webhook names.
- Each entry has a distinct URL, mapping cleanly to upstream provider configurations.
