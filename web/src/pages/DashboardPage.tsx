import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import {
  AlertOctagon,
  Bug,
  Layers,
  Package,
  Play,
  ScrollText,
  Server,
  ShieldCheck,
  Syringe,
} from 'lucide-react';
import { Card } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { SeverityDonut } from '@/components/charts/SeverityDonut';
import { ScanFrequency } from '@/components/charts/ScanFrequency';
import { HeroGlow } from '@/components/HeroGlow';
import { api } from '@/lib/api';
import { cn } from '@/lib/utils';
import type { ScanListItem, Verdict, VerdictLabel } from '@/types/api';

/**
 * DashboardPage — v0.4 redesign.
 *
 * The v0.3 dashboard was three em-dash counters. v0.4 surfaces actionable
 * security telemetry: how many plugins are unsafe right now, severity
 * distribution across the corpus, and scan frequency over the last two
 * weeks. Inspired by the "Trust & Authority" pattern: hero stats first,
 * supporting charts second, latest fleet card as a call-to-action.
 */
export function DashboardPage() {
  const { data: inventory, isLoading: invLoading, error: invError } = useQuery({
    queryKey: ['inventory'],
    queryFn: api.getInventory,
  });
  const { data: scans, isLoading: scansLoading, error: scansError } = useQuery({
    queryKey: ['scans'],
    queryFn: api.listScans,
  });
  const { data: fleets } = useQuery({
    queryKey: ['fleet-list'],
    queryFn: api.listFleets,
  });
  const { data: supply } = useQuery({
    queryKey: ['supply-chain'],
    queryFn: api.getSupplyChainSummary,
    refetchInterval: 60_000,
  });

  const pluginCount = (inventory?.items ?? []).filter((it) => it.kind === 'claude-code-plugin').length;
  const mcpServerCount = (inventory?.items ?? []).filter((it) => it.kind === 'mcp-server').length;
  const completedScans = (scans?.items ?? []).filter((s) => s.status === 'complete');
  const latestPerTarget = pickLatestPerTarget(completedScans);

  // Fetch verdicts for the latest scan of every target (capped) so we can
  // render severity distribution + the "Most recent verdicts" rail without
  // round-tripping 50 audit.json files.
  const targetIds = latestPerTarget.map((s) => s.scan_id);
  const verdictsKey = ['verdicts', ...targetIds].sort().join('|');
  const { data: verdicts } = useQuery({
    queryKey: ['verdicts-for-dashboard', verdictsKey],
    queryFn: async () => {
      const out: Verdict[] = [];
      for (const id of targetIds.slice(0, 30)) {
        try {
          const result = await api.getScanResult(id);
          if (result.kind === 'verdict') out.push(result.data);
        } catch {
          /* skip */
        }
      }
      return out;
    },
    enabled: targetIds.length > 0,
  });

  const verdictCounts = countByVerdict(verdicts ?? []);
  const unsafePlugins = (verdicts ?? []).filter((v) => v.verdict === 'unsafe');
  const latestFleet = fleets?.items?.[0];

  // Hero posture — computed from loaded verdicts. While the verdict fetch is
  // still in flight we show an honest "checking…" state rather than a premature
  // "all clear" that could flip to unsafe a moment later.
  const auditedCount = verdicts?.length ?? 0;
  const unsafeCount = unsafePlugins.length;
  const heroPending = verdicts === undefined && targetIds.length > 0;

  return (
    <div className="fade-in flex flex-col gap-8 max-w-6xl">
      {/* Hero posture band — the one number that matters, stated plainly, sitting
          over a whisper-faint indigo glow. Replaces the old plain page title so a
          reviewer reads the current risk state before anything else. */}
      <header className="relative isolate flex flex-col gap-3">
        <HeroGlow />
        <p className="text-[11px] font-medium uppercase tracking-[0.2em] text-[color:var(--color-muted-foreground)]">
          Security posture
        </p>
        <h1 className="text-[2rem] font-semibold leading-[1.08] tracking-tight sm:text-[2.6rem]">
          {heroPending ? (
            <span>
              {pluginCount} plugin{pluginCount === 1 ? '' : 's'} installed
              <span className="text-[color:var(--color-muted-foreground)]"> · checking posture…</span>
            </span>
          ) : auditedCount === 0 ? (
            <span>No plugins audited yet</span>
          ) : (
            <>
              <span>
                {auditedCount} plugin{auditedCount === 1 ? '' : 's'} audited
              </span>
              <span className="mx-2.5 font-normal text-[color:var(--color-muted-foreground)]">·</span>
              {unsafeCount > 0 ? (
                <span className="text-[color:var(--color-danger)]">
                  {unsafeCount} unsafe right now
                </span>
              ) : (
                <span className="inline-flex items-baseline gap-2 text-[color:var(--color-success)]">
                  <ShieldCheck className="size-6 translate-y-1" />
                  all clear
                </span>
              )}
            </>
          )}
        </h1>
        <p className="max-w-xl text-sm text-[color:var(--color-muted-foreground)]">
          {auditedCount === 0 && !heroPending
            ? "Nothing's been audited yet. Run a scan to see what's installed and where the risk lives."
            : "What's installed, what's been audited, and where the risk lives. Click any tile to drill in."}
        </p>
      </header>

      {(invError || scansError) && (
        <div className="rounded-md border border-[color:color-mix(in_oklab,var(--color-danger)_40%,transparent)] bg-[color:var(--color-danger-soft)] p-3 text-sm text-[color:var(--color-danger)]">
          {invError && <div>Failed to load inventory: {(invError as Error).message}</div>}
          {scansError && <div>Failed to load scans: {(scansError as Error).message}</div>}
        </div>
      )}

      {/* KPI row — clean stat cards with a single semantic accent per tile */}
      <section className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <StatTile
          icon={<Package className="size-4" />}
          label="Plugins"
          value={pluginCount}
          loading={invLoading}
          href="/inventory"
        />
        <StatTile
          icon={<Server className="size-4" />}
          label="MCP servers"
          value={mcpServerCount}
          loading={invLoading}
          href="/inventory"
        />
        <StatTile
          icon={<ScrollText className="size-4" />}
          label="Completed scans"
          value={completedScans.length}
          loading={scansLoading}
          href="/scans"
        />
        <StatTile
          icon={<AlertOctagon className="size-4" />}
          label="Unsafe plugins"
          value={unsafePlugins.length}
          accent={unsafePlugins.length > 0 ? 'danger' : 'good'}
          loading={!verdicts && targetIds.length > 0}
          href="/scans"
        />
      </section>

      {/* Supply-chain summary — fleet-wide SCA + tool-poison counters.
          The two new deterministic detectors land here so the user sees
          the supply-chain posture at a glance without opening reports. */}
      <SupplyChainCard summary={supply} />

      {/* Chart row — donut on the left, scan-frequency on the right.
          Stacks to one column under md. */}
      <section className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <Card className="lg:col-span-1 p-5 flex flex-col gap-2">
          <header className="flex items-baseline justify-between">
            <h2 className="text-sm font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
              Severity distribution
            </h2>
            <span className="text-xs text-[color:var(--color-muted-foreground)]">latest per target</span>
          </header>
          {verdicts === undefined && targetIds.length > 0 ? (
            <Skeleton className="h-[260px] w-full" />
          ) : (
            <SeverityDonut verdicts={verdicts ?? []} />
          )}
        </Card>

        <Card className="lg:col-span-2 p-5 flex flex-col gap-3">
          <header className="flex items-baseline justify-between">
            <h2 className="text-sm font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
              Scan activity — last 14 days
            </h2>
            <div className="text-xs text-[color:var(--color-muted-foreground)] flex items-center gap-3">
              <Mini icon={<ShieldCheck className="size-3 text-[color:var(--color-success)]" />} label={`${verdictCounts.safe} safe`} />
              <Mini icon={<AlertOctagon className="size-3 text-[color:var(--color-warning)]" />} label={`${verdictCounts.caution} caution`} />
              <Mini icon={<AlertOctagon className="size-3 text-[color:var(--color-danger)]" />} label={`${verdictCounts.unsafe} unsafe`} />
            </div>
          </header>
          <ScanFrequency scans={scans?.items ?? []} />
        </Card>
      </section>

      {/* CTA + latest fleet rail */}
      <section className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <Card className="p-5 flex flex-col gap-3">
          <h2 className="text-sm font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
            Quick actions
          </h2>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
            <Link to="/scan/new" className="contents">
              <Button className="w-full justify-center">
                <Play className="size-4" />
                Run a scan
              </Button>
            </Link>
            <Link to="/fleet" className="contents">
              <Button variant="outline" className="w-full justify-center">
                <Layers className="size-4" />
                Fleet scan
              </Button>
            </Link>
            <Link to="/scans" className="contents">
              <Button variant="ghost" className="w-full justify-center">
                <ScrollText className="size-4" />
                Browse reports
              </Button>
            </Link>
          </div>
          <p className="text-xs text-[color:var(--color-muted-foreground)] mt-1">
            Scans run via your Claude Code subscription quota in MCP mode (default). No Anthropic API key required.
          </p>
        </Card>

        <Card className="p-5 flex flex-col gap-3">
          <header className="flex items-baseline justify-between gap-2">
            <h2 className="text-sm font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
              Latest fleet scan
            </h2>
            {latestFleet && (
              <Link
                to="/fleet/$id"
                params={{ id: latestFleet.fleet_id }}
                className="text-xs text-[color:var(--color-primary)] hover:underline"
              >
                View →
              </Link>
            )}
          </header>
          {!latestFleet ? (
            <div className="flex flex-col gap-2">
              <p className="text-sm text-[color:var(--color-muted-foreground)]">
                You haven't run a fleet scan yet. Fleet mode scans every installed plugin in parallel and aggregates the verdicts.
              </p>
              <Link to="/fleet">
                <Button variant="outline" size="sm">
                  <Layers className="size-3.5" />
                  Run fleet scan
                </Button>
              </Link>
            </div>
          ) : (
            <div className="flex flex-col gap-1.5">
              <div className="flex items-center gap-2 text-sm">
                <FleetStatusDot status={latestFleet.status} />
                <span className="font-medium capitalize">{latestFleet.status}</span>
                <span className="text-[color:var(--color-muted-foreground)]">·</span>
                <span className="text-[color:var(--color-muted-foreground)]">{latestFleet.members.length} plugin{latestFleet.members.length === 1 ? '' : 's'}</span>
              </div>
              <div className="text-xs text-[color:var(--color-muted-foreground)]">
                Started {new Date(latestFleet.started_at).toLocaleString()}
              </div>
              <div className="mt-2 font-mono text-xs text-[color:var(--color-muted-foreground)] truncate">
                {latestFleet.fleet_id}
              </div>
            </div>
          )}
        </Card>
      </section>
    </div>
  );
}

