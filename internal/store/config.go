package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the user-facing Assay configuration, persisted as TOML.
type Config struct {
	Models    ModelsConfig    `toml:"models" json:"models"`
	Scan      ScanConfig      `toml:"scan" json:"scan"`
	Telemetry TelemetryConfig `toml:"telemetry" json:"telemetry"`
}

// ModelsConfig pins which Claude model runs each scanner stage. An empty
// string means "auto": defer to Claude Code's own model selection, which is
// the only choice guaranteed to be valid for the user's subscription tier
// (Pro tiers cannot run Opus). Pinning a concrete model ID here is opt-in.
type ModelsConfig struct {
	Default       string `toml:"default" json:"default"`
	Investigation string `toml:"investigation" json:"investigation"`
	// Provider selects the LLM "brain". Empty == "claude-code" (the Claude Code
	// subscription via the MCP-spawn path, the default that needs no API key).
	// Other values: CLI agents ("gemini-cli", "codex-cli") or direct-API
	// providers ("anthropic-api", "gemini-api", "openai-api"). See the AgentID
	// type in internal/provider. A per-scan request may override this.
	Provider string `toml:"provider" json:"provider"`
}

// ScanConfig controls scan execution behavior.
type ScanConfig struct {
	SubagentConcurrency int     `toml:"subagent_concurrency" json:"subagent_concurrency"`
	BudgetUSD           float64 `toml:"budget_usd" json:"budget_usd"`
	// DeepScan, when true, runs MCP-mode investigation as parallel per-threat
	// Claude Code subagents (the Task tool) instead of one sequential agent.
	// Deeper + less context dilution, but spends more of the user's
	// subscription quota — so it is opt-in. Default false.
	DeepScan bool `toml:"deep_scan" json:"deep_scan"`
}

// TelemetryConfig is reserved for v0.2; in v0 it is forced off.
type TelemetryConfig struct {
	Enabled bool `toml:"enabled" json:"enabled"`
}

// DefaultConfig returns the configuration applied when no file is present.
func DefaultConfig() Config {
	return Config{
		Models: ModelsConfig{
			// Empty = "auto": let Claude Code pick the model its subscription
			// allows. Hard-pinning a version string here is a latent
			// availability bomb — when the alias retires, every scan breaks
			// with a cryptic subprocess exit code.
			Default:       "",
			Investigation: "",
		},
		Scan: ScanConfig{
			SubagentConcurrency: 3,
			BudgetUSD:           5.0,
		},
		Telemetry: TelemetryConfig{Enabled: false},
	}
}

// LoadConfig reads the TOML config at path. If the file does not exist,
// it returns DefaultConfig with a nil error.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the user's config file by design
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	cfg := DefaultConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// SaveConfig writes cfg to path, creating the parent directory if needed.
func SaveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- path is the user's config file by design
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return nil
}
