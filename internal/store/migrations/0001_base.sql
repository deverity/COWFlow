-- 0001_base: the first SCOPE.md §13 table.
CREATE TABLE projects (
  id         TEXT PRIMARY KEY, -- prj_<ULID> (§13)
  name       TEXT NOT NULL,
  repo_root  TEXT NOT NULL UNIQUE, -- canonical path (platform.Canonical)
  created_at TEXT NOT NULL -- RFC 3339 UTC from domain.Clock
) STRICT;
