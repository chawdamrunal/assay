package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// registerScanStateTools wires the four mutating tools Claude uses to drive a
// scan: allocate a scan dir, append progress events, append findings, and
// finalize the verdict. State lives entirely on disk so any process tailing
// the scan dir (notably assay serve's SSE bridge) sees the same updates.
func registerScanStateTools(s *server.MCPServer, st *ScanState) {
	registerScanStart(s, st)
	registerEmitProgress(s, st)
	registerRecordFinding(s, st)
	registerFinalizeScan(s, st)
}

func registerScanStart(s *server.MCPServer, st *ScanState) {
	tool := mcp.NewTool("assay_scan_start",
		mcp.WithDescription("Allocate a new scan directory for the given target and return its scan_id. Call this FIRST before any record/progress/finalize tool."),
		mcp.WithString("target",
			mcp.Description("Absolute path of the artifact being scanned."),
			mcp.Required(),
		),
		mcp.WithString("scan_id",
			mcp.Description("Optional caller-supplied scan_id. Omit to have one allocated (UUID)."),
		),
	)
	s.AddTool(tool, func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		target, ok := args["target"].(string)
		if !ok || !filepath.IsAbs(target) {
			return mcp.NewToolResultError("target must be an absolute path"), nil
		}
		scanID, _ := args["scan_id"].(string)
		if scanID == "" {
			scanID = uuid.NewString()
		}
		scanDir, err := st.Allocate(scanID, target)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("assay_scan_start: %v", err)), nil
		}
		// Emit a synthetic start event so SSE subscribers see the lifecycle.
		_ = st.AppendEvent(scanID, ProgressEvent{Stage: "prepass", Status: "start", At: time.Now().UTC().Format(time.RFC3339Nano)})
		// Return JSON so the agent can parse without regex.
		body, _ := json.Marshal(map[string]string{
			"scan_id":  scanID,
			"scan_dir": scanDir,
		})
		return mcp.NewToolResultText(string(body)), nil
	})
}

func registerEmitProgress(s *server.MCPServer, st *ScanState) {
	tool := mcp.NewTool("assay_emit_progress",
		mcp.WithDescription("Append a progress event for a scan. SSE subscribers (the web UI) re-broadcast each event. Stages: prepass, triage, claims, threat_model, investigation, exploitability, synthesis, done. Statuses: start, complete, error."),
		mcp.WithString("scan_id", mcp.Description("scan_id returned by assay_scan_start."), mcp.Required()),
		mcp.WithString("stage", mcp.Description("Pipeline stage."), mcp.Required()),
		mcp.WithString("status", mcp.Description("start | complete | error"), mcp.Required()),
		mcp.WithString("message", mcp.Description("Optional human-readable detail.")),
	)
	s.AddTool(tool, func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		scanID, _ := args["scan_id"].(string)
		stage, _ := args["stage"].(string)
		status, _ := args["status"].(string)
		msg, _ := args["message"].(string)
		if scanID == "" || stage == "" || status == "" {
			return mcp.NewToolResultError("scan_id, stage, and status are required"), nil
		}
		if err := st.AppendEvent(scanID, ProgressEvent{Stage: stage, Status: status, Message: msg}); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("assay_emit_progress: %v", err)), nil
		}
		return mcp.NewToolResultText("ok"), nil
	})
}

func registerRecordFinding(s *server.MCPServer, st *ScanState) {
	tool := mcp.NewTool("assay_record_finding",
		mcp.WithDescription(`Append one Finding to the scan's findings.jsonl.

Required: id, severity (critical|high|medium|low|info), category, title, evidence (array of {file, line, snippet} with VERBATIM snippets — the post-validator drops findings whose snippet does not appear at the cited file:line).

REQUIRED for the report to read well — DO NOT skip these, do not write boilerplate:
  - description: 2-4 sentences. WHAT specifically is wrong, in plain language. Names the actual function/file/data involved.
  - context: WHERE in this target's data flow the issue lives. Reference the data-flow diagram's node names (e.g. "between the 'format() entrypoint' node and the 'attacker.example.com' sink"). Not generic.
  - impact: CONSEQUENCE for THIS target. Names the affected data class (AWS creds, session tokens, etc.), the affected users (anyone who installs / anyone who invokes / the host process), and the business/compliance outcome. Avoid generic "could lead to security issues."
  - mitigation: SPECIFIC code-level fix. Name the framework/library/API the target already uses. State the exact change ("drop the fs.readFileSync at src/main.js:23; add a unit test that asserts the function makes no fs/network calls"). Do not write "validate inputs."
  - exploit_scenario: 1-3 sentences. Concrete attack walk-through.
  - recommended_action: high-level action the user can take WITHOUT reading code (uninstall, rotate keys, file upstream issue).
  - threat_id: the T1/T2/... id from the threat model.`),
		mcp.WithString("scan_id", mcp.Description("scan_id returned by assay_scan_start."), mcp.Required()),
		mcp.WithObject("finding",
			mcp.Description("Finding object matching the verdict schema."),
			mcp.Required(),
		),
	)
	s.AddTool(tool, func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		scanID, _ := args["scan_id"].(string)
		if scanID == "" {
			return mcp.NewToolResultError("scan_id is required"), nil
		}
		finding, ok := args["finding"].(map[string]any)
		if !ok || len(finding) == 0 {
			return mcp.NewToolResultError("finding must be a non-empty object"), nil
		}
		// Minimal schema check before we persist; finalize re-validates.
		for _, k := range []string{"id", "severity", "category", "title"} {
			if _, present := finding[k]; !present {
				return mcp.NewToolResultError(fmt.Sprintf("finding missing required field %q", k)), nil
			}
		}
		if err := st.AppendFinding(scanID, finding); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("assay_record_finding: %v", err)), nil
		}
		return mcp.NewToolResultText("ok"), nil
	})
}

