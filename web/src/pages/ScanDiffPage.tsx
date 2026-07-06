import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ArrowLeft, GitCompareArrows } from 'lucide-react';
import { Route as DiffRoute } from '@/routes/scans.diff';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { FindingCard } from '@/components/FindingCard';
import { VerdictBadge } from '@/components/VerdictBadge';
import { api } from '@/lib/api';
import type { Finding } from '@/types/api';

/**
 * ScanDiffPage renders a side-by-side comparison of two scans of the same
 * target. The page is reached by deep-link from ScanReportPage's "Compare to
 * previous" button or by manually constructing /scans/diff?a=&b=.
 *
 * Rendering strategy: rather than literal side-by-side columns (cramped on
 * narrow viewports), we group findings by their diff bucket:
 *   1. Added  (green) — present only in B
 *   2. Changed (amber) — both, but severity/evidence/description drifted
 *   3. Resolved (grey, struck-through) — present only in A
 *   4. Stable (collapsed by default) — present in both, unchanged
 *
 * Each finding card is reused from FindingCard, which already knows how to
 * paint its own diff chip from `finding.diff`.
 */
export function ScanDiffPage() {
  const { a, b } = DiffRoute.useSearch();

  const { data, isLoading, error } = useQuery({
    queryKey: ['diff', a, b],
    queryFn: () => api.getDiff(a, b),
    enabled: Boolean(a && b),
  });

  if (!a || !b) {
    return (
      <div className="flex flex-col gap-4 max-w-3xl">
        <BackLink />
        <Card className="p-6 text-sm">
          Two scan IDs required. Use the URL <code>/scans/diff?a=&lt;id&gt;&amp;b=&lt;id&gt;</code> or click "Compare to previous" on a scan report.
        </Card>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="flex flex-col gap-6 max-w-5xl">
        <Skeleton className="h-12 w-2/3" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="flex flex-col gap-4 max-w-3xl">
        <BackLink />
        <div className="rounded-md border border-danger/40 bg-danger/10 p-4 text-sm">
          Failed to load diff: {(error as Error | undefined)?.message ?? 'unknown error'}
        </div>
      </div>
    );
  }

  const counts = {
    added: data.added.length,
    changed: data.changed.length,
    resolved: data.resolved.length,
    stable: data.stable.length,
  };

  return (
    <div className="fade-in flex flex-col gap-8 max-w-5xl">
      <header className="flex flex-col gap-4">
        <BackLink />
        <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-3">
          <div className="min-w-0">
            <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight flex items-center gap-3 flex-wrap">
              <GitCompareArrows className="size-6 text-[color:var(--color-muted-foreground)]" />
              Diff: {data.a.target.name}
            </h1>
            <p className="mt-2 text-xs sm:text-sm text-[color:var(--color-muted-foreground)]">
              <Link
                to="/scans/$id"
                params={{ id: data.a.scan_id }}
                className="font-mono hover:underline"
              >
                {data.a.scan_id.slice(0, 8)}
              </Link>
              {' '}({new Date(data.a.scanned_at).toLocaleString()})
              {' → '}
              <Link
                to="/scans/$id"
                params={{ id: data.b.scan_id }}
                className="font-mono hover:underline"
              >
                {data.b.scan_id.slice(0, 8)}
              </Link>
              {' '}({new Date(data.b.scanned_at).toLocaleString()})
            </p>
          </div>
          <div className="flex items-center gap-3 shrink-0">
            <div className="flex flex-col items-center text-xs text-[color:var(--color-muted-foreground)]">
              <span>before</span>
              <VerdictBadge verdict={data.a.verdict} size="sm" />
            </div>
            <span className="text-2xl text-[color:var(--color-muted-foreground)]">→</span>
            <div className="flex flex-col items-center text-xs text-[color:var(--color-muted-foreground)]">
              <span>after</span>
              <VerdictBadge verdict={data.b.verdict} size="sm" />
            </div>
          </div>
        </div>
      </header>

      <DiffSummary counts={counts} />

      <Section title="Added" subtitle={`${counts.added} new finding${counts.added === 1 ? '' : 's'} in this scan`} accent="emerald">
        {counts.added === 0 ? (
          <EmptyBucket message="No new findings." />
        ) : (
          <FindingList findings={data.added} />
        )}
      </Section>

      <Section title="Changed" subtitle={`${counts.changed} finding${counts.changed === 1 ? '' : 's'} drifted in severity, evidence, or description`} accent="amber">
        {counts.changed === 0 ? (
          <EmptyBucket message="Nothing changed." />
        ) : (
          <FindingList findings={data.changed} />
        )}
      </Section>

      <Section title="Resolved" subtitle={`${counts.resolved} finding${counts.resolved === 1 ? '' : 's'} present before, gone now`} accent="neutral">
        {counts.resolved === 0 ? (
          <EmptyBucket message="No findings disappeared." />
        ) : (
          <FindingList findings={data.resolved} />
        )}
      </Section>

      {counts.stable > 0 && (
        <Section title="Stable" subtitle={`${counts.stable} finding${counts.stable === 1 ? '' : 's'} unchanged`} accent="neutral">
          <FindingList findings={data.stable} />
        </Section>
      )}
    </div>
  );
}

