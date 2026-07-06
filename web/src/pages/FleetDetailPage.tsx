import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from '@tanstack/react-router';
import { AlertOctagon, ArrowLeft, CheckCircle2, Clock, Layers, ShieldAlert, ShieldCheck, ShieldX } from 'lucide-react';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { ProgressTimeline } from '@/components/ProgressTimeline';
import { HeroGlow } from '@/components/HeroGlow';
import { cn } from '@/lib/utils';
import { api, openFleetStream } from '@/lib/api';
import type { FleetEvent, FleetMemberReport } from '@/types/api';

/**
 * FleetDetailPage shows one fleet's aggregate report + live per-plugin
 * progress. We poll the snapshot every 4s while the fleet is running, and
 * subscribe to the SSE stream to drive per-plugin timeline state.
 */
export function FleetDetailPage() {
  const { id } = useParams({ from: '/fleet/$id' });
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['fleet', id],
    queryFn: () => api.getFleet(id),
    refetchInterval: (q) => (q.state.data?.status === 'complete' ? false : 4000),
  });

  // Per-member stageStatus map driven by SSE events. Once a member emits
  // {stage:'done'} we keep its final status in the map (the timeline
  // component renders complete steps as green checkmarks).
  const [stageByScan, setStageByScan] = useState<Record<string, Record<string, string>>>({});

  useEffect(() => {
    if (!id) return;
    const unsub = openFleetStream(
      id,
      (ev: FleetEvent) => {
        if (!ev.scan_id) {
          // Fleet-wide event ("fleet/complete"); trigger a snapshot refetch.
          void refetch();
          return;
        }
        setStageByScan((prev) => {
          const inner = { ...(prev[ev.scan_id] ?? {}) };
          inner[ev.stage] = ev.status;
          return { ...prev, [ev.scan_id]: inner };
        });
      },
      () => { /* errors are non-fatal — fall back to snapshot polling */ },
    );
    return unsub;
  }, [id, refetch]);

  if (isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-12 w-2/3" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="flex flex-col gap-4">
        <BackLink />
        <div className="rounded-md border border-danger/40 bg-danger/10 p-4 text-sm">
          Failed to load fleet: {(error as Error | undefined)?.message ?? 'unknown'}
        </div>
      </div>
    );
  }

  return (
    <div className="fade-in flex flex-col gap-6 max-w-5xl">
      <header className="relative isolate flex flex-col gap-3">
        <HeroGlow />
        <BackLink />
        <div className="flex flex-col sm:flex-row sm:items-baseline sm:justify-between gap-2">
          <div className="flex items-baseline gap-3 min-w-0">
            <Layers className="size-5 text-[color:var(--color-muted-foreground)]" />
            <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Fleet scan</h1>
            <span className="font-mono text-xs text-[color:var(--color-muted-foreground)] truncate">{id}</span>
          </div>
          <StatusPill status={data.status} />
        </div>
        <p className="text-xs text-[color:var(--color-muted-foreground)] flex items-center gap-1.5">
          <Clock className="size-3.5" />
          Started {new Date(data.started_at).toLocaleString()}
          {data.finished_at && ` · Finished ${new Date(data.finished_at).toLocaleString()}`}
        </p>
      </header>

      <AggregateTiles report={data} />

      <section className="flex flex-col gap-2">
        <h2 className="text-sm font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
          Per-plugin progress
        </h2>
        <ul className="grid grid-cols-1 lg:grid-cols-2 gap-3">
          {data.members.map((m) => (
            <li key={m.scan_id}>
              <MemberCard member={m} stageStatus={stageByScan[m.scan_id] ?? {}} />
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}

function AggregateTiles({ report }: { report: { verdict_counts: { safe: number; caution: number; unsafe: number }; severity_counts: { critical: number; high: number; medium: number; low: number; info: number } } }) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
      <Tile label="Safe" value={report.verdict_counts.safe} tone="success" icon={<ShieldCheck className="size-4" />} />
      <Tile label="Caution" value={report.verdict_counts.caution} tone="warning" icon={<ShieldAlert className="size-4" />} />
      <Tile label="Unsafe" value={report.verdict_counts.unsafe} tone="danger" icon={<ShieldX className="size-4" />} feature />
      <Tile label="Critical + High" value={report.severity_counts.critical + report.severity_counts.high} tone="danger" icon={<AlertOctagon className="size-4" />} />
    </div>
  );
}

type Tone = 'success' | 'warning' | 'danger';

/**
 * Tile — the Dashboard StatTile treatment for a fleet aggregate: a soft-tinted
 * icon chip, an uppercase label, and a big tabular value colored by state. The
 * "Unsafe" tile (feature) earns a red ring + glow only when its count is > 0,
 * pulling the eye to the one number that matters.
 */
function Tile({ label, value, tone, icon, feature }: { label: string; value: number; tone: Tone; icon: ReactNode; feature?: boolean }) {
  const bad = feature === true && value > 0;
  const chip = tone === 'success' ? 'bg-success/12 text-success'
    : tone === 'warning' ? 'bg-warning/12 text-warning'
      : 'bg-danger/12 text-danger';
  const valueColor = tone === 'success' ? 'text-success'
    : tone === 'warning' ? 'text-warning'
      : 'text-danger';
  return (
    <Card
      className={cn(
        'p-4 transition-all duration-200 ease-out hover:-translate-y-0.5 hover:shadow-[var(--shadow-elevated)]',
        bad
          ? 'border-danger/45 shadow-[var(--shadow-glow-danger)]'
          : 'border-[color:var(--color-border)] hover:border-[color:var(--color-border-strong)]',
      )}
    >
      <span className={cn('grid size-9 place-items-center rounded-lg', chip)}>{icon}</span>
      <div className="mt-3 text-[11px] font-medium uppercase tracking-wider text-[color:var(--color-muted-foreground)]">{label}</div>
      <div className={cn('mt-0.5 text-[2rem] font-semibold leading-none tabular-nums', value === 0 ? 'text-[color:var(--color-foreground)]' : valueColor)}>
        {value}
      </div>
    </Card>
  );
}

function MemberCard({ member, stageStatus }: { member: FleetMemberReport; stageStatus: Record<string, string> }) {
  const effectiveStage = useMemo(() => {
    if (member.status === 'complete') {
      return { prepass: 'complete', triage: 'complete', claims: 'complete', threat_model: 'complete', investigation: 'complete', exploitability: 'complete', synthesis: 'complete', done: 'complete' };
    }
    return stageStatus;
  }, [member.status, stageStatus]);
  const targetName = member.target.split('/').pop() ?? member.target;
  return (
    <Card className="p-4 flex flex-col gap-3">
      <div className="flex items-baseline justify-between gap-2">
        <Link to="/scans/$id" params={{ id: member.scan_id }} className="font-medium hover:underline truncate">
          {targetName}
        </Link>
        <MemberBadge member={member} />
      </div>
      <ProgressTimeline stageStatus={effectiveStage} />
      {member.status === 'complete' && (
        <div className="text-xs text-[color:var(--color-muted-foreground)]">
          {member.findings ?? 0} finding{(member.findings ?? 0) === 1 ? '' : 's'}
          {(member.critical ?? 0) > 0 && ` · ${member.critical} critical`}
          {(member.high ?? 0) > 0 && ` · ${member.high} high`}
        </div>
      )}
      {member.status === 'failed' && member.error_reason && (
        <div className="text-xs text-danger break-words">
          {member.error_reason}
        </div>
      )}
    </Card>
  );
}

function MemberBadge({ member }: { member: FleetMemberReport }) {
  if (member.status === 'complete' && member.verdict) {
    const color = member.verdict === 'safe'
      ? 'text-success border-success/40 bg-success/10'
      : member.verdict === 'caution'
        ? 'text-warning border-warning/40 bg-warning/10'
        : 'text-danger border-danger/40 bg-danger/10';
    return (
      <span className={`inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium uppercase ${color}`}>
        {member.verdict === 'unsafe' && <AlertOctagon className="size-3" />}
        {member.verdict === 'safe' && <CheckCircle2 className="size-3" />}
        {member.verdict}
      </span>
    );
  }
  if (member.status === 'failed') {
    return (
      <span className="inline-flex items-center gap-1 rounded-md border border-danger/40 bg-danger/10 px-2 py-0.5 text-xs text-danger">
        failed
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-md border border-warning/40 bg-warning/10 px-2 py-0.5 text-xs text-warning">
      running
    </span>
  );
}

function StatusPill({ status }: { status: 'running' | 'complete' | 'failed' }) {
  const label = status === 'complete' ? 'COMPLETE' : status === 'failed' ? 'FAILED' : 'RUNNING';
  const cls = status === 'complete'
    ? 'border-success/40 bg-success/10 text-success'
    : status === 'failed'
      ? 'border-danger/40 bg-danger/10 text-danger'
      : 'border-warning/40 bg-warning/10 text-warning';
  return (
    <span className={`inline-flex items-center rounded-md border px-2.5 py-0.5 text-xs font-semibold uppercase ${cls}`}>
      {label}
    </span>
  );
}

function BackLink() {
  return (
    <Link to="/fleet" className="inline-flex items-center gap-1.5 text-sm text-[color:var(--color-muted-foreground)] hover:text-[color:var(--color-foreground)]">
      <ArrowLeft className="size-4" />
      All fleet scans
    </Link>
  );
}
