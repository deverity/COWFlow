---
name: collaboration-protocol
description: Use when implementing or changing internal/collab (presence, intents, overlap detection, expand_scope, stubs, cross-requests, contracts, invariants, watchdog) or the API/MCP/CLI surfaces and events that expose them.
---
# Collaboration Protocol (internal/collab)

## When to use
- Adding/changing presence states, heartbeat/TTL, intents, `expand_scope`, overlap detection, declared stubs.
- Cross-request lifecycle, SLA/escalation, comments, reassign; contract status and `confirm_contract` flow.
- `[[invariants]]` evaluation on publish; watchdog task kind; any `intent.*`, `cross_request.*`, `contract.changed`, `invariant.violated`, `agent.presence_changed` event.
- Wiring these into `/v1` endpoints (§12.2, §12.3), MCP tools (§15), or `cowflow` CLI (§14).

## Source of truth
SCOPE.md wins on any conflict. Derived from:
- §2.2 Principles (2, 3, 4, 11); §4 Glossary; §5.3 Modes
- §8.2 Change manifest (`touches_contracts`), §8.3 Derived index
- §9.1 Presence, §9.2 Intent, §9.3 Cross-requests, §9.4 Lead, §9.5 Contracts, §9.6 Invariants, §9.7 Watchdog, §9.9 Events
- §12.2 Presence, intents, contracts, index; §12.3 Cross-requests; §13 Data model; §14 CLI (Team, Cross-requests); §15 / §15.1 / §15.5 MCP
- §18 / §18.1 Resilience + hermetic harness; §20 Acceptance criteria; §22 `[collaboration]`, `[[invariants]]`, `[watchdog]`
- §23 Risks / Open questions; §24.4, §24.6, §24.8, §24.9, §24.11, §24.12, §24.13, §24.14, §24.16

## Rules
**General**
- MUST be server-side enforcement: the server refuses, the model does not choose (§2.2.3, §15.5). Refusals map to exit code `4` (refused by policy), invalid state to `2` (§14).
- NEVER implement edit locks or path reservation; overlap warns, never blocks (§2.2.2, §3.2, §9.2).
- MUST write state change + event in the same SQLite transaction via the outbox; prefixed ULID IDs (§9.9, §13).
- MUST work daemonless for intents, requests, index, polling (§5.3); watchdog is daemon-only (§5.3).

**Presence (§9.1)**
- States exactly: `starting | working | waiting | blocked | publishing | syncing | validating | completed | failed | stopped`.
- `blocked` MUST carry a reason. Presence has a TTL: `presence_ttl_seconds = 90`, `heartbeat_interval_seconds = 20` (§22).
- Changes emit `agent.presence_changed`; a dead session yields `session.lost` (§9.9, §18).

**Intents (§9.2, §24.9)**
- Fields: task, session, workspace, goal, scope (`paths`), artifacts, dependencies, contracts, `base_sha`, state.
- No intent → no publish: `change publish` / `cowflow_publish_change` MUST be refused without an intent (§2.2.3, §15.5, §20.4).
- One active intent per session; changed goals update it, never create a second (§24.9).
- Overlap MUST be detected through the derived index only (`path_index`), emitting `intent.overlap` to **both** sessions (§8.3, §9.2).
- `expand_scope` (`POST /v1/intents/{id}/expand-scope`, `cowflow_expand_scope`) widens `paths` and updates sparse checkout (§12.2, §15).
- Publish with files outside the intent → `scope.exceeded` warning in the manifest, not a refusal (§24.9); outside-scope changes generate `intent.overlap` or a scope warning visible in review (§24.4).
- Stub: `stub: true` in the intent; lists what was stubbed and which request/contract resolves it (§9.3, §24.9). A task with an active stub NEVER enters an integration group (§24.6, §24.12).
- `touches_deps: true` declares root `go.mod`/`package.json` changes; undeclared → warning to lead (§24.14).