/**
 * SupplyChainCard surfaces fleet-wide SCA + tool-poisoning counts.
 *
 *   ┌─ Supply chain ─────────────────────────────────────────────┐
 *   │  Vulnerable deps     Poisoned tools     Affected plugins   │
 *   │   3 critical          2 findings         5 of 12 plugins   │
 *   │   1 high                                                   │
 *   └────────────────────────────────────────────────────────────┘
 *
 * Polls every 60s. Empty state ("All clear") is its own pleasant
 * artifact — security tools should celebrate when nothing's wrong.
 */
function SupplyChainCard({ summary }: { summary?: import('@/types/api').SupplyChainSummary }) {
  const dep = summary
    ? summary.dependency_critical + summary.dependency_high + summary.dependency_medium
    : 0;
  const poison = summary?.poison_findings ?? 0;
  const allClear = summary && dep === 0 && poison === 0;
  return (
    <Card className="p-5 flex flex-col gap-3">
      <header className="flex items-baseline justify-between flex-wrap gap-2">
        <h2 className="text-sm font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
          Supply chain
        </h2>
        <span className="text-xs text-[color:var(--color-muted-foreground)]">
          {summary ? `${summary.affected_plugins} of ${summary.total_scans} plugins affected` : 'loading…'}
        </span>
      </header>
      {allClear ? (
        <div className="flex items-center gap-2 text-sm">
          <ShieldCheck className="size-4 text-[color:var(--color-success)]" />
          <span>All clear — no known vulnerable dependencies, no poisoned tool descriptions across {summary.total_scans} scanned plugin{summary.total_scans === 1 ? '' : 's'}.</span>
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <SupplyChainTile
            icon={<Bug className="size-4" />}
            label="Vulnerable dependencies"
            value={dep}
            danger={(summary?.dependency_critical ?? 0) + (summary?.dependency_high ?? 0)}
            breakdown={summary ? `${summary.dependency_critical} critical · ${summary.dependency_high} high · ${summary.dependency_medium} medium` : ''}
          />
          <SupplyChainTile
            icon={<Syringe className="size-4" />}
            label="Poisoned tool descriptions"
            value={poison}
            danger={poison}
            breakdown={poison > 0 ? 'Tool descriptions contain instructions or role manipulation' : 'No tool-poisoning patterns detected'}
          />
        </div>
      )}
    </Card>
  );
}

