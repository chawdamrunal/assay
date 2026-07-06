// Command assay is the CLI entry point for the Assay AI dev stack security scanner.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// exitCodeError lets a command request a specific process exit code. main()
// honors it so CI can distinguish "scan found problems" (the --fail-on gate,
// code 2) from "the tool crashed" (any other error, code 1). The message, if
// any, is printed to stderr without the "error:" prefix used for crashes.
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string { return e.msg }
func (e *exitCodeError) ExitCode() int { return e.code }

// version is set by goreleaser at build time via -ldflags.
var version = "dev"
var commit = "unknown"
var buildDate = "unknown"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "assay",
		Short:         "Security scanner for the AI dev stack",
		Long:          "Assay inventories installed Claude Code plugins, MCP servers, hooks, and settings, and scans plugins, MCP servers, skills, and connectors for security threats.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newVersionCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newInventoryCmd())
	root.AddCommand(newServeCmd())
	root.AddCommand(newScanCmd())
	root.AddCommand(newScanAllCmd())
	root.AddCommand(newAuthCmd())
	root.AddCommand(newMCPCmd())
	root.AddCommand(newHookCmd())
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		var ec *exitCodeError
		if errors.As(err, &ec) {
			if ec.msg != "" {
				fmt.Fprintln(os.Stderr, ec.msg)
			}
			os.Exit(ec.code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// homeDir is the function used to resolve the user's home directory.
// Tests use the --claude-dir flag instead of overriding this.
var homeDir = os.UserHomeDir
