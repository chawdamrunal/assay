import type {
  AssistantReply,
  SupplyChainSummary,
  Config,
  Finding,
  FleetDoneEvent,
  FleetEvent,
  FleetList,
  FleetReport,
  FleetStartRequest,
  FleetStartResponse,
  Inventory,
  KeyStatus,
  ScanFailure,
  ScansList,
  ScanStartResponse,
  StatusResponse,
  Verdict,
  ScanProgressEvent,
} from '@/types/api';

export type ScanResult =
  | { kind: 'verdict'; data: Verdict }
  | { kind: 'failure'; data: ScanFailure };

/** Response body of GET /api/scans/diff?a=...&b=... */
export interface DiffResponse {
  a: Verdict;
  b: Verdict;
  added: Finding[];
  changed: Finding[];
  stable: Finding[];
  resolved: Finding[];
}

// CSRF header constants — must match internal/api/csrf.go. The header value
// is incidental; what matters for CSRF defence is that the header is custom
// (forcing browsers to preflight cross-origin requests, which the server
// then refuses).
const CSRF_HEADER_NAME = 'X-Assay-CSRF';
const CSRF_HEADER_VALUE = '1';

async function jsonFetch<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, {
    ...init,
    headers: {
      Accept: 'application/json',
      [CSRF_HEADER_NAME]: CSRF_HEADER_VALUE,
      ...(init?.headers ?? {}),
    },
  });
  if (!resp.ok) {
    let detail = `HTTP ${resp.status}`;
    try {
      const body = (await resp.json()) as { error?: string };
      if (body.error) detail = body.error;
    } catch {
      /* non-JSON body */
    }
    throw new Error(detail);
  }
  return resp.json() as Promise<T>;
}