function SupplyChainTile({
  icon,
  label,
  value,
  danger,
  breakdown,
}: {
  icon: React.ReactNode;
  label: string;
  value: number;
  danger: number;
  breakdown: string;
}) {
  const bad = danger > 0;
  const warn = !bad && value > 0;
  const frame = bad
    ? 'border-[color:color-mix(in_oklab,var(--color-danger)_38%,transparent)] bg-[color:var(--color-danger-soft)]'
    : warn
      ? 'border-[color:color-mix(in_oklab,var(--color-warning)_38%,transparent)] bg-[color:var(--color-warning-soft)]'
      : 'border-[color:color-mix(in_oklab,var(--color-success)_30%,transparent)] bg-[color:var(--color-success-soft)]';
  const chip = bad
    ? 'bg-[color:var(--color-danger-soft)] text-[color:var(--color-danger)]'
    : warn
      ? 'bg-[color:var(--color-warning-soft)] text-[color:var(--color-warning)]'
      : 'bg-[color:var(--color-success-soft)] text-[color:var(--color-success)]';
  const valueColor = bad
    ? 'text-[color:var(--color-danger)]'
    : warn
      ? 'text-[color:var(--color-warning)]'
      : 'text-[color:var(--color-success)]';
  return (
    <div className={cn('rounded-lg border p-3.5', frame)}>
      <div className="flex items-center gap-2.5">
        <span className={cn('grid size-8 place-items-center rounded-lg', chip)}>{icon}</span>
        <span className="text-[11px] font-medium uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
          {label}
        </span>
      </div>
      <div className={cn('mt-2.5 text-3xl font-semibold tabular-nums', valueColor)}>{value}</div>
      <div className="mt-1 text-xs text-[color:var(--color-muted-foreground)]">{breakdown}</div>
    </div>
  );
}

