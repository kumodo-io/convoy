# Shard-Scoped Caches

> **Status: planned / design.** [Sharding & High Availability](04-sharding.md) splits *reconcile work* across replicas but not *memory*: today each replica's informers `LIST + WATCH` every object and cache the whole set, filtering ownership only at reconcile time. So per-replica memory is `O(total objects)` **regardless of replica count** — adding replicas does not reduce it. At 10k–50k Releases that becomes the binding constraint. This document designs **shard-scoped informers** so each replica caches only the objects for the shards it owns, making per-replica memory `≈ O(total / N)` — a knob you tune with replica count. For the concrete sizing formulas (per-replica limits, when to scale, the object-count ceiling), see [Sharding § Capacity planning](04-sharding.md#capacity-planning).

## The two partitions

The design rests on keeping two partitions strictly separate:

| Partition | Rule | Changes when… | Lives as |
|-----------|------|---------------|----------|
| **objects → shards** | `shard = hash(shardKey) % S` | only if `S` changes | a **label** on the object |
| **shards → replicas** | striped over live members | every time `N` changes | **leases** (see [04](04-sharding.md)) |

[Sharding](04-sharding.md) already builds the second partition (the lease coordinator, striped assignment, `OnChange`). This document adds the **first** partition as a label, and switches informers from "watch everything, filter later" to "watch only my shards." Scaling replicas moves *ownership and watch selectors*; it never rewrites labels.

## The shard label

A single content-derived label, `convoy.kumodo.io/shard = hash(shardKey) % S`, where the shard key is the object's existing sharding identity:

| Object | Shard key | Notes |
|--------|-----------|-------|
| `Release` | its **cluster** (`spec.clusterRef`, immutable) | so a cluster's Releases co-locate |
| `Cluster` | **self** (`namespace/name`) | |
| `GitRepository` / `HelmRegistry` / `OCIRegistry` | **self** (`namespace/name`) | |
| `ReleaseInventory` | its **Release's cluster** | == the owning Release's shard |

Because the label is a pure function of an immutable key, it is **write-once and never changes** while `S` is fixed. A useful consequence of the keys above: a `Release`, its target `Cluster`, and its `ReleaseInventory` all hash on the same cluster identity, so **they always land in the same shard** — a replica that owns a shard has a Release, the Cluster it targets, and its inventory together.

## Who stamps the label

One rule: **whoever writes an object stamps its shard label from that object's shard key.**

- **User-written** (`Release`, `Cluster`, sources) → the **aggregated apiserver's storage strategy** stamps it, in both `PrepareForCreate` and `PrepareForUpdate`, always overwriting to the computed value. Stamping on *every* write (not just create) makes the label **tamper-proof** (a user editing it is overwritten) and self-healing. The apiserver takes the same `--shards` value as the controllers.
- **Controller-written** (`ReleaseInventory`) → the **release controller** stamps it. The RI carries no `clusterRef`, and self-hashing it (`hash(RI namespace/name)`) would give a *different* shard than its Release — so it must inherit the Release's shard. The controller is the RI's sole creator and already knows the cluster, so it includes the label in the same SSA apply, under **its own field manager**. The observer's SSA writes to the RI must *not* include the label; SSA then guarantees they can't strip it.
- **Observer** stamps nothing — pure consumer.

This guarantees the critical invariant below: no object is ever born unlabeled.

## Which caches are scoped, which stay full

Shard-scope only the **high-cardinality, per-Release** caches; keep the **low-cardinality, shared** ones full:

| Informer | Mode | Why |
|----------|------|-----|
| `Release` | **shard-scoped** | `O(releases)` — the dominant memory cost |
| `ReleaseInventory` | **shard-scoped** | `O(releases)`; same shard as its Release |
| `Cluster` | **shard-scoped** | co-located with the Releases that target it, so a replica already has the clusters it needs |
| `GitRepository` / `HelmRegistry` / `OCIRegistry` | **full** | few, shared across many Releases; a Release's source may be in any shard, and fetching is demand-driven anyway |
| `GlobalHealthRuleSet` / `HealthRuleSet` | **full** | few, small; a Release may select any of them by label selector |

The memory that scales with clusters is Releases + inventories, so scoping those (plus the co-located Clusters) captures nearly all the benefit while the handful of sources and rule sets stay cheap to hold everywhere.

## Shard-scoped informers

The selector is built from **the shards this replica currently owns** (from its leases) — never from the pod name:

