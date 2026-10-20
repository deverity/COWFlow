-- 0002_workspaces_golden: tasks (minimal), workspaces, golden_trees, audit_log.
-- Times are UTC with a fixed 9-digit fraction so ordering is a string sort.

-- S2-E1 extends tasks.
CREATE TABLE tasks (
  id         TEXT PRIMARY KEY, -- tsk_<ULID>
  project_id TEXT NOT NULL REFERENCES projects(id),
  slug       TEXT NOT NULL,
  title      TEXT NOT NULL DEFAULT '',
  kind       TEXT NOT NULL DEFAULT '', -- empty unless the gitflow preset is active (C2)
  base       TEXT NOT NULL,
  base_sha   TEXT NOT NULL,
  branch     TEXT NOT NULL, -- computed once by BranchNamer, never recomputed
  paths      TEXT NOT NULL DEFAULT '[]', -- JSON array
  sparse     INTEGER NOT NULL DEFAULT 1,
  state      TEXT NOT NULL, -- 'created' in Sprint 1
  created_at TEXT NOT NULL,
  UNIQUE (project_id, slug)
) STRICT;

CREATE TABLE workspaces (
  id           TEXT PRIMARY KEY, -- wks_<ULID>
  project_id   TEXT NOT NULL REFERENCES projects(id),
  task_id      TEXT NOT NULL REFERENCES tasks(id),
  session_id   TEXT, -- NULL when no agent session owns it
  provider     TEXT NOT NULL,
  cow_kind     TEXT NOT NULL,
  filesystem   TEXT NOT NULL,
  path         TEXT NOT NULL,
  branch       TEXT NOT NULL,
  base_sha     TEXT NOT NULL,
  sparse_paths TEXT NOT NULL DEFAULT '[]', -- JSON array
  port_slot    INTEGER NOT NULL,
  port_start   INTEGER NOT NULL,
  port_end     INTEGER NOT NULL, -- inclusive (U-PLAT-14)
  state        TEXT NOT NULL CHECK (state IN
    ('creating','pending_mount','ready','failed','pending_cleanup','destroyed')), -- U-DATA-5
  last_error   TEXT NOT NULL DEFAULT '',
  op_id        TEXT NOT NULL UNIQUE,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
) STRICT;

-- The WHERE clause must match the live-slot query in workspaces.go exactly.
CREATE UNIQUE INDEX workspaces_live_slot ON workspaces(project_id, port_slot)
  WHERE state NOT IN ('destroyed','failed');
CREATE INDEX workspaces_task ON workspaces(task_id);

-- The directory of a golden tree is <golden_root>/<base_sha> (SCOPE.md §23
-- decision 18), so no path column.
CREATE TABLE golden_trees (
  id            TEXT PRIMARY KEY, -- gld_<ULID>
  project_id    TEXT NOT NULL REFERENCES projects(id),
  base_sha      TEXT NOT NULL,
  state         TEXT NOT NULL CHECK (state IN ('building','ready','failed','deleting')), -- U-PLAT-13
  size_bytes    INTEGER NOT NULL DEFAULT 0,
  file_count    INTEGER NOT NULL DEFAULT 0,
  in_use        INTEGER NOT NULL DEFAULT 0 CHECK (in_use >= 0),
  builder_op_id TEXT NOT NULL,
  last_error    TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  last_used_at  TEXT NOT NULL,
  updated_at    TEXT NOT NULL,
  UNIQUE (project_id, base_sha)
) STRICT;

-- Guidance columns (sprint 2, S2-E2, may extend it).
CREATE TABLE audit_log (
  id          TEXT PRIMARY KEY, -- aud_<ULID>
  at          TEXT NOT NULL,
  actor_kind  TEXT NOT NULL DEFAULT '',
  actor_id    TEXT NOT NULL DEFAULT '',
  action      TEXT NOT NULL,
  subject_id  TEXT NOT NULL,
  detail_json TEXT NOT NULL DEFAULT '{}'
) STRICT;