function DiffSummary({ counts }: { counts: { added: number; changed: number; resolved: number; stable: number } }) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
      <SummaryTile label="Added" value={counts.added} accent="emerald" />
      <SummaryTile label="Changed" value={counts.changed} accent="amber" />
      <SummaryTile label="Resolved" value={counts.resolved} accent="neutral" />
      <SummaryTile label="Stable" value={counts.stable} accent="neutral" />
    </div>
  );
}

function SummaryTile({ label, value, accent }: { label: string; value: number; accent: 'emerald' | 'amber' | 'neutral' }) {
  const ring = accent === 'emerald'
    ? 'border-success/40'
    : accent === 'amber'
      ? 'border-warning/40'
      : 'border-[color:var(--color-border)]';
  return (
    <Card className={`p-4 border ${ring}`}>
      <div className="text-xs uppercase tracking-wider text-[color:var(--color-muted-foreground)]">{label}</div>
      <div className="mt-1 text-3xl font-semibold tabular-nums">{value}</div>
    </Card>
  );
}

function Section({
  title,
  subtitle,
  accent,
  children,
}: {
  title: string;
  subtitle: string;
  accent: 'emerald' | 'amber' | 'neutral';
  children: React.ReactNode;
}) {
  const bar = accent === 'emerald'
    ? 'bg-success/60'
    : accent === 'amber'
      ? 'bg-warning/60'
      : 'bg-[color:var(--color-muted-foreground)]/40';
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-baseline gap-3">
        <span className={`inline-block h-3 w-1 rounded ${bar}`} aria-hidden="true" />
        <h2 className="text-lg font-semibold">{title}</h2>
        <span className="text-xs text-[color:var(--color-muted-foreground)]">{subtitle}</span>
      </div>
      {children}
    </section>
  );
}

function FindingList({ findings }: { findings: Finding[] }) {
  return (
    <ol className="flex flex-col gap-3">
      {findings.map((f) => (
        <li key={f.id}>
          <FindingCard finding={f} />
        </li>
      ))}
    </ol>
  );
}

function EmptyBucket({ message }: { message: string }) {
  return (
    <Card className="p-6 text-center text-xs text-[color:var(--color-muted-foreground)]">
      {message}
    </Card>
  );
}

function BackLink() {
  return (
    <Link
      to="/scans"
      className="inline-flex items-center gap-1.5 text-sm text-[color:var(--color-muted-foreground)] hover:text-[color:var(--color-foreground)]"
    >
      <ArrowLeft className="size-4" />
      All scans
    </Link>
  );
}
