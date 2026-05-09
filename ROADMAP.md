# convoy Roadmap

## PoC Scope

The goal of the PoC is to validate the core rendering and deployment pipeline end-to-end on a real cluster.

### In Scope

**Controllers**
- [ ] Source Controller — Git polling, sets `GitRepository /sync` state to `NotSynced` on change
- [ ] Render Controller — Helm rendering + mutation pipeline (helmValues, goTemplate, jsonPatch, strategicMergePatch), S3 artifact storage
- [ ] Executor — SSA-based apply, S3 state storage, local FS cache, health assessment
- [ ] Sync Controller — periodic drift detection, sets `Release /sync` state to `NotSynced` on drift, `ignoreFields` support

**Storage**
- [ ] S3 state backend (rendered artifacts + deployed state)
- [ ] Local FS cache with S3 hydration on startup
- [ ] Control plane proxies S3 content to executors

**Deployment**
- [ ] Server-Side Apply with field manager
- [ ] `ignoreFields` mechanism
- [ ] Sync waves (`convoy.kumodo.io/sync-wave` annotation, `waitForApplied` strategy)

**Health Checking**
- [ ] Built-in CEL rules for standard resources (Deployment, StatefulSet, DaemonSet, Pod, PVC, Job)
- [ ] `HealthRule` resource for user-defined CEL rules

**Multi-Cluster**
- [ ] `Cluster` resource with pluggable auth interface
- [ ] Kubeconfig auth provider
- [ ] Workload Identity auth provider (IRSA / GKE)
- [ ] Agentless executor mode

**Pin and Rollback**
- [ ] `/pin` subresource — `freeze` and `rollback` intents

**Lifecycle Hooks**
- [ ] `/lifecycle` subresource on `Release`
- [ ] `PreSync`, `PostSync`, `SyncFail` phases
- [ ] `Job` hook type with CEL `when` conditions

**API**
- [ ] Aggregation API server setup (auth delegation, APIService registration, generic registry)
- [ ] User-facing resources: `GitRepository`, `HelmRegistry`, `OCIRegistry`, `HealthRule`, `Release`, `Cluster`
- [ ] `Release` subresources: `/mutations`, `/sync`, `/preview`, `/diff`, `/pin`, `/history`, `/lifecycle`
- [ ] Source subresources: `/sync`, `/webhooks`
- [ ] Internal resources: `SourceSyncRequest`, `ReleaseSyncRequest`

**Observability**
- [ ] Structured Kubernetes event emission from all controllers (required foundation for future notification controller)

### Out of Scope for PoC

- CLI / kubectl plugin
- Notification controller
- `ReleaseSet` — app grouping / fleet templating
- Agent executor mode
- `celTransform` mutation type
- `PreRender`, `PostRender`, `PreDelete`, `PostDelete` lifecycle hook phases
- `Pod` hook type
- `waitForHealthy` / `waitForHealthyWithTimeout` wave strategies
- OCI registry source (`OCIRegistry`)
- Webhook-based source triggering

---

## Future Development

### Phase 2 — Operational Completeness

**Notification Controller**
- Separate runnable subscribing to events from all controllers
- `NotificationConfig` resource defining destinations (Slack, webhook, PagerDuty) and event filters
- Designed as a standalone component — no changes required to existing controllers

**Source Enhancements**
- `OCIRegistry` source support
- Webhook receivers for push-based triggering (`/webhooks` subresource on all source kinds)
- `celTransform` mutation type

**Lifecycle Hook Phases**
- `PreRender`, `PostRender`, `PreDelete`, `PostDelete` phases
- `Pod` hook type

**Wave Strategies**
- `waitForHealthy` and `waitForHealthyWithTimeout` transition strategies

**CLI / kubectl Plugin**
- `kubectl kumo` plugin for common operations: list releases, trigger sync, view diff, pin/unpin, view history
- Standalone binary as an alternative distribution

### Phase 3 — Scale and UX

**Agent Executor Mode**
- In-cluster executor deployment
- Agent registration and identity (`Cluster.spec.agent.registrationTokenRef`)
- Mixed agentless + agent fleets

**`ReleaseSet` — Fleet Templating**
- Higher-level resource generating per-cluster `Release` objects from a template + cluster selector
- Enables fleet-wide deployments from a single definition
- Cross-Release ordering (`ReleaseSet` dependency graph)

### Phase 4 — Ecosystem

**Agent Protocol**
- Full wire format, registration flow, identity verification, reconnect semantics
- mTLS and OIDC service-account auth schemes

**Additional Auth Providers**
- Certificate-based auth
- Token projection / bound service account tokens

**UI**
- Web dashboard for release overview, sync history, diff viewer, health status
- Read-only initially; write operations via CLI or kubectl
