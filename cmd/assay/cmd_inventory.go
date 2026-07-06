package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/chawdamrunal/assay/internal/inventory"
)

func newInventoryCmd() *cobra.Command {
	var claudeDir string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "inventory",
		Short: "List installed Claude Code plugins, MCP servers, hooks, and settings overrides",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if claudeDir == "" {
				home, err := homeDir()
				if err != nil {
					return err
				}
				claudeDir = filepath.Join(home, ".claude")
			}

			inv, err := inventory.EnumerateAll(inventory.OptionsForClaudeDir(claudeDir))
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(inv)
			}
			return renderInventoryTable(cmd, inv)
		},
	}

	cmd.Flags().StringVar(&claudeDir, "claude-dir", "", "Path to ~/.claude (overrides default)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit machine-readable JSON")
	return cmd
}

func renderInventoryTable(cmd *cobra.Command, inv inventory.Inventory) error {
	if len(inv.Items) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "No plugins, MCP servers, hooks, or settings overrides found.")
		return err
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "NAME\tKIND\tVERSION\tSOURCE"); err != nil {
		return err
	}
	for _, it := range inv.Items {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			it.Name, it.Kind, dashIfEmpty(it.Version), dashIfEmpty(it.Source)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "\nTotal: %d items\n", len(inv.Items)); err != nil {
		return err
	}
	return w.Flush()
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
