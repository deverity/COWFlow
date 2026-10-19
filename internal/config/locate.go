package config

import "errors"

// errLocationUndecided is returned by Locate until SPEC-GAPS U-API-1 is
// decided.
var errLocationUndecided = errors.New(
	"config: the config.toml location is undecided (SPEC-GAPS U-API-1); pass the path explicitly")

// Locate returns the location of config.toml.
//
// PENDING U-API-1: the location is undecided, so Locate never guesses one;
// callers inject explicit paths (see cli.Deps.ConfigPath) until it is.
func Locate() (string, error) { return "", errLocationUndecided }
