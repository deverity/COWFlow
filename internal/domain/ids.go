package domain

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"sync"

	"github.com/oklog/ulid/v2"
)

// Kind identifies the entity an ID belongs to; each Kind has one prefix.
type Kind string

// Kinds of entity that carry a prefixed ULID (§13). Tables keyed by name or
// by a composite key have no ID and no Kind.
const (
	KindProject             Kind = "project"
	KindTask                Kind = "task"
	KindContract            Kind = "contract"
	KindInvariant           Kind = "invariant"
	KindAgentSession        Kind = "agent_session"
	KindWorkspace           Kind = "workspace"
	KindGoldenTree          Kind = "golden_tree"
	KindIntent              Kind = "intent"
	KindCrossRequest        Kind = "cross_request"
	KindCrossRequestComment Kind = "cross_request_comment"
	KindChange              Kind = "change"
	KindSyncAttempt         Kind = "sync_attempt"
	KindIntegrationGroup    Kind = "integration_group"
	KindIntegrationAttempt  Kind = "integration_attempt"
	KindResourceLease       Kind = "resource_lease"
	KindEvent               Kind = "event"
	KindBudget              Kind = "budget"
	KindAuditEntry          Kind = "audit_entry"
	KindOperation           Kind = "operation"
)

// IDGen mints prefixed ULIDs (§13). Safe for concurrent use.
type IDGen interface {
	New(kind Kind) (string, error)
}

// prefixes maps each Kind to its ID prefix: 2-3 lower-case letters, joined to
// the 26-character ULID by "_" (§13).
var prefixes = map[Kind]string{
	KindProject:             "prj",
	KindTask:                "tsk",
	KindContract:            "ctr",
	KindInvariant:           "inv",
	KindAgentSession:        "ses",
	KindWorkspace:           "wks",
	KindGoldenTree:          "gld",
	KindIntent:              "int",
	KindCrossRequest:        "req",
	KindCrossRequestComment: "cmt",
	KindChange:              "chg",
	KindSyncAttempt:         "syn",
	KindIntegrationGroup:    "grp",
	KindIntegrationAttempt:  "att",
	KindResourceLease:       "lse",
	KindEvent:               "evt",
	KindBudget:              "bud",
	KindAuditEntry:          "aud",
	KindOperation:           "op",
}

// ULIDGen is the IDGen backed by oklog/ulid and crypto/rand. The injected
// Clock supplies the timestamp so fake clocks work.
type ULIDGen struct {
	mu      sync.Mutex // ulid.MonotonicEntropy is not goroutine-safe
	clock   Clock
	entropy *ulid.MonotonicEntropy
}

var _ IDGen = (*ULIDGen)(nil)

// NewULIDGen returns a generator reading time from c.
func NewULIDGen(c Clock) *ULIDGen {
	return &ULIDGen{clock: c, entropy: ulid.Monotonic(rand.Reader, 0)}
}

// New mints a prefixed ULID for kind. It errors for a kind without a prefix.
func (g *ULIDGen) New(kind Kind) (string, error) {
	p, ok := prefixes[kind]
	if !ok {
		return "", fmt.Errorf("ids: no prefix for kind %q", kind)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	id, err := ulid.New(ulid.Timestamp(g.clock.Now()), g.entropy)
	if err != nil {
		return "", fmt.Errorf("ids: %w", err)
	}
	return p + "_" + id.String(), nil
}

var idPattern = regexp.MustCompile(`^([a-z]{2,3})_[0-9A-HJKMNP-TV-Z]{26}$`)

// ParseID validates id as "<prefix>_<26-char Crockford ULID>" and returns the
// Kind that owns its prefix. It errors for malformed IDs and unknown prefixes.
func ParseID(id string) (Kind, error) {
	m := idPattern.FindStringSubmatch(id)
	if m == nil {
		return "", fmt.Errorf("ids: malformed id %q", id)
	}
	for k, p := range prefixes {
		if p == m[1] {
			return k, nil
		}
	}
	return "", fmt.Errorf("ids: unknown prefix %q", m[1])
}
