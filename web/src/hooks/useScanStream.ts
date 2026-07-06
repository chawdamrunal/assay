import { useEffect, useMemo, useRef, useState } from 'react';
import { openScanStream } from '@/lib/api';
import type { ScanProgressEvent } from '@/types/api';

export interface ScanStreamState {
  /** All events received so far, in arrival order. */
  events: ScanProgressEvent[];
  /** True once the terminal `done` event has been received. */
  done: boolean;
  /** Last event received, or null. */
  lastEvent: ScanProgressEvent | null;
  /** Map of stage → most recent status for that stage. */
  stageStatus: Record<string, string>;
  /** Set once the EventSource has errored at least once. */
  errored: boolean;
}

/**
 * Subscribe to scan progress events for the given scan ID.
 *
 * Passing `null` for scanID disables the subscription (useful before a scan
 * has started). Changing the scanID closes the previous subscription and
 * opens a fresh one. The hook auto-unsubscribes on unmount.
 */
export function useScanStream(scanID: string | null): ScanStreamState {
  const [events, setEvents] = useState<ScanProgressEvent[]>([]);
  const [done, setDone] = useState(false);
  const [errored, setErrored] = useState(false);
  const unsubRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    // Reset state for a new scan.
    setEvents([]);
    setDone(false);
    setErrored(false);

    if (!scanID) {
      return;
    }

    const unsubscribe = openScanStream(
      scanID,
      (e) => {
        setEvents((prev) => [...prev, e]);
        if (e.stage === 'done') {
          setDone(true);
        }
      },
      () => {
        setErrored(true);
      },
    );
    unsubRef.current = unsubscribe;

    return () => {
      if (unsubRef.current) {
        unsubRef.current();
        unsubRef.current = null;
      }
    };
  }, [scanID]);

  const stageStatus = useMemo(() => {
    const m: Record<string, string> = {};
    for (const e of events) {
      m[e.stage] = e.status;
    }
    return m;
  }, [events]);

  const lastEvent = events.length > 0 ? events[events.length - 1] : null;

  return { events, done, lastEvent, stageStatus, errored };
}
