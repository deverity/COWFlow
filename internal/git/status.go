package git

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

// StatusKind classifies a `git status --porcelain=v2` entry.
type StatusKind int

// Status entry kinds, one per porcelain v2 record type.
const (
	StatusChanged   StatusKind = iota + 1 // "1": ordinary change
	StatusRenamed                         // "2": rename or copy
	StatusUnmerged                        // "u": unmerged path
	StatusUntracked                       // "?": untracked file
)

// String returns the kind's name.
func (k StatusKind) String() string {
	switch k {
	case StatusChanged:
		return "changed"
	case StatusRenamed:
		return "renamed"
	case StatusUnmerged:
		return "unmerged"
	case StatusUntracked:
		return "untracked"
	}
	return fmt.Sprintf("StatusKind(%d)", int(k))
}

// StatusEntry is one path reported by `git status --porcelain=v2 -z`.
type StatusEntry struct {
	Kind     StatusKind // Changed | Renamed | Unmerged | Untracked
	XY       string     // two-letter porcelain code; "??" for untracked
	Path     string
	OrigPath string // renames/copies only
}

// Status lists every uncommitted change in worktree, untracked files
// included, with `git -C <wt> status --porcelain=v2 -z --untracked-files=all`.
// Ignored files are not listed (no --ignored).
func (c *Client) Status(ctx context.Context, worktree string) ([]StatusEntry, error) {
	if err := checkAbs("worktree", worktree); err != nil {
		return nil, err
	}
	out, err := c.run(ctx, worktree, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	return ParseStatusV2(out)
}

// Number of space-separated fields before the path in each record type,
// including the type letter.
const (
	changedFields  = 9  // 1 XY sub mH mI mW hH hI path
	renamedFields  = 10 // 2 XY sub mH mI mW hH hI Xscore path
	unmergedFields = 11 // u XY sub m1 m2 m3 mW h1 h2 h3 path
)

// ParseStatusV2 parses `git status --porcelain=v2 -z` output without
// --branch, --show-stash or --ignored. Every record is NUL-terminated; a
// rename record is followed by its original path as a separate NUL-terminated
// field. Truncated or malformed output returns ErrUnexpectedOutput and no
// entries.
func ParseStatusV2(out []byte) ([]StatusEntry, error) {
	bad := func(format string, a ...any) ([]StatusEntry, error) {
		return nil, fmt.Errorf("%w: status: %s", ErrUnexpectedOutput, fmt.Sprintf(format, a...))
	}
	if len(out) == 0 {
		return nil, nil
	}
	if out[len(out)-1] != 0 {
		return bad("output does not end with NUL")
	}
	recs := strings.Split(string(out[:len(out)-1]), "\x00")
	var list []StatusEntry
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if len(rec) < 2 || rec[1] != ' ' {
			return bad("malformed record %q", truncate(rec, 80))
		}
		switch rec[0] {
		case '?':
			p := rec[2:]
			if p == "" {
				return bad("untracked record without a path")
			}
			list = append(list, StatusEntry{Kind: StatusUntracked, XY: "??", Path: p})
		case '1':
			f, err := statusFields(rec, changedFields, 2)
			if err != nil {
				return bad("%v", err)
			}
			list = append(list, StatusEntry{Kind: StatusChanged, XY: f[1], Path: f[changedFields-1]})
		case '2':
			f, err := statusFields(rec, renamedFields, 2)
			if err != nil {
				return bad("%v", err)
			}
			if !validScore(f[8]) {
				return bad("bad rename score %q", truncate(f[8], 20))
			}
			if i+1 >= len(recs) || recs[i+1] == "" {
				return bad("rename record without an original path")
			}
			i++
			list = append(list, StatusEntry{Kind: StatusRenamed, XY: f[1], Path: f[renamedFields-1], OrigPath: recs[i]})
		case 'u':
			f, err := statusFields(rec, unmergedFields, 3)
			if err != nil {
				return bad("%v", err)
			}
			list = append(list, StatusEntry{Kind: StatusUnmerged, XY: f[1], Path: f[unmergedFields-1]})
		default:
			return bad("unexpected record type %q", rec[:1])
		}
	}
	return list, nil
}

