# Progressive Rollout & Failure Policy

> **Status: planned / design.** Convoy today applies a Release's render and reports health, but it does not *gate* a rollout on health or act on a failed one: a bad update stays applied and unhealthy until a human notices and rolls back (manually, via `/pin`). This document designs an **opt-in `spec.failurePolicy`** that (1) validates a render with a **dry-run** before applying it, (2) waits a bounded `timeout` for the new render to become healthy, and (3) on timeout, optionally **rolls back once** to the last-healthy render and stops — no retry storms, no good↔bad flapping. It builds directly on existing pieces: the observer's health assessment ([01](01-health-assessment.md)), the durable render store + rollback ([03](03-persistent-storage.md)), and the sync-wave gate's health-timed wait (Release sync-wave strategy).

## The two failure classes (the core idea)

A render can fail for two fundamentally different reasons, and conflating them is what makes naive "retry N times" designs oscillate:

| Class | Example | Deterministic? | Right response |
|-------|---------|----------------|----------------|
| **Un-appliable** | edit an immutable field; schema/typo; RBAC or admission rejects it | yes — it will *never* apply | **don't apply.** A dry-run catches it up front. |
| **Applied-but-unhealthy** | image doesn't exist; expired external creds; a dependency isn't up yet | no — may self-heal, cause is often outside convoy | **apply, wait, roll back once** if it doesn't converge. |

The first is caught **before** anything is stored or applied. The second is a genuine rollout that didn't converge — convoy rolls back **once** to the last-known-good render and stops, because repeatedly re-applying a render whose failure convoy can't fix just flaps the workload. Fixing the external cause and re-driving is an explicit user action (`/sync --force`), not an automatic loop.

## Spec

```yaml
spec:
  failurePolicy:
    timeout: 10s      # how long to wait for the new render to become healthy
    rollback: true    # on timeout-still-unhealthy, roll back to lastHealthy once
```

`failurePolicy` is **optional**; absent, rollouts behave exactly as today (apply, report health, never auto-rollback). Both fields are required when the block is present.

- **`timeout`** — the health-convergence budget, measured from the new render's first apply. Must be **greater than `spec.healthAssessment.deadline`** (validated on create/update): the observer keeps a resource `Progressing` until its own `deadline` elapses and only then declares it `Degraded`, so a rollout `timeout` shorter than `deadline` would give up while the workload is still legitimately starting. See [Health gate](#the-health-gate).
- **`rollback`** — `true`: on timeout, apply `status.lastHealthy.renderRef` once (if one exists) and mark the rollout `Failed`. `false`: leave the failed render applied and mark `Failed` (so it can be inspected). Either way the controller **stops** driving that render.

## Status

```yaml
status:
  lastHealthy:
    renderRef: sha256:abc…      # the last render that reached healthy; the rollback target
  rollout:
    state: Progressing | Succeeded | Failed
    renderRef: sha256:def…      # the render this rollout is about
    startedAt: 2026-08-17T09:00:00Z
```