**Cross-requests (§9.3, §24.11)**
- States: `open → accepted → delivered → closed`; `rejected` only from `open` or `accepted`. Lead can `close`/`reassign` (§9.3).
- Kinds: `implement | expose_interface | publish | review | test | confirm_contract | fix_incompat`.
- `expected_output`: `commit | interface | test | contract | answer`. Request without `expected_output` MUST be rejected (§24.11).
- `accept` carries `eta_minutes`; overrun → comment with new ETA or `reject` (§24.11).
- `reject` without a comment MUST be rejected; the reason lives in a comment (§24.6, §24.11).
- `deliver` without `change_id` accepted only for `expected_output = answer` (§24.11); acceptance 6 requires a linked change (§20.6).
- Requester closes after sync + test; lead closes abandoned ones (§24.11).
- SLA: `high` 15 min to accept/reject, `normal` 60 min, `low` none; missed → automatic escalation (§24.11). Overdue requests go to the lead via `escalation_after` (`escalation_after_minutes = 30`) (§9.4, §22).
- Waiting options injected by the runner: `blocked` + event, declared stub, escalate to lead (§9.3). Blocked > `escalation_after` → escalate (§24.6).
- `blocked` agent MUST be unblockable by an event (§20.7).
- Publish-time trailer `Cowflow-Request: <id>` when a change fulfills a request (§24.3).
- `integration.failed` opens a `fix_incompat` request to the task flagged by the gate (§24.12); post-promotion failure opens requests to involved tasks (§10.2).

**Contracts (§9.5, §24.8)**
- File `contracts/<slug>.md`; owner = implementing task; header `Owner`, `Consumers`, `Status: draft | agreed | implemented | deprecated`; sections Interface, Errors, Examples, Changelog. No implementation inside.
- `draft → agreed` only by the lead. Consumer NEVER implements against `draft` without `stub: true`.
- Changing an `agreed` contract requires a `confirm_contract` request to all consumers **before** publish; publish changing a contract without an open request MUST be rejected.
- `contract.changed` to dependents (via `contract_refs`); promoting a change that alters a contract referenced by an active task requires the lead (§9.5, §20.20).
- Workers NEVER edit another task's `contracts/*` without an accepted `confirm_contract` (§24.14).
- `deprecated` keeps the file for one release. Owner's contract must be `implemented` for Done (§24.16).
- Running B without A's contract requires `--no-contract`, recorded in audit (§24.7).

**Invariants (§9.6)**
- Kinds `pattern` (`deny` globs) and `cmd`. Gate in every integration group; on publish when `check_on_publish = true`.
- Violation → diagnostic to the agent, publish refused, `invariant.violated` (§9.6, §9.9, §20.4).
- `pattern` invariants are diff-only gates (no workspace) (§10.1); `cmd` invariants are workspace gates (Phase 5).

**Watchdog (§9.7)**
- Task `kind = "watchdog"`: no code workspace; subscribes to events and `/metrics`; can open cross-requests, comment, escalate; NEVER publishes changes; own budget (`[watchdog] budget_tokens_per_hour`).
- MCP role `watchdog` tools only: `cowflow_subscribe_events`, `cowflow_read_metrics`, `cowflow_send_request`, `cowflow_escalate` (§15.1).

## Implementation guide
Packages (§16.1): `internal/collab/{presence,intents,contracts,requests,invariants,watchdog}.go`; persistence in `internal/store` (tables `presence`, `intents`, `cross_requests`, `cross_request_comments`, `contracts`, `contract_refs`, `invariants`, `path_index`, `events`, `outbox`); types in `internal/domain`; surfaces in `internal/api`, `internal/mcp`, `internal/cli`.

