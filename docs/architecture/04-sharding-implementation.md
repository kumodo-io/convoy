# Sharding — Implementation Plan

> Companion to [Sharding & High Availability](04-sharding.md) (the design). This document is the concrete build plan: package layout, the lease algorithm, integration points in the existing controllers, configuration, RBAC, testing, and phasing. Locked sub-decisions: **striped fair-share** assignment, **member leases** for membership, and **source controllers are sharded** too.

## Design in one line

A per-component **`Ownership`** (which of `S` fixed shards this replica owns right now, maintained by a lease loop) plus a pure **`Sharder`** (`identity → shard`), consulted as a **gate** at the top of each reconcile. Everything else is wiring.

## Grounding in the current code

- Both managers (`internal/controllermanager/manager.go`, `internal/observermanager/manager.go`) build a shared informer factory, construct controllers, and run them via `errGroup.Go(ctrl.Run(ctx, workers))`.
- Controllers use one harness, `internal/controller/base/controller.go`: a rate-limited workqueue with `ReconcileFunc(ctx, key)`, `WatchInformer`, `enqueue`, `EnqueueAfter`, `Run`.
- Every reconcile already resolves the target cluster from the key — e.g. the release controller at `controller.go:493`: `clusterNamespace := orDefault(rel.Spec.ClusterRef.Namespace, rel.Namespace)`. That identity is the shard key.
- The `releaseByCluster` informer index already groups Releases by cluster.

These give small, clean insertion points: a gate inside each `ReconcileFunc`, an optional enqueue filter in the harness, and one `Coordinator` per manager.

## Package: `internal/sharding`

Standalone, no dependency on the controllers.

```go
// Sharder maps any work identity into a fixed shard space [0,S).
type Sharder struct { S int }
func (s Sharder) Shard(ns, name string) int   // fnv32a(ns+"/"+name) % S — pure, stable

// Ownership answers "do I own this shard?" — read by reconcilers on the hot path.
type Ownership interface {
    Owns(shard int) bool
    OwnedShards() []int          // for resync fan-out
    OnChange(func())             // fired when the owned set changes (rebalance trigger)
}

// Coordinator runs the membership + shard-lease loop and implements Ownership.
type Coordinator struct { /* leaseClient, component, replicaID, S, durations */ }
func (c *Coordinator) Run(ctx context.Context) error
```

### Lease model

`coordination.k8s.io/v1` `Lease` objects directly — **not** client-go's `leaderelection` runner (that is single-leader semantics).

- **Member lease** — one per replica, `convoy-<component>-member-<replicaID>`, heartbeated. The set of member leases with a fresh `renewTime` is the live membership → yields `N` and this replica's sorted index.
- **Shard leases** — `S` of them, `convoy-<component>-shard-<i>`; `holderIdentity` is the owning replica. Acquired/renewed via **optimistic concurrency** (update guarded by `resourceVersion`; a conflict means another replica won — move on). Exactly one holder per shard by construction.

### Loop (every renew interval)

1. Renew this replica's member lease.
2. List member leases → `liveMembers` (fresh `renewTime`), sort by `holderIdentity` → `(myIndex, N)`.
3. Compute the **desired shard set** deterministically from `(myIndex, N, S)` — **striped fair-share**: this replica desires shard `i` iff `i % N == myIndex`. Even spread; deterministic, so two replicas never target the same shard (near-zero lease conflicts).
4. Acquire/renew leases for desired shards not yet held; **release** leases for shards no longer desired (proactive release ⇒ instant handoff, not a TTL wait).
5. If the owned set changed, fire `OnChange`.

### Recommended durations

Lease duration `30s`, renew interval `10s` (duration ≫ worst-case reconcile). A member is "live" if its lease `renewTime` is within the lease duration.

## Harness hooks (`internal/controller/base/controller.go`)

Two backward-compatible additions:

```go
func WithEnqueueFilter(keep func(obj any) bool) Option // drop non-owned objects at enqueue (efficiency)
func (c *Controller) EnqueueKeys(keys []string)        // used by OnChange to re-drive newly-owned work
```

The **gate itself stays inside each `ReconcileFunc`** (the correctness layer). The enqueue filter is only an optimization — informers deliver regardless, and ownership can change while an item is queued.

## Integration — controller-manager

