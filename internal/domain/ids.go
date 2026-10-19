package domain

import (
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/oklog/ulid/v2"
)

// Kind identifies the entity an ID belongs to; its prefix is pending
// SPEC-GAPS U-DATA-2 (prefixes for prefixed ULIDs).
type Kind string

// KindProject identifies a project.
const KindProject Kind = "project"

// IDGen mints prefixed ULIDs (§13). Safe for concurrent use.
type IDGen interface {
	New(kind Kind) (string, error)
}

// prefixes maps each Kind to its ID prefix.
// PENDING U-DATA-2: the table stays empty until the maintainer decides it.
var prefixes = map[Kind]string{}

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
	return p + id.String(), nil
}
