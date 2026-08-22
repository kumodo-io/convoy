# convoy Roadmap

convoy is a Kubernetes-native continuous delivery system: an aggregated API server, a controller manager that fetches sources / renders them / applies the result to target clusters, and an observer that detects drift and serves preview/diff — for the control-plane cluster itself or separate managed clusters.

This document reflects **what is implemented today** and the **planned work**, grouped by what actually gates each item. It is not strictly ordered within a group.

---

## Implemented

**API server**
- Aggregated API server — authn/authz delegated to host-cluster RBAC, APIService registration, etcd-backed generic registry
- Resources served with a `/status` subresource: `GitRepository`, `HelmRegistry`, `OCIRegistry`, `Cluster`, `Release`, `KindHealthRule`, `ResourceHealthRule`
- `Release` subresources: `/status`, `/pin`, `/sync`, `/preview`, `/diff`
- Release field selector on `spec.clusterRef.{name,namespace}`

**Components**
- `convoy-apiserver` — serves the API only (subresource handlers proxy to other components)
- `convoy-controller-manager` — five per-kind reconcilers (Cluster, Git/Helm/OCI sources, Release) on a shared work-queue harness; fetch → render → apply
- `convoy-observer` — standalone binary; drift-detection loop + on-demand `/preview`/`/diff` HTTP server (the apiserver proxies to it)

**Sources** (reusable, credentialed endpoints; the revision/coordinate lives on the `Release`)
- `GitRepository` — public repos (auth `none`), branch/tag/commit/semver refs
- `HelmRegistry` — classic HTTP repos (`index.yaml` + chart tarball), auth `none`
- `OCIRegistry` — Helm charts pulled from OCI registries (ORAS), auth `none`
- Fetch coordinator dispatching on source type; content-addressed in-memory artifact store

**Content & rendering**
- `internal/render` — standalone render library shared by the controller-manager (apply) and the observer (compare)
- `manifests` — raw YAML/JSON from source paths
- `helm` — in-process Helm SDK render (no `helm install`, no shelling), vendored dependencies
- Helm values pipeline — ordered, deep-merged (later wins) over chart defaults, from `inline` / `configMap` / `secret` / `gitRepository` sources

**Delivery & multi-cluster**
- Connection modes: `inCluster` (self) and `agentless` (control-plane push)
- Cluster auth: `kubeconfig`
- Server-Side Apply with field manager `convoy` (re-applied every reconcile → periodic drift correction on owned fields)

**Sync policy**
- `automatic` — manual mode (`automatic: false`): the controller still reconciles/observes (renders, projects status) but withholds the apply until a `/sync` is pending; reports `WaitingForSync`. Automatic mode applies every reconcile.
- `interval` — per-`Release` self-requeue for drift re-checks
- `prune` — inventory-based GC of resources dropped from the render (`orphaned` / `includeCRDs` / `includeNamespaces`); success-only, never on a partial render; CRDs/Namespaces protected by default
- `retryBackoff` — per-`Release` failure backoff

