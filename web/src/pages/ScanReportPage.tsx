import { lazy, Suspense, useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { AlertOctagon, ArrowLeft, FileJson, GitCompareArrows, RefreshCw, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { Markdown } from '@/components/Markdown';
import { VerdictBadge } from '@/components/VerdictBadge';
import { FindingCard, severityRank } from '@/components/FindingCard';
import { ThreatModelView } from '@/components/ThreatModelView';
import { ClaimsVsRealityView } from '@/components/ClaimsVsRealityView';
import { CopyButton } from '@/components/CopyButton';
import { HeroGlow } from '@/components/HeroGlow';
import { SectionRail, SectionPills, useScrollSpy, type NavSection } from '@/components/ReportSectionNav';

// FlowGraph lays the scan's data-flow Mermaid out as a structured React Flow
// graph (trust-boundary groups, kind-coloured nodes, red animated exfil edges).
// It parses the same Mermaid the LLM already emits — no backend change — and
// falls back to the raw Mermaid SVG renderer (ThreatDiagram) on any parse miss,
// so it is never worse than the previous render.
const FlowGraph = lazy(() =>
  import('@/components/FlowGraph').then((m) => ({ default: m.FlowGraph })),
);
import { api } from '@/lib/api';
import { useScanProgress } from '@/lib/scan-progress';
import type { Severity, Finding, ScanFailure, VerdictLabel } from '@/types/api';
import { cn } from '@/lib/utils';

const allSeverities: Severity[] = ['critical', 'high', 'medium', 'low', 'info'];

export function ScanReportPage() {
  const { id } = useParams({ from: '/scans/$id' });
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: result, isLoading, error } = useQuery({
    queryKey: ['scan', id],
    queryFn: () => api.getScanResult(id),
    retry: 1,
  });
  const verdict = result?.kind === 'verdict' ? result.data : undefined;
  const failure = result?.kind === 'failure' ? result.data : undefined;

  // A 404 here usually doesn't mean "gone" — it means the scan hasn't
  // finalized yet (audit.json absent), which is exactly what you get when you
  // refresh onto the report URL while the scan is still running. Probe the
  // scan list: if it's still `pending`, send the user to the live view (which
  // streams progress and auto-returns here on completion) instead of dead-
  // ending on "scan not found". Only fires when the report load failed.
  const pendingProbe = useQuery({
    queryKey: ['scans', 'report-pending-probe'],
    queryFn: () => api.listScans(),
    enabled: Boolean(error),
  });
  const isStillRunning =
    Boolean(error) &&
    (pendingProbe.data?.items.some((s) => s.scan_id === id && s.status === 'pending') ?? false);
  useEffect(() => {
    if (isStillRunning) {
      navigate({ to: '/scans/live/$id', params: { id }, replace: true });
    }
  }, [isStillRunning, id, navigate]);

  const [filter, setFilter] = useState<Severity | 'all'>('all');
  const [askDelete, setAskDelete] = useState(false);
  const del = useMutation({
    mutationFn: () => api.deleteScan(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['scans'] });
      navigate({ to: '/scans' });
    },
  });

  const sortedFindings: Finding[] = useMemo(() => {
    if (!verdict) return [];
    return [...(verdict.findings ?? [])].sort(
      (a, b) => severityRank(b.severity) - severityRank(a.severity),
    );
  }, [verdict]);

  const filteredFindings = useMemo(() => {
    if (filter === 'all') return sortedFindings;
    return sortedFindings.filter((f) => f.severity === filter);
  }, [filter, sortedFindings]);

  const severityCounts = useMemo(() => {
    const counts = { critical: 0, high: 0, medium: 0, low: 0, info: 0 } as Record<Severity, number>;
    for (const f of sortedFindings) counts[f.severity]++;
    return counts;
  }, [sortedFindings]);

  // Section list for the "On this page" nav — only the sections that actually
  // render (Data Flow / Threat Model / Claims / Open Questions are conditional).
  const navSections = useMemo<NavSection[]>(() => {
    if (!verdict) return [];
    return [
      { id: 'overview', label: 'Overview' },
      ...(verdict.summary ? [{ id: 'summary', label: 'Summary' }] : []),
      ...(verdict.data_flow_diagram ? [{ id: 'dataflow', label: 'Data Flow' }] : []),
      ...(verdict.threat_model ? [{ id: 'threats', label: 'Threat Model' }] : []),
      ...(verdict.claims_vs_reality ? [{ id: 'claims', label: 'Claims vs Reality' }] : []),
      { id: 'findings', label: 'Findings' },
      ...(verdict.open_questions && verdict.open_questions.length > 0
        ? [{ id: 'questions', label: 'Open Questions' }]
        : []),
      { id: 'metadata', label: 'Metadata' },
    ];
  }, [verdict]);
  const spy = useScrollSpy(navSections.map((s) => s.id));

  if (isLoading) {
    return (
      <div className="flex flex-col gap-6 max-w-5xl">
        <Skeleton className="h-12 w-2/3" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (error) {
    // Don't flash the error while we're still checking whether this is a
    // live scan to resume (or already redirecting to the live view).
    if (pendingProbe.isLoading || isStillRunning) {
      return (
        <div className="flex flex-col gap-6 max-w-5xl">
          <Skeleton className="h-12 w-2/3" />
          <Skeleton className="h-32 w-full" />
        </div>
      );
    }
    return (
      <div className="flex flex-col gap-4 max-w-3xl">
        <BackLink />
        <div className="rounded-md border border-danger/40 bg-danger/10 p-4 text-sm">
          Failed to load scan: {error.message}
        </div>
      </div>
    );
  }

  if (failure) {
    return <FailedScanReport id={id} failure={failure} />;
  }

  if (!verdict) {
    return (
      <div className="flex flex-col gap-4 max-w-3xl">
        <BackLink />
        <p className="text-[color:var(--color-muted-foreground)]">Scan not found.</p>
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-4">
      {/* Mobile section nav — sticky pills (desktop uses the right rail below). */}
      <SectionPills sections={navSections} active={spy.active} onNavigate={spy.scrollTo} />

      <div className="lg:flex lg:gap-8">
        <main className="flex min-w-0 flex-1 flex-col gap-10">
          {/* 1. Verdict hero */}
          <header id="overview" className="reveal relative isolate scroll-mt-20">
            <HeroGlow />
            <BackLink />
            <Card className="mt-3 flex flex-col gap-4 p-6 md:p-8">
              <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                <div className="min-w-0 flex-1">
                  <h1 className="break-words text-2xl font-semibold tracking-tight sm:text-3xl">
                    {friendlyTargetName(verdict.target.name)}
                    {verdict.target.version && (
                      <span className="ml-2 text-base text-[color:var(--color-muted-foreground)] sm:ml-3 sm:text-xl">
                        v{verdict.target.version}
                      </span>
                    )}
                  </h1>
                  {/* Show the original (hash-suffixed) name as a subtitle when we
                      stripped it for display, so the canonical name stays
                      visible/grep-able without bloating the headline. */}
                  {friendlyTargetName(verdict.target.name) !== verdict.target.name && (
                    <p className="mt-1 break-all font-mono text-xs text-[color:var(--color-muted-foreground)]">
                      {verdict.target.name}
                    </p>
                  )}
                  {/* Derived, factual posture line (from the real counts). */}
                  <p className="mt-3 max-w-2xl text-sm leading-relaxed sm:text-base">
                    <PostureLine
                      verdict={verdict.verdict}
                      counts={severityCounts}
                      total={sortedFindings.length}
                    />
                  </p>
                </div>
                <div className="shrink-0">
                  <VerdictBadge verdict={verdict.verdict} size="lg" />
                </div>
              </div>

              {sortedFindings.length > 0 && <SeverityDots counts={severityCounts} />}

              <p className="break-words text-xs text-[color:var(--color-muted-foreground)]">
                Scanned {new Date(verdict.scanned_at).toLocaleString()} ·{' '}
                {verdict.scanner.name} v{verdict.scanner.version} using {verdict.scanner.model} with prompts {verdict.scanner.prompt_version}
              </p>

              {/* Action row */}
              <div className="flex flex-wrap items-center gap-2 border-t border-[color:var(--color-border)] pt-4">
                <Link to="/scan/new">
                  <Button variant="outline" size="sm">
                    <RefreshCw className="size-3.5" />
                    Re-scan
                  </Button>
                </Link>
                <a
                  href={`/api/scans/${encodeURIComponent(id)}`}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex"
                >
                  <Button variant="ghost" size="sm">
                    <FileJson className="size-3.5" />
                    View JSON
                  </Button>
                </a>
                {verdict.prior_scan_id && (
                  <Link to="/scans/diff" search={{ a: verdict.prior_scan_id, b: id }}>
                    <Button variant="outline" size="sm">
                      <GitCompareArrows className="size-3.5" />
                      Compare to previous
                    </Button>
                  </Link>
                )}
                {askDelete ? (
                  <span className="ml-auto inline-flex items-center gap-2">
                    <span className="text-xs text-[color:var(--color-muted-foreground)]">
                      Delete this scan permanently?
                    </span>
                    <Button variant="ghost" size="sm" onClick={() => setAskDelete(false)} disabled={del.isPending}>
                      Cancel
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="border-danger/40 text-danger hover:bg-danger/10"
                      onClick={() => del.mutate()}
                      disabled={del.isPending}
                    >
                      {del.isPending ? 'Deleting…' : 'Confirm'}
                    </Button>
                  </span>
                ) : (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setAskDelete(true)}
                    className="ml-auto text-[color:var(--color-muted-foreground)]"
                  >
                    <Trash2 className="size-3.5" />
                    Delete
                  </Button>
                )}
                {del.isError && (
                  <div className="basis-full rounded-md border border-danger/40 bg-danger/10 p-2 text-xs text-danger">
                    Delete failed: {(del.error as Error).message}
                  </div>
                )}
              </div>
            </Card>
          </header>

      {/* Executive Summary */}
      {verdict.summary && (
        <Section id="summary" title="Executive Summary" className="reveal" style={{ animationDelay: '45ms' }}>
          <Markdown source={verdict.summary} />
        </Section>
      )}

      {/* Data Flow Diagram — plain Mermaid SVG via the lazy ThreatDiagram.
          The threat model references these nodes, so this section is
          placed before it. */}
      {verdict.data_flow_diagram && (
        <Section id="dataflow" title="Data Flow" className="reveal" style={{ animationDelay: '90ms' }}>
          <Suspense
            fallback={
              <div className="my-4 text-sm text-[color:var(--color-muted-foreground)]">
                Loading diagram…
              </div>
            }
          >
            <FlowGraph mermaid={stripMermaidFence(verdict.data_flow_diagram)} />
          </Suspense>
        </Section>
      )}

      {/* Threat Model — structured cards (T1..Tn) with metadata pills
          and finding-id anchor links instead of a wall of markdown. */}
      {verdict.threat_model && (
        <Section id="threats" title="Threat Model" className="reveal" style={{ animationDelay: '135ms' }}>
          <ThreatModelView markdown={verdict.threat_model} />
        </Section>
      )}

      {/* Claims vs Reality — side-by-side comparison rows showing
          marketing claim vs what the code actually does. */}
      {verdict.claims_vs_reality && (
        <Section id="claims" title="Claims vs. Reality" className="reveal" style={{ animationDelay: '180ms' }}>
          <ClaimsVsRealityView markdown={verdict.claims_vs_reality} />
        </Section>
      )}

      {/* Findings */}
      <section id="findings" className="reveal flex scroll-mt-20 flex-col gap-3" style={{ animationDelay: '225ms' }}>
        <div className="flex items-baseline justify-between px-1">
          <h2 className="text-sm font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
            Findings
          </h2>
          <span className="text-xs text-[color:var(--color-muted-foreground)]">
            {sortedFindings.length} total
          </span>
        </div>
        {sortedFindings.length === 0 ? (
          <Card className="p-8 text-center text-[color:var(--color-muted-foreground)]">
            No findings. This artifact passed all investigations.
          </Card>
        ) : (
          <>
            {/* Sticky filter strip — stays visible while the user scrolls
                through 10+ findings. Backdrop-blur keeps text readable when
                a finding card scrolls under it. */}
            <div className="sticky top-11 z-20 -mx-1 flex flex-wrap items-center justify-between gap-2 rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-card)]/95 px-3 py-2 backdrop-blur supports-[backdrop-filter]:bg-[color:var(--color-card)]/75 lg:top-0">
              <div className="flex flex-wrap items-center gap-1.5">
                <FilterChip active={filter === 'all'} onClick={() => setFilter('all')} label="All" count={sortedFindings.length} />
                {allSeverities.map((sev) =>
                  severityCounts[sev] > 0 ? (
                    <FilterChip
                      key={sev}
                      active={filter === sev}
                      onClick={() => setFilter(sev)}
                      label={sev}
                      count={severityCounts[sev]}
                    />
                  ) : null,
                )}
              </div>
              <div className="text-[11px] text-[color:var(--color-muted-foreground)]">
                Click a finding to expand
              </div>
            </div>
            <ol className="flex flex-col gap-2">
              {filteredFindings.map((f) => (
                <li key={f.id}>
                  <FindingCard finding={f} />
                </li>
              ))}
            </ol>
          </>
        )}
      </section>

      {/* Open Questions */}
      {verdict.open_questions && verdict.open_questions.length > 0 && (
        <Section id="questions" title="Open Questions" className="reveal" style={{ animationDelay: '270ms' }}>
          <ul className="space-y-1.5 text-sm">
            {verdict.open_questions.map((q, idx) => (
              <li key={idx} className="flex gap-2">
                <span className="text-[color:var(--color-muted-foreground)]">•</span>
                <span>{q}</span>
              </li>
            ))}
          </ul>
        </Section>
      )}

          {/* Audit Metadata */}
          <Section id="metadata" title="Audit Metadata" className="reveal" style={{ animationDelay: '315ms' }}>
            <dl className="grid grid-cols-1 gap-x-8 gap-y-4 sm:grid-cols-2">
              <Meta k="Scan ID" v={verdict.scan_id} mono copy />
              <Meta k="Target kind" v={verdict.target.kind} />
              {verdict.target.source && <Meta k="Source" v={verdict.target.source} mono />}
              {verdict.target.hash && <Meta k="Target hash" v={verdict.target.hash} mono copy />}
              <Meta k="Model" v={verdict.scanner.model} />
              <Meta k="Prompt version" v={verdict.scanner.prompt_version} />
              <Meta k="Schema version" v={verdict.schema_version} />
            </dl>
          </Section>
        </main>

        {/* Desktop right rail — "On this page" scroll-spy nav. */}
        <SectionRail sections={navSections} active={spy.active} onNavigate={spy.scrollTo} />
      </div>
    </div>
  );
}

function FailedScanReport({ id, failure }: { id: string; failure: ScanFailure }) {
  const failedAt = failure.failed_at ? new Date(failure.failed_at) : null;
  const navigate = useNavigate();
  const { register } = useScanProgress();
  // Retry: re-POST /api/scans with the same target. On success we register
  // the new scan with the global tracker and navigate to its live page.
  const retry = useMutation({
    mutationFn: () => {
      if (!failure.target) throw new Error('failure record has no target — cannot retry');
      return api.startScan(failure.target, true /* offline */);
    },
    onSuccess: (resp) => {
      register(resp.scan_id, failure.target ?? resp.scan_id.slice(0, 8));
      navigate({
        to: '/scans/live/$id',
        params: { id: resp.scan_id },
        search: failure.target ? { target: failure.target } : {},
      });
    },
  });
  return (
    <div className="fade-in flex flex-col gap-6 max-w-3xl">
      <BackLink />
      <header className="flex flex-col gap-2">
        <h1 className="text-3xl font-semibold tracking-tight">Scan failed</h1>
        <p className="text-sm text-[color:var(--color-muted-foreground)] font-mono">
          scan id: {id}
        </p>
      </header>
      <Card className="p-5 border-danger/40 bg-danger/5 flex items-start gap-3">
        <AlertOctagon className="size-5 text-danger shrink-0 mt-0.5" />
        <div className="flex flex-col gap-2 min-w-0">
          <div className="font-semibold text-danger">
            {failure.stage ? `Failed during ${failure.stage}` : 'Scanner error'}
          </div>
          <pre className="whitespace-pre-wrap break-words text-sm text-danger/90 font-mono">
            {failure.error}
          </pre>
          {failure.target && (
            <div className="text-xs text-[color:var(--color-muted-foreground)]">
              Target: <span className="font-mono">{failure.target}</span>
            </div>
          )}
          {failedAt && (
            <div className="text-xs text-[color:var(--color-muted-foreground)]">
              Failed at {failedAt.toLocaleString()}
            </div>
          )}
        </div>
      </Card>
      <div className="flex flex-wrap gap-2">
        {failure.target && (
          <Button onClick={() => retry.mutate()} disabled={retry.isPending}>
            <RefreshCw className={cn('size-4', retry.isPending && 'animate-spin')} />
            {retry.isPending ? 'Restarting…' : 'Retry this scan'}
          </Button>
        )}
        <Link to="/scan/new">
          <Button variant={failure.target ? 'outline' : 'default'}>
            <RefreshCw className="size-4" />
            Try another scan
          </Button>
        </Link>
        <a
          href={`/api/scans/${encodeURIComponent(id)}`}
          target="_blank"
          rel="noreferrer"
          className="inline-flex"
        >
          <Button variant="ghost">
            <FileJson className="size-4" />
            View raw error.json
          </Button>
        </a>
      </div>
    </div>
  );
}

// friendlyTargetName trims the trailing -<8hex> suffix the GitHub auto-clone
// path appends to every cached source (e.g. "appsecco-vulnerable-lab-6ad09be1"
// → "appsecco-vulnerable-lab"). Keeps the original visible as a subtitle so
// the canonical name stays grep-able. Returns the input unchanged when no
// hash suffix is present.
function friendlyTargetName(raw: string): string {
  const hashSuffix = /-[0-9a-f]{8}$/i;
  if (!hashSuffix.test(raw)) return raw;
  return raw.replace(hashSuffix, '');
}

// stripMermaidFence removes the ```mermaid ... ``` wrapper the LLM
// sometimes adds around its diagram output, so ThreatDiagram receives the
// raw flowchart body. If no fence is present the original text is returned.
function stripMermaidFence(src: string): string {
  const trimmed = src.trim();
  if (!trimmed.startsWith('```')) return trimmed;
  const lines = trimmed.split('\n');
  lines.shift();
  if (lines[lines.length - 1]?.trim() === '```') lines.pop();
  return lines.join('\n');
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

function Section({
  id,
  title,
  right,
  children,
  className,
  style,
}: {
  id?: string;
  title: string;
  right?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  style?: React.CSSProperties;
}) {
  return (
    <section id={id} style={style} className={cn('scroll-mt-20', className)}>
      <Card className="flex flex-col gap-5 p-6 md:p-8">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h2 className="text-sm font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
            {title}
          </h2>
          {right}
        </div>
        {children}
      </Card>
    </section>
  );
}

function FilterChip({
  active,
  onClick,
  label,
  count,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  count: number;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'inline-flex cursor-pointer items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium uppercase tracking-wide transition-colors',
        active
          ? 'border-[color:var(--color-primary)] bg-[color:var(--color-primary)] text-[color:var(--color-primary-foreground)]'
          : 'border-[color:var(--color-border)] text-[color:var(--color-muted-foreground)] hover:border-[color:var(--color-border-strong)] hover:bg-[color:var(--color-muted)] hover:text-[color:var(--color-foreground)]',
      )}
    >
      {label}
      <span
        className={cn(
          'rounded-full px-1.5 py-px text-[10px] tabular-nums',
          active ? 'bg-black/20' : 'bg-[color:var(--color-muted)]',
        )}
      >
        {count}
      </span>
    </button>
  );
}

