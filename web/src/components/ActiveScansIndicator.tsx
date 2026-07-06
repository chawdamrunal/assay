import { useState } from 'react';
import { Link } from '@tanstack/react-router';
import { AnimatePresence, motion } from 'framer-motion';
import { AlertOctagon, CheckCircle2, ChevronDown, Loader2, ShieldCheck } from 'lucide-react';
import { percentForScan, useScanProgress, type ActiveScan } from '@/lib/scan-progress';
import { cn } from '@/lib/utils';

/**
 * ActiveScansIndicator renders in the TopBar whenever one or more scans
 * are in flight. Two layers:
 *
 *  1. A compact summary chip (always visible while scans are active):
 *     spinner + "N scanning · 42%" with the aggregate progress across all
 *     in-flight scans. Click to expand.
 *
 *  2. A dropdown listing each scan with its own progress bar, stage label,
 *     and a "View" link that jumps to /scans/live/:id.
 *
 * Both layers come straight from the ScanProgressProvider context, which
 * itself owns the SSE subscriptions and survives every navigation. So
 * the user can move between Dashboard / Settings / anywhere and the
 * indicator keeps updating without re-mounting any subscriptions.
 */
export function ActiveScansIndicator({
  align = 'right',
  className,
}: {
  align?: 'left' | 'right';
  className?: string;
} = {}) {
  const { scans } = useScanProgress();
  const [open, setOpen] = useState(false);

  const list = Object.values(scans).sort((a, b) => b.startedAt - a.startedAt);
  if (list.length === 0) return null;

  const inFlight = list.filter((s) => !s.done && !s.errored);
  const aggregatePct =
    list.length === 0
      ? 0
      : Math.round(list.reduce((acc, s) => acc + percentForScan(s), 0) / list.length);

  const summaryLabel =
    inFlight.length > 0
      ? `${inFlight.length} scanning`
      : list.some((s) => s.errored)
        ? `${list.length} scan${list.length === 1 ? '' : 's'} · error`
        : `${list.length} complete`;

  return (
    <div className={cn('relative', className)}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={cn(
          'flex items-center gap-2 rounded-full border px-2.5 py-1 text-xs font-medium transition-colors',
          inFlight.length > 0
            ? 'border-primary/40 bg-primary/10 text-primary hover:bg-primary/15'
            : list.some((s) => s.errored)
              ? 'border-danger/40 bg-danger/10 text-danger'
              : 'border-success/40 bg-success/10 text-success',
        )}
        aria-expanded={open}
        aria-label="Toggle active scans"
      >
        {inFlight.length > 0 ? (
          <Loader2 className="size-3 animate-spin" />
        ) : list.some((s) => s.errored) ? (
          <AlertOctagon className="size-3" />
        ) : (
          <ShieldCheck className="size-3" />
        )}
        <span>{summaryLabel}</span>
        {inFlight.length > 0 && <span className="tabular-nums opacity-80">· {aggregatePct}%</span>}
        <ChevronDown className={cn('size-3 transition-transform', open && 'rotate-180')} />
      </button>

      <AnimatePresence>
        {open && (
          <motion.div
            initial={{ opacity: 0, y: -6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.15 }}
            className={cn(
              'absolute top-full z-50 mt-2 w-80 max-w-[calc(100vw-2rem)] rounded-lg border border-[color:var(--color-border)] bg-[color:var(--color-card)] p-2 shadow-2xl',
              align === 'left' ? 'left-0' : 'right-0',
            )}
          >
            <div className="px-2 py-1 text-[10px] uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
              Active scans
            </div>
            <ul className="flex flex-col gap-1">
              {list.map((s) => (
                <li key={s.scanID}>
                  <ScanRow scan={s} onClose={() => setOpen(false)} />
                </li>
              ))}
            </ul>
            <div className="mt-1 px-2 pt-1.5 pb-0.5 text-[10px] text-[color:var(--color-muted-foreground)] border-t border-[color:var(--color-border)]">
              Scans keep running in the background while you navigate.
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

function ScanRow({ scan, onClose }: { scan: ActiveScan; onClose: () => void }) {
  const pct = percentForScan(scan);
  const currentStage = currentStageLabel(scan);
  const barColor = scan.errored
    ? 'bg-danger'
    : scan.done
      ? 'bg-success'
      : 'bg-primary';

  return (
    <Link
      to="/scans/live/$id"
      params={{ id: scan.scanID }}
      search={{ target: scan.target }}
      onClick={onClose}
      className="block rounded-md px-2 py-2 hover:bg-[color:var(--color-muted)]"
    >
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{scan.target}</div>
          <div className="text-[10px] text-[color:var(--color-muted-foreground)]">{currentStage}</div>
        </div>
        <div className="shrink-0">
          {scan.done ? (
            <CheckCircle2 className="size-4 text-success" />
          ) : scan.errored ? (
            <AlertOctagon className="size-4 text-danger" />
          ) : (
            <span className="text-xs tabular-nums text-[color:var(--color-muted-foreground)]">{pct}%</span>
          )}
        </div>
      </div>
      <div className="mt-1.5 h-1 w-full overflow-hidden rounded-full bg-[color:var(--color-muted)]">
        <div
          className={cn('h-full transition-[width] duration-500', barColor)}
          style={{ width: `${pct}%` }}
        />
      </div>
    </Link>
  );
}

function currentStageLabel(scan: ActiveScan): string {
  if (scan.errored) return 'errored';
  if (scan.done) return 'complete';
  // Find the most recent running stage; fall back to last completed.
  let running: string | undefined;
  let lastComplete: string | undefined;
  for (const [stage, status] of Object.entries(scan.stages)) {
    if (status === 'running') running = stage;
    if (status === 'complete') lastComplete = stage;
  }
  if (running) return `running ${prettyStage(running)}`;
  if (lastComplete) return `finished ${prettyStage(lastComplete)}`;
  return 'starting…';
}

function prettyStage(stage: string): string {
  switch (stage) {
    case 'prepass':
      return 'pre-pass';
    case 'threat_model':
      return 'threat modeling';
    case 'exploitability':
      return 'exploitability analysis';
    case 'claims':
      return 'claim extraction';
    default:
      return stage;
  }
}
