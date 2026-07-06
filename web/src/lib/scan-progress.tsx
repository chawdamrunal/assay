/**
 * ScanProgressProvider — a root-level context that tracks every scan the
 * user has started during this session, regardless of which page they're
 * looking at. Lives in the AppShell so navigation between routes never
 * tears down the SSE subscription that drives live progress.
 *
 * Three problems it solves:
 *
 *  1. **Scans stay alive when the user navigates.** Without this provider,
 *     LiveScanPage's useScanStream hook unmounts on navigation, closing
 *     the SSE. The scan continues on the server but the user has no live
 *     feedback unless they navigate back. Now every active scan keeps its
 *     SSE open while the AppShell is mounted (i.e. for the whole session).
 *
 *  2. **A persistent global progress UI.** The TopBar reads from this
 *     context to render an ActiveScansIndicator showing each running scan
 *     with a percentage bar, so users see "3 stages of 8 complete" no
 *     matter where they are in the app.
 *
 *  3. **Reload survives in-flight scans.** Active scan IDs are persisted
 *     in localStorage. On a fresh mount the provider re-subscribes so an
 *     accidental refresh doesn't make the user feel like they lost work.
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { openScanStream } from '@/lib/api';
import type { ScanProgressEvent } from '@/types/api';

// The 8 canonical pipeline stages. Order matches internal/mcp/methodology.md.
// Each `complete` event for one of these counts toward the % progress.
export const SCAN_STAGES = [
  'prepass',
  'triage',
  'claims',
  'threat_model',
  'investigation',
  'exploitability',
  'synthesis',
  'done',
] as const;

export type ScanStage = (typeof SCAN_STAGES)[number];
export type StageStatus = 'pending' | 'running' | 'complete' | 'error';

export interface ActiveScan {
  scanID: string;
  target: string;
  startedAt: number;       // epoch ms
  stages: Record<string, StageStatus>;
  done: boolean;
  errored: boolean;
  lastMessage?: string;
}

interface ScanProgressContextValue {
  /** Map of scan_id → live state. Re-renders on every event. */
  scans: Record<string, ActiveScan>;
  /** Begin tracking a scan. Idempotent — second call with same id is no-op. */
  register: (scanID: string, target: string) => void;
  /** Stop tracking a scan and close its SSE. Called when the user dismisses. */
  unregister: (scanID: string) => void;
}

const ScanProgressContext = createContext<ScanProgressContextValue | null>(null);

// localStorage key — keep the array of currently-tracked scans so a page
// refresh re-subscribes instead of "losing" the user's work.
const LS_KEY = 'assay.activeScans.v1';

interface StoredScan {
  scanID: string;
  target: string;
  startedAt: number;
}

