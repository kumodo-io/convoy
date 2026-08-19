# Progressive Rollout & Failure Policy

> **Status: planned / design.** Convoy today applies a Release's render and reports health, but it does not *act* on a failed rollout: a bad update stays applied and unhealthy until a human rolls back (manually, via `/pin`). This document designs an opt-in top-level **`spec.failurePolicy`** that (1) **dry-runs** a render before applying it, so a change that can never apply (immutable-field edit, schema error, RBAC) is caught up front; (2) waits for the rollout to become healthy — per-wave for intermediate waves, and a whole-release **`deadline`** for the terminal wave; and (3) on failure, optionally **rolls back once** to the last-healthy render and stops. It builds on existing pieces: the observer's health assessment ([01](01-health-assessment.md)), the durable render store + rollback ([03](03-persistent-storage.md)), the sync-wave gate, and the `/sync` request mechanism.

## The two failure classes

A render can fail two fundamentally different ways; conflating them is what makes naive retry designs oscillate.

| Class | Example | Deterministic? | Response |
|-------|---------|----------------|----------|
| **Un-appliable** | immutable-field edit; schema/typo; RBAC/admission reject | yes — never applies | **dry-run** catches it → don't persist, don't apply. Nothing to roll back (cluster untouched). |
| **Applied-but-unhealthy** | bad image; expired external creds; dependency not up | no — may self-heal, cause often external | apply, wait (`deadline` / wave timeout), **roll back once** per policy, then stop. |

The first is caught **before** anything is stored or applied. The second is a genuine rollout that didn't converge — rolled back **once** and stopped, because convoy can't fix an external cause by re-applying. Re-driving is an explicit user action (`/sync --force`), never an automatic loop.

## Spec

```yaml
spec:
  failurePolicy:
    rollback: Never | OnWaveFailure | OnReleaseFailure   # what triggers a rollback
    deadline: 5m                                          # whole-release health budget (terminal/single wave)
```