function Meta({ k, v, mono, copy }: { k: string; v: string; mono?: boolean; copy?: boolean }) {
  return (
    <div className="min-w-0">
      <dt className="mb-1 text-xs font-medium uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
        {k}
      </dt>
      <dd className="flex items-center gap-1.5 break-all">
        <span className={cn('text-sm', mono && 'font-mono text-xs')}>{v}</span>
        {copy && <CopyButton text={v} label={`Copy ${k}`} className="-my-1 shrink-0" />}
      </dd>
    </div>
  );
}

// PostureLine — one derived, factual sentence summarizing the verdict from the
// real severity counts (no fabricated prose). Renders inline in the hero.
function PostureLine({
  verdict,
  counts,
  total,
}: {
  verdict: VerdictLabel;
  counts: Record<Severity, number>;
  total: number;
}) {
  if (verdict === 'unsafe') {
    const parts: string[] = [];
    if (counts.critical > 0) parts.push(`${counts.critical} critical`);
    if (counts.high > 0) parts.push(`${counts.high} high-severity`);
    const lead = parts.length
      ? `${parts.join(' and ')} finding${counts.critical + counts.high === 1 ? '' : 's'}`
      : `${total} finding${total === 1 ? '' : 's'}`;
    return (
      <>
        <strong className="font-semibold text-[color:var(--color-danger)]">Unsafe</strong> — {lead} across {total}{' '}
        total.
      </>
    );
  }
  if (verdict === 'caution') {
    return (
      <>
        <strong className="font-semibold text-[color:var(--color-warning)]">Caution</strong> — {total} finding
        {total === 1 ? '' : 's'} warrant{total === 1 ? 's' : ''} review before you rely on this.
      </>
    );
  }
  return (
    <>
      <strong className="font-semibold text-[color:var(--color-success)]">Safe</strong> — passed every investigation
      {total > 0 ? ` (${total} informational note${total === 1 ? '' : 's'})` : ''}.
    </>
  );
}

