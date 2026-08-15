# Persistent Storage

> **Status: write path implemented; read features pending.** The durable backend is built (`internal/blobstore`, `internal/renderstore`) and wired behind `--render-store-url` (a [gocloud.dev/blob](https://gocloud.dev) URL — `s3://`, `gs://`, `azblob://`, `file://`, `mem://`). When set, the release controller **persists each render before applying it (persist-then-apply)** and appends an ordered deploy-log entry, so renders and history are now durable. Still **pending** (slices 5–6): surfacing them — the `/history` subresource, enabling **rollback** in `/pin` validation, **diff against a previous render** in the observer, and **GC/retention** (`keepHistory` + prefix-delete on finalize). Source artifacts remain in the in-process cache (repopulated on fetch); only renders are persisted. This document specifies the interface and semantics; the concrete backend is gocloud, mocked at the `BlobStore` seam in tests.

## Why storage, and why so little of it

The instinct is to treat "convoy state" as one thing that either survives restart or doesn't. It isn't. Convoy already has a durable backend — **etcd**, via the aggregated API server — and most of its state lives there. Disaggregating the state shows how small the actual gap is:

| State                                                             | Source of truth                               | Durable?                    | Where it belongs                                   |
|-------------------------------------------------------------------|-----------------------------------------------|-----------------------------|----------------------------------------------------|
| Desired state (`Release`/`Cluster`/sources/rule sets)             | the API object                                | ✅ etcd                     | etcd — solved                                      |
| Applied inventory + per-resource health (`ReleaseInventory`)      | the API object                                | ✅ etcd                     | etcd — solved                                      |
| Fetched source artifacts (Git/chart bytes)                        | **upstream** (Git/registry)                   | ❌ in-memory                | a **cache** — never persisted (re-fetchable)       |
| **Rendered manifest snapshots** (the bytes `renderRef` hashes)    | the **render store** (or recomputed, if none) | ◑ **opt-in** — this backend | the **object store** — the gap this backend closes |
| Runtime state (informer caches, work queues, in-flight reconcile) | derivable from etcd                           | ❌ (by design)              | **never persist** — rebuilt on start               |

So the durable-storage question reduced to one thing: **persist render snapshots** — which this backend does, behind `--render-store-url` (empty ⇒ renders are still recomputed, as before). Everything else is already on etcd, or a re-fetchable cache, or ephemeral by design.

Two consequences settle the shape of this design:

- **Source bytes are never persisted.** The stored render is the *frozen artifact of record*. Source can change; rollback, history, and historical diff read the render, never re-fetch or re-render source. The in-memory source cache stays a pure fetch optimization.
- **Compute stays stateless over externalized state.** Because durable state lives in etcd + the object store (not in replica memory), a replica that acquires a cluster shard holds nothing — it reads etcd and the shared bucket and continues. There is no leader→standby state sync; the shared store *is* the sync mechanism. Controllers run as ordinary Deployments, with work partitioned by cluster shard (see [Sharding & High Availability](04-sharding.md)), not StatefulSets holding local state.

## What storage gates (and what it doesn't)

A persistent backend flips exactly one thing — whether a render store is configured — which ungates the operations that need a durable record of a *past* render. Everything else works identically with or without it. (The write path — persist-then-apply and the history log — is implemented; the read features in the right-hand column are slice 5, still pending.)

| Works with the in-memory store                                 | Needs a persistent store                                     |
|----------------------------------------------------------------|--------------------------------------------------------------|
| freeze `/pin` (hold current render) & unpin                    | rollback `/pin` (apply a **previous** render)                |
| `/preview`, current-render `/diff` (render-on-demand vs. live) | `/diff` against a pinned/historical render                   |
| drift detection + self-heal                                    | `/history` + retention                                       |
| health assessment, lifecycle hooks                             | orphan/extra-object drift *(durable per-revision inventory)* |

The boundary is precise: **"does this operation need *content* from a past render?"** Freeze-pin does not — it is *compare-and-hold*: it holds the render already applied and, on a spec change, withholds the apply rather than re-applying stored bytes, so it only needs the `renderRef` (which lives in etcd), never the content. Rollback does need the content, because it re-applies a past render that may not re-render identically. That single distinction is why freeze-pin ships on the in-memory tier and rollback is gated behind this backend.

## The store seam

The store is a byte-level, content-addressed interface (`internal/blobstore`) with pluggable backends. It is a **new** package, separate from the source-artifact cache: the existing `artifact.Store` (`Put(key, Artifact)` / `Get(key) (Artifact, bool)`, in-memory) is untouched and stays a pure fetch optimization. `BlobStore` is deliberately minimal — dumb key→bytes CRUD — so a backend implements five methods and all render semantics layer above it:

```go
type BlobStore interface {
    Put(ctx context.Context, key string, data []byte) error      // content-addressed ⇒ idempotent
    Get(ctx context.Context, key string) ([]byte, error)         // ErrNotFound if absent
    Exists(ctx context.Context, key string) (bool, error)
    List(ctx context.Context, prefix string) ([]string, error)   // ordered; for history + GC
    Delete(ctx context.Context, key string) error                // idempotent
}
```

- **Backends:** an in-process `NewMemory()` (dev / tests / the mock double) and a gocloud-backed impl opened from a URL (`blobstore.Open(ctx, url)`) — see [Configuration & credentials](#configuration--credentials). gocloud is the *only* place a concrete cloud SDK is referenced; everything above `BlobStore` mocks it. Anything gocloud can't serve is just another `BlobStore` impl.
- **`RenderStore`** (`internal/renderstore`) layers the render/history semantics over a `BlobStore`, keyed by `(namespace, name, renderRef)`: `PutRender` / `GetRender` / `HasRender` (rollback validation), and `AppendHistory` / `History` / `LastHistoryRef` (the ordered deploy log), plus `DeleteRelease` (drop a release's prefix). It is unit-tested against `NewMemory()` — never a real cloud.
- **Gating is by presence, not a capability method.** A component with a render store configured (non-nil) persists and can serve the time-travel features; without one it behaves exactly as before. There is no `PersistsRenders()` method. (Wiring the read features to check the store and reject when absent is slice 5 — pending.)

## Object layout & keying

Renders are **scoped to the owning Release** and split into two prefixes — immutable content addressed by `renderRef` (already a `sha256` of the render), plus an append-only, ordered log of deploy events. There is **no reliance on object-store versioning** (not every S3-compatible backend supports it, and content-addressing makes it redundant).

Keys start at the bucket root (there is no configurable key prefix — isolate instances by bucket):

```
{bucket}/
  <namespace>/<name>/
    blobs/
      <renderRef>                     # immutable render content — content-addressed, deduped, self-verifying
    history/
      <sortable-ts>-<renderRef>.json  # append-only ORDERED deploy log → { renderRef, syncedAt, summary }
```

| Key                                         | Content                                                     | Written                                   | Mutability |
|---------------------------------------------|-------------------------------------------------------------|-------------------------------------------|------------|
| `<ns>/<name>/blobs/<renderRef>`             | the rendered manifest set (exactly what `renderRef` hashes) | on each new render (persist-then-apply)   | immutable  |
| `<ns>/<name>/history/<ts>-<renderRef>.json` | a deploy-event record pointing at a blob                    | only when the deployed render **changes** | immutable  |

The **object store is the authoritative, self-contained history** — it answers every historical query on its own:

- **History for a release** → `list <ns>/<name>/history/`.
- **Ordered timeline / track changes** → `history/` keys are timestamp-prefixed, so the listing *is* chronological; each entry carries `syncedAt` and a summary of what changed. Ordering needs no counter and nothing in etcd; same-instant ties break on the `renderRef` suffix.
- **Historical diff** → fetch two entries' `blobs/<renderRef>` and diff.
- **Rollback** → fetch an old entry's `blobs/<renderRef>`, apply it, and append a new `history/` entry for the rollback event.

**etcd holds only current state** — `status.lastSuccessful.renderRef` (what is deployed *now*), like any Kubernetes object. It never holds a history list. The two prefixes serve two access patterns: `blobs/` is fetch-by-`renderRef` (the current pointer, a rollback target) and dedups identical content; `history/` is the ordered log of small metadata records. This is the container-registry shape — blobs by digest plus a per-repository tag/manifest log.

A `history/` entry is recorded **only when the render changes**, so the timeline is a change-log, not reconcile-noise. Content-addressing dedups identical `blobs/` *within* a release; cross-release dedup is traded for the self-describing per-release layout — a small cost, since per-cluster templating usually makes cross-release renders differ. Self-verification holds: re-hash a fetched blob, compare to the `renderRef` in its key. (The identifier is the namespaced name for a navigable bucket; delete-and-recreate under the same name is self-correcting as old blobs age out via GC — use the Release **UID** if strict identity across recreation matters.)

## The write path — persist before apply (invariant)

A render that reaches a cluster **must** already be in the store, or live state and recorded history could diverge. This is an ordering invariant, not a tunable policy:

1. **Render** the Release → compute `renderRef`.
2. **Persist** the blob to `<ns>/<name>/blobs/<renderRef>`. Content-addressed ⇒ idempotent: a re-apply of the same render just re-stores identical bytes (a dedup overwrite), so the persist runs unconditionally on every apply rather than guarding on an existence check.
3. **Apply** the manifest set to the cluster (SSA).
4. On apply success, **commit**: if the render changed, append a `<ns>/<name>/history/<ts>-<renderRef>.json` entry **to the store**; set `status.lastSuccessful.renderRef` (the current pointer) in etcd; update inventory. "Changed" is decided against the **last logged history entry** (read back from the store, which is read-after-write consistent), *not* against `status.lastSuccessful` — the latter lags in the informer cache across the several reconciles one deploy triggers (e.g. lifecycle-hook phases), which would otherwise append duplicate entries for a single deploy. Appending history is best-effort: the blob (the rollback target) is already durable, so a failed append is logged, not fatal.

Every failure point stays consistent under level-triggered reconciliation:

- **Persist (2) fails** → the sync fails with a render-stage error and retries via `retryBackoff`. Nothing was applied; nothing to undo. *You never apply a render you could not store.*
- **Apply (3) fails** → the blob is already stored and referenced by `status.lastAttempted` (so it is not orphaned); the failure is recorded; retry re-applies (idempotent persist + idempotent SSA).
- **Commit (4) fails after apply** → the cluster has the render and the blob exists; the next reconcile re-renders (same `renderRef`), re-persists (no-op), re-applies (no-op), and re-commits. Eventually consistent, never inconsistent.

Because the blob is written in step 2 but only referenced — by the current pointer (`status.lastAttempted`/`lastSuccessful.renderRef`) or a `history/` entry — from step 4, GC must not reap a blob during that window; the GC grace window below covers it.

## History & retention

**History lives in the store** (implemented) — the release's `history/` prefix is an ordered log of deploy events, one small object each, appended on change. Depth is limited only by retention, not by any etcd object size — etcd carries just the current `renderRef` pointer.

> **Retention & GC are planned (slice 6, not built).** The `spec.retentionPolicy.keepHistory` field exists on the API but is **not yet enforced**: history and blobs currently accumulate. The reclaimer below is the design:

- **Per-release, reference-aware GC.** For each release, list `<ns>/<name>/history/`, keep the newest `keepHistory` entries, delete the rest; then delete any `blobs/<renderRef>` no longer referenced by a surviving `history/` entry or the current pointer, **older than a grace window** (e.g. 1h — this also covers the persist→commit gap above). Deleting a Release drops its whole `<ns>/<name>/` prefix via `RenderStore.DeleteRelease` — one operation, no bucket-wide sweep (the method exists; wiring it into the finalizer is part of this slice).
- **Partitioned by ownership, no global runner.** GC runs on every replica but each reclaims only the Releases in the clusters it owns (see [Sharding](04-sharding.md)) — exclusive ownership means it cannot race itself, so no leader is needed.
- Object-store **lifecycle rules are a backstop only** — time-based expiry can't be reference-aware and would happily delete a still-pinned render, so it can never be the primary reclaimer.

## Configuration & credentials

The store is selected by a single flag, `--render-store-url`, on every component that
touches it — a [gocloud.dev/blob](https://gocloud.dev) URL whose scheme picks the
backend and whose query parameters carry backend options. One S3 implementation
serves AWS S3, MinIO, Ceph RGW, Cloudflare R2, and GCS-in-S3-mode via an endpoint
override plus a path-style toggle; GCS and Azure have native schemes.

| Backend                        | Example `--render-store-url`                                                                                                     |
|--------------------------------|----------------------------------------------------------------------------------------------------------------------------------|
| AWS S3                         | `s3://convoy-renders?region=eu-west-1`                                                                                           |
| S3-compatible (MinIO/Ceph/R2)  | `s3://convoy-renders?endpoint=http://minio:9000&region=us-east-1&use_path_style=true&disable_https=true&hostname_immutable=true` |
| GCS (native)                   | `gs://convoy-renders`                                                                                                            |
| Azure Blob (native)            | `azblob://convoy-renders`                                                                                                        |
| Filesystem (PVC / single-node) | `file:///var/lib/convoy/renders?create_dir=true&no_tmp_dir=true`                                                                 |
| In-memory (dev only)           | `mem://`                                                                                                                         |

An empty flag disables persistence — renders are not stored and the time-travel
features stay gated. There is no `prefix` parameter; isolate multiple convoy
instances by giving each its own bucket. (`file://` needs `no_tmp_dir=true` on a
mounted volume: otherwise fileblob writes a temp file under `$TMPDIR` and renames
into place, which fails cross-device against the PVC.)

**Credentials are ambient to the pod, not convoy configuration.** Convoy has no
credential block and no `credentials:` field — gocloud opens the bucket through the
cloud SDK's *default credential chain*, so the operator grants the pod access by the
platform's normal means and gocloud picks it up:

| Backend  | Credential-free (recommended)                                | Static fallback                                                   |
|----------|--------------------------------------------------------------|-------------------------------------------------------------------|
| S3       | **IRSA** on EKS (ServiceAccount → IAM role; projected token) | `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` env (from a Secret) |
| GCS      | **GKE Workload Identity** (KSA → GSA)                        | `GOOGLE_APPLICATION_CREDENTIALS` → a mounted key file             |
| Azure    | **AAD Workload Identity** / managed identity                 | `AZURE_STORAGE_ACCOUNT` + `AZURE_STORAGE_KEY` (or a SAS) env      |
| file/mem | — (no credentials)                                           | —                                                                 |

Prefer **workload identity** in production: the pod carries no long-lived secret and
access is an IAM/role binding on the component's ServiceAccount. Static keys via a
mounted Secret are the simple/dev path (what the `storage-minio` dev overlay uses).
Either way, credentials are wired at the pod — env vars, a mounted file, or the
ServiceAccount identity — and never passed to convoy as flags or embedded in
`--render-store-url`.

**Every component that opens the store needs this access.** Today that is the
**controller-manager** (the write path — the only store consumer wired so far). When
the read features land (`/history`, rollback validation, historical `/diff`), the
**observer** and the **aggregated apiserver** open the same bucket too, so the
identity/credentials must be granted to all three ServiceAccounts — scoped
least-privilege to the bucket (see [Security](#security)).

## Topology & scaling

The release controller (writes renders, reads them for rollback), the observer (reads past renders for historical diff), and the aggregated apiserver (`/history`, rollback validation) all point at the **same bucket**. Today only the controller is wired (the write path); the observer and apiserver join when the read features land (slice 5). Content-addressed + shared means **no coordination and no separate service** — the store is an in-process library behind the interface. The seam stays clean enough to extract a dedicated `convoy-artifact` service later (a single-writer/caching front, as Flux does) if fleet scale ever demands it, but that is not needed to ship this backend.

Compute is partitioned by **cluster shard** — see [Sharding & High Availability](04-sharding.md). That model is exclusive-ownership, which is what keeps this backend simple:

- **The bucket stays global and content-addressed**; a replica writes only the Releases it owns (`<ns>/<name>/…`), so no two replicas contend for the same keys. Storage never fragments with the shards.
- **GC needs no global leader** — each replica reclaims only the Releases in the clusters it owns; ownership *is* the partition (this supersedes the "single runner" note above).
- **Single replica + object store** remains a valid deployment (the `N=1` degenerate shard case): durable storage without HA compute.

## Security

Rendered manifests can contain whatever the source produced — including inline `Secret` objects. The render store may therefore hold sensitive data. Accordingly:

- **Server-side encryption** (SSE-S3 or SSE-KMS), a **private** bucket, and a **least-privilege** IAM policy (or workload-identity role) scoped to the bucket — granted to each component's ServiceAccount (see [Configuration & credentials](#configuration--credentials)).
- Access is via convoy's own identity, never the end user's — a tenant cannot reach the bucket directly, so tenant isolation is enforced at convoy's layer. A single shared bucket across namespaces is acceptable for v1; per-tenant prefixes/buckets are a later refinement.
- Convoy's stated non-goal is that secrets come from a secrets manager (External Secrets / Vault) rather than being rendered inline — but the store must be treated as sensitive regardless, since it cannot enforce that.

## Decisions

- **No object-store versioning** — content-addressed immutable keys instead; portable across S3-compatible backends that lack versioning.
- **Persist-then-apply is an invariant** — never apply an unstored render (see the write path). No "apply now, archive best-effort" mode.
- **Renders only** — source bytes are never persisted; the render is the frozen artifact of record.
- **Client library: `gocloud.dev/blob`** — chosen (an explicit, approved dependency) for native S3/GCS/Azure plus `file`/`mem` behind one URL-opened API. The `BlobStore` interface still owns the seam, so gocloud is swappable and mocked out of tests, and a backend it doesn't cover can be a separate impl.
- **Gating by store presence, not a capability flag** — a configured (non-nil) render store enables the durable features; there is no `PersistsRenders()` method.

## Relationship to the in-memory tier & phasing

Enabling durable storage is a **configuration switch** (`--render-store-url`), not a migration: content-addressed keys mean a fresh backend simply starts empty and fills as renders happen. Switching backends (or from none) loses nothing that was durable (etcd is untouched); it only changes whether renders survive a restart.

Build order:

1. **(done)** `BlobStore` interface (context + error) + gocloud backend + in-memory backend; opened from `--render-store-url`.
2. **(done)** `RenderStore` wrapper over `BlobStore`; **persist-then-apply**; store the render bytes keyed by `renderRef` on the release controller's apply path.
3. **(partly done)** `history/` deploy-log entries are appended on change (best-effort). **Remaining:** un-gate the read features — enable rollback in `/pin` validation, implement the `/history` subresource, and historical `/diff` in the observer — by giving those components the render store and checking for a stored render.
4. **(remaining)** ownership-partitioned, reference-aware GC with a grace window, `keepHistory` retention, and prefix-delete on finalize.