function loadStored(): StoredScan[] {
  if (typeof window === 'undefined') return [];
  try {
    const raw = window.localStorage.getItem(LS_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as StoredScan[];
    if (!Array.isArray(parsed)) return [];
    // Drop entries older than 12h — likely abandoned / already complete.
    const cutoff = Date.now() - 12 * 60 * 60 * 1000;
    return parsed.filter((s) => s && s.scanID && s.startedAt > cutoff);
  } catch {
    return [];
  }
}

function persistStored(scans: Record<string, ActiveScan>) {
  if (typeof window === 'undefined') return;
  const arr: StoredScan[] = Object.values(scans)
    .filter((s) => !s.done && !s.errored)
    .map((s) => ({ scanID: s.scanID, target: s.target, startedAt: s.startedAt }));
  try {
    window.localStorage.setItem(LS_KEY, JSON.stringify(arr));
  } catch {
    /* quota — ignore */
  }
}

export function ScanProgressProvider({ children }: { children: ReactNode }) {
  const [scans, setScans] = useState<Record<string, ActiveScan>>({});
  // Track open EventSource handles per scan so we can close them on
  // unregister. Map lives in a ref so it's stable across renders.
  const unsubsRef = useRef<Map<string, () => void>>(new Map());

  const updateScan = useCallback((scanID: string, patch: (prev: ActiveScan) => ActiveScan) => {
    setScans((curr) => {
      const prev = curr[scanID];
      if (!prev) return curr;
      return { ...curr, [scanID]: patch(prev) };
    });
  }, []);

  const subscribe = useCallback(
    (scanID: string) => {
      if (unsubsRef.current.has(scanID)) return;
      const unsub = openScanStream(
        scanID,
        (e: ScanProgressEvent) => {
          updateScan(scanID, (prev) => {
            const stages = { ...prev.stages };
            const status: StageStatus =
              e.status === 'error'
                ? 'error'
                : e.status === 'complete'
                  ? 'complete'
                  : e.status === 'start'
                    ? 'running'
                    : 'pending';
            stages[e.stage] = status;
            return {
              ...prev,
              stages,
              lastMessage: e.message,
              done: prev.done || e.stage === 'done',
              errored: prev.errored || e.status === 'error',
            };
          });
        },
        () => {
          // Error on the SSE itself — surface as errored so the bar can
          // show a red state. The actual failure detail comes from the
          // /api/scans/:id polling LiveScanPage already does on demand.
          updateScan(scanID, (prev) => ({ ...prev, errored: true }));
        },
      );
      unsubsRef.current.set(scanID, unsub);
    },
    [updateScan],
  );

  const register = useCallback(
    (scanID: string, target: string) => {
      setScans((curr) => {
        if (curr[scanID]) return curr; // idempotent
        return {
          ...curr,
          [scanID]: {
            scanID,
            target,
            startedAt: Date.now(),
            stages: {},
            done: false,
            errored: false,
          },
        };
      });
      subscribe(scanID);
    },
    [subscribe],
  );

  const unregister = useCallback((scanID: string) => {
    const unsub = unsubsRef.current.get(scanID);
    if (unsub) unsub();
    unsubsRef.current.delete(scanID);
    setScans((curr) => {
      const next = { ...curr };
      delete next[scanID];
      return next;
    });
  }, []);

  // Hydrate from localStorage once on mount, re-subscribing to any
  // in-flight scans the user had before reload.
  useEffect(() => {
    const stored = loadStored();
    if (stored.length === 0) return;
    setScans((curr) => {
      const next = { ...curr };
      for (const s of stored) {
        if (!next[s.scanID]) {
          next[s.scanID] = {
            scanID: s.scanID,
            target: s.target,
            startedAt: s.startedAt,
            stages: {},
            done: false,
            errored: false,
          };
        }
      }
      return next;
    });
    for (const s of stored) subscribe(s.scanID);
    // Cleanup on full unmount (whole app teardown).
    return () => {
      for (const u of unsubsRef.current.values()) u();
      unsubsRef.current.clear();
    };
  }, [subscribe]);

  // Persist on every change. Completed scans roll out of localStorage so a
  // refresh after success doesn't re-show them.
  useEffect(() => {
    persistStored(scans);
  }, [scans]);

  // Auto-cleanup: completed-or-errored scans roll out of the active map
  // after 8 seconds so the progress bar disappears with a small "done"
  // flash. Components that want to keep showing the scan can navigate to
  // /scans/$id which loads from disk.
  useEffect(() => {
    const timers: Record<string, ReturnType<typeof setTimeout>> = {};
    for (const [id, scan] of Object.entries(scans)) {
      if ((scan.done || scan.errored) && !timers[id]) {
        timers[id] = setTimeout(() => unregister(id), 8000);
      }
    }
    return () => {
      for (const t of Object.values(timers)) clearTimeout(t);
    };
  }, [scans, unregister]);

  const value = useMemo<ScanProgressContextValue>(
    () => ({ scans, register, unregister }),
    [scans, register, unregister],
  );

  return <ScanProgressContext.Provider value={value}>{children}</ScanProgressContext.Provider>;
}

export function useScanProgress(): ScanProgressContextValue {
  const ctx = useContext(ScanProgressContext);
  if (!ctx) throw new Error('useScanProgress must be used inside <ScanProgressProvider>');
  return ctx;
}

/**
 * percentForScan computes a 0-100 progress percentage from a scan's stage
 * map. Each completed stage in the 8-stage canonical list contributes 1/8
 * (12.5%). A "running" stage contributes a half-tick so the bar moves
 * smoothly between completions instead of jumping in 12.5% steps.
 */
export function percentForScan(scan: ActiveScan): number {
  let pct = 0;
  for (const stage of SCAN_STAGES) {
    const s = scan.stages[stage];
    if (s === 'complete') pct += 100 / SCAN_STAGES.length;
    else if (s === 'running') pct += 100 / SCAN_STAGES.length / 2;
  }
  if (scan.done) return 100;
  return Math.min(99, Math.round(pct));
}