```
owned shards {0, 2}  →  LIST/WATCH with labelSelector "convoy.kumodo.io/shard in (0,2)"
```

The apiserver filters server-side, so the replica only ever receives (and caches) matching objects. When ownership changes, the coordinator's existing `OnChange` fires and the informers are **rebuilt** with the new selector.

**Recommended shape: one set-selector informer per kind, rebuilt on ownership change** (rather than one informer per owned shard). A single replica owning all `S` shards then holds ~3 watch streams, not `S × 3`; the cost is that any ownership change re-`LIST`s all currently-owned shards for that kind — acceptable because rebalances are rare. Mechanics: build a new filtered informer factory (`WithTweakListOptions` setting the label selector), start it, **wait for its cache to sync, then swap the listers and stop the old** (brief double-cache during overlap; never a gap in serving).

## Worked example (S = 4, striped)

Clusters `prod-eu` (`hash%4 = 2`) and `prod-us` (`hash%4 = 1`); pods `A`, `B`.

1. **Converged at N=2:** A owns `{0,2}` → watches `shard in (0,2)`; B owns `{1,3}` → watches `shard in (1,3)`.
2. **Create Release `app-x` → `prod-eu`:** apiserver stamps `shard=2`. A's watch matches → A caches & reconciles it; B never receives it. `app-x` lives in exactly one cache.
3. **Scale up (C joins), N=3:** striped → A `{0,3}`, B `{1}`, C `{2}`. Leases move; `OnChange` rebuilds selectors. A drops `shard=2` from cache (now watches `0,3`); C picks up `shard 2` and pulls `app-x` in. **`app-x`'s label is still `shard=2`** — the watch re-pointed, the object didn't move.
4. **Scale down (C leaves), N=2:** striped → A `{0,2}`, B `{1,3}`. Survivors *widen* their selectors to absorb C's shards; A re-acquires `shard 2` and `app-x` returns to A's cache. **Still no relabel.**

## Invariants, hazards, migrations

- **Unlabeled object = invisible (the #1 risk).** An object with no shard label matches *no* shard-selective watch → it is in nobody's cache and never reconciled. Mitigated by guaranteeing the label at the write path (above) **and** a one-time backfill before switching informers to selector mode.
- **Transient unwatched window (safe).** During a handoff, a shard is briefly owned by nobody (released, not yet re-acquired), so its objects are momentarily uncached — equivalent to the controller being down for that shard for a few seconds. Level-triggered reconciliation catches up on the re-`LIST`; no work is lost.
- **Rebuild window (safe).** Selector rebuilds sync the new informer before swapping, so there is never a serving gap — only brief extra memory during overlap.
- **Backfill migration (one-time).** List-and-patch all existing `Release`/`Cluster`/source/`ReleaseInventory` objects with their shard label, then flip the informers to selector mode. Order matters: label first, switch second.
- **Reshard migration (rare, deliberate).** Changing `S` changes `hash % S` for every object, so it requires re-stamping all labels + a coordinated restart — the same "changing `S` is a fleet event" cost already noted in [04](04-sharding.md). Changing the **replica count never touches `S`**, so scale up/down are always zero-relabel.

## What stays exactly as in [04](04-sharding.md)

Unchanged: the lease coordinator, member + shard leases, striped fair-share assignment, `OnChange`, the shard key, and the reconcile-time ownership gate (kept as the correctness backstop). This document changes only the **informer layer** (add labels; scope watches) — it is an optimization of *where objects are cached*, layered on the ownership model that already exists.

## Build items

1. **Shard-label stamping** — apiserver storage strategy for `Release`/`Cluster`/sources (create+update, overwrite); `--shards` flag on the apiserver; release controller stamps `ReleaseInventory` under its field manager.
2. **Backfill migration** — one-time labeler for existing objects.
3. **Shard-scoped informers** — filtered factory per kind for `Release`/`ReleaseInventory`/`Cluster`, rebuilt from the owned-shard set on `OnChange`, sync-before-swap; sources + rule sets stay full.
4. **Cutover** — enable selector mode only after backfill; keep the reconcile gate as the backstop.

## Open decisions

- **Informer shape** — single set-selector informer per kind (recommended; fewer watches, coarser re-`LIST`) vs. one informer per owned shard (more watches, granular re-`LIST`).
- **Cluster informer scope** — shard-scoped (recommended, co-located) vs. full (if the cluster count stays small enough that it doesn't matter).
- **Backfill mechanism** — a one-shot `Job` vs. a self-disabling in-process migration on first start under selector mode.
