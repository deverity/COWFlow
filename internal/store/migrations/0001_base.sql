-- 0001_base: the first SCOPE.md §13 table.
-- No Dev Drive columns until SPEC-GAPS U-PLAT-10 is decided.
CREATE TABLE projects (
  id         TEXT PRIMARY KEY, -- prefixed ULID (prefix pending U-DATA-2)
  name       TEXT NOT NULL,
  repo_root  TEXT NOT NULL UNIQUE, -- canonical path (platform.Canonical)
  created_at TEXT NOT NULL -- RFC 3339 UTC from domain.Clock
) STRICT;
