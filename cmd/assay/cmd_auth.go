package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/chawdamrunal/assay/internal/auth"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect Assay authentication state",
	}
	cmd.AddCommand(newAuthStatusCmd())
	return cmd
}

func newAuthStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show which credential sources are available and which is active",
		RunE: func(cmd *cobra.Command, _ []string) error {
			winner, statuses := auth.ResolveAll(configKeyringService)

			if asJSON {
				out := map[string]any{
					"active":  activeMethodName(winner),
					"methods": jsonMethods(statuses),
				}
				if winner != nil && !winner.ExpiresAt.IsZero() {
					out["expires_at"] = winner.ExpiresAt.Format(time.RFC3339)
				}
				if winner != nil && winner.Subscription != "" {
					out["subscription"] = winner.Subscription
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			return writeAuthStatusHuman(cmd.OutOrStdout(), winner, statuses)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit machine-readable JSON")
	return cmd
}

func writeAuthStatusHuman(w io.Writer, winner *auth.Credentials, statuses []auth.MethodStatus) error {
	if winner == nil {
		if _, err := fmt.Fprintln(w, "no credentials available"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(w, "Active: %s (%s)\n", winner.Source, winner.Kind); err != nil {
			return err
		}
		if !winner.ExpiresAt.IsZero() {
			if _, err := fmt.Fprintf(w, "Expires: %s\n", winner.ExpiresAt.Format(time.RFC1123)); err != nil {
				return err
			}
		}
		if winner.Subscription != "" {
			if _, err := fmt.Fprintf(w, "Subscription: %s\n", winner.Subscription); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "Method status (priority order):"); err != nil {
		return err
	}
	for _, s := range statuses {
		icon := "✗"
		if s.Available {
			icon = "✓"
		}
		if _, err := fmt.Fprintf(w, "  %s %-12s %s\n", icon, s.Method, s.Detail); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if winner == nil {
		lines := []string{
			"To enable authentication:",
			"  - Set ANTHROPIC_API_KEY in your environment, OR",
			"  - Run `assay config set api-key sk-ant-…`, OR",
			"  - Log into Claude Code (Assay will reuse those credentials automatically)",
		}
		for _, line := range lines {
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
	return nil
}

func activeMethodName(c *auth.Credentials) string {
	if c == nil {
		return ""
	}
	return string(c.Source)
}

func jsonMethods(statuses []auth.MethodStatus) []map[string]any {
	out := make([]map[string]any, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, map[string]any{
			"method":    string(s.Method),
			"available": s.Available,
			"detail":    s.Detail,
		})
	}
	return out
}