**Subresource operations**
- `/pin` (freeze current) — hold the current render; GET/PUT (empty clears); server validates freeze-current-only (rollback-to-previous is storage-gated); `status.pin` projection + `Pinned` condition reason
- `/sync` — annotation-based reconcile request (stamps `sync-requested-at`; the controller's informer filter wakes on it); also drives manual-mode apply
- `/preview` — rendered desired objects (render-only), served by the observer, proxied by the apiserver
- `/diff` (current render) — structured per-object drift (`InSync`/`Modified`/`Missing` + field-level changes) via SSA dry-run vs. live; served by the observer, proxied by the apiserver

**Drift detection**
- Observer drift loop: render desired → SSA dry-run vs. live (server-defaulting cancels out) → `InSync` condition; in automatic mode fires `/sync` to self-heal. Single compare engine shared with `/diff`, so loop and `/diff` never disagree. Covers *declared* objects (orphan/extra-object drift is deferred — needs cross-process inventory).

**Status**
- Sources: `LastSuccessfulProbeAt`, `Ready` condition
- `Release`: `LastAttempted` / `LastSuccessful` render records (resolved source & cluster, phase, failure stage, `renderRef`), `ReconciledAt`, `LastHandledSyncAt`, `status.pin`
- **Conditions with single-writer ownership via SSA field managers**: `Applied` (owned by the controller-manager, `convoy-release`) and `InSync` (owned by the observer, `convoy-observe`). No composite `Ready` — each signal has exactly one writer, on disjoint fields (`listType=map` conditions). `Healthy` is planned.
- Status writes never self-trigger reconciles (informer update filter skips status/metadata-only changes)

**CLI**
- `kubectl-convoy` plugin: `release pin` / `unpin` / `sync` / `preview` / `diff` (noun-first, room for `cluster …`)

**Internal**
- Applied-resource inventory (in-memory; fails safe — can only under-prune, never over-delete)
- `releaseByCluster` informer index for cluster→release fan-out

**Dev & CI**
- kind + Tilt dev harness (live-reload) for apiserver / controller-manager / observer
- CI: lint/test on PRs, image build + push on merge/tag

---

## Planned — unblocked

Deliverable without new infrastructure.

- **Health assessment** (`Healthy` condition) — the observer's second loop: evaluate built-in rules + `KindHealthRule` / `ResourceHealthRule` → `Healthy` condition (owned by the observer) + Release-level aggregation into `status.health` (rule *types* exist; nothing evaluates them yet). Independent of drift — a Release can be `InSync: True`, `Healthy: False`.
- **Private source auth** — Git `token` / `basic` / `sshKey`; a sane subset of `basic` / `token` / `oidc` / `cloudIdentity` for Helm/OCI registries
- **Content types** — `jsonnet` (`cue` later); `helm` `resolve` dependency mode. (`kustomize` ✅ done — in-process build over the fetched artifact, `targetNamespace` honored via kustomize's native namespace transformer.)
- **Event-driven drift** — target-cluster watches to re-check on observed change (today drift is periodic on the sync interval)
- **Sync waves** — `waitForApplied` → `waitForHealthy[WithTimeout]`; hook-annotation translation (depends on health assessment)
- **`/lifecycle`** — hooks across `PreRender`…`PostDelete`, `Job`/`Pod`, CEL `when` conditions

## Planned — storage-gated (history & rollback)

Blocked on a durable persistence backend. Storage gates *time travel*, not self-healing.

- **Storage-model design** — decide persisted entities (render snapshots, per-revision inventories, retention), keying, and a tiered backend (in-memory → PVC → object store); this design de-risks everything below
- **Durable state backend** — the above, implemented; survives restarts, shareable across replicas
- **`/pin` (rollback / previous revision)** and **`/diff` against a pinned or historical render**
- **`/history`** + retention
- **Orphan/extra-object drift** — reporting live objects convoy no longer renders; needs a durable per-revision inventory (today in-memory, per-controller)

## Planned — cross-cutting (security & operability)

- **Same-namespace reference guard** — ✅ **done**. Release validation rejects any ref (`clusterRef`, `source.*`, `content.helm.values[].gitRepository`) whose namespace isn't empty or the Release's own, on create and update (`field.Forbidden`). Closes the privilege-escalation path (referencing a credentialed `Cluster`/`Source` in a namespace you can't access) and matches the documented v1 same-namespace posture. Cross-namespace refs return via the `ReferenceGrant` model below.
- **Cross-namespace references — `ReferenceGrant` model** (deferred design) — the real multi-tenant answer, chosen over `SubjectAccessReview`. A namespaced consent object in the **target** namespace declares "Releases from namespace X may reference this kind here," decoupling *may-reference* from *may-read* (an `SAR` approach would force referrers to have read on credential-bearing `Cluster`/`Source` objects). Checked by the controller at reconcile (which has no user identity). Pairs with the fleet central-registry / shared-template use case.
- **Least-privilege apply/observe identity** (security hardening; replaces the dev-only `cluster-admin` bindings on controller-manager and observer) — **invariant: the observer dry-runs as the same principal the deployer applies as**, so observer-capability ≡ deployer-capability and "observer forbidden on kind X" ≡ "Release not deployable" (honest signal, not a privilege bug). Realized as **one intentional convoy identity per target cluster** that both deploy and dry-run/observe route through:
  - `inCluster` → observer's own SA is read-only; it **impersonates** the controller-manager's dedicated apply identity for the dry-run
  - `agentless` → act as a dedicated convoy identity on the remote (not the ambient handed-over kubeconfig)
  - `agent` (pull) → the agent's in-cluster SA natively *is* that identity (one process deploys + observes; no impersonation) — the cleanest realization; push-mode impersonation approximates it
  - Note: SSA **dry-run authorizes as a real apply** (the footgun); that acting identity must be granted the union of kinds its Releases render (unavoidable for any SSA-based deploy+dry-run; document in target-cluster onboarding). Impersonation buys no-standing-broad-credential + auth/identity decoupling + auditability, not extra capability.
- **Operability** — controller/observer metrics; leader election / HA (relates to future work-sharding for horizontal scale)

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

Tenancy is same-namespace (per the design doc); cross-namespace fleets (central cluster registry / shared template library) depend on the `ReferenceGrant` model under *cross-cutting*.

## Planned — later

- **`/webhooks`** — push-based source triggering (after source auth lands)
- **Notifications** — notification controller over controller events; `NotificationConfig` (Slack / webhook / PagerDuty) with filters
- **Agent (pull) mode** — in-cluster executor pulling work over outbound HTTPS for NAT/firewalled/air-gapped targets; combines deployer + the shared drift/health loops (inheriting the capability-parity identity model — its own in-cluster SA is the per-target identity); registration/identity (mTLS / bound SA tokens); the **agent-manager** control-plane serving surface; mixed agentless + agent fleets
- **Web dashboard** — release overview, history, diff, health (read-only first)

---

## Explicit non-goals

- **Secret management** — values come from a secrets manager (External Secrets, Vault, …), not stored in Git/object storage unencrypted
- **Progressive delivery** — canary/blue-green is delegated to external tooling
- **Non-Kubernetes workloads** — convoy targets Kubernetes exclusively
