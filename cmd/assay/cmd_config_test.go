package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/chawdamrunal/assay/internal/store"
)

func runCmd(t *testing.T, paths *store.Paths, args ...string) string {
	t.Helper()
	cmd := newRootCmd()
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	configOverridePaths = paths
	configKeyringService = "assay-test"
	require.NoError(t, cmd.Execute())
	return out.String()
}

func TestConfigSetAndGetBudget(t *testing.T) {
	keyring.MockInit()
	tmp := t.TempDir()
	paths := &store.Paths{
		ConfigDir:  tmp,
		ConfigFile: filepath.Join(tmp, "config.toml"),
		DataDir:    tmp,
		ScansDir:   filepath.Join(tmp, "scans"),
		CacheDir:   filepath.Join(tmp, "cache"),
	}

	runCmd(t, paths, "config", "set", "scan.budget_usd", "12.5")
	out := runCmd(t, paths, "config", "get", "scan.budget_usd")
	assert.Contains(t, out, "12.5")
}

func TestConfigSetAndGetAPIKey(t *testing.T) {
	keyring.MockInit()
	tmp := t.TempDir()
	paths := &store.Paths{
		ConfigDir:  tmp,
		ConfigFile: filepath.Join(tmp, "config.toml"),
		DataDir:    tmp,
		ScansDir:   filepath.Join(tmp, "scans"),
		CacheDir:   filepath.Join(tmp, "cache"),
	}

	runCmd(t, paths, "config", "set", "api-key", "sk-ant-xxx")
	out := runCmd(t, paths, "config", "get", "api-key")
	assert.Contains(t, out, "***") // masked
	assert.NotContains(t, out, "sk-ant-xxx")
}

func TestConfigList(t *testing.T) {
	keyring.MockInit()
	tmp := t.TempDir()
	paths := &store.Paths{
		ConfigDir:  tmp,
		ConfigFile: filepath.Join(tmp, "config.toml"),
		DataDir:    tmp,
		ScansDir:   filepath.Join(tmp, "scans"),
		CacheDir:   filepath.Join(tmp, "cache"),
	}

	out := runCmd(t, paths, "config", "list")
	assert.True(t, strings.Contains(out, "scan.budget_usd"))
	assert.True(t, strings.Contains(out, "models.default"))
}