func registerFinalizeScan(s *server.MCPServer, st *ScanState) {
	tool := mcp.NewTool("assay_finalize_scan",
		mcp.WithDescription("Write the final audit.json + audit.md for a scan, combining the verdict, summary, data-flow diagram, threat_model, claims_vs_reality, and all recorded findings. The post-validator re-reads cited file:line snippets and drops any finding whose quote does not appear in the source — this enforces Assay's hard-quote rule. Emits a terminal 'done' progress event."),
		mcp.WithString("scan_id", mcp.Description("scan_id returned by assay_scan_start."), mcp.Required()),
		mcp.WithString("target", mcp.Description("Absolute path to the scan target (used by the citation validator)."), mcp.Required()),
		mcp.WithString("verdict", mcp.Description("safe | caution | unsafe"), mcp.Required()),
		mcp.WithString("summary", mcp.Description("Executive summary (Markdown).")),
		mcp.WithString("data_flow_diagram", mcp.Description("Mermaid flowchart of how data moves through the artifact: external inputs → trust boundaries → processing → sinks/outputs. Should call out credential reads, network destinations, and any data-side effects. Used as the visual anchor for the threat model.")),
		mcp.WithString("threat_model", mcp.Description("Threat-model markdown (T1, T2, … sections) tied to the data-flow diagram's nodes.")),
		mcp.WithString("claims_vs_reality", mcp.Description("Claims-vs-reality narrative (Markdown).")),
		mcp.WithString("model", mcp.Description("Optional: model name that produced this verdict (e.g. claude-sonnet-4-6 via Claude Code).")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		scanID, _ := args["scan_id"].(string)
		target, _ := args["target"].(string)
		verdictLabel, _ := args["verdict"].(string)
		if scanID == "" || target == "" || verdictLabel == "" {
			return mcp.NewToolResultError("scan_id, target, and verdict are required"), nil
		}
		switch verdictLabel {
		case "safe", "caution", "unsafe":
		default:
			return mcp.NewToolResultError("verdict must be safe | caution | unsafe"), nil
		}
		summary, _ := args["summary"].(string)
		dataFlow, _ := args["data_flow_diagram"].(string)
		threat, _ := args["threat_model"].(string)
		claims, _ := args["claims_vs_reality"].(string)
		model, _ := args["model"].(string)
		if model == "" {
			model = "claude-code"
		}

		// Build the public Verdict from the recorded findings.
		findings, err := st.LoadFindings(scanID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("assay_finalize_scan: load findings: %v", err)), nil
		}

		auditJSON, auditMD, err := assembleVerdict(ctx, scanID, target, verdictLabel, summary, dataFlow, threat, claims, model, st.offline, findings)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("assay_finalize_scan: assemble: %v", err)), nil
		}

		scanDir, err := st.WriteAudit(scanID, auditJSON, auditMD)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("assay_finalize_scan: write: %v", err)), nil
		}

		// Terminal event so SSE subscribers stop waiting.
		_ = st.AppendEvent(scanID, ProgressEvent{Stage: "done", Status: "complete", Message: verdictLabel})

		resp, _ := json.Marshal(map[string]string{
			"scan_id":  scanID,
			"scan_dir": scanDir,
			"verdict":  verdictLabel,
		})
		return mcp.NewToolResultText(string(resp)), nil
	})
}