export const api = {
  getInventory: () => jsonFetch<Inventory>('/api/inventory'),
  getConfig: () => jsonFetch<Config>('/api/config'),
  putConfig: (cfg: Config) =>
    jsonFetch<Config>('/api/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cfg),
    }),
  listScans: () => jsonFetch<ScansList>('/api/scans'),

  startScan: (target: string, offline = false, since?: string) =>
    jsonFetch<ScanStartResponse>('/api/scans', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ target, offline, since }),
    }),
  getDiff: (a: string, b: string) =>
    jsonFetch<DiffResponse>(`/api/scans/diff?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}`),
  listFleets: () => jsonFetch<FleetList>('/api/fleet'),
  startFleetScan: (req: FleetStartRequest = {}) =>
    jsonFetch<FleetStartResponse>('/api/fleet/scan', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    }),
  getFleet: (id: string) =>
    jsonFetch<FleetReport>(`/api/fleet/${encodeURIComponent(id)}`),
  getStatus: () => jsonFetch<StatusResponse>('/api/status'),
  /** Fleet-level supply-chain summary for the Dashboard tile. */
  getSupplyChainSummary: () => jsonFetch<SupplyChainSummary>('/api/supply-chain/summary'),

  /** Which direct-API providers have a key configured (booleans only). */
  getKeyStatus: () => jsonFetch<KeyStatus>('/api/keys/status'),
  /**
   * Store a direct-API provider key in the OS keychain (POST /api/keys → 204).
   * Write-only: the key is never returned by any endpoint. Throws on error.
   */
  setApiKey: async (provider: string, key: string): Promise<void> => {
    const resp = await fetch('/api/keys', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        [CSRF_HEADER_NAME]: CSRF_HEADER_VALUE,
      },
      body: JSON.stringify({ provider, key }),
    });
    if (!resp.ok && resp.status !== 204) {
      let detail = `HTTP ${resp.status}`;
      try {
        const body = (await resp.json()) as { error?: string };
        if (body.error) detail = body.error;
      } catch {
        /* non-JSON body */
      }
      throw new Error(detail);
    }
  },

  /**
   * Store a GitHub personal access token in the OS keychain
   * (POST /api/github-token → 204) so Assay can clone + scan private repos.
   * Write-only: the token is never returned by any endpoint. Throws on error.
   */
  setGitHubToken: async (token: string): Promise<void> => {
    const resp = await fetch('/api/github-token', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        [CSRF_HEADER_NAME]: CSRF_HEADER_VALUE,
      },
      body: JSON.stringify({ token }),
    });
    if (!resp.ok && resp.status !== 204) {
      let detail = `HTTP ${resp.status}`;
      try {
        const body = (await resp.json()) as { error?: string };
        if (body.error) detail = body.error;
      } catch {
        /* non-JSON body */
      }
      throw new Error(detail);
    }
  },

  /** Remove the stored GitHub PAT (DELETE /api/github-token → 204). */
  deleteGitHubToken: async (): Promise<void> => {
    const resp = await fetch('/api/github-token', {
      method: 'DELETE',
      headers: { Accept: 'application/json', [CSRF_HEADER_NAME]: CSRF_HEADER_VALUE },
    });
    if (!resp.ok && resp.status !== 204) {
      let detail = `HTTP ${resp.status}`;
      try {
        const body = (await resp.json()) as { error?: string };
        if (body.error) detail = body.error;
      } catch {
        /* non-JSON body */
      }
      throw new Error(detail);
    }
  },

  /** Send a chat message to the assistant. conversationID is optional on
   * the first turn; the server returns the allocated ID for subsequent calls. */
  sendAssistantMessage: (text: string, conversationID?: string) =>
    jsonFetch<AssistantReply>('/api/assistant/message', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text, conversation_id: conversationID }),
    }),

  getScan: (id: string) => jsonFetch<Verdict>(`/api/scans/${encodeURIComponent(id)}`),
  /** Hard-delete a scan from disk (DELETE /api/scans/:id). 204 on success. */
  deleteScan: async (id: string): Promise<void> => {
    const resp = await fetch(`/api/scans/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      headers: { Accept: 'application/json', [CSRF_HEADER_NAME]: CSRF_HEADER_VALUE },
    });
    if (!resp.ok && resp.status !== 204) {
      let detail = `HTTP ${resp.status}`;
      try {
        const body = (await resp.json()) as { error?: string };
        if (body.error) detail = body.error;
      } catch {
        /* non-json */
      }
      throw new Error(detail);
    }
  },
  /**
   * Like getScan but also handles the 410 Gone response the server sends when
   * a scan failed before producing audit.json. Returns a discriminated union so
   * callers branch on result.kind === 'verdict' vs 'failure'.
   */
  getScanResult: async (id: string): Promise<ScanResult> => {
    const resp = await fetch(`/api/scans/${encodeURIComponent(id)}`, {
      headers: { Accept: 'application/json' },
    });
    if (resp.status === 410) {
      const data = (await resp.json()) as ScanFailure;
      return { kind: 'failure', data };
    }
    if (!resp.ok) {
      let detail = `HTTP ${resp.status}`;
      try {
        const body = (await resp.json()) as { error?: string };
        if (body.error) detail = body.error;
      } catch {
        /* non-JSON body */
      }
      throw new Error(detail);
    }
    const data = (await resp.json()) as Verdict;
    return { kind: 'verdict', data };
  },
};

/**
 * Subscribe to scan progress events for the given scan ID via Server-Sent Events.
 * The provided onEvent callback fires for each progress event.
 * Returns an unsubscribe function that closes the EventSource.
 *
 * On error, onError fires once with the error. The connection is closed
 * automatically when the server sends the terminal "done" event.
 */
/**
 * Subscribe to a fleet's merged event stream. The server fans events out
 * with the scan_id envelope; this client just hands each event to onEvent.
 * Auto-closes when the server sends the terminal `done` event.
 */
export function openFleetStream(
  fleetID: string,
  onEvent: (e: FleetEvent) => void,
  onError?: (err: Event) => void,
): () => void {
  const es = new EventSource(`/api/fleet/${encodeURIComponent(fleetID)}/stream`);
  // The fleet handler uses the stage name as the SSE `event:` field — same
  // pattern as the per-scan stream — so we register for each known stage
  // plus a synthetic "done".
  const stages = [
    'prepass', 'triage', 'claims', 'threat_model',
    'investigation', 'exploitability', 'synthesis', 'done', 'fleet', 'scan',
  ];
  for (const stage of stages) {
    es.addEventListener(stage, (raw) => {
      try {
        if (stage === 'done') {
          // The `done` payload is {fleet_id: string} — not a FleetEvent envelope.
          // Parse and discard; close the stream.
          JSON.parse((raw as MessageEvent).data) as FleetDoneEvent;
          es.close();
        } else {
          const data = JSON.parse((raw as MessageEvent).data) as FleetEvent;
          onEvent(data);
        }
      } catch {
        /* ignore */
      }
    });
  }
  if (onError) es.addEventListener('error', onError);
  return () => es.close();
}

export function openScanStream(
  id: string,
  onEvent: (e: ScanProgressEvent) => void,
  onError?: (err: Event) => void,
): () => void {
  const es = new EventSource(`/api/scans/${encodeURIComponent(id)}/stream`);

  // Each stage emits its own named event (event: prepass, event: triage, etc.).
  // We handle them via the generic onmessage and parse the data ourselves to
  // get the stage from the payload — but EventSource only fires onmessage for
  // unnamed events. So we register a handler for every known stage.
  // Note: the server also emits a `ping` event at stream open to flush headers
  // (server-side only; not forwarded to the client as a meaningful payload).
  const stages = [
    'prepass', 'triage', 'claims', 'threat_model',
    'investigation', 'exploitability', 'synthesis', 'done', 'scan',
  ];
  for (const stage of stages) {
    es.addEventListener(stage, (raw) => {
      try {
        const data = JSON.parse((raw as MessageEvent).data) as ScanProgressEvent;
        onEvent(data);
        if (stage === 'done') {
          es.close();
        }
      } catch {
        /* ignore malformed events */
      }
    });
  }

  if (onError) {
    es.addEventListener('error', onError);
  }

  return () => es.close();
}
