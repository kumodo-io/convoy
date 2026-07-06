# convoy Roadmap

convoy is a Kubernetes-native continuous delivery system: an aggregated API server plus a controller manager that fetch sources, render them, and apply the result to target clusters — the control-plane cluster itself or separate managed clusters.

This document reflects **what is implemented today** and the **planned work**, grouped by what actually gates each item. It is not strictly ordered within a group.

---

## Implemented

**API server**
- Aggregated API server — authn/authz delegated to host-cluster RBAC, APIService registration, etcd-backed generic registry
- Resources served with a `/status` subresource: `GitRepository`, `HelmRegistry`, `OCIRegistry`, `Cluster`, `Release`, `KindHealthRule`, `ResourceHealthRule`
- Release field selector on `spec.clusterRef.{name,namespace}`

**Sources** (reusable, credentialed endpoints; the revision/coordinate lives on the `Release`)
- `GitRepository` — public repos (auth `none`), branch/tag/commit/semver refs
- `HelmRegistry` — classic HTTP repos (`index.yaml` + chart tarball), auth `none`
- `OCIRegistry` — Helm charts pulled from OCI registries (ORAS), auth `none`
- Fetch coordinator dispatching on source type; content-addressed in-memory artifact store

**Content & rendering**
- `manifests` — raw YAML/JSON from source paths
- `helm` — in-process Helm SDK render (no `helm install`, no shelling), vendored dependencies
- Helm values pipeline — ordered, deep-merged (later wins) over chart defaults, from `inline` / `configMap` / `secret` / `gitRepository` sources

**Delivery & multi-cluster**
- Connection modes: `inCluster` (self) and `agentless` (control-plane push)
- Cluster auth: `kubeconfig`
- Server-Side Apply with field manager `convoy` (re-applied every reconcile → periodic drift correction on owned fields)

**Sync policy**
- `interval` — per-`Release` self-requeue for drift re-checks
- `prune` — inventory-based GC of resources dropped from the render (`orphaned` / `includeCRDs` / `includeNamespaces`); success-only, never on a partial render; CRDs/Namespaces protected by default
- `retryBackoff` — per-`Release` failure backoff

**Deletion policy** (finalizer-backed)
- `Cluster.deletionPolicy.pruneReleases` — orphan (default) or cascade-delete referencing Releases
- `Release.deletionPolicy.{keepWorkloads, blockOnClusterUnreachable}` — prune (default) or keep workloads; block-and-retry vs. best-effort when the target is unreachable

**Status**
- Sources: `LastSuccessfulProbeAt`, `Ready` condition
- `Release`: `LastAttempted` / `LastSuccessful` render records (resolved source & cluster, phase, failure stage, `renderRef`), `ReconciledAt`, `Applied`/`Ready` conditions
- Status writes never self-trigger reconciles (informer update filter skips status/metadata-only changes)

**Internal**
- Applied-resource inventory (in-memory; fails safe — can only under-prune, never over-delete)
- `releaseByCluster` informer index for cluster→release fan-out

**Dev & CI**
- kind + Tilt dev harness (live-reload)
- CI: lint/test on PRs, image build + push on merge/tag

---

## Planned — unblocked

Deliverable without new infrastructure.

- **Private source auth** — Git `token` / `basic` / `sshKey`; a sane subset of `basic` / `token` / `oidc` / `cloudIdentity` for Helm/OCI registries
- **`/preview`** — render without applying
- **`/diff` (current render)** — desired-vs-live via SSA dry-run + live read (no storage; only diff against a *pinned/historical* render needs it)
- **`/pin` (freeze current)** — hold the current render (rollback / pin-to-previous is storage-gated)
- **Drift detection & self-heal (current render)** — periodic diff → re-apply; event-driven re-apply via target-cluster watches
- **`/sync` (Release + Cluster)** — on-demand sync; also enables honoring `syncPolicy.automatic: false` (manual mode)
- **Health assessment** — evaluate built-in rules + `KindHealthRule` / `ResourceHealthRule` → `Healthy` condition + Release-level aggregation (rule *types* exist; nothing evaluates them yet)
- **CLI** — `kubectl` plugin and/or standalone tool (list, sync, diff, pin/unpin); valuable early for exercising subresources on real clusters
- **Content types** — `kustomize`, `jsonnet` (`cue` later)
- **Sync waves** — `waitForApplied` → `waitForHealthy[WithTimeout]`; hook-annotation translation (depends on health assessment)
- **`/lifecycle`** — hooks across `PreRender`…`PostDelete`, `Job`/`Pod`, CEL `when` conditions

