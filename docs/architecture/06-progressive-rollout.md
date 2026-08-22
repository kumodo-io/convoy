# Progressive Rollout & Failure Policy

> **Status: implemented, with a gate redesign in progress.** Convoy applies a Release's render and reports health; `spec.failurePolicy` makes it (1) **dry-run** a render before applying it, so a change that can never apply (immutable-field edit, schema error, RBAC) is caught up front; (2) wait for the render to become healthy; and (3) on failure, optionally **roll back once** to the last-healthy render. It builds on the observer's health assessment ([01](01-health-assessment.md)), the durable render store + rollback ([03](03-persistent-storage.md)), the sync-wave gate, and the `/sync` request mechanism.
>
> **The rollout is a single-transition-per-reconcile state machine** ([The rollout gate](#the-rollout-gate)). Each reconcile reads the *persisted* rollout state, advances it by **at most one** transition, applies the render that state implies, and stamps — so every state (`Progressing`/`Succeeded`/`Failed`/`RolledBack`) is observable in `/get` and no run collapses several transitions into one. Failure is decided on *positive evidence* (a resource not passing) measured against a **controller-owned deadline** from `status.rollout.startedAt` — not the observer's per-resource clock — so every render (including a rollback-forward to a good image) gets a fresh deadline by construction, with no cross-writer race and no false rollback during an outage (no verdict → keep waiting).

## The two failure classes

A render can fail two fundamentally different ways; conflating them is what makes naive retry designs oscillate.

| Class | Example | Deterministic? | Response |
|-------|---------|----------------|----------|
| **Un-appliable** | immutable-field edit; schema/typo; RBAC/admission reject | yes — never applies | **dry-run** catches it → don't persist, don't apply. Nothing to roll back (cluster untouched). |
| **Applied-but-unhealthy** | bad image; expired external creds; dependency not up | no — may self-heal, cause often external | apply, wait for the observer's verdict, **roll back once** per policy if it goes `Degraded`. |

The first is caught **before** anything is stored or applied. The second is a genuine rollout that didn't converge — rolled back **once** and stopped, because convoy can't fix an external cause by re-applying. Re-driving is an explicit user action (`/sync --force`), never an automatic loop.

## Spec

```yaml
spec:
  failurePolicy:
    rollback: Never | OnWaveFailure | OnReleaseFailure   # what triggers a rollback
```

`failurePolicy` is **optional**; absent ⇒ `rollback: Never` (today's behaviour: apply, report health, never auto-rollback). Dry-run, however, is **always on** (see [the apply path](#the-apply-path)) — it is a correctness guard, not a policy knob.

- **`rollback`** — which failures trigger a one-shot rollback to `status.lastHealthy`:
  - `Never` — mark the rollout `Failed`, leave the failed render applied, do not roll back.
  - `OnReleaseFailure` — roll back only when the **whole-release** health verdict is `Degraded` (the terminal/only wave went unhealthy).
  - `OnWaveFailure` — roll back on **any** wave failure: an intermediate wave failing its `waveStrategy.timeout`, *or* the release verdict going `Degraded`. Fail-fast; `OnWaveFailure ⊇ OnReleaseFailure`.

There is **no `failurePolicy.deadline` field.** "How long to wait for healthy" is `spec.healthAssessment.deadline` — but the **controller** applies it, measured from `status.rollout.startedAt` (when this rollout entered `Progressing`), not the observer's per-resource clock. Owning the clock in the controller is deliberate: `startedAt` is stamped fresh whenever a render becomes the active rollout, so every render — including a rollback-forward to a good image — gets its own deadline by construction, with no cross-writer race between the controller's applied render and the observer's per-resource anchor.

Because a health failure is the only thing that fails a rollout, and it only fails once `healthAssessment.deadline` elapses, **`rollback` other than `Never` requires `spec.healthAssessment.deadline`** — validation rejects the Release otherwise. Without it a failing rollout would wait forever and the policy could never act. (`rollback: Never` needs no health — there is nothing to fail.)

## The health verdict

The controller derives a per-render **verdict** (`Healthy` / `Failed` / `Pending`) from the per-resource health the observer records on the `ReleaseInventory`:

- **`State`** ∈ `Healthy` / `Progressing` / `Degraded` / `Unknown` — the observer's *instantaneous* per-resource assessment. The controller reads `Healthy` as "passing" and `Progressing`/`Degraded` as "not passing" (`Unknown` = can't assess). The observer's own `Progressing→Degraded` deadline is now only for the `Healthy` *condition* display, not the rollout.
- **`AssessedRenderRef`** — the render that assessment is *about*. Health counts for render `R` only when `AssessedRenderRef == R`; otherwise it is stale (a previous render) and ignored.

The verdict, measured from the rollout's `startedAt`:

- **`Healthy`** — every resource assessed for `R` is passing.
- **`Failed`** — some resource assessed for `R` is definitively not passing (`Progressing`/`Degraded`, not `Unknown`) **and** `now - startedAt > healthAssessment.deadline`.
- **`Pending`** — otherwise: still converging, within the deadline, or a resource is `Unknown`/unassessed.

So it acts only on *positive evidence within the render's own deadline*: during an apiserver/observer outage there is no fresh passing/failing signal → the verdict stays `Pending` → the gate **waits** (no false rollback), and resolves once the observer recovers.

`(State, AssessedRenderRef)` carries everything the decision needs — no assessment-timestamp field is required. (A "last assessed at" stamp would only serve observability, not the decision, and is out of scope.)

## Status

```yaml
status:
  lastHealthy:
    renderRef: sha256:abc…      # last render that reached healthy — the rollback target
  rollout:
    state: Progressing | Succeeded | Failed | RolledBack
    renderRef: sha256:def…      # the render this rollout is about (the FAILED render on Failed/RolledBack)
    startedAt: 2026-08-17T09:00:00Z
    message: "…"                # human reason on Failed/RolledBack (which resource degraded; what it rolled back to)
```

- **`lastHealthy`** — a pointer with a required `renderRef`: **nil ⇒ no rollback target** (nothing has ever gone healthy); non-nil ⇒ its `renderRef` is stored in the render store. Updated the moment a render is confirmed healthy (all resources `Healthy` for that render), **independent of `failurePolicy`** — a rollback target accrues even before the policy is enabled.
- **`rollout`** — the state machine for the render being rolled out, keyed by `renderRef`. **`Progressing` is the only *active* state**; the three others are terminal *for that render* (they don't re-open until a spec change or `/sync --force`). A spec change opens the *new* render's rollout **immediately** — as soon as that render becomes the active one, even before its apply completes and even if the apply holds mid-wave — so the state never lingers on the superseded render's terminal outcome. `rollout.state` is the *convergence outcome of a rollout* — deliberately **not** a live health mirror.

### Rollout states

```
Progressing ─ verdict Healthy (for R) ──────────────────────────▶ Succeeded    (terminal for R)
     │
     └─ verdict Degraded (for R), or an intermediate wave fails ─▶  failure
                                    ├─ rollback = Never / no target ─▶ Failed      (render R left applied)
                                    └─ rollback applies last-healthy ─▶ RolledBack (last-healthy running)
```

**Terminal ≠ frozen.** A terminal rollout stops convoy *re-driving the failed render* — it does **not** freeze the Release's other status. The controller keeps reconciling the render that is *actually running*, so `Applied`/`InSync`/`Healthy` stay live:

| Terminal state | Cluster runs | `Applied` | `InSync` / `Healthy` reflect | Recovery |
|----------------|--------------|-----------|------------------------------|----------|
| `Succeeded` | render `R` | `True` | render `R` (steady state) | — |
| `Failed` | render `R` (unhealthy, applied) | `True` | render `R` (`Healthy=False`) | spec change, `/sync --force`, or `R` self-heals → `Succeeded` |
| `RolledBack` | `lastHealthy` | `True` | **`lastHealthy`** (healthy) | spec change or `/sync --force` |

So a successful rollback reads coherently: `Applied=True` (last-healthy *is* applied), `InSync=True`, `Healthy=True`, and `rollout.state=RolledBack` records *why* it is on last-healthy. This requires (a) the controller to keep applying the running render while terminal — `lastHealthy` when `RolledBack`, `R` when `Failed` — and (b) the observer to be **rollout-aware**: while `RolledBack`, the *desired* render for drift/health is `lastHealthy`, not the spec's failed render (mirroring the existing `/pin` awareness in [03](03-persistent-storage.md)).

A release-level `RolloutSucceeded` **condition** mirrors `rollout.state` for `kubectl` visibility, alongside `Applied`/`Healthy`/`InSync`: `True` for `Succeeded`, `False` for `Failed`/`RolledBack` (reason naming which), `Unknown` while `Progressing`.

## The apply path

`failurePolicy` reshapes the apply into: **dry-run → persist → apply waves → gate**.

1. **Render** the Release → compute `renderRef`.
2. **Dry-run apply** (SSA `DryRunAll` — the mechanism the observer uses for `/diff`). **Lenient about not-yet-existing types**: a kind with no REST mapping yet (e.g. a CR whose CRD is in an earlier wave) is *skipped*, not failed. Only definite rejections (immutable field, schema on a known type, RBAC/admission) fail → **do not persist, do not apply**; report the `un-appliable` error (terminal; a spec fix or `/sync --force` resolves it; the cluster is untouched, so nothing to roll back). Never reaches the store, so it can't orphan a blob.
3. **Persist** the render (write-once, [03](03-persistent-storage.md#the-write-path--persist-before-apply-invariant)).
4. **Apply waves** in order. Intermediate waves gate on the preceding wave's health per `waveStrategy` (existing). An intermediate wave that exceeds `waveStrategy.timeout` → a **wave failure**.
5. **Record history** (first-deploy of a distinct render) once the terminal wave applies, and open/advance the **rollout gate**.

## The rollout gate (single transition per reconcile)

The rollout is an explicit state machine advanced **one transition per reconcile**. Each reconcile: render → read the *persisted* `status.rollout` → compute the single next state + which render to apply → apply it → stamp. It never chains transitions in a single run, so `/get` observes every state, and each decision is made against persisted state (never in-memory reasoning that raced). The transition table, from persisted state + the verdict for `R` (measured from the rollout's own `startedAt`):

| persisted | condition | next | apply |
|-----------|-----------|------|-------|
| none / `forced` / render changed | — | `Progressing{R}` (fresh `startedAt`) | source `R` |
| `Progressing{R}` | verdict `Healthy` | `Succeeded{R}` | source `R` |
| `Progressing{R}` | verdict `Failed`, policy rolls back | `RolledBack{R}` | last-healthy |
| `Progressing{R}` | verdict `Failed`, no rollback | `Failed{R}` | source `R` |
| `Progressing{R}` | verdict `Pending` | `Progressing{R}` | source `R` (wait) |
| `Succeeded{R}` | — | `Succeeded{R}` | source `R` (sticky) |
| `Failed{R}` | verdict `Healthy` | `Succeeded{R}` (self-heal) | source `R` |
| `Failed{R}` | otherwise | `Failed{R}` | source `R` (one-shot) |
| `RolledBack{R}` | — | `RolledBack{R}` | last-healthy (hold) |

A spec change / first deploy always yields `Progressing` (never a verdict-driven terminal in the *same* reconcile), so a verdict carried over from a superseded render can never flash a premature `Succeeded`/`Failed` — the verdict is only applied on a later reconcile once `R` is the active rollout.

> **Maintainer note — two decision sites.** `planRollout` is the single source of truth for every *health-driven* transition, decided **pre-apply** from the persisted state. There is exactly **one** other place a rollout transition is decided: the **intermediate-wave-timeout** in `waveHealthGate`, inside the wave loop. It is unavoidably **apply-time** — a wave timeout is only observable while walking the wave loop (you learn wave N-1 didn't go healthy within its timeout only when applying wave N), a fact `planRollout` does not have before the apply. When it fires it sets `outcome.rollout` directly, and `reconcile` prefers that over the plan (`if out.rollout == nil { out.rollout = plan.rollout }`); it routes through the same primitives (`applyLastHealthy` for the rollback, the same `Failed{…}` shape, `priorRolloutStartedAt` to keep the rollout's clock), so behaviour stays consistent with the table above — it rolls back under `OnWaveFailure`, else `Failed`. **If you change rollout transition logic, check both sites.** This could be folded into `planRollout` by computing the wave-timeout condition pre-apply (group the rendered objects into waves, check each bounded wave's preceding-wave health + timeout against `status.waves` `startedAt`) and reducing `waveHealthGate` to a pure hold; it was left separate to avoid duplicating the wave-grouping + health-rollup that already lives in the wave loop.

**Prompt confirmation.** The observer writes health to the RI; the controller **wakes on RI health changes** (`inventory.HealthChanged`) so it advances within a watch latency rather than a full poll. `RolledBack` holds by re-applying last-healthy each reconcile (`Applied` stays true); if last-healthy can't be loaded (a transient store error) the controller **holds** — it never falls through to re-apply the failed render.

## Rollback (one-shot)

On a failure the policy selects (`OnWaveFailure`: any wave or release failure; `OnReleaseFailure`: release failure only) **and** `lastHealthy` is set:

- Apply `lastHealthy.renderRef`'s stored render ([03 rollback path](03-persistent-storage.md#object-layout--keying) — fetch the blob, apply it, prune what the failed render added).
- Set `rollout.state = RolledBack` with a `message`; `lastHealthy.renderRef` is now the running render.
- **Stop re-driving `R`.** No history entry (that render is already in the log). While `RolledBack`, the controller keeps applying `lastHealthy` (self-heal, `Applied=True`) and the observer assesses `lastHealthy` — so the release reads healthy and in-sync, not broken.

`rollback: Never`, a failure the policy doesn't select, or **no `lastHealthy`** (e.g. the first-ever deploy fails) → the rollout rests at `Failed`, the failed render left applied. If that render later self-heals to `Healthy` for `R`, the gate advances it to `Succeeded` (it reflects the current verdict — no rollback happened, `R` is the spec render).

## The three sync modes

"Give up" means the **automatic** loop stops re-driving a render that ended `Failed` or `RolledBack` — not that convoy refuses all retries.

| Trigger | Dry-run | On a `Failed` / `RolledBack` render |
|---------|---------|-------------------------------------|
| Controller auto-reconcile | yes | keeps the running render applied (`Failed`→re-applies `R`; `RolledBack`→re-applies `lastHealthy`); does not re-open the rollout |
| `/sync` | yes | same as auto-reconcile |
| `/sync?force=true` | **bypassed** | **flushes** the terminal state → re-applies `R`, fresh `Progressing` |

A `Failed`/`RolledBack` render is re-driven only by a **spec change** (new `renderRef` resets the machine) or **`/sync --force`**. A forced sync flushes the rollout to a fresh `Progressing` even if the re-drive holds mid-wave (so a stale `RolledBack` is never left frozen), then the verdict gate re-evaluates.

### Implementing force

`/sync` only stamps an annotation that wakes the controller; the apply, dry-run-bypass, and terminal-state flush happen in the **controller**. Force is communicated **through the object**, mirroring the existing `SyncRequestedAtAnnotation` / `status.lastHandledSyncAt` pair:

- **Annotation** `convoy.kumodo.io/sync-force-requested-at` + **status field** `status.lastHandledForceSyncAt`.
- The `/sync` `Connect` handler reads `?force=true` and stamps **both** the normal annotation (wake) and the force annotation (intent).
- The controller's `pendingForceSync(rel)` (twin of `pendingSync`) fires when the force annotation is newer than `status.lastHandledForceSyncAt`; on such a reconcile it **flushes the terminal rollout** and **skips dry-run**, then records the force time (one-shot).
- CLI: `convoy release sync --force`.

Force bypasses dry-run safely: the real apply still gates (SSA rejects an invalid manifest); it only skips the pre-flight — useful when the dry-run itself is failing transiently (a webhook is down) or the user wants to push through.

## Edge cases

- **No health configured.** With `rollback: Never` (or no `failurePolicy`) there is no verdict ⇒ the gate can never fail; a render is `Succeeded` once applied — the inert, backward-compatible case. With `rollback` other than `Never`, a missing `healthAssessment.deadline` is a **validation error** (see [Spec](#spec)): the policy could never fire, so it is rejected rather than silently ignored.
- **A resource that never appears.** A resource that is persistently `NotFound` or unreadable stays `Unknown` — the observer only escalates to `Degraded` from a *live read that fails its rule*, never from absence. So a rollout whose resource never materializes waits (never a false rollback), rather than failing. This is inherent to the health model (absence is genuinely ambiguous: "still coming" vs "will never come"), not specific to rollout; `/sync --force` or a spec edit is the escape hatch.
- **Apiserver / observer outage.** The observer stops writing health; the verdict stays `Progressing`/stale-for-`R`; the gate **waits** and resolves when the observer recovers. This is the bug this redesign fixes — a blind deadline used to roll back here.
- **No rollback target** (`lastHealthy == nil`) — first deploy fails. Any `rollback` mode rests at `Failed` (no earlier good state).
- **A converged release later degrades** (creds expire on a healthy, unchanged Release). Does **not** roll back: the gate is active only while `Progressing`; this render is already `Succeeded`. Even if it re-opened, `lastHealthy == the running render`, so a rollback is a no-op.
- **Wave strategy interaction.** `OnWaveFailure`'s intermediate-wave rollback fires only when intermediate waves are health-gated *with a timeout* (`waveStrategy: waitForHealthyWithTimeout`); under `waitForHealthy` (unbounded) an intermediate wave holds indefinitely, as today. The release verdict applies under any strategy that produces per-resource health.
- **User `/pin`.** An explicit pin takes precedence: while pinned the controller applies the pinned render and `failurePolicy` does not open a rollout against a render the user overrode.
- **Manual sync mode** (`syncPolicy.automatic=false`). The apply (and rollout) happens only when a `/sync` is pending; once applied, the gate runs identically. A terminal render is re-driven only by `/sync --force` or a spec change.

## What this reuses vs. adds

**Reuses:** the observer's per-resource health + `AssessedRenderRef` on the `ReleaseInventory` ([01](01-health-assessment.md)); SSA `DryRunAll` (observer diff path); the render store + rollback-apply ([03](03-persistent-storage.md)); the sync-wave gate; the `/sync` annotation + `lastHandledSyncAt`; the `/pin` observer-awareness pattern.

**Adds:** top-level `spec.failurePolicy{rollback}` (+ the `healthAssessment.deadline`-required precondition when `rollback != Never`); `status.lastHealthy` + `status.rollout` + a `RolloutSucceeded` condition; the always-on dry-run step + `Deployer.DryRun` (lenient on unknown types); the verdict-based rollout gate; the controller's wake-on-RI-health-change; observer **rollout-awareness** (desired = `lastHealthy` while `RolledBack`); the `sync-force-requested-at` annotation + `lastHandledForceSyncAt`.

## Build items

Slices 1–5 shipped an earlier, deadline-based gate; the items below fold in the redesign.

1. **API + validation** — `spec.failurePolicy{rollback}` (`deadline` **removed**); `status.lastHealthy`, `status.rollout`; the `RolloutSucceeded` condition; validation now **requires `healthAssessment.deadline` when `rollback != Never`** (replacing the old `deadline > healthAssessment.deadline` check). *(shipped; deadline removal + validation precondition pending.)*
2. **Dry-run** — `Deployer.DryRun` (lenient on unknown types); pre-persist step; `un-appliable` condition/reason. *(shipped.)*
3. **Verdict gate** — replace the wall-clock deadline with the `(State, AssessedRenderRef)` verdict: `Healthy`→Succeeded, `Degraded`→failure, else wait. Wake the controller on RI health changes (`inventory.HealthChanged`). *(wake-on-health shipped; verdict replacement pending.)*
4. **Rollback + coherent terminal status** — one-shot rollback per the `rollback` enum; while `RolledBack`, keep applying `lastHealthy` (`Applied=True`); a `Failed` render keeps re-applying `R` (`Applied=True`) and advances to `Succeeded` if it self-heals; make the observer **rollout-aware** (desired = `lastHealthy`) so `InSync`/`Healthy` reflect the running render. *(rollback shipped; terminal-status coherence + observer rollout-awareness pending.)*
5. **Force sync** — `sync-force-requested-at` + `lastHandledForceSyncAt`; `/sync?force`; `pendingForceSync` flush; CLI `--force`. *(shipped.)*

## Resolved decisions

- **No hard cap for a never-settling render.** With no wall-clock, could a render stay `Progressing` forever? For a resource that *exists and is readable*, no: the observer's `healthAssessment.deadline` forces `Progressing → Degraded`, and validation guarantees that deadline is set whenever `rollback != Never` — so a genuinely failing rollout always reaches a verdict. The only indefinite wait is a resource that never appears (`Unknown`, see [Edge cases](#edge-cases)), which is deliberately treated as "keep waiting" rather than failed. Waiting is the safe default; a stuck `Progressing` is visible, and `/sync --force` or a spec edit is the escape hatch. No optional backstop is added.
