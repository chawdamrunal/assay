import { useMemo, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { ChevronRight, FolderOpen, Package, Play, Server } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';
import { api } from '@/lib/api';
import { useScanProgress } from '@/lib/scan-progress';
import type { Item } from '@/types/api';

interface PickerEntry {
  name: string;
  kind: Item['kind'];
  path: string;
  version?: string;
  metaText?: string;
}

export function NewScanPage() {
  const navigate = useNavigate();
  const { register } = useScanProgress();
  const [target, setTarget] = useState('');
  const [offline, setOffline] = useState(true);
  const [compareToPrevious, setCompareToPrevious] = useState(true);
  const [submitError, setSubmitError] = useState<string | null>(null);
  // Last requested human name — threaded into the live-scan URL so Assay
  // can address the user with the plugin name instead of the scan UUID.
  const [pendingName, setPendingName] = useState<string | null>(null);

  const { data: inventory, isLoading: invLoading, error: invError } = useQuery({
    queryKey: ['inventory'],
    queryFn: api.getInventory,
  });

  const startScan = useMutation({
    mutationFn: ({ target, offline, since }: { target: string; offline: boolean; since?: string }) =>
      api.startScan(target, offline, since),
    onSuccess: (resp) => {
      // Register with the global tracker BEFORE navigating so the TopBar
      // chip appears immediately and the SSE subscription keeps streaming
      // even if the user clicks somewhere else before the scan completes.
      register(resp.scan_id, pendingName ?? resp.scan_id.slice(0, 8));
      navigate({
        to: '/scans/live/$id',
        params: { id: resp.scan_id },
        search: pendingName ? { target: pendingName } : {},
      });
    },
    onError: (err: Error) => setSubmitError(err.message),
  });

  const { plugins, mcpServers, others } = useMemo(() => {
    const items = inventory?.items ?? [];
    const p: PickerEntry[] = [];
    const m: PickerEntry[] = [];
    const o: PickerEntry[] = [];
    for (const it of items) {
      if (!it.local_path) {
        // Hooks + MCP servers without resolvable paths surface in "Other".
        const meta = it.metadata?.commandLine || it.metadata?.command;
        o.push({ name: it.name, kind: it.kind, path: it.local_path ?? '', version: it.version, metaText: meta });
        continue;
      }
      const entry: PickerEntry = {
        name: it.name,
        kind: it.kind,
        path: it.local_path,
        version: it.version,
        metaText: it.metadata?.marketplace,
      };
      if (it.kind === 'claude-code-plugin') p.push(entry);
      else if (it.kind === 'mcp-server') m.push(entry);
      else o.push(entry);
    }
    return { plugins: p, mcpServers: m, others: o };
  }, [inventory]);

  const runScan = (path: string, name?: string) => {
    setSubmitError(null);
    setPendingName(name ?? null);
    startScan.mutate({ target: path, offline, since: compareToPrevious ? 'latest' : undefined });
  };

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitError(null);
    const trimmed = target.trim();
    if (!trimmed) {
      setSubmitError('Target path is required');
      return;
    }
    // Derive a friendly name from the path basename for custom paths.
    const basename = trimmed.replace(/\/+$/, '').split('/').pop() || trimmed;
    runScan(trimmed, basename);
  };

  // Render-time hint shown beneath the picker grids: tells the user the
  // compare-to-previous toggle is honored for every click below.
  const compareHint = compareToPrevious
    ? 'Auto-diffing against the latest prior scan of each target (toggle off in the Custom path card).'
    : 'Auto-diff disabled — each scan will be reported standalone.';

  return (
    <div className="fade-in flex flex-col gap-6 max-w-3xl">
      <header className="flex flex-col gap-2">
        <h1 className="text-3xl font-semibold tracking-tight">New Scan</h1>
        <p className="text-sm text-[color:var(--color-muted-foreground)]">
          Pick an installed plugin or MCP server below, or scan an arbitrary path. Scans run via Claude Code — no API key needed.
        </p>
        <p className="text-xs text-[color:var(--color-muted-foreground)]">{compareHint}</p>
      </header>

      <Section icon={<Package className="size-4" />} title="Installed plugins" count={plugins.length}>
        {invLoading ? (
          <PickerSkeleton />
        ) : invError ? (
          <ErrorBox message={`Couldn't load inventory: ${(invError as Error).message}`} />
        ) : plugins.length === 0 ? (
          <EmptyHint text="No Claude Code plugins detected under ~/.claude/plugins/installed_plugins.json." />
        ) : (
          <PickerGrid entries={plugins} onPick={runScan} disabled={startScan.isPending} />
        )}
      </Section>

      <Section icon={<Server className="size-4" />} title="MCP servers" count={mcpServers.length}>
        {mcpServers.length === 0 ? (
          <EmptyHint text="No MCP servers with a local install path in your settings.json. Servers launched via npx/uvx don't have a scannable source directory locally." />
        ) : (
          <PickerGrid entries={mcpServers} onPick={runScan} disabled={startScan.isPending} />
        )}
      </Section>

      {others.length > 0 && (
        <Section icon={<FolderOpen className="size-4" />} title="Other" count={others.length}>
          <PickerGrid entries={others} onPick={runScan} disabled={startScan.isPending} />
        </Section>
      )}

      <form onSubmit={onSubmit}>
        <Card className="p-5 flex flex-col gap-4">
          <div className="flex items-center gap-2">
            <FolderOpen className="size-4 text-[color:var(--color-muted-foreground)]" />
            <div className="text-sm font-medium">Custom path</div>
          </div>
          <input
            id="target"
            type="text"
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            placeholder="/absolute/path/to/any/plugin/or/mcp/source"
            className="rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-card)] px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-[color:var(--color-primary)]"
          />
          <div className="flex items-start gap-2">
            <input
              id="offline"
              type="checkbox"
              checked={offline}
              onChange={(e) => setOffline(e.target.checked)}
              className="mt-1"
            />
            <label htmlFor="offline" className="text-sm">
              <div className="font-medium">Offline mode</div>
              <div className="text-xs text-[color:var(--color-muted-foreground)]">
                Skip OSV CVE lookups (no outbound network from the scanner). Recommended for first runs.
              </div>
            </label>
          </div>
          <div className="flex items-start gap-2">
            <input
              id="compare"
              type="checkbox"
              checked={compareToPrevious}
              onChange={(e) => setCompareToPrevious(e.target.checked)}
              className="mt-1"
            />
            <label htmlFor="compare" className="text-sm">
              <div className="font-medium">Compare against latest prior scan</div>
              <div className="text-xs text-[color:var(--color-muted-foreground)]">
                Auto-diffs the new audit against the most-recent existing scan of this target. Findings get new / changed / stable badges; resolved findings appear in a separate "Resolved" section.
              </div>
            </label>
          </div>

          {submitError && <ErrorBox message={submitError} />}

          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-xs text-[color:var(--color-muted-foreground)]">
              Scans use your Claude Code subscription quota when serve runs in <code>--scan-mode mcp</code> (default).
            </p>
            <Button type="submit" disabled={startScan.isPending || !target.trim()}>
              <Play className="size-4" />
              {startScan.isPending ? 'Starting…' : 'Scan custom path'}
            </Button>
          </div>
        </Card>
      </form>
    </div>
  );
}