- Construct **one `Coordinator`** for the CM component; `errGroup.Go(coordinator.Run)`.
- Each controller gets a **key-extractor** `shardKeyFor(obj) → (ns, name)` and the shared `Ownership`:
  - **release** → the Release's `spec.clusterRef` (`orDefault(ns, rel.Namespace)`, name).
  - **cluster** → the Cluster's own `ns/name`.
  - **git / helm / oci** → the source object's own `ns/name` (a cluster-independent partition of the same `S` space; source-object ownership governs only probe/status writes, **not** artifact consumption, which stays demand-driven through the fetch coordinator).
- **Gate** at the top of each reconcile, right after the lister `Get` (so a missing/deleted object still short-circuits normally):

  ```go
  if !c.ownership.Owns(c.sharder.Shard(clusterNs, clusterName)) {
      return nil // not ours; skip without requeue
  }
  ```

- **Enqueue filter** wired via `WithEnqueueFilter` using the same extractor.
- **Rebalance:** `ownership.OnChange(resync)` where `resync` lists owned objects from the informer and calls `EnqueueKeys`. Over-enqueuing owned items is cheap (idempotent reconcile); newly-owned shards get driven immediately rather than waiting for the next resync.

## Integration — observer

Identical pattern, its **own** `Coordinator` (`convoy-obs-*` leases) — an independent lease set that may run a different replica count. Both `obs` and `healthCtl` share that one observer `Ownership`, keyed by the Release's target cluster. No coupling to the CM's assignment.

## Correctness under handoff

Brief overlap is safe by construction — SSA is idempotent, and `Applied`/`InSync`/`Healthy`/`LifecycleSucceeded` have single-writer field managers. Add the cheap guards:

- Lease duration ≫ reconcile time; gate at reconcile entry.
- Idempotency backstops on the non-idempotent operations: **history append** keyed by render transition; **`/sync` fire** and **lifecycle-hook arming** deduped on their existing annotation/identity keys.
- **Graceful shutdown:** on `ctx` cancel, release held shard + member leases so failover is immediate.
- Explicitly **not** implementing apiserver fencing tokens (they don't exist) — entry-gate + idempotency is the contract.

## Configuration, RBAC, observability

- **Flags** (both managers): `--shards` (S, default `32`; **must be uniform fleet-wide** — changing it needs a coordinated restart; document loudly), `--replica-id` (default `$POD_NAME` / hostname), `--lease-namespace` (default pod namespace), `--lease-duration`, `--lease-renew-interval`.
- **RBAC:** `coordination.k8s.io/leases` `get,list,watch,create,update` in the lease namespace, for both service accounts.
- **Metrics / logs:** owned-shard count (gauge), lease acquire/renew failures, membership size, rebalance events.

## Testing

- **Unit:** shard distribution + stability; striped fair-share math for `S % N != 0`; deterministic desired-set given a member set; acquire/release against a **fake clientset**; kill-a-member → every shard re-owned exactly once, none double-owned.
- **Reconcile gate:** table test that a non-owned key returns `nil` with no side effects.
- **e2e:** kind with `replicas: 2`, kill one replica, assert reassignment and no double-apply.

## Verification on the live dev setup

- `dev/deploy/` gains a `single-replica/` (current, `replicas: 1`) and `multi-replica/` (`replicas: 2+`, sharding exercised) overlay.
- Contexts: `kind-convoy-dev` (control plane) and `kind-convoy-managed` (managed target); `Cluster` objects `dev-local` and `convoy-managed`. With two clusters and `replicas: 2`, assert the two clusters' Releases are owned by different replicas.

## Phasing

Each phase: `make lint` + `make test`, then a `feat: <description>` commit (no remote push).

0. **`internal/sharding`** — `Sharder` + `Ownership` + lease `Coordinator`, fully unit-tested, no wiring.
1. **Harness hooks** — `WithEnqueueFilter` + `EnqueueKeys` in `base.Controller`.
2. **controller-manager wiring** — one `Coordinator`; key-extractor gate in each reconcile; enqueue filter; `OnChange` resync; flags + RBAC.
3. **observer wiring** — same, for `obs` + `healthCtl`.
4. **Rebalance & shutdown correctness** — `OnChange` re-enqueue of owned; graceful lease release; idempotency backstops.
5. **Config, deploy overlays, observability** — `single-replica/` + `multi-replica/` overlays; metrics; kind kill-test.
