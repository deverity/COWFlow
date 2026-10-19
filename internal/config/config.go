package config

import "errors"

// errNotImplemented is returned by contract entry points whose behavior has
// not landed yet.
var errNotImplemented = errors.New("config: not implemented")

// Config is the root of config.toml. Its fields mirror SCOPE.md §22.
type Config struct{}

// Default returns a configuration holding the spec-stated defaults only
// (§22, §6.3, §9.4, §23).
func Default() *Config { return &Config{} }

// Load decodes the file at path, rejects unknown keys, applies defaults, and
// validates the result.
func Load(_ string) (*Config, error) { return nil, errNotImplemented }

// Validate reports every problem found in c, joined with errors.Join.
func (*Config) Validate() error { return errNotImplemented }

// Save writes c to path atomically (temporary file plus rename).
func Save(_ string, _ *Config) error { return errNotImplemented }

// Locate returns the location of config.toml.
//
// PENDING U-API-1: the location is undecided; callers inject explicit paths
// (see cli.Deps.ConfigPath) until it is.
func Locate() (string, error) { return "", errNotImplemented }
