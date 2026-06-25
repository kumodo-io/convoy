# convoy Roadmap

convoy is built in milestones: a proof-of-concept that validates the core delivery loop, an MVP that makes it usable for real GitOps, and feature work beyond that.

---

## Proof of Concept (current)

The PoC validates the core pipeline end-to-end on real clusters: a Git source is fetched, rendered, and applied to a target cluster — both to the control-plane cluster itself and to a separate managed cluster.

**API**
- [x] Aggregated API server — authn/authz delegated to host-cluster RBAC, APIService registration, etcd-backed generic registry
- [x] User-facing resources served with `/status`: `GitRepository`, `HelmRegistry`, `OCIRegistry`, `Cluster`, `Release`, `KindHealthRule`, `ResourceHealthRule`

**Controllers** (`convoy-controller-manager`, one process)
- [x] **Cluster** — connects to the target and records reachability, discovered version, and API versions
- [x] **GitRepository** — fetches public repos (auth `none`), resolves branch/tag/commit, records `LastFetched`, publishes content to the artifact store
- [x] **Release** — renders `manifests` content from the source artifact and applies it to the target via Server-Side Apply

**Delivery & multi-cluster**
- [x] Connection modes: `inCluster` (self) and `agentless` (control-plane push)
- [x] Cluster auth: `kubeconfig`
- [x] Server-Side Apply with field manager `convoy`
- [x] In-memory artifact store (source → release handoff)

**Dev & CI**
- [x] kind + Tilt dev harness (live-reload)
- [x] CI workflows: lint/test on PRs, image build + push on merge/tag

---

## MVP — Toward a usable CD tool

The PoC *demonstrates* the loop; the MVP makes it something you could actually run. The "must" set is the GitOps spine (continuous, self-healing delivery of real charts); "should" rounds it out.

### Must

**Sources & content**
- [ ] **Helm content** — render charts from a Git source (values from chart defaults, then inline values)
- [ ] **Private source auth** — Git `token` / `basic` / `sshKey`

**Continuous reconciliation**
- [ ] **Inventory tracking** — record the set of resources applied per `Release` (prerequisite for prune + drift)
- [ ] **Pruning / GC** — remove resources deleted from the source
- [ ] **Drift detection + correction** — re-apply when live cluster state diverges, not only on resync
- [ ] **Event-driven re-apply** — re-enqueue `Release`s when their source revision changes

**Status**
- [ ] **Richer `Release` status** — applied revision, inventory, and clear `Applied` / `Ready` conditions

### Should

- [ ] **Health assessment** — evaluate built-in rules + `KindHealthRule` / `ResourceHealthRule` → `Healthy` condition + Release-level aggregation (the rule *types* already exist; nothing evaluates them yet)
- [ ] **`HelmRegistry` / `OCIRegistry` sources** — Helm charts and OCI artifacts, not just Git
- [ ] **Durable state (S3)** — artifacts + inventory + history; survives restarts and unlocks pin/rollback
- [ ] **More cluster auth** — `token`, `clientCert`, `cloudIdentity` (cloud workload identity) for agentless mode

---

## Beyond MVP

Feature work once the core is solid. Not strictly ordered.

### Rendering & mutations
- Full mutation pipeline: `helmValues`, `goTemplate`, `jsonPatch`, `strategicMergePatch`, `celTransform`
- `/mutations` subresource — separately addressable for field-level RBAC and audit
- Additional content types: `kustomize`, `jsonnet`, `cue`

### Operations UX (subresources)
- `/preview` and `/diff` — see what would deploy / what differs
- `/pin` — `freeze` and `rollback` intents (depends on durable history)
- `/history` — past render records

### Ordering & lifecycle
- Sync waves (`convoy.kumodo.io/sync-wave`) with `waitForApplied` → `waitForHealthy` / `waitForHealthyWithTimeout` strategies; ArgoCD/Helm hook-annotation translation
- Lifecycle hooks via `/lifecycle` — all phases (`PreRender`…`PostDelete`), `Job` and `Pod` types, CEL `when` conditions

### Triggers
- Webhook receivers for push-based source triggering (`/webhooks` subresource)
- `/sync` subresources + internal `SourceSyncRequest` / `ReleaseSyncRequest` resources

### Notifications
- Notification controller subscribing to controller events (all controllers emit structured events from the start)
- `NotificationConfig` resource — destinations (Slack, webhook, PagerDuty) and event filters

### Fleet
- `ReleaseSet` — generate per-cluster `Release`s from a template + cluster selector; cross-Release ordering

### Agent (pull) mode
- In-cluster executor that pulls work over outbound HTTPS (NAT/firewalled/air-gapped targets)
- Agent registration/identity, control-plane S3 proxying, mixed agentless + agent fleets
- Full agent wire protocol: registration flow, identity verification (mTLS / bound SA tokens), reconnect semantics

### Clients
- `kubectl` plugin for common operations (list, sync, diff, pin/unpin, history)
- Web dashboard — release overview, history, diff, health (read-only first)

---

## Explicit non-goals

- **Secret management** — values come from a secrets manager (External Secrets, Vault, …), not stored in Git/S3 unencrypted
- **Progressive delivery** — canary/blue-green is delegated to external tooling
- **Non-Kubernetes workloads** — convoy targets Kubernetes exclusively
