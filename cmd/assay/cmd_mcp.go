package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	assaymcp "github.com/chawdamrunal/assay/internal/mcp"
)

func newMCPCmd() *cobra.Command {
	var transport string
	var bind string
	var offline bool

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run Assay as an MCP server (stdio or HTTP)",
		Long: `Run Assay as a Model Context Protocol server.

Claude Code (or any MCP-compatible client) connects and calls assay_* tools
to drive a scan: list/read target files, record findings, finalize a verdict.
The web UI at 'assay serve' reads the same on-disk artifacts.

Two transports:
  --transport stdio        For Claude Code launching assay as a subprocess.
  --transport http         For HTTP MCP clients. Use --bind to choose the addr.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := resolvePaths()
			if err != nil {
				return err
			}
			if err := paths.Ensure(); err != nil {
				return err
			}
			s := assaymcp.NewServerWithStateOffline(paths.ScansDir, offline)
			switch transport {
			case "stdio":
				// stdio: silent on stdout/stderr unless something breaks; Claude
				// Code parses the JSON-RPC frames.
				return server.ServeStdio(s)
			case "http":
				h := server.NewStreamableHTTPServer(s)
				fmt.Fprintf(cmd.ErrOrStderr(), "Assay MCP HTTP server listening on %s\n", bind)
				srv := &http.Server{Addr: bind, Handler: h, ReadHeaderTimeout: 5 * time.Second}
				return srv.ListenAndServe()
			default:
				return fmt.Errorf("unknown transport %q (want stdio or http)", transport)
			}
		},
	}
	cmd.Flags().StringVar(&transport, "transport", "stdio", "MCP transport: stdio or http")
	cmd.Flags().StringVar(&bind, "bind", "127.0.0.1:7374", "HTTP bind address (only with --transport http)")
	cmd.Flags().BoolVar(&offline, "offline", false, "Skip the OSV.dev network lookup in the deterministic SCA floor (air-gapped scans)")
	return cmd
}
