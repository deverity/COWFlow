package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExampleConfig_LoadsAndValidates(t *testing.T) {
	c, err := Load(filepath.Join("..", "..", "config.example.toml"))
	require.NoError(t, err)
	require.NotEmpty(t, c.Gates)
	require.Contains(t, c.Runner.Defs, "claude-code")
}