// statusFields splits rec into n fields (the last, the path, may contain
// spaces) and validates XY, the submodule state, the modes and the object
// names. hashes is the number of object names right before the path.
func statusFields(rec string, n, hashes int) ([]string, error) {
	f := strings.SplitN(rec, " ", n)
	if len(f) != n || f[n-1] == "" {
		return nil, fmt.Errorf("short record %q", truncate(rec, 80))
	}
	if !validXY(f[1]) {
		return nil, fmt.Errorf("bad XY %q", truncate(f[1], 10))
	}
	if !validSub(f[2]) {
		return nil, fmt.Errorf("bad submodule state %q", truncate(f[2], 10))
	}
	// Layout: type XY sub modes... hashes... [score] path.
	last := n - 1
	if f[0] == "2" {
		last-- // score
	}
	modesEnd := last - hashes
	for _, m := range f[3:modesEnd] {
		if !validMode(m) {
			return nil, fmt.Errorf("bad mode %q", truncate(m, 10))
		}
	}
	for _, h := range f[modesEnd:last] {
		if !IsSHA(h) {
			return nil, fmt.Errorf("bad object name %q", truncate(h, 80))
		}
	}
	return f, nil
}

func validXY(s string) bool {
	if len(s) != 2 {
		return false
	}
	for i := range 2 {
		if !strings.ContainsRune(".MTADRCU", rune(s[i])) {
			return false
		}
	}
	return true
}

// validSub accepts "N..." (not a submodule) or "S<c><m><u>".
func validSub(s string) bool {
	if s == "N..." {
		return true
	}
	if len(s) != 4 || s[0] != 'S' {
		return false
	}
	return (s[1] == 'C' || s[1] == '.') && (s[2] == 'M' || s[2] == '.') && (s[3] == 'U' || s[3] == '.')
}

func validMode(s string) bool {
	if len(s) != 6 {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '7' {
			return false
		}
	}
	return true
}

// validScore accepts "R<0-100>" or "C<0-100>".
func validScore(s string) bool {
	if len(s) < 2 || len(s) > 4 || (s[0] != 'R' && s[0] != 'C') {
		return false
	}
	n := 0
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n <= 100
}

// Unpublished lists the commits on worktree's HEAD that are neither in
// baseSHA nor under any published change ref, with
// `git -C <wt> rev-list HEAD --not <base> --glob=<ns>/*`. refNamespace is
// [git].shared_ref_namespace (for example "refs/cowflow/changes") and is
// validated with git check-ref-format first (§17). An empty result means
// every commit is preserved.
func (c *Client) Unpublished(ctx context.Context, worktree, baseSHA, refNamespace string) ([]string, error) {
	if err := checkAbs("worktree", worktree); err != nil {
		return nil, err
	}
	if err := checkSHA("base", baseSHA); err != nil {
		return nil, err
	}
	if strings.ContainsAny(refNamespace, "*?[") || strings.HasSuffix(refNamespace, "/") {
		return nil, fmt.Errorf("%w: %q", ErrInvalidRefName, truncate(refNamespace, 80))
	}
	if err := CheckRefName(ctx, c.r, refNamespace); err != nil {
		return nil, fmt.Errorf("ref namespace: %w", err)
	}
	out, err := c.run(ctx, worktree, "rev-list", "HEAD", "--not", baseSHA, "--glob="+refNamespace+"/*")
	if err != nil {
		return nil, fmt.Errorf("unpublished commits: %w", err)
	}
	return parseSHALines("rev-list", out)
}

// parseSHALines parses newline-terminated SHAs. Truncated output (no final
// newline, or a partial SHA) is an error.
func parseSHALines(what string, out []byte) ([]string, error) {
	if len(out) == 0 {
		return nil, nil
	}
	if !bytes.HasSuffix(out, []byte("\n")) {
		return nil, fmt.Errorf("%w: %s: output does not end with a newline", ErrUnexpectedOutput, what)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	shas := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		if !IsSHA(l) {
			return nil, fmt.Errorf("%w: %s: bad SHA %q", ErrUnexpectedOutput, what, truncate(l, 80))
		}
		shas = append(shas, l)
	}
	return shas, nil
}
