**Health Assessment — Execution Architecture**

<!-- TOC -->
  * [Component Model](#component-model)
  * [Communication Direction: PULL with Sync-Check](#communication-direction-pull-with-sync-check)
    * [Sync-Check Optimization](#sync-check-optimization)
    * [PUSH Not Supported in v1](#push-not-supported-in-v1)
  * [Content Delivery: Control Plane Proxies S3](#content-delivery-control-plane-proxies-s3)
    * [Why Not S3-Direct](#why-not-s3-direct)
    * [Streaming End-to-End](#streaming-end-to-end)
  * [YAML Stream Parsing](#yaml-stream-parsing)
  * [Memory Bounds](#memory-bounds)
    * [Executor Configuration](#executor-configuration)
    * [Control Plane Configuration](#control-plane-configuration)
  * [Sharding](#sharding)
    * [Central Executor (Agentless Clusters)](#central-executor-agentless-clusters)
    * [Agent Mode](#agent-mode)
  * [Mixed Fleets](#mixed-fleets)
  * [What's Deferred to Phase 4](#whats-deferred-to-phase-4)
  * [Summary Table](#summary-table)
<!-- TOC -->

This document covers where health assessment runs, how agents communicate with the control plane, and how memory is bounded across the system. It complements the Health Assessment design document, which describes *what* health assessment does (rules, states, aggregation, status shape).

> **Status: partially built.** The **observer** and its **health-assessor loop** exist today in control-plane **PUSH** mode (`convoy-observer`): it evaluates the `GlobalHealthRuleSet`/`HealthRuleSet` rules a Release selects against live resources, records per-resource state on the `ReleaseInventory`, and serves results via the `/health` subresource. What remains **planned** is everything about **agent-mode (PULL)** delivery: the agent-manager, the in-cluster agent, content proxying, and the memory-bounding/sharding mechanisms. Read the agent-mode components and protocols below as a design proposal; the push-mode observer and health evaluation are real.
>
> **Terminology note (this doc's "executor" vs. the current component model).** This document predates the component model in [00-overview](00-overview.md#target-evolution-planned). Rendering is always central (controller-manager); only *apply* and *observe* move by `connectionMode`. There is no single "executor" — the responsibilities the "executor" describes below map onto distinct components:
> - **Deploy** — controller-manager (push) or the in-cluster **agent** (pull).
> - **Observe** — the **observer** binary (push), which runs a **drift-detector loop** and a **health-assessor loop** *in parallel* (distinct responsibilities sharing a live-state reader, not one folded into the other), or the in-cluster **agent** (pull), which colocates the same two loops plus the deployer.
>
> So where this document says "executor," read it as: in pull mode, the **agent** (deploy + observe); in push mode, the **observer** (observe only — deploy is the controller-manager). The two observe loops are shared code between the observer and the agent.

## Component Model

_Status: the observer (push mode) with its drift + health-assessor loops is built. The agent-manager and in-cluster agent (PULL mode) do not exist yet._

The design has two roles that move by `Cluster.spec.connectionMode` — **deploy** (apply desired state) and **observe** (drift detection + health assessment) — while rendering stays central in the controller-manager.

**Push clusters (`inCluster`/`agentless`):** the control plane drives everything. The controller-manager renders, stores, and applies; a central **observer** binary reads each target cluster's live state and runs the drift + health loops for it.

**Pull clusters (`agent`):** an **agent** binary runs inside the target cluster, using its in-cluster ServiceAccount. It pulls the centrally-rendered state (served by the agent-manager), inventories it locally, applies it, and runs the **same** drift + health loops against the local cluster — reporting results back through the agent-manager. This suits clusters the control plane cannot reach.

The **observe** loops are identical code in both topologies; only their deployment and how they reach the cluster differ (central + remote credentials vs. in-cluster + ServiceAccount). Each loop is a distinct responsibility:

- **Drift-detector loop** — compares live managed-resource state against the desired render; on drift, requests a reconcile so the applier re-applies.
- **Health-assessor loop** — reads the managed resources recorded on the `ReleaseInventory`, evaluates each against the CEL rules of the `GlobalHealthRuleSet`/`HealthRuleSet` sets the Release selects, records per-resource state back on the `ReleaseInventory`, and aggregates the verdict into the Release's `Healthy` condition + `status.healthSummary`.

Colocating the two loops lets them share an in-process live-state cache and the target-cluster connection; keeping them logically separate preserves their distinct inputs (drift needs the desired render; health needs only live state + rules) and cadence.

## Communication Direction: PULL with Sync-Check

_Status: planned (agent mode). Today's PUSH path is the reverse — the control plane connects out to agentless clusters and applies directly. The PULL protocol below applies only to the future `agent` connection mode; push-mode (`inCluster`/`agentless`) clusters are driven by the control plane connecting out (controller-manager applies, observer reads live state)._

> **Scope note.** The sections from here on (protocol, content delivery, memory bounds, sharding) are the **original executor-centric proposal** and have not been re-derived against the current component split. Read the legacy term **"executor"** as: the in-cluster **agent** in pull mode; and, for the centrally-deployed multi-cluster figures (memory, sharding), the control-plane workers — **observer** (observe) and **controller-manager** (deploy) — each carrying the subset of these bounds relevant to its role. The mechanisms carry over; the per-component bounds and sharding need a dedicated re-derivation.

Communication is **agent-initiated**: the in-cluster agent makes outbound HTTPS requests to the control plane; the control plane never opens connections to it.

This direction was chosen because:

- It works in every network topology — NAT, firewalls, air-gapped target clusters
- Agents in regulated environments often have a single allowed egress; PULL fits this constraint
- Control plane doesn't need to track agent endpoints or maintain per-agent connections
- Scaling is request-driven; adding agents adds incoming requests to handle

### Sync-Check Optimization

Most polling cycles will find no changes. The sync request includes content hashes, allowing the control plane to short-circuit when nothing has changed:

```
POST /api/v1/agents/<cluster-id>/sync
Body: {
"manifestSha": "sha256:abc...",
"rulesSha": "sha256:def..."
}
```

**Response when up-to-date:**

```
204 No Content
{ "nextPollAfter": "30s" }
```

**Response when changes available:**

```
200 OK
Content-Type: application/x-yaml-stream
Transfer-Encoding: chunked
X-Manifest-Sha: sha256:def...
X-Rules-Sha: sha256:ghi...
<streamed manifest set + rules>
```

Routine polling is essentially free (small request, 204 response). Bandwidth and CPU costs scale with actual change frequency, not polling frequency.

### PUSH Not Supported in v1

Long-lived bidirectional streams (e.g., gRPC server streaming) are a possible v1.x evolution if PULL polling latency becomes a problem. Direct PUSH from control plane to agent endpoints is not planned — it conflicts with the network topologies the tool needs to support.

## Content Delivery: Control Plane Proxies S3

_Status: planned. Depends on both the S3 backend and agent mode, neither of which exists today._

The control plane reads rendered manifests from its S3 backend and streams them to agents over the same authenticated sync channel. **Agents never access S3 directly.**

### Why Not S3-Direct

Agents fetching from S3 directly was considered and rejected. The reasons:

- **Cross-cloud authentication is fragile.** Control plane in AWS with agents in Azure or GCP requires cross-cloud federation, long-lived static credentials, or pre-signed URLs with short TTLs. All options trade off security or operational simplicity.
- **Air-gapped target clusters often forbid additional egress.** Network policy may permit only the control-plane endpoint as outbound destination. S3-direct doesn't work.
- **Egress costs.** Each agent pulling independently from S3 multiplies cloud bandwidth charges.
- **Audit trail is split.** Tracking "what was sent to which agent" requires correlating S3 access logs with control-plane logs.

The control plane has appropriate S3 credentials (typically workload identity in its own cloud); agents don't need any.

### Streaming End-to-End

Content flows from S3 through the control plane to the agent without any side holding the full manifest set in memory. All three hops use chunked streaming:

1. Control plane reads from S3 in range chunks
2. Control plane streams chunks directly to the agent's HTTP response (no intermediate buffering of the full payload)
3. Agent reads the response body in chunks, parsing YAML documents one at a time up to a configured batch size

This makes manifest sets of arbitrary size flow through the system with bounded memory on every component.

## YAML Stream Parsing

_Status: planned (part of the agent-mode content-delivery path)._

Manifest streams are parsed as multi-document YAML using a proper streaming decoder, not by splitting text on `---`. Splitting on `---` fails for:

- `---` appearing inside string values (e.g., multi-line strings in ConfigMaps)
- Single-document streams without `---`
- Documents containing comments with `---`

The agent uses Go's `yaml.v3` decoder loop (or equivalent) to yield each document discretely. Documents are accumulated into a batch up to the configured size, processed, and the next batch begins.

## Memory Bounds

_Status: planned. The config keys below are proposed, not implemented._

Both sides of the connection have configurable bounds that make memory consumption predictable.

### Executor Configuration

```yaml
performance:
  # Maximum clusters processed concurrently in this executor process.
  # Agentless mode: the central executor handles up to this many clusters in parallel.
  # Agent mode: should always be 1 (the local cluster).
  maxConcurrentClusters: 10
  
  # Maximum bytes of manifest content held in memory per cluster being processed.
  # Manifests are streamed in; held content is bounded by this batch size.
  manifestBatchSize: 10Mi
  
  # Maximum number of resources watched per cluster for health assessment.
  # Beyond this limit, additional resources fall back to periodic polling.
  maxWatchedResourcesPerCluster: 5000
  
  # Maximum bytes for the live-state cache per cluster (resource snapshots used
  # in health evaluation).
  liveStateCacheBytes: 50Mi
  
  # Polling interval for live state when watches are not used.
  pollInterval: 30s
  
  # Maximum parallel resources evaluated within a single cluster.
  # Prevents a single large cluster from saturating goroutines.
  maxConcurrentResourcesPerCluster: 50
```

**Total memory bound (executor):**

`maxConcurrentClusters * (manifestBatchSize + liveStateCacheBytes) + overhead`

For the example values: `10 * (10MiB + 50MiB) ≈ 600MiB` plus goroutine stacks and runtime overhead. Predictable.

### Control Plane Configuration

```yaml
performance:
  # Maximum concurrent agent sync requests served at once.
  maxConcurrentSyncRequests: 100
  
  # Per-request streaming buffer (held in memory while streaming S3 → agent).
  streamingBufferSize: 1Mi
```

**Total memory bound (control plane sync handling):**

`maxConcurrentSyncRequests * streamingBufferSize + overhead`

For the example values: `100 * 1MiB = 100MiB` plus other control-plane work.

These configs are independent — the executor and control plane each manage their own memory consumption. Together they bound system-wide memory regardless of fleet size and manifest size.

## Sharding

_Status: planned. Today all controllers run as a single `convoy-controller-manager` with no sharding._

### Central Executor (Agentless Clusters)

Cluster-level sharding via consistent hashing on `Cluster.metadata.uid`. Each shard processes the clusters whose UIDs fall in its hash range.

```yaml
shard:
  index: 2
  totalShards: 4
```

Deploy as a Kubernetes `Deployment` with N replicas; each replica reads its shard config from environment or labels. On scale-up or scale-down, only a small fraction of clusters are reassigned.

If a shard pod fails, Kubernetes replaces it. The clusters owned by that shard are not processed during the gap. For v1 this is acceptable; cross-shard takeover can be added later if needed.

### Agent Mode

No sharding required. Each agent processes its single local cluster. Adding clusters to the fleet means deploying more agents.

## Mixed Fleets

A fleet can have a mix of `agentless` and `agent` Clusters. The control plane dispatches based on each `Cluster.spec.connectionMode`:

- For `agentless` clusters: included in the central executor's scope
- For `agent` clusters: serves sync requests from the in-cluster agents

No coordination needed between the two modes; they operate independently and produce the same `Healthy`-condition + `status.healthSummary` updates (with per-resource state on the `ReleaseInventory`).

## What's Deferred to Phase 4

The agent protocol design itself — full message formats, registration flow, identity verification, reconnect semantics, error handling — is a Phase 4 cross-cutting concern. The high-level architecture above settles the operational shape; the protocol's wire format and detailed semantics are designed separately.

`Cluster.spec.agent.registrationTokenRef` already exists in the `Cluster` schema as the v1alpha1 placeholder for agent identity. Token-based registration is sufficient to ship; mTLS, OIDC service-account auth, and other identity schemes can be added through additive schema changes without breaking v1alpha1.

## Summary Table

_Every row below describes the **target** execution architecture and is **not yet built** — today's delivery is control-plane PUSH via the Release controller (see the Status note at the top)._

| Aspect                     | Decision                                                                          |
|----------------------------|-----------------------------------------------------------------------------------|
| Component                  | Single binary (executor); deploys centrally or as in-cluster agent                |
| Internal structure         | Two parallel loops (deploy, health) sharing in-process state                      |
| Communication direction    | Agent-initiated PULL with sync-check optimization                                 |
| Sync-check                 | Hash-based; 204 when up-to-date, full content stream when changes available       |
| Content delivery           | Control plane proxies S3 to agents; agents never access S3 directly               |
| Streaming                  | End-to-end chunked; bounded memory on all three hops (S3 → control plane → agent) |
| YAML parsing               | Streaming multi-document decoder, batched by size limit                           |
| Executor memory bound      | `maxConcurrentClusters * (manifestBatchSize + liveStateCacheBytes)`               |
| Control plane memory bound | `maxConcurrentSyncRequests * streamingBufferSize`                                 |
| Sharding (agentless)       | Cluster-level via consistent hashing on Cluster UID                               |
| Sharding (agent mode)      | Inherent — one agent per cluster                                                  |
| Cross-cloud support        | Trivial — agents only need HTTPS reachability to control plane                    |
| Air-gapped support         | Yes — single egress to control plane suffices                                     |
