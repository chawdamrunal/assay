export type Kind =
  | 'claude-code-plugin'
  | 'mcp-server'
  | 'connector'
  | 'hook'
  | 'settings';

export interface Item {
  name: string;
  kind: Kind;
  version?: string;
  source?: string;
  local_path?: string;
  permissions?: string[];
  hash?: string;
  metadata?: Record<string, string>;
}

export interface Inventory {
  generated_at: string;
  items: Item[];
}

export interface Config {
  models: { default: string; investigation: string; provider?: string };
  scan: { subagent_concurrency: number; budget_usd: number; deep_scan: boolean };
  telemetry: { enabled: boolean };
}

export interface ScansList {
  items: ScanListItem[];
}

// ---- Scan / Verdict types (match schemas/verdict-v0.1.json + scanner.Event) ----

export type Severity = 'critical' | 'high' | 'medium' | 'low' | 'info';
export type VerdictLabel = 'safe' | 'caution' | 'unsafe';

export interface Evidence {
  file: string;
  line: number;
  snippet: string;
}

/**
 * DiffAnnotation tags a finding with its status relative to a prior scan of
 * the same target. Populated by `verdict.Diff` server-side after a scan that
 * was run with `?since=latest`.
 *
 * - `new`      — present in current scan, not in prior
 * - `stable`   — present in both, no material change
 * - `changed`  — present in both, description/severity/evidence drifted
 * - `resolved` — present in prior, absent in current (returned separately by
 *                the diff endpoint as a list of resolved Findings)
 */
export type DiffStatus = 'new' | 'stable' | 'changed' | 'resolved';

export interface DiffAnnotation {
  status: DiffStatus;
  since_scan?: string;
  prior_id?: string;
}

export interface Finding {
  id: string;
  severity: Severity;
  category: string;
  title: string;
  description?: string;
  evidence?: Evidence[];
  context?: string;
  impact?: string;
  mitigation?: string;
  exploit_scenario?: string;
  recommended_action?: string;
  threat_id?: string;
  diff?: DiffAnnotation;
  /** Origin of the finding: 'llm' (default), 'sca' (dependency scanner), or 'poison' (tool-poisoning detector). */
  source?: 'llm' | 'sca' | 'poison';
}

export interface VerdictTarget {
  kind: string;
  name: string;
  version?: string;
  source?: string;
  hash?: string;
}

export interface ScannerInfo {
  name: string;
  version: string;
  model: string;
  prompt_version: string;
}

export interface Verdict {
  schema_version: string;
  scan_id: string;
  target: VerdictTarget;
  scanned_at: string; // ISO-8601
  scanner: ScannerInfo;
  verdict: VerdictLabel;
  summary?: string;
  data_flow_diagram?: string; // Mermaid flowchart, produced before threat model
  threat_model?: string;
  claims_vs_reality?: string;
  findings: Finding[];
  open_questions?: string[];
  /** ID of the prior scan this verdict was diffed against, when applicable. */
  prior_scan_id?: string;
}

export interface ScanStartResponse {
  scan_id: string;
}

export type ScanStatus = 'complete' | 'failed' | 'pending';

export interface ScanListItem {
  scan_id: string;
  target: string;
  dir: string;
  /** Present on serve-managed scans; absent on legacy CLI dirs. */
  status?: ScanStatus;
  /** RFC3339 directory mtime, used as fallback when scan_id is not a timestamp. */
  created_at?: string;
  /** Overall verdict for the scan, present when the scan has completed. */
  verdict?: VerdictLabel;
}

/** Body of a 410 Gone response from GET /api/scans/:id when the scan failed. */
export interface ScanFailure {
  scan_id: string;
  target?: string;
  stage?: string;
  error: string;
  failed_at?: string;
}

export interface ScanProgressEvent {
  stage: string;  // "prepass" | "triage" | "claims" | "threat_model" | "investigation" | "exploitability" | "synthesis" | "done"
  status: string; // "start" | "complete" | "error"
  message?: string;
  at?: string;    // RFC3339 timestamp, carried through from the MCP progress event
}

// ---- Fleet types (match internal/fleet) ----

export type FleetStatus = 'running' | 'complete' | 'failed';

export interface FleetMember {
  target: string;
  scan_id: string;
}

export interface FleetMeta {
  fleet_id: string;
  started_at: string;
  status: FleetStatus;
  members: FleetMember[];
  excludes?: string[];
}

export interface FleetMemberReport {
  target: string;
  scan_id: string;
  status: 'complete' | 'failed' | 'pending';
  verdict?: VerdictLabel;
  findings?: number;
  critical?: number;
  high?: number;
  medium?: number;
  error_reason?: string;
}

export interface FleetReport {
  fleet_id: string;
  started_at: string;
  finished_at?: string;
  status: FleetStatus;
  members: FleetMemberReport[];
  verdict_counts: { safe: number; caution: number; unsafe: number };
  severity_counts: { critical: number; high: number; medium: number; low: number; info: number };
}

export interface FleetList {
  items: FleetMeta[];
}

export interface FleetStartRequest {
  exclude?: string[];
  parallel?: number;
  quick?: boolean;
}

export interface FleetStartResponse {
  fleet_id: string;
  members: FleetMember[];
}

/** Fleet SSE event envelope from /api/fleet/:id/stream. */
export interface FleetEvent {
  scan_id: string;
  stage: string;
  status: string;
  message?: string;
  at?: string;
}

/** Terminal `done` SSE payload from /api/fleet/:id/stream — distinct from FleetEvent. */
export interface FleetDoneEvent {
  fleet_id: string;
}

// ---- Connection status (GET /api/status) ----

export type StatusLevel = 'ok' | 'warn' | 'error';
export type StatusKind =
  | 'claude-code'
  | 'mcp'
  | 'auth'
  | 'filesystem'
  | 'hook'
  | 'anthropic-key'
  | 'gemini-key'
  | 'openai-key'
  | 'github';

/** GET /api/keys/status — which direct-API providers have a key configured.
 * Booleans only; key values are never exposed by the API. */
export interface KeyStatus {
  providers: Record<string, boolean>;
}

export interface StatusCheck {
  name: string;
  kind: StatusKind;
  level: StatusLevel;
  detail: string;
  last_checked: string;
}

export interface StatusResponse {
  generated_at: string;
  checks: StatusCheck[];
}

// ---- Assistant chat (POST /api/assistant/message) ----

export type AssistantReplyKind = 'text' | 'proposal' | 'scan_started' | 'error';

export interface AssistantCandidate {
  name: string;
  kind: 'installed-plugin' | 'marketplace-plugin' | 'mcp-server' | string;
  local_path: string;
  version?: string;
  marketplace?: string;
  description?: string;
}

export interface AssistantSuggestion {
  name: string;
  kind: string;
}

// ---- Supply chain summary (GET /api/supply-chain/summary) ----

export interface SupplyChainSummary {
  dependency_critical: number;
  dependency_high: number;
  dependency_medium: number;
  poison_findings: number;
  affected_plugins: number;
  total_scans: number;
}

export interface AssistantReply {
  kind: AssistantReplyKind;
  text: string;
  conversation_id: string;
  candidates?: AssistantCandidate[];
  /** Soft "did you mean?" hints rendered as clickable chips when Candidates is empty. */
  suggestions?: AssistantSuggestion[];
  scan_id?: string;
  target?: string;
  /** Set when the user mentioned a GitHub URL; FE renders it as an external link. */
  github_url?: string;
}