interface StatTileProps {
  icon: React.ReactNode;
  label: string;
  value: number | undefined;
  loading?: boolean;
  href?: string;
  accent?: 'default' | 'good' | 'danger';
}

function StatTile({ icon, label, value, loading, href, accent = 'default' }: StatTileProps) {
  const bad = accent === 'danger' && (value ?? 0) > 0;
  const good = accent === 'good' && (value ?? 0) === 0;
  // The "unsafe" tile earns a red ring + glow only when it's actually bad —
  // a security dashboard should pull the eye to the one number that matters.
  const ringClass = bad
    ? 'border-[color:color-mix(in_oklab,var(--color-danger)_45%,transparent)] shadow-[var(--shadow-glow-danger)]'
    : good
      ? 'border-[color:color-mix(in_oklab,var(--color-success)_40%,transparent)]'
      : 'border-[color:var(--color-border)] hover:border-[color:var(--color-border-strong)]';
  const iconChip = bad
    ? 'bg-[color:var(--color-danger-soft)] text-[color:var(--color-danger)]'
    : good
      ? 'bg-[color:var(--color-success-soft)] text-[color:var(--color-success)]'
      : 'bg-[color:var(--color-primary-soft)] text-[color:var(--color-primary)]';
  const valueColor = bad
    ? 'text-[color:var(--color-danger)]'
    : good
      ? 'text-[color:var(--color-success)]'
      : 'text-[color:var(--color-foreground)]';
  const display = loading ? '…' : value === undefined ? '—' : String(value);
  const inner = (
    <Card
      className={cn(
        'group relative isolate overflow-hidden p-4 transition-all duration-200 ease-out',
        'hover:-translate-y-0.5 hover:shadow-[var(--shadow-elevated)]',
        ringClass,
      )}
    >
      <span className={cn('grid size-9 place-items-center rounded-lg transition-colors', iconChip)}>
        {icon}
      </span>
      <div className="mt-3.5 text-[11px] font-medium uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
        {label}
      </div>
      <div className={cn('mt-0.5 text-[2rem] font-semibold leading-none tabular-nums', valueColor)}>
        {display}
      </div>
    </Card>
  );
  if (!href) return inner;
  return (
    <Link to={href} className="block">
      {inner}
    </Link>
  );
}

function Mini({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      {icon}
      <span>{label}</span>
    </span>
  );
}

function FleetStatusDot({ status }: { status: 'running' | 'complete' | 'failed' }) {
  const cls =
    status === 'complete'
      ? 'bg-[color:var(--color-success)]'
      : status === 'failed'
        ? 'bg-[color:var(--color-danger)]'
        : 'bg-[color:var(--color-warning)] animate-pulse';
  return <span className={`size-2 rounded-full ${cls}`} aria-hidden="true" />;
}

// pickLatestPerTarget collapses the scans list to one row per target (the
// most recent completed scan). Dashboard charts then represent each target's
// current state, not the entire scan history.
function pickLatestPerTarget(items: ScanListItem[]): ScanListItem[] {
  const byTarget = new Map<string, ScanListItem>();
  for (const it of items) {
    const prev = byTarget.get(it.target);
    if (!prev) {
      byTarget.set(it.target, it);
      continue;
    }
    const a = prev.created_at ?? prev.scan_id;
    const b = it.created_at ?? it.scan_id;
    if (b > a) byTarget.set(it.target, it);
  }
  return Array.from(byTarget.values());
}

function countByVerdict(verdicts: Verdict[]): Record<VerdictLabel, number> {
  const out: Record<VerdictLabel, number> = { safe: 0, caution: 0, unsafe: 0 };
  for (const v of verdicts) {
    if (v.verdict in out) out[v.verdict]++;
  }
  return out;
}