## Planned — storage-gated (history & rollback)

Blocked on a durable persistence backend. Storage gates *time travel*, not self-healing.

- **Storage-model design** — decide persisted entities (render snapshots, per-revision inventories, retention), keying, and a tiered backend (in-memory → PVC → object store); this design de-risks everything below
- **Durable state backend** — the above, implemented; survives restarts, shareable across replicas
- **`/pin` (rollback / previous revision)** and **`/diff` against a pinned or historical render**
- **`/history`** + retention

## Planned — cross-cutting

- **Authorization on cross-namespace references** — a `Release` referencing a `Cluster` (or `Source`) in another namespace is a privilege decision; needs an allow-list / `SubjectAccessReview` model before multi-tenant use
- **Operability** — controller metrics; leader election / HA (relates to future work-sharding for horizontal scale)

## Fleet-wide orchestration & mutations

`mutations`, cluster-metadata templating, and fleet-wide orchestration are the **same per-target-customization substrate at different scopes** — per-target customization is what fleet fan-out drives. They were designed together to avoid rework; the design is in [`docs/design/api/core/fleet.md`](docs/design/api/core/fleet.md). Key decision: **templating is resolved at generation time**, so `Release` stays single-cluster and unchanged — the whole existing pipeline (incl. deletion finalizers and the `releaseByCluster` index) is reused per generated `Release`.

Two new namespaced `core` kinds:

- **`ReleaseTemplate`** — a reusable single-app definition (`source` + `content` + app-specific mutations + optional per-app policy overrides) with `{{ .Cluster.* }}` placeholders and no cluster ref; one labeled object per service.
- **`Fleet`** — `clusterSelector` × `templateSelector` → generates one owned `Release` per pair. Carries fleet-wide **policy defaults** (sync/deletion/retention; template overrides win), **`commonMutations`** (org-wide / cluster-metadata overlays, composed template-first then fleet-last), and a **rollout strategy**.

Build items:

- **`ReleaseTemplate` + `Fleet` kinds** — API types, validation, generation controller (materialize per-pair Releases, deterministic naming, ownerRefs)
- **Generation-time templating** — Go `text/template` over `{{ .Cluster.name/namespace/labels/metadata }}`, on a defined set of string fields (values paths, `content` path, inline values, mutation args, name suffix)
- **Selector-driven membership + GC** — add/remove labeled `ReleaseTemplate`/`Cluster` re-generates; unmatch/`Fleet` delete removes generated Releases via the existing deletion policy + finalizers
- **Full mutation pipeline** — `helmValues`, `goTemplate`, `jsonPatch`, `strategicMergePatch`, `celTransform` (the Release-level transform engine used by both template and fleet-common mutations)
- **`/mutations` subresource** — separately addressable for field-level RBAC / audit
- **`helm.ignoreMissingValuesFiles`** — helm-content-wide flag (Argo-style) so per-cluster override files that don't exist are skipped rather than erroring
- **Rollout strategy** — `allAtOnce` first; staged / health-gated cluster waves after health assessment lands (cross-*cluster* ordering, distinct from the in-cluster progressive-delivery non-goal)

Tenancy is same-namespace (per the design doc); cross-namespace fleets (central cluster registry / shared template library) depend on the cross-namespace grant model under *cross-cutting*.

## Planned — later

- **`/webhooks`** — push-based source triggering (after source auth lands)
- **Notifications** — notification controller over controller events; `NotificationConfig` (Slack / webhook / PagerDuty) with filters
- **Agent (pull) mode** — in-cluster executor pulling work over outbound HTTPS for NAT/firewalled/air-gapped targets; registration/identity (mTLS / bound SA tokens), mixed agentless + agent fleets
- **Web dashboard** — release overview, history, diff, health (read-only first)

---

## Explicit non-goals

- **Secret management** — values come from a secrets manager (External Secrets, Vault, …), not stored in Git/object storage unencrypted
- **Progressive delivery** — canary/blue-green is delegated to external tooling
- **Non-Kubernetes workloads** — convoy targets Kubernetes exclusively