const SEV_DOT: Record<'critical' | 'high' | 'medium' | 'low', string> = {
  critical: 'bg-severity-critical',
  high: 'bg-severity-high',
  medium: 'bg-severity-medium',
  low: 'bg-severity-low',
};
const SEV_TEXT: Record<'critical' | 'high' | 'medium' | 'low', string> = {
  critical: 'text-severity-critical',
  high: 'text-severity-high',
  medium: 'text-severity-medium',
  low: 'text-severity-low',
};

// SeverityDots — the at-a-glance count row in the hero (critical/high/medium/low).
function SeverityDots({ counts }: { counts: Record<Severity, number> }) {
  const order = ['critical', 'high', 'medium', 'low'] as const;
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
      {order.map((sev) => (
        <span key={sev} className="inline-flex items-center gap-1.5 text-xs">
          <span
            aria-hidden="true"
            className={cn(
              'size-2 rounded-full',
              counts[sev] > 0 ? SEV_DOT[sev] : 'bg-[color:var(--color-border-strong)]',
            )}
          />
          <span className="text-[color:var(--color-muted-foreground)]">
            <span
              className={cn(
                'font-semibold tabular-nums',
                counts[sev] > 0 ? SEV_TEXT[sev] : 'text-[color:var(--color-muted-foreground)]',
              )}
            >
              {counts[sev]}
            </span>{' '}
            {sev}
          </span>
        </span>
      ))}
    </div>
  );
}
