---
name: scheduler-and-leases
description: Use when implementing or changing internal/scheduler — agent/build/test/integration/disk limits, LLM budgets and per-provider rate limits, resource leases (named semaphores, queue, TTL, heartbeat, announce, `cowflow exec --resource`), or the task-decomposition rules (depends depth, priority) the scheduler and validators enforce.
---
# Scheduler and Resource Leases (internal/scheduler)

## When to use
- Dispatching sessions under `[scheduler]` limits; retry/backoff; timeouts; pause/resume; disk/golden budgets.
- LLM budgets: `[scheduler.llm]`, `[scheduler.llm.<provider>]`, task `budget_tokens`, `on_budget_exceeded`, `budget.exceeded`.
- `[[resources]]` leases: check/acquire/release/revoke/announce, queues, TTL expiry, reconciliation; `cowflow exec --resource`.
- Validating `task create` input: `depends` depth/cycles, `priority`.

## Source of truth
SCOPE.md wins on any conflict. Derived from:
- §2.2 Principles (2, 8); §4 Glossary; §5.3 Modes; §5.4 Practical limits
- §9.8 Resource leases (shared slots); §9.9 Events (`resource.*`, `session.lost`, `budget.exceeded`)
- §11 Scheduler; §13 Data model (`resources`, `resource_leases`, `resource_queue`, `budgets`)
- §14 CLI (Resources, `run`, `budget`, `task pause|resume`); §15 / §15.1 MCP; §15.3 per-provider budget
- §18 / §18.1 Resilience + hermetic harness; §19 Phase 4; §20 Acceptance criteria; §21 Observability
- §22 Configuration (`[scheduler]`, `[scheduler.llm*]`, `[[resources]]`, `[watchdog]`)
- §23 Risks / decisions; §24.5 Resources and parallelism; §24.7 Task decomposition; §24.15 Budget

## Rules
**Limits (§11, §22)**
- Keys: `max_agents`, `max_builds`, `max_tests`, `max_integrations_per_target`, `session_timeout_seconds`, `command_timeout_seconds`, `retry_limit`, `retry_backoff_seconds`, `workspace_disk_budget_gb`, `golden_cache_gb`.
- MUST have a persistent queue, pause/resume, `session.lost`, cleanup only when no unpreserved changes, golden cache GC by LRU (§11).
- `max_builds`, `max_tests`, `max_integrations_per_target` are aliases for resources `compile`, `test`, `integrate:<target>`; old config stays valid (§9.8).
- Scheduler, heartbeat, limits, LLM budget are daemon-mode capabilities (§5.3, §19 Phase 4).
- Agent crash → `session.lost`, workspace/logs preserved, retry (§18) within `retry_limit` / `retry_backoff_seconds`.

**LLM budget and rate limits (§11, §15.3, §24.15)**
- `[scheduler.llm]`: `max_concurrent_requests`, `tokens_per_hour_budget`, `cost_per_day_budget_usd`, `on_budget_exceeded = "pause"`.
- `[scheduler.llm.<provider>]` per runner `provider` (`anthropic`, `openai`, `google`, ...). Dispatch per provider; a provider at its limit MUST NOT block the others (§15.3, §20.28).
- Task `budget_tokens` optional; absent → inherits from provider (§24.15).
- 80% → warning to agent and lead. 100% → session paused, `budget.exceeded`, workspace preserved (§24.15, §20.19).
- Retries due to agent failure count against the same budget (§24.15).
- Watchdog has its own budget: `[watchdog] budget_tokens_per_hour` (§9.7, §22).

**Resource leases (§9.8, §24.5)**
- Named semaphores limit resource consumption, NEVER file access; not an edit lock.
- `[[resources]]`: `name`, `slots`, `lease_ttl_seconds`, `queue = "fifo" | "priority"`.
- A lease belongs to the session; another agent NEVER releases it; lead can (`cowflow resource revoke`, `cowflow_resource_revoke`).
- TTL expiry → slot returns, `resource.expired`, session notified. Heartbeat renews an active lease. Expired is final — renew before TTL (§24.5).
- `wait=true` waits via `wait_events`, NEVER polling.
- Queue persisted in SQLite; survives restart; reconciliation expires leases of dead sessions.
- `queue = "priority"`: task with `priority = high` or a `high` cross-request jumps ahead.
- Integration gates use the **same** queues (`compile`, `test`); NEVER a parallel engine queue.
- `cowflow_resource_announce(name, eta_seconds)` only notifies, NEVER reserves.
- `cowflow exec --resource <name> -- <cmd...>` acquires, executes, releases. Direct `go build` is not blocked but does not count; optional invariant `require_resource_exec` detects it from the process log and opens a diagnostic.
- Events: `resource.acquired|released|granted|queued|expired|revoked|announced`.
- Tables: `resource_leases(id, resource, session_id, task_id, acquired_at, expires_at, released_at, state)`, `resource_queue(resource, session_id, priority, enqueued_at)`.

**Decomposition rules enforced (§24.7)**
- `depends` with depth > 2 or a cycle MUST be rejected.
- `priority` ∈ `low | normal | high`; `high` reserved for blocking other tasks or hotfixes.
- Task without `--paths` → warning, not refusal. Task should fit in one session (≤ `session_timeout`).

