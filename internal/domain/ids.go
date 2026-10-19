package domain

// Kind identifies the entity an ID belongs to; its prefix is pending
// SPEC-GAPS U-DATA-2 (prefixes for prefixed ULIDs).
type Kind string

// KindProject identifies a project.
const KindProject Kind = "project"

// IDGen mints prefixed ULIDs (§13). Safe for concurrent use.
type IDGen interface {
	New(kind Kind) (string, error)
}