function Section({
  icon,
  title,
  count,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  count: number;
  children: React.ReactNode;
}) {
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        {icon}
        <h2 className="text-sm font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
          {title}
        </h2>
        <span className="text-xs text-[color:var(--color-muted-foreground)]">({count})</span>
      </div>
      {children}
    </section>
  );
}

function PickerGrid({
  entries,
  onPick,
  disabled,
}: {
  entries: PickerEntry[];
  onPick: (path: string, name?: string) => void;
  disabled: boolean;
}) {
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
      {entries.map((e) => (
        <button
          key={e.path || e.name}
          type="button"
          onClick={() => e.path && onPick(e.path, e.name)}
          disabled={disabled || !e.path}
          className={cn(
            'group text-left rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-card)] p-3',
            'transition-colors',
            !e.path ? 'opacity-60 cursor-not-allowed' : 'hover:border-[color:var(--color-primary)] hover:bg-[color:var(--color-muted)] cursor-pointer',
          )}
        >
          <div className="flex items-start gap-2">
            <div className="flex-1 min-w-0">
              <div className="flex items-baseline gap-2">
                <div className="font-medium truncate">{e.name}</div>
                {e.version && (
                  <span className="text-xs text-[color:var(--color-muted-foreground)] tabular-nums truncate">
                    v{e.version}
                  </span>
                )}
              </div>
              <div className="mt-0.5 text-xs text-[color:var(--color-muted-foreground)] truncate font-mono">
                {e.path || e.metaText || '(no scannable path)'}
              </div>
              {e.metaText && e.path && (
                <div className="mt-0.5 text-xs text-[color:var(--color-muted-foreground)] truncate">
                  {e.metaText}
                </div>
              )}
            </div>
            <ChevronRight className="size-4 shrink-0 text-[color:var(--color-muted-foreground)] group-hover:text-[color:var(--color-primary)]" />
          </div>
        </button>
      ))}
    </div>
  );
}

function PickerSkeleton() {
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
      <Skeleton className="h-16 w-full" />
      <Skeleton className="h-16 w-full" />
      <Skeleton className="h-16 w-full" />
      <Skeleton className="h-16 w-full" />
    </div>
  );
}

function EmptyHint({ text }: { text: string }) {
  return (
    <div className="rounded-md border border-dashed border-[color:var(--color-border)] bg-[color:var(--color-card)] p-4 text-xs text-[color:var(--color-muted-foreground)]">
      {text}
    </div>
  );
}

function ErrorBox({ message }: { message: string }) {
  return (
    <div className="rounded-md border border-danger/40 bg-danger/10 p-3 text-sm text-danger">
      {message}
    </div>
  );
}