`failurePolicy` is **optional**; absent ⇒ `rollback: Never` and no deadline gate (today's behaviour: apply, report health, never auto-rollback). Dry-run, however, is **always on** (see [the apply path](#the-apply-path)) — it is a correctness guard, not a policy knob.

- **`rollback`** — which failures trigger a one-shot rollback to `status.lastHealthy`:
  - `Never` — mark the rollout `Failed`, leave the failed render applied, do not roll back.
  - `OnReleaseFailure` — roll back only when the **terminal-wave `deadline`** verdict is unhealthy (the whole-release health check).
  - `OnWaveFailure` — roll back on **any** wave failure: an intermediate wave exceeding its `waveStrategy.timeout`, *or* the terminal-wave `deadline`. Fail-fast; `OnWaveFailure ⊇ OnReleaseFailure`.
- **`deadline`** (`Duration`) — the health budget for the **terminal** (last, or single) wave, measured from that wave's apply (`rollout.startedAt`). Must be **greater than `spec.healthAssessment.deadline`** (validated on create/update): the observer keeps a resource `Progressing` until *its* deadline elapses and only then declares `Degraded`, so a shorter release deadline would give up while the workload is still legitimately starting.

## Two timeouts, split by wave position

The tension between "per-wave timeout" and "whole-release timeout" resolves by **wave position** — each governs the waves the other cannot:

| Wave | Gated by | Rationale |
|------|----------|-----------|
| **Intermediate** (has a successor) | `spec.syncPolicy.waveStrategy.timeout` (existing) | Its health is a *progression* gate — wave N+1 must not apply until wave N is healthy. The wave timeout bounds that wait. |
| **Terminal** (last of many, or the single/only wave) | `spec.failurePolicy.deadline` (new) | Nothing waits *on* it, so its health isn't a progression gate — it is the *whole-release* verdict, and stamps `rollout.state`. A generous release budget, independent of the tighter per-wave timeout. |

So every wave is covered: intermediate waves by the wave timeout (progression), the terminal wave by the release deadline (the final verdict). A single-wave Release is just the degenerate case — its only wave is terminal, gated by `deadline`. This is also why the two timeouts can differ: fast per-stage gating during progression, a roomier deadline for the whole thing to settle.

> Note the terminal wave's health is **not** gated today — the sync-wave gate only makes wave N wait for wave N−1, so the last (or only) wave's health is never assessed for a verdict. `failurePolicy.deadline` closes that gap; it is where a single-Deployment bad-image update (the most common case) is actually caught.

## Status

```yaml
status:
  lastHealthy:
    renderRef: sha256:abc…      # last render that reached healthy — the rollback target
  rollout:
    state: Progressing | Succeeded | Failed | RolledBack
    renderRef: sha256:def…      # the render this rollout is about (the FAILED render on Failed/RolledBack)
    startedAt: 2026-08-17T09:00:00Z   # anchors the terminal-wave deadline clock
    message: "…"                # human reason on Failed/RolledBack (which resource stayed degraded; what it rolled back to)
```

- **`lastHealthy`** — a pointer with a required `renderRef`: **nil ⇒ no rollback target** (nothing has ever gone healthy); non-nil ⇒ its `renderRef` is guaranteed set and stored in the render store. Updated the moment a render reaches healthy, **independent of `failurePolicy`** — so a rollback target accrues even before the policy is enabled. On `RolledBack` it is also *what is now running*, so `rollout.renderRef` (the failed render) and `lastHealthy.renderRef` (the render rolled back to) together describe the rollback with no extra field.
- **`rollout`** — the state machine for the render being rolled out, keyed by `renderRef`. `startedAt` is set when the terminal wave applies and anchors the `deadline` clock. It is deliberately **separate from the live `Healthy` condition**: `rollout.state` is the *convergence outcome of a rollout*, not the *current health of the workload*. A `Succeeded` rollout whose workload later degrades stays `Succeeded`; a `RolledBack` rollout whose last-healthy target then also fails to run stays `RolledBack` while the live `Healthy` condition honestly reports `False` (see [Edge cases](#edge-cases)).

### Rollout states

```
Progressing ─ healthy within budget ─────────────────────────────▶ Succeeded    (terminal)
     │
     └─ unhealthy past wave-timeout / deadline ─▶ Failed
                                                   ├─ rollback = Never / no target ─▶ (terminal, bad render still applied)
                                                   └─ rollback applies last-healthy ─▶ RolledBack (terminal, good render running)
```

`Failed` and `RolledBack` are both **held** terminal states: the controller does not re-drive the failed render; a spec change (new `renderRef`) or `/sync --force` resets the machine. `Failed` means the bad render is still applied and unhealthy; `RolledBack` means it was reverted to `lastHealthy`. `RolledBack` is entered the moment the rollback render is **applied** — last-healthy is a known-good render applied idempotently, so it is not re-gated.

A release-level `RolloutSucceeded` **condition** mirrors `rollout.state` for `kubectl` visibility, alongside the existing `Applied`/`Healthy`/`InSync`: `True` for `Succeeded`, `False` for `Failed`/`RolledBack` (with the reason naming which), `Unknown` while `Progressing`.

## The apply path

`failurePolicy` reshapes the apply into: **dry-run → persist → apply waves → gate**.

1. **Render** the Release → compute `renderRef`.
2. **Dry-run apply** (SSA `DryRunAll` — the mechanism the observer already uses for `/diff`). It is **lenient about not-yet-existing types**: a resource whose kind isn't known yet (e.g. a CR whose CRD is in an earlier wave) is *skipped*, not failed — real apply, wave-ordered, creates it. Only definite rejections (immutable field on an existing object, schema validation on a known type, RBAC/admission) fail. On failure → **do not persist, do not apply**; report the `un-appliable` error (retryable — a spec fix resolves it; the cluster is untouched, so nothing to roll back). This class never reaches the store, so it can't orphan a blob or evict history.
3. **Persist** the render (write-once, [03](03-persistent-storage.md#the-write-path--persist-before-apply-invariant)).
4. **Apply waves** in order. Intermediate waves gate on the preceding wave's health per `waveStrategy` (existing). An intermediate wave that exceeds `waveStrategy.timeout` → a **wave failure**.
5. **Record history** (first-deploy of a distinct render) once the terminal wave applies, and enter the **release gate**.

## The release gate

When the **terminal** wave applies, the controller opens the release gate:

- Set `rollout = {state: Progressing, renderRef: R, startedAt: now}` and **requeue** (the controller polls health on each reconcile like the wave gate; it never blocks a worker). `startedAt` persists.
- On each reconcile while `Progressing`:
  - **Release healthy** (`Applied=True` and `Healthy=True`) → `rollout.state = Succeeded`, `lastHealthy.renderRef = R`. Terminal.
  - **`now − startedAt > deadline`** and still unhealthy → **release failure** → `rollout.state = Failed`, then apply the rollback policy (below), which may advance it to `RolledBack`.
  - Otherwise → still converging; requeue.

Intermediate wave failures (step 4) are the *other* entry into the failure path — surfaced during apply, before the release gate opens. Either failure (wave or release) runs the rollback policy below.

Health is read from the same observer-written rollup on the `ReleaseInventory` the wave gate already uses — no new health machinery.

## Rollback (one-shot)

On a failure whose trigger the policy selects (`OnWaveFailure`: any wave or release failure; `OnReleaseFailure`: release failure only) **and** `lastHealthy` is set:

- Apply `lastHealthy.renderRef`'s stored render **once** ([03 rollback path](03-persistent-storage.md#object-layout--keying) — fetch the blob, apply it).
- Set `rollout.state = RolledBack` with a `message` (the desired render R failed; the cluster now holds the last-good render `lastHealthy.renderRef`).
- **Stop.** The controller does not re-apply R. No history entry for the rollback (that render is already in the log — history is a set of distinct renders). No second attempt, no good↔bad flapping.

`rollback: Never`, or a failure the policy doesn't select, or **no `lastHealthy`** (e.g. the first-ever deploy fails, nothing to fall back to) → the rollout stays at `rollout.state = Failed`, the failed render is left applied, stop.

## The three sync modes

"Give up" means the **automatic** reconcile loop stops re-driving a render that ended `Failed` or `RolledBack` — not that convoy refuses all retries.

| Trigger | Dry-run | On a `Failed` / `RolledBack` render |
|---------|---------|-------------------------------------|
| Controller auto-reconcile | yes | **holds** — does not re-apply |
| `/sync` | yes | **holds** |
| `/sync?force=true` | **bypassed** | **clears the held state**, applies directly |

A `Failed` / `RolledBack` render is held until the **spec changes** (new `renderRef` resets the state machine) or **`/sync --force`** is issued. Plain `/sync` is the normal reconcile-request path and does not override a held render.

### Implementing force

`/sync` only stamps an annotation that wakes the controller; the apply, dry-run-bypass, and held-state (`Failed`/`RolledBack`) clear happen in the **controller** (a separate process). So force is communicated **through the object**, mirroring the existing `SyncRequestedAtAnnotation` / `status.lastHandledSyncAt` pair one-to-one:

- **New annotation** `convoy.kumodo.io/sync-force-requested-at` + **new status field** `status.lastHandledForceSyncAt`.
- The `/sync` `Connect` handler reads `force` from its request (`req.URL.Query().Get("force")`); when set it stamps **both** the normal annotation (to wake the controller as today) and the force annotation (to mark intent).
- The controller's `pendingForceSync(rel)` (twin of `pendingSync`) reports a new force request when the force annotation's time is newer than `status.lastHandledForceSyncAt`. On such a reconcile it **clears the held `Failed`/`RolledBack` state** and **skips dry-run**, then records the force time (one-shot).
- CLI: `convoy release sync --force` → `POST /sync?force=true`.

Force bypasses dry-run safely: the real apply is still the gate (SSA rejects a genuinely-invalid manifest); it only skips the pre-flight — useful when the dry-run itself is failing transiently (a webhook is down) or the user simply wants to push through.

## Edge cases

- **No health configured.** With no matching `HealthRuleSet`, the observer reports `Healthy=True` (`reason: NotConfigured`). The release gate therefore passes **immediately** — `rollout` goes straight to `Succeeded`, no rollback. `failurePolicy` is **inert-by-passing** when there is nothing to measure (you cannot gate on health you do not assess).
- **No rollback target** (`lastHealthy == nil`) — e.g. the first deploy of a Release fails. Any `rollback` mode falls through to "mark `Failed`, leave applied, stop" — there is no earlier good state.
- **A converged release later degrades** (creds expire on a healthy, unchanged Release). Does **not** trigger a rollback: the gate runs only during a rollout (a new render converging), and this render's rollout is already `Succeeded`. Doubly safe — even if it did, `lastHealthy.renderRef == the running render`, so a rollback is a no-op.
- **Wave strategy interaction.** `OnWaveFailure`'s intermediate-wave rollback only fires when intermediate waves are health-gated *with a timeout* (`waveStrategy: waitForHealthyWithTimeout`); under `waitForHealthy` (unbounded) an intermediate wave holds indefinitely with no `WaveFailed`, as today. The terminal-wave `deadline` applies under any strategy. `waitForApplied` (no health gating) leaves intermediate waves ungated but still applies the release `deadline` to the terminal wave.
- **User `/pin`.** An explicit pin is the user's deliberate override and takes precedence: while pinned, the controller applies the pinned render and `failurePolicy` does not open a rollout against a source render the user has overridden.
- **Manual sync mode** (`syncPolicy.automatic=false`). The apply (and thus the rollout) happens only when a `/sync` is pending; once applied, the gate + policy run identically. A `Failed`/`RolledBack` render is re-driven only by `/sync --force` or a spec change.

## What this reuses vs. adds

**Reuses:** the observer's health assessment + per-resource rollup on `ReleaseInventory` ([01](01-health-assessment.md)); SSA `DryRunAll` (observer diff path); the render store + rollback-apply ([03](03-persistent-storage.md)); the sync-wave gate + `status.waves[].StartedAt`; the `/sync` annotation + `lastHandledSyncAt` request-tracking.

**Adds:** top-level `spec.failurePolicy{rollback, deadline}` + the `deadline > healthAssessment.deadline` validation; `status.lastHealthy` + `status.rollout` (states `Progressing`/`Succeeded`/`Failed`/`RolledBack`) + a `RolloutSucceeded` condition; the always-on dry-run step + `Deployer.DryRun` (lenient on unknown types); the terminal-wave release gate; the `sync-force-requested-at` annotation + `lastHandledForceSyncAt`.

## Build items

1. **API** — `spec.failurePolicy{rollback enum, deadline}`; `status.lastHealthy`, `status.rollout` (states `Progressing`/`Succeeded`/`Failed`/`RolledBack`); the `RolloutSucceeded` condition; validation `deadline > healthAssessment.deadline`; codegen. `lastHealthy` is written whenever a render reaches healthy (independent of `failurePolicy`).
2. **Dry-run** — `Deployer.DryRun` (SSA `DryRunAll`, lenient on not-yet-existing types); the pre-persist dry-run step; wire the `un-appliable` failure to a clear condition/reason.
3. **Release gate + state machine** — open the rollout when the terminal wave applies; `Progressing → Succeeded` / `→ Failed` on the deadline; requeue cadence; terminal-hold on `Failed`/`RolledBack`.
4. **Rollback** — one-shot apply of `lastHealthy` per the `rollback` enum (wave vs release failure), transitioning `Failed → RolledBack`; resting at `Failed` for `Never` / no-target.
5. **Force sync** — `sync-force-requested-at` + `lastHandledForceSyncAt`; `/sync?force` handler; `pendingForceSync`; CLI `--force`.

## Open decisions

- **`deadline` default when `failurePolicy` is set but `deadline` omitted** — require it, or default (e.g. to `healthAssessment.deadline × k`)? Leaning: require it when `rollback != Never` (a rollback needs a verdict clock), optional/ignored for `Never`.
