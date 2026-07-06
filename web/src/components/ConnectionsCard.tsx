import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  AlertOctagon,
  Bot,
  CheckCircle2,
  Database,
  Github,
  KeyRound,
  Layers,
  RefreshCw,
  Terminal,
  Webhook,
} from 'lucide-react';
import { Card } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { api } from '@/lib/api';
import type { StatusCheck, StatusKind, StatusLevel } from '@/types/api';

/**
 * ConnectionsCard renders the live health of every integration Assay depends
 * on: Claude Code CLI, the assay MCP server, Anthropic credentials, the
 * data dir on disk, and the pre-install gate hook. Polls /api/status every
 * 30 seconds with a manual "Refresh" affordance.
 *
 * Semantic levels (ok/warn/error) map to a single colored dot per row —
 * no other color usage so the FindingCard severity palette stays the only
 * place red/amber convey damage.
 */
export function ConnectionsCard() {
  const qc = useQueryClient();
  const { data, isLoading, error, isFetching } = useQuery({
    queryKey: ['status'],
    queryFn: api.getStatus,
    refetchInterval: 30_000,
    refetchOnWindowFocus: true,
  });

  return (
    <Card className="p-5 flex flex-col gap-3">
      <header className="flex items-baseline justify-between gap-3 flex-wrap">
        <div className="flex flex-col gap-0.5">
          <h2 className="text-sm font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
            Connections
          </h2>
          <p className="text-xs text-[color:var(--color-muted-foreground)]">
            Live health of Claude Code, the Assay MCP server, credentials, and the install gate.
          </p>
        </div>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => qc.invalidateQueries({ queryKey: ['status'] })}
          disabled={isFetching}
          aria-label="Refresh connection status"
        >
          <RefreshCw className={`size-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </header>

      {isLoading && (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-14 w-full" />
          <Skeleton className="h-14 w-full" />
          <Skeleton className="h-14 w-full" />
        </div>
      )}

      {error && (
        <div className="rounded-md border border-danger/40 bg-danger/10 p-3 text-sm text-danger">
          Status probe failed: {(error as Error).message}
        </div>
      )}

      {data && (
        <ul className="flex flex-col gap-2">
          {data.checks.map((c) => (
            <li key={c.kind}>
              <Row check={c} />
            </li>
          ))}
        </ul>
      )}

      {data && (
        <div className="text-[10px] text-[color:var(--color-muted-foreground)] mt-1">
          Last probed {new Date(data.generated_at).toLocaleTimeString()}
        </div>
      )}
    </Card>
  );
}

function Row({ check }: { check: StatusCheck }) {
  return (
    <div className="flex items-start gap-3 rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-card)] p-3">
      <KindIcon kind={check.kind} level={check.level} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-sm font-medium">{check.name}</span>
          <LevelBadge level={check.level} />
        </div>
        <p className="mt-1 text-xs text-[color:var(--color-muted-foreground)] break-words">
          {check.detail}
        </p>
      </div>
    </div>
  );
}

function KindIcon({ kind, level }: { kind: StatusKind; level: StatusLevel }) {
  const ring =
    level === 'ok'
      ? 'border-success/40 text-success'
      : level === 'warn'
        ? 'border-warning/40 text-warning'
        : 'border-danger/40 text-danger';
  const map: Record<StatusKind, React.ReactNode> = {
    'claude-code': <Bot className="size-4" />,
    'mcp': <Layers className="size-4" />,
    'auth': <KeyRound className="size-4" />,
    'filesystem': <Database className="size-4" />,
    'hook': <Webhook className="size-4" />,
    'anthropic-key': <KeyRound className="size-4" />,
    'gemini-key': <KeyRound className="size-4" />,
    'openai-key': <KeyRound className="size-4" />,
    'github': <Github className="size-4" />,
  };
  return (
    <div className={`grid size-9 shrink-0 place-items-center rounded-md border ${ring} bg-[color:var(--color-background)]`}>
      {map[kind] ?? <Terminal className="size-4" />}
    </div>
  );
}

function LevelBadge({ level }: { level: StatusLevel }) {
  if (level === 'ok') {
    return (
      <span className="inline-flex items-center gap-1 rounded-full border border-success/40 bg-success/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-success">
        <CheckCircle2 className="size-2.5" />
        connected
      </span>
    );
  }
  if (level === 'warn') {
    return (
      <span className="inline-flex items-center gap-1 rounded-full border border-warning/40 bg-warning/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-warning">
        <AlertOctagon className="size-2.5" />
        attention
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-full border border-danger/40 bg-danger/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-danger">
      <AlertOctagon className="size-2.5" />
      error
    </span>
  );
}
