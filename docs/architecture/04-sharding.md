# Sharding & High Availability

> **Status: planned / design.** Convoy runs single-replica today — no leader election, no sharding. This document designs how the `controller-manager` and `observer` divide work across replicas. It pairs with [Persistent Storage](03-persistent-storage.md), which assumes the ownership model defined here (who reconciles a Release, who persists its renders, who runs GC).

## One mechanism for HA and scale

Convoy does **not** have a separate leader-election path. Work is partitioned into **shards**, each guarded by its own `Lease`, and a replica does a shard's work only while it holds that shard's lease. A per-shard lease is a micro leader-election, so the same mechanism covers both goals at different replica counts:

| Replicas | Behavior |
|----------|----------|
| `N=1` | the single replica holds every shard (single active — the degenerate case) |
| `N≥2`, sized for failover | shards split across replicas; a dead replica's shards are re-acquired by survivors → **HA** |
| `N=k` for load | shards spread `k` ways → **horizontal scale** |

This works because compute is **stateless over externalized state** (etcd + the render object store). A replica that acquires a shard reconciles it straight from etcd — level-triggered, no state handoff — so rebalancing is free.

## Shard key: the target Cluster

All Releases targeting a cluster are owned by whichever replica owns that cluster's shard:

```
shard = hash(clusterNamespace + "/" + clusterName) mod S
```

`S` is a fixed shard count (e.g. 32), chosen `≥` the largest replica count you expect (`S` is the parallelism ceiling — replicas beyond `S` sit idle). The key is the Release's immutable `spec.clusterRef` identity, computable without any lookup; the Cluster controller shards on the Cluster's own identity, so both agree. (Use the Cluster **UID** instead if strict identity across delete-and-recreate matters, at the cost of a lookup.)

Why the Cluster and not the Release:

- **Cluster waves (Axis B) need a single per-cluster coordinator.** Ordering "which Release-wave applies next for this cluster" across replicas would require cross-replica coordination; co-locating a cluster's Releases keeps it a local decision.
- **Connection reuse** — one replica holds one client/connection to a target cluster, not every replica to every cluster.
- **Per-cluster GC and inventory** stay coherent on one owner.
- The existing **`releaseByCluster` informer index** means the owner of cluster `C` reconciles exactly `index[C]`.

Source kinds (`GitRepository`/`HelmRegistry`/`OCIRegistry`) are cluster-independent and light — shard them separately by `hash(sourceUID) mod S`. Their fetched artifacts are still consumed on demand by whichever replica owns the *Release* (below), so source-object sharding is only about probe/status work.

## Membership & shard leases

- **Membership** — each replica heartbeats a per-replica `Lease` (named by pod identity). The set of unexpired replica-leases is the live membership; fair share is `ceil(S / liveReplicas)`.
- **Shard leases** — `S` `Lease` objects (`convoy-cm-shard-<i>`, `convoy-obs-shard-<i>`). A replica acquires shard leases up to its fair share, renewing them on a heartbeat, and reconciles a cluster **only while it holds a valid, unexpired lease** for that cluster's shard.
- **Failover & rebalance** — a replica dies → its shard leases expire → survivors, seeing a membership change and free shards, acquire them up to the new fair share. Acquisition is atomic (optimistic concurrency on the `Lease`), so two replicas never both win a shard; the loser moves on. Convergence is eventual and needs no coordinator.
- **Member-lease GC** — a dead replica's *member* lease is never renewed, so it would otherwise linger. Once a member lease is far past its validity (an order of magnitude beyond the liveness window, so a briefly-partitioned live replica is never touched), any surviving replica reclaims it by deleting it. A live replica that had its lease deleted simply recreates it on its next tick.

## controller-manager and observer shard independently

They share the same shard *key* (cluster) but keep **independent** shard-lease sets and may run **different replica counts**. Their only coupling is the API server — the observer fires `/sync`; the controller-manager reacts; each writes its own status fields under its own field manager. Nothing requires them to co-locate or agree on assignment, so their scaling is decoupled (apply load and drift/health load grow differently).

## Correctness under handoff

Lease TTLs bound the window in which an old owner's lease has not yet expired while a new owner acquires — but most work is safe even during a brief overlap:

- **Apply** — Server-Side Apply under field manager `convoy` is idempotent (same content → no-op).
- **Status** — `Applied`/`InSync`/`Healthy`/`LifecycleSucceeded` already have single-writer field managers.

The operations that genuinely need single ownership get it from the shard lease, with idempotency as a backstop:

- **History append** (`history/<ts>-<renderRef>`) — append-on-change and idempotent per render transition, so a rare double-write collapses.
- **`/sync` fire** and **lifecycle-hook arming** — deduped on the request annotation / hook identity (already keyed).
- **GC** — partitioned by ownership (below), so it cannot race.

The rule everywhere: **act only while holding a valid lease, renew before acting.**

## Filtering to owned shards

- **Compute-time filter** (implemented) — the informer watches all objects; the reconciler early-returns for clusters it doesn't own (`if !ownsShard(cluster) { return }`). Simplest; matches Argo CD. It splits reconcile *work* but not *memory*: every replica still caches all objects, so per-replica memory is `O(total)` regardless of replica count. Ceiling: the all-objects informer cache at very large fleets (10k–50k Releases).
- **Label-selector filter** (planned) — a content-derived shard label lets each replica watch only its shards, so the cache holds only owned objects and per-replica memory becomes `O(total / N)`. This is designed in [Shard-Scoped Caches](05-shard-scoped-caches.md); it's the path to make memory scale with replicas.

## Interplay with storage

Sharding composes cleanly with [Persistent Storage](03-persistent-storage.md) precisely because ownership is exclusive:

- **Artifact cache** stays per-replica, scoped to owned work — the owner fetches (via the demand-driven coordinator) exactly the artifacts its Releases need into its own in-memory cache. No cross-replica artifact sharing.
- **Render object store** is shared and content-addressed — the owner writes `<ns>/<name>/…`; no conflict, because only one replica owns the Release.
- **GC needs no global leader** — each replica GCs only the Releases in the clusters it owns. Ownership *is* the partition.

## Deployment

- **N=1** — dev / small; single active, no failover target.
- **N=2, each sized to handle all shards** — HA: either can carry the whole load if the other dies.
- **N=k, each sized for a fair share** — horizontal scale; `k ≤ S`.

## Build items

1. Per-replica membership `Lease` (heartbeat) + live-membership read.
2. `S` shard `Lease`s per component; fair-share acquisition/renewal loop with atomic acquire.
3. `ownsShard(cluster)` gate in the Release/Cluster reconcilers (compute-time filter); source-object sharding by source UID.
4. Idempotency guards on history-append / `/sync` / hook-arming; ownership-partitioned GC.
5. *(scale-further)* label-selector informers — [Shard-Scoped Caches](05-shard-scoped-caches.md), the path to `O(total / N)` per-replica memory.

## Required follow-ups (not yet built)

- **Observability.** Sharding currently emits only logs (`"shard ownership changed" owned=[…]`, lease errors). It needs **metrics** before it can be operated confidently at scale: an owned-shard-count gauge per replica, a membership-size gauge, lease acquire/renew/GC failure counters, and a rebalance-event counter — so operators can see the partition, spot an unowned shard, and alarm on renewal failures. This is gated on convoy having an observability stack at all (none exists yet), so it is tracked as part of that broader o11y work rather than inline here.
