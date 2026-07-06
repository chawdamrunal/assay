import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams, useSearch } from '@tanstack/react-router';
import { AlertOctagon, ArrowRight, Sparkles } from 'lucide-react';
import { Card } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { ProgressTimeline } from '@/components/ProgressTimeline';
import { ChatScanThread } from '@/components/ChatScanThread';
import { useScanStream } from '@/hooks/useScanStream';
import { useScanProgress } from '@/lib/scan-progress';
import type { ScanFailure } from '@/types/api';

/**
 * LiveScanPage frames a live scan as a conversation with Assay. The chat
 * thread (left/main) is the primary surface — each SSE stage event becomes
 * an Assay "message" with a typewriter reveal. The pipeline timeline lives
 * in a side panel for users who want the strict 8-stage view.
 */
export function LiveScanPage() {
  const { id } = useParams({ from: '/scans/live/$id' });
  // The list page passes the plugin name through search params so we can
  // address the user with a real target name (not the scan UUID). Falls
  // back to the scan ID when navigated to directly.
  const search = useSearch({ from: '/scans/live/$id' }) as { target?: string };
  const target = search?.target ?? id;
  const navigate = useNavigate();
  const { events, done, errored, stageStatus } = useScanStream(id);
  // Register with the global tracker so navigating away doesn't tear down
  // live progress and so the TopBar chip surfaces this scan even when the
  // user opened it from a deep link (history list, fleet card, etc.).
  const { register } = useScanProgress();
  useEffect(() => {
    if (id) register(id, target);
  }, [id, target, register]);

  const [streamFailure, setStreamFailure] = useState<ScanFailure | null>(null);
  useEffect(() => {
    if (!errored || events.length > 0 || !id) return;
    let cancelled = false;
    void (async () => {
      try {
        const resp = await fetch(`/api/scans/${encodeURIComponent(id)}`, {
          headers: { Accept: 'application/json' },
        });
        if (cancelled) return;
        if (resp.status === 410) {
          const body = (await resp.json()) as ScanFailure;
          setStreamFailure(body);
        } else if (resp.status === 404) {
          setStreamFailure({ scan_id: id, error: 'Scan not found — it may have been deleted or never started.' });
        }
      } catch {
        /* fall through to generic message */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [errored, events.length, id]);

  useEffect(() => {
    if (!done) return;
    // Do not auto-navigate when the scan errored — keep the user on this page
    // so they can see the error state (the error banner below).
    if (errored) return;
    const t = setTimeout(() => {
      navigate({ to: '/scans/$id', params: { id } });
    }, 1800);
    return () => clearTimeout(t);
  }, [done, errored, id, navigate]);

  const hasError = errored || events.some((e) => e.status === 'error');
  const inEventError = events.find((e) => e.status === 'error')?.message;
  const errorMessage =
    inEventError
    ?? (streamFailure
      ? `${streamFailure.stage ? `[${streamFailure.stage}] ` : ''}${streamFailure.error}`
      : undefined);

  const completedStages = useMemo(
    () => Object.values(stageStatus).filter((s) => s === 'complete').length,
    [stageStatus],
  );

  return (
    <div className="fade-in flex flex-col gap-6">
      <header className="flex flex-col gap-3">
        <div className="flex items-center gap-2 text-xs uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
          <Sparkles className="size-3.5 text-[color:var(--color-primary)]" />
          Live conversation with Assay
        </div>
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div className="min-w-0">
            <h1 className="text-3xl font-semibold tracking-tight break-words">{target}</h1>
            <p className="mt-1 text-xs text-[color:var(--color-muted-foreground)] font-mono">
              scan {id}
            </p>
          </div>
          {done && !hasError && (
            <Button onClick={() => navigate({ to: '/scans/$id', params: { id } })}>
              View report
              <ArrowRight className="size-4" />
            </Button>
          )}
        </div>
      </header>

      <div className="grid grid-cols-1 lg:grid-cols-[1fr_300px] gap-6 items-start">
        {/* Chat thread — primary surface */}
        <Card className="p-4 sm:p-6">
          <ChatScanThread events={events} done={done} target={target} />

          {hasError && (
            <div className="mt-4 rounded-md border border-danger/40 bg-danger/10 p-4 text-sm flex items-start gap-3">
              <AlertOctagon className="size-5 text-danger shrink-0" />
              <div>
                <div className="font-semibold text-danger mb-1">Scan failed</div>
                <div className="text-danger/80">{errorMessage ?? 'Assay couldn\'t complete this scan. See log for details.'}</div>
              </div>
            </div>
          )}
        </Card>

        {/* Pipeline sidebar — strict view of the 8 stages */}
        <div className="flex flex-col gap-4 lg:sticky lg:top-4">
          <Card className="p-5">
            <div className="mb-3 flex items-baseline justify-between">
              <h2 className="text-xs font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
                Pipeline
              </h2>
              <span className="text-[10px] text-[color:var(--color-muted-foreground)]">
                {completedStages}/8 stages
              </span>
            </div>
            <ProgressTimeline stageStatus={stageStatus} />
          </Card>
          <p className="px-1 text-xs text-[color:var(--color-muted-foreground)]">
            The pipeline runs seven analysis stages plus a wrap-up. Each stage that
            uses an LLM is signed and cited in the final report — nothing is
            hand-waved.
          </p>
        </div>
      </div>
    </div>
  );
}
