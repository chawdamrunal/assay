package main

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/chawdamrunal/assay/internal/store"
)

// Test seams: tests override these to point at a temp dir + mock keyring.
var (
	configOverridePaths  *store.Paths
	configKeyringService = "assay"
)

func resolvePaths() (*store.Paths, error) {
	if configOverridePaths != nil {
		return configOverridePaths, nil
	}
	return store.NewPaths()
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage Assay configuration",
	}
	cmd.AddCommand(newConfigGetCmd(), newConfigSetCmd(), newConfigListCmd())
	return cmd
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := resolvePaths()
			if err != nil {
				return err
			}
			key := args[0]

			if key == "api-key" {
				kr := store.NewKeyring(configKeyringService)
				v, err := kr.GetAPIKey()
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), maskAPIKey(v))
				return err
			}

			cfg, err := store.LoadConfig(paths.ConfigFile)
			if err != nil {
				return err
			}
			val, ok := lookupConfig(cfg, key)
			if !ok {
				return fmt.Errorf("unknown config key %q", key)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), val)
			return err
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := resolvePaths()
			if err != nil {
				return err
			}
			key, value := args[0], args[1]

			if key == "api-key" {
				kr := store.NewKeyring(configKeyringService)
				if err := kr.SetAPIKey(value); err != nil {
					return err
				}
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "API key saved to OS keychain")
				return err
			}

			cfg, err := store.LoadConfig(paths.ConfigFile)
			if err != nil {
				return err
			}
			if err := assignConfig(&cfg, key, value); err != nil {
				return err
			}
			return store.SaveConfig(paths.ConfigFile, cfg)
		},
	}
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all config keys and values",
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := resolvePaths()
			if err != nil {
				return err
			}
			cfg, err := store.LoadConfig(paths.ConfigFile)
			if err != nil {
				return err
			}
			rows := configRows(cfg)
			keys := make([]string, 0, len(rows))
			for k := range rows {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%-32s %s\n", k, rows[k]); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func configRows(c store.Config) map[string]string {
	return map[string]string{
		"models.default":            c.Models.Default,
		"models.investigation":      c.Models.Investigation,
		"scan.subagent_concurrency": strconv.Itoa(c.Scan.SubagentConcurrency),
		"scan.budget_usd":           strconv.FormatFloat(c.Scan.BudgetUSD, 'f', -1, 64),
		"scan.deep_scan":            strconv.FormatBool(c.Scan.DeepScan),
		"telemetry.enabled":         strconv.FormatBool(c.Telemetry.Enabled),
	}
}

func lookupConfig(c store.Config, key string) (string, bool) {
	v, ok := configRows(c)[key]
	return v, ok
}

func assignConfig(c *store.Config, key, value string) error {
	switch key {
	case "models.default":
		c.Models.Default = value
	case "models.investigation":
		c.Models.Investigation = value
	case "scan.subagent_concurrency":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("scan.subagent_concurrency must be an integer: %w", err)
		}
		c.Scan.SubagentConcurrency = n
	case "scan.budget_usd":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("scan.budget_usd must be a number: %w", err)
		}
		c.Scan.BudgetUSD = f
	case "scan.deep_scan":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("scan.deep_scan must be true or false: %w", err)
		}
		c.Scan.DeepScan = b
	case "telemetry.enabled":
		return fmt.Errorf("telemetry is forced off in v0")
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}

func maskAPIKey(v string) string {
	if len(v) < 8 {
		return "***"
	}
	return v[:4] + "***" + v[len(v)-2:]
}