```go
type PresenceState string // starting|working|waiting|blocked|publishing|syncing|validating|completed|failed|stopped
type CrossRequestState string // open|accepted|delivered|closed|rejected
type RequestKind string       // implement|expose_interface|publish|review|test|confirm_contract|fix_incompat
type ExpectedOutput string    // commit|interface|test|contract|answer
type ContractStatus string    // draft|agreed|implemented|deprecated

var requestTransitions = map[CrossRequestState][]CrossRequestState{
    "open": {"accepted", "rejected"}, "accepted": {"delivered", "rejected"},
    "delivered": {"closed"},
}
// Guidance: lead close/reassign is a separate privileged path; whether lead `close`
// is allowed from every non-terminal state is not specified — keep it explicit and audited.
```
Steps:
1. Domain types + transition table; one `func (s *Service) Transition(ctx, opID, id, to, actor)` doing validate → update → append outbox event in one `store.Tx`.
2. Intents: on create/update, recompute `path_index` rows for the intent in the same tx, then query overlapping prefixes from other active intents/changes; emit one `intent.overlap` per affected session.
3. Publish hooks (called by `internal/git` publish path): require active intent; diff manifest `files` vs intent `paths` → `scope.exceeded`; `touches_contracts` vs open `confirm_contract` requests; run `check_on_publish` invariants.
4. Requests: validation (`expected_output`, reject comment, `deliver` `change_id`), comments, reassign (lead token only, audited §17).
5. SLA/escalation: Guidance: compute `due_at` at creation from priority and persist it so restart reconciliation can re-arm timers; drive checks with an injectable clock.
6. Contracts: parse `contracts/<slug>.md` header; populate `contract_refs` from task `contracts = [...]`; emit `contract.changed` to dependents.
7. Watchdog: runs as a normal session with role `watchdog` and no workspace provisioning.
Guidance: inject `Clock`, `Store`, `EventSink` interfaces; no package-level state; `context.Context` first arg; errors wrapped with `%w` and sentinel errors (`ErrNoIntent`, `ErrMissingExpectedOutput`, ...) mapped to exit code `4`.

## Pitfalls
- Querying Git for overlap or `who --path` — must use the index only (§8.3, §20.23).
- Emitting events outside the state transaction (duplicates/loss after crash, §20.15).
- Treating overlap as a block; treating `scope.exceeded` as a refusal.
- Allowing `rejected` from `delivered`, or `deliver` without `change_id` for non-`answer` outputs.
- Letting a stubbed task into an integration group.
- Escalation timers held only in memory (lost on restart, §20.8).
- Open (do not decide): declared stub format (§23 Q2); minimum scope of the agent lead (§23 Q4).
- Unspecified: exact `cross_request.*` sub-event names; how §24.11 priority SLA and `escalation_after_minutes` combine; whether `scope.exceeded` is also an event; where contract `Status` is authoritative (file header vs `contracts` table); who moves a contract to `implemented`.
- Example invariant `import "cowflow/cmd` (§9.6) does not match module path `github.com/deverity/cowflow` — configure real patterns accordingly.

## Tests required
- Unit: every presence state; request transition table (all legal + illegal pairs); validation refusals (`expected_output`, empty reject comment, `deliver` without `change_id`).
- Publish refused without intent; refused with violated invariant; refused when changing a contract without an open `confirm_contract` (§20.4, §24.8).
- Overlap: two intents on same prefix → `intent.overlap` to both; `who --path` answers with Git mocked and never called (§20.23).
- Complete cross-request with linked change end-to-end (§20.6); `blocked` agent unblocked by event (§20.7).
- Contract change notifies dependents and requires lead (§20.20).
- Watchdog detects an injected anomaly and opens a cross-request (§20.25).
- Harness (§18.1): fake clock for presence TTL, SLA (`high` 15 min, `normal` 60 min) and `escalation_after`; daemon crash between outbox write and delivery → redelivery without duplication; 20+ simulated sessions without event loss/duplication (§20.15); restart preserves state (§20.8).
- Daemonless: intent → request → publish → sync cycle in two terminals without daemon (Phase 2 output).
- Fuzz API payloads for intents/requests (§18.1); `-race` on all.

## Done checklist
- [ ] All states/kinds/outputs use the spec's exact strings.
- [ ] Every mutation is transactional with its outbox event and idempotent by `op_id`.
- [ ] Enforcement refusals implemented server-side and return exit code `4`.
- [ ] Overlap/`who` paths use only `path_index`/`contract_refs`.
- [ ] SLA/escalation survives restart and is tested with fake clock.
- [ ] `/v1` endpoints of §12.2/§12.3, MCP tools of §15/§15.1, CLI of §14 wired with `--json`.
- [ ] Lead-only actions (`draft → agreed`, `close`/`reassign` of others' requests, `resource revoke`) check lead token and are audited.
- [ ] Open questions (§23 Q2, Q4) left configurable, not decided.
- [ ] Acceptance §20.4, 6, 7, 15, 20, 23, 25 covered by tests on 3 platforms.
