package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// DirName is the per-project COWFlow directory.
	DirName = ".cowflow"
	// FileName is the config file inside DirName.
	FileName = "config.toml"
)

// Locate returns the config.toml path for the current working directory. See
// LocateFrom.
func Locate() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("config: locate: %w", err)
	}
	return LocateFrom(cwd)
}

// LocateFrom walks up from dir (SPEC-GAPS U-API-1):
//
//  1. the first directory containing .cowflow/config.toml wins;
//  2. else the first directory containing .git (a directory or a file) gives
//     <that dir>/.cowflow/config.toml, which may not exist yet (init creates
//     it);
//  3. else <dir>/.cowflow/config.toml.
//
// It only inspects the file system: no git subprocess, no environment
// variable.
func LocateFrom(dir string) (string, error) {
	start, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("config: locate %s: %w", dir, err)
	}
	for d := start; ; d = filepath.Dir(d) {
		p := filepath.Join(d, DirName, FileName)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	for d := start; ; d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return filepath.Join(d, DirName, FileName), nil
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	return filepath.Join(start, DirName, FileName), nil
}

// StatePath returns the daemon state directory: daemon.state_dir when set
// (a relative value resolves against the directory of the config file at
// cfgPath), else <dir of config.toml>/state.
func StatePath(cfgPath string, cfg *Config) string {
	base := filepath.Dir(cfgPath)
	sd := cfg.Daemon.StateDir
	switch {
	case sd == "":
		return filepath.Join(base, "state")
	case filepath.IsAbs(sd):
		return filepath.Clean(sd)
	default:
		return filepath.Join(base, sd)
	}
}

// DBPath returns the SQLite database file: <state dir>/cowflow.db.
func DBPath(cfgPath string, cfg *Config) string {
	return filepath.Join(StatePath(cfgPath, cfg), "cowflow.db")
}