## Implementation guide
Packages (§16.1): `internal/scheduler/{scheduler,limits,budget,leases,queue}.go`; tables in `internal/store`; config structs in `internal/config`; CLI `resources`, `resource`, `exec`, `budget`, `run` in `internal/cli`; MCP tools in `internal/mcp`; process-tree kill via `internal/platform`.

```go
type LeaseState string // Guidance: e.g. active|released|expired|revoked (spec names table column `state`, not its values)
type QueueMode string  // fifo|priority

type ResourceSpec struct { Name string; Slots int; LeaseTTL time.Duration; Queue QueueMode }

type Leases interface {
    Check(ctx context.Context, name string) (Status, error)           // {slots, free, queue, holders}
    Acquire(ctx context.Context, sess SessionID, name string, wait bool, timeout time.Duration) (Grant, error) // {lease_id, acquired} | {queued, position}
    Renew(ctx context.Context, sess SessionID, leaseID string) error  // via heartbeat
    Release(ctx context.Context, sess SessionID, leaseID string) error
    Revoke(ctx context.Context, lead LeadToken, leaseID string) error
    Announce(ctx context.Context, sess SessionID, name string, eta time.Duration) error
}
type ProviderBudget struct { MaxConcurrentRequests int; TokensPerHour int64; CostPerDayUSD float64 }
```
Steps:
1. Config: parse `[[resources]]`; synthesize `compile`/`test`/`integrate:<target>` from `max_builds`/`max_tests`/`max_integrations_per_target` when not declared. Guidance: if both alias and explicit `[[resources]]` exist with different slots, fail validation loudly — precedence is unspecified.
2. Leases: acquire = one tx (count active leases < slots → insert lease + `resource.acquired`; else insert `resource_queue` + `resource.queued`). Release/expire/revoke = tx that frees slot, pops next by mode (FIFO by `enqueued_at`; priority then `enqueued_at`) and emits `resource.granted`.
3. Expiry sweeper driven by an injectable clock; reconciliation on daemon start expires leases of dead sessions.
4. `exec`: acquire (wait) → run under `command_timeout_seconds` with process-tree kill → release in `defer`, also on cancel.
5. Dispatcher: admit a session only if `max_agents` and its provider's `max_concurrent_requests` and budgets allow; per-provider queues so one saturated provider does not stall others.
6. Budget accounting per session/task/provider/day (`budgets` table); thresholds emit warning at 80%, pause + `budget.exceeded` at 100%.
7. Task validation: build `task_dependencies` graph, reject cycles and depth > 2 with exit code `4`.
Guidance: all state + event writes in one transaction (§13); idempotent `op_id` (§18); no goroutine-held truth — SQLite is authoritative.

## Pitfalls
- Polling for queue position instead of `wait_events`.
- A separate queue for integration gates (forbidden).
- Granting a slot outside the tx that freed it (double grants under `-race` / 20+ sessions).
- Letting a stuck holder keep a slot past TTL ("one more minute").
- Global provider block when one vendor hits its limit.
- Releasing in `exec` only on success — must release on error, timeout, cancel.
- Unspecified: `LeaseState` values; behavior when `acquire` `timeout` elapses while queued; leases in daemonless mode; how tokens/cost are measured per runner; `on_budget_exceeded` values other than `"pause"`; whether 80%/100% apply to provider/hourly/daily budgets as well as task `budget_tokens`; exact meaning of "depth > 2"; tie-break inside priority class; `require_resource_exec` invariant kind/config.

## Tests required
- Acceptance §20.30: `compile.slots = 3`, 6 workers → max 3 run, others queued, `resource.granted` in order, dead session's lease expires and frees the slot.
- Priority queue: `high` task / `high` cross-request jumps ahead; FIFO preserves order.
- Ownership: other session cannot release; lead `revoke` works and emits `resource.revoked`.
- TTL expiry and heartbeat renewal with fake clock; restart preserves queue and leases (§18.1, §20.8).
- `exec --resource` releases on success, non-zero exit, timeout, cancellation; Windows tree kill (§20.16).
- Alias mapping: legacy `max_builds`/`max_tests` config still valid.
- §20.28: limit 1 on one provider while the other continues. §20.19: exceeded budget pauses and notifies; 80% warning.
- Retries counted against budget; `retry_limit`/`retry_backoff_seconds` honored with fake clock.
- Decomposition: cycle and depth > 2 rejected; invalid `priority` rejected.
- 20 simulated + real sessions under limits (Phase 4 output); `-race`; harness crash between outbox write and delivery.

## Done checklist
- [ ] All config keys of §11/§22 parsed with spec defaults; aliases resolved.
- [ ] Lease/queue operations transactional with `resource.*` events; queue survives restart.
- [ ] `wait=true` uses `wait_events`; no polling loops.
- [ ] Gates share `compile`/`test` queues with agents.
- [ ] Per-provider dispatch and budgets; 80%/100% behavior; `budget.exceeded`.
- [ ] CLI `resources`, `resource acquire|release|revoke|announce`, `exec --resource`, `budget` with `--json`; MCP `cowflow_resource_*`.
- [ ] Depth/cycle/priority validation in `task create`.
- [ ] `/metrics` exposes tokens and cost per session/task/day (§21).
- [ ] Acceptance §20.16, 19, 28, 30 green on 3 platforms.
