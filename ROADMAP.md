# kumo-cd Roadmap

## PoC Scope

The goal of the PoC is to validate the core rendering and deployment pipeline end-to-end on a real cluster.

### In Scope

**Controllers**
- [ ] Source Controller — Git polling, triggers source sync via internal AA resource
- [ ] Render Controller — Helm rendering + Go template rendering on patches + JSON/strategic-merge/Kustomize patching, S3 artifact storage
- [ ] Deploy Controller — SSA-based apply, S3 state storage, local FS cache
- [ ] Sync Controller — periodic drift detection, triggers drift sync via internal AA resource, `ignoreFields` support

**Storage**
- [ ] S3 state backend (rendered artifacts + deployed state)
- [ ] Local FS cache with S3 hydration on startup

**Deployment**
- [ ] Server-Side Apply with field manager
- [ ] `ignoreFields` mechanism

**Health Checking**
- [ ] Built-in CEL rules for standard resources (Deployment, StatefulSet, DaemonSet, Pod, PVC, Job)
- [ ] User-defined CEL rules for custom resources

**Multi-Cluster**
- [ ] Cluster registration resource
- [ ] Pluggable auth interface
- [ ] Kubeconfig auth provider
- [ ] Workload Identity auth provider (IRSA / GKE)

**API**
- [ ] Aggregation API server setup (auth delegation, APIService registration, generic registry)
- [ ] User-facing resources: `Source`, `Patch`, `Application`, `ClusterRegistration`
- [ ] Application subresources: `/sync`, `/drift-sync`, `/diff`, `/rollback`
- [ ] Internal resources: `SourceSyncRequest`, `DriftSyncRequest`

**Observability**
- [ ] Structured Kubernetes event emission from all controllers (required foundation for future notification controller)

### Out of Scope for PoC

- CLI / kubectl plugin
- Notification controller
- App grouping / templating (ApplicationSet-like)
- OCI registry source
- Webhook-based source triggering

---

## Future Development

### Phase 2 — Operational Completeness

**Notification Controller**
- Separate runnable subscribing to events from all controllers
- `NotificationConfig` resource defining destinations (Slack, webhook, PagerDuty) and event filters
- Designed as a standalone component — no changes required to existing controllers

**Source Enhancements**
- OCI registry source support
- Webhook receivers for push-based triggering (GitHub, GitLab, Harbor, etc.)
- Source composition — combine multiple sources into a single render

**CLI / kubectl Plugin**
- `kubectl kumo` plugin for common operations: list apps, trigger sync, view diff, rollback
- Standalone binary as an alternative distribution
- Both can coexist

### Phase 3 — Scale and UX

**App Grouping / Templating**
- Higher-level `ApplicationSet`-like resource
- Generates per-cluster `Application` objects from a template + cluster selector
- Enables fleet-wide deployments from a single definition
- Particularly valuable for multi-cluster setups

### Phase 4 — Ecosystem

**Additional Auth Providers**
- Certificate-based auth
- Token projection / bound service account tokens

**Additional Source Types**
- Flux-compatible `GitRepository` / `HelmRepository` source references (migration path)
- S3 / GCS as manifest sources

**UI**
- Web dashboard for application overview, sync history, diff viewer
- Read-only initially; write operations via CLI or kubectl