- **`lastHealthy`** is a pointer with a required `renderRef`: **nil ⇒ no rollback target exists** (nothing has ever gone healthy), so `rollback: true` has nothing to fall back to (see [Edge cases](#edge-cases)); non-nil ⇒ its `renderRef` is guaranteed set and is stored in the render store. It is updated the moment a render reaches healthy.
- **`rollout`** is the state machine for the render currently being rolled out, keyed by its `renderRef`. `startedAt` anchors the `timeout` clock. **This is deliberately separate from the live `Healthy` condition**: `rollout.state` records the *convergence outcome of a rollout*, not the *current health of the workload*. A `Succeeded` rollout whose workload later degrades stays `Succeeded` (the observer's `Healthy` condition flips, but no new rollout is in flight — see [Edge cases](#edge-cases)).

Optionally, a release-level `Progressing`/`Failed` condition mirrors `rollout.state` for `kubectl` visibility; `rollout` is the source of truth.

## The apply path (dry-run before persist)

`failurePolicy` inserts a dry-run ahead of the existing [persist-then-apply](03-persistent-storage.md#the-write-path--persist-before-apply-invariant) invariant:

1. **Render** the Release → compute `renderRef`.
2. **Dry-run apply** (SSA with `DryRunAll`, the same mechanism the observer already uses for `/diff`). If it fails → **do not persist, do not apply**; report the error (retryable — a spec fix or a transient recovery resolves it). This is the *un-appliable* class, caught here.
3. **Persist** the render (write-once).
4. **Apply** for real.
5. **Record history** (first-deploy of a distinct render) and enter the health gate.

A useful consequence: because dry-run precedes persist, a render that can never apply is **never stored** — no orphan blob, cleaner than gating only at the real apply.

## The health gate

After a real apply of a *new* render `R` (`R ≠ rollout.renderRef` for a terminal rollout, and `R ≠ lastHealthy.renderRef`), the controller opens a rollout:

- Set `rollout = {state: Progressing, renderRef: R, startedAt: now}` and **requeue**. The controller does not block a worker waiting for health; it re-checks on each reconcile (informer events + the poll interval), exactly like the sync-wave gate.
- On each reconcile while `Progressing`:
  - **Release healthy** (`Healthy=True`) → `rollout.state = Succeeded`, `lastHealthy.renderRef = R`. Terminal.
  - **`now - startedAt > timeout`** and still not healthy → the rollout **failed**: run the rollback policy below, set `rollout.state = Failed`. Terminal.
  - Otherwise → still converging; requeue.

Health is read from the same rollup the sync-wave gate uses (the observer-written per-resource health on the `ReleaseInventory`), so the gate needs no new health machinery.

## Rollback (one-shot)

When a rollout fails and `rollback: true` **and** `lastHealthy` is set:

- Apply `lastHealthy.renderRef`'s stored render (the [rollback path](03-persistent-storage.md#object-layout--keying) — fetch the blob, apply it), **once**.
- Set `rollout.state = Failed` (the *desired* render R failed; the cluster now holds the last-good render).
- **Stop.** The controller does not re-apply R. The live state is the last-healthy render (healthy); `spec` still asks for R, but R is `Failed` and held.

No history entry is written for the rollback (that render is already in the log — history is a set of distinct renders; the rollback is audited via `rollout` + `status.pin` semantics if a pin is involved). No second attempt, no flapping.

## The three sync modes

"Give up" means the **automatic** reconcile loop stops re-driving a `Failed` render — not that convoy refuses all retries. A user can always re-drive explicitly.

| Trigger | Dry-run | On a `Failed` render |
|---------|---------|----------------------|
| Controller auto-reconcile | yes | **holds** — does not re-apply |
| `/sync` | yes | **holds** — wakes the controller, which still holds |
| `/sync?force=true` | **bypassed** | **clears `Failed`**, applies directly |

`Failed` is terminal for a `renderRef` until either the **spec changes** (new `renderRef` resets the state machine) or **`/sync --force`** is issued. Plain `/sync` intentionally does not override `Failed`: it is the normal reconcile-request path.

### Implementing force

`/sync` only stamps an annotation that wakes the controller; the apply, dry-run-bypass, and `Failed`-clear all happen in the **controller** (a separate process). So force is communicated **through the object**, mirroring the existing `SyncRequestedAtAnnotation` / `status.lastHandledSyncAt` pair one-to-one:

- **New annotation** `convoy.kumodo.io/sync-force-requested-at` + **new status field** `status.lastHandledForceSyncAt`.
- The `/sync` subresource's `Connect` handler reads `force` from its request (`req.URL.Query().Get("force")`); when set it stamps **both** the normal annotation (to wake the controller as today) and the force annotation (to mark intent).
- The controller's `pendingForceSync(rel)` (a twin of the existing `pendingSync`) reports a new force request when the force annotation's time is newer than `status.lastHandledForceSyncAt`. On such a reconcile it **clears `rollout.state = Failed`** and **skips the dry-run**, then records the force time in status so it is one-shot.
- CLI: `convoy release sync --force` → `POST /sync?force=true`.

Force bypasses dry-run safely: the real apply is still the real gate (SSA rejects a genuinely-invalid manifest), so force only skips the pre-flight — useful when the dry-run itself is failing transiently (e.g. a webhook is down) or the user simply wants to push through.

## Edge cases

- **No health configured.** With no matching `HealthRuleSet`, the observer reports `Healthy=True` (`reason: NotConfigured`). The gate therefore passes **immediately** on the first check — `rollout` goes straight to `Succeeded`, no rollback. `failurePolicy` is thus **inert-by-passing** when there is nothing to measure; it never spuriously fails a rollout. (You cannot gate on health you do not assess.)
- **No rollback target** (`lastHealthy == nil`) — e.g. the very first deploy of a Release fails. `rollback: true` has nothing to fall back to: mark `Failed`, leave the failed render applied (there is no earlier good state), and stop. Same resting state as `rollback: false` for this case.
- **A converged release later degrades** (e.g. creds expire on a healthy, unchanged Release). This does **not** trigger a rollback: the gate runs only during a *rollout* (a new render converging), and this render's rollout is already `Succeeded` — the observer's `Healthy` condition flips, but no new rollout opens. Doubly safe: even if it did, `lastHealthy.renderRef == the running render`, so a "rollback" would re-apply the same render (a no-op).
- **Manual sync mode** (`syncPolicy.automatic=false`). The apply (and thus the rollout) happens only when a `/sync` is pending, as today; once applied, the health gate + failure policy run identically. A `Failed` render in manual mode is re-driven only by `/sync --force` or a spec change, consistent with automatic mode.
- **User `/pin` interaction.** An explicit pin is a user's deliberate choice and takes precedence: while pinned, the controller applies the pinned render (freeze/rollback) and `failurePolicy` does not open a rollout against a source render the user has overridden.
- **Sync waves.** The per-wave `waitForHealthy` gate orders resources *within* a render; `failurePolicy` gates the *whole render's* convergence. They compose: a render whose waves never finish (a wave's health bar never clears) is simply still-`Progressing` to the rollout gate and will hit `timeout` like any other non-convergence.

## What this reuses vs. adds

**Reuses:** the observer's health assessment and per-resource rollup on `ReleaseInventory` ([01](01-health-assessment.md)); SSA `DryRunAll` (already in the observer's diff path); the render store + rollback-apply ([03](03-persistent-storage.md)); the `/sync` annotation + `lastHandledSyncAt` request-tracking; the sync-wave gate's "apply, requeue, poll-health-until-timeout" pattern.

**Adds:** `spec.failurePolicy`; `status.lastHealthy` + `status.rollout`; a dry-run step + `Deployer.DryRun`; the `sync-force-requested-at` annotation + `lastHandledForceSyncAt`; the `timeout > healthAssessment.deadline` validation.

## Build items

1. **API** — `spec.failurePolicy{timeout, rollback}`, `status.lastHealthy`, `status.rollout`; validation `timeout > healthAssessment.deadline`; codegen. `status.lastHealthy` is written whenever a render reaches healthy (independent of `failurePolicy`, so a rollback target accrues even before the policy is enabled).
2. **Dry-run** — `Deployer.DryRun` (SSA `DryRunAll`); insert the pre-persist dry-run step; wire the `un-appliable` failure to a clear condition/reason.
3. **Rollout state machine** — open a rollout on a new render's apply; the `Progressing → Succeeded/Failed` transitions on the health gate; requeue cadence; terminal-hold on `Failed`.
4. **Rollback** — one-shot apply of `lastHealthy` on `rollback: true`; resting states for both `rollback` values and the no-target case.
5. **Force sync** — the `sync-force-requested-at` annotation + `lastHandledForceSyncAt`; `/sync?force` handler; `pendingForceSync`; CLI `--force`.

## Open decisions

- **Release-level `Progressing`/`Failed` condition** — mirror `rollout.state` as a `metav1.Condition` for `kubectl` visibility, or keep `rollout` the sole signal? (Leaning: add the condition; it is cheap and matches how `Applied`/`Healthy`/`InSync` already surface.)
- **`rollout.message`** — carry a human reason on `Failed` (which resource stayed degraded, or the dry-run error)? Useful, small; likely yes.
