import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { CheckCircle2, Clock, Layers, Play } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { api } from '@/lib/api';
import type { FleetMeta } from '@/types/api';

/**
 * FleetListPage shows past fleet scans and exposes the "Run new fleet scan"
 * action. Each card links to the fleet detail page, which is where live
 * progress + the aggregate report live.
 */
export function FleetListPage() {
  const qc = useQueryClient();
  const navigate = useNavigate();

  const { data, isLoading, error } = useQuery({
    queryKey: ['fleet-list'],
    queryFn: api.listFleets,
  });

  const start = useMutation({
    mutationFn: () => api.startFleetScan({}),
    onSuccess: (resp) => {
      qc.invalidateQueries({ queryKey: ['fleet-list'] });
      navigate({ to: '/fleet/$id', params: { id: resp.fleet_id } });
    },
  });

  return (
    <div className="fade-in flex flex-col gap-6">
      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-3xl font-semibold tracking-tight">Fleet scans</h1>
          <p className="text-sm text-[color:var(--color-muted-foreground)]">
            Scans every installed Claude Code plugin in parallel and aggregates the verdicts. This is the question <code className="font-mono text-xs">git clone</code> can't answer at scale.
          </p>
        </div>
        <Button onClick={() => start.mutate()} disabled={start.isPending}>
          <Play className="size-4" />
          {start.isPending ? 'Starting…' : 'Run new fleet scan'}
        </Button>
      </header>

      {start.isError && (
        <div className="rounded-md border border-danger/40 bg-danger/10 p-3 text-sm text-danger">
          {(start.error as Error).message}
        </div>
      )}

      {isLoading && (
        <>
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-24 w-full" />
        </>
      )}

      {error && (
        <div className="rounded-md border border-danger/40 bg-danger/10 p-4 text-sm">
          Failed to load fleets: {(error as Error).message}
        </div>
      )}

      {data && data.items.length === 0 && (
        <Card className="p-8 text-center">
          <Layers className="mx-auto size-8 text-[color:var(--color-muted-foreground)] mb-2" />
          <p className="text-[color:var(--color-muted-foreground)]">
            No fleet scans yet. Click "Run new fleet scan" above to scan every installed plugin in parallel.
          </p>
        </Card>
      )}

      <ul className="grid grid-cols-1 lg:grid-cols-2 gap-3">
        {(data?.items ?? []).map((meta) => (
          <li key={meta.fleet_id}>
            <FleetCard meta={meta} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function FleetCard({ meta }: { meta: FleetMeta }) {
  const isComplete = meta.status === 'complete';
  return (
    <Link to="/fleet/$id" params={{ id: meta.fleet_id }} className="block">
      <Card className="p-4 flex flex-col gap-2 hover:bg-[color:var(--color-muted)]/40 transition-colors">
        <div className="flex items-center justify-between gap-2">
          <div className="font-mono text-xs truncate text-[color:var(--color-muted-foreground)]">
            {meta.fleet_id}
          </div>
          <StatusPill status={meta.status} />
        </div>
        <div className="flex items-center gap-1.5 text-xs text-[color:var(--color-muted-foreground)]">
          <Clock className="size-3.5" />
          {new Date(meta.started_at).toLocaleString()}
        </div>
        <div className="text-sm">{meta.members.length} plugin{meta.members.length === 1 ? '' : 's'}</div>
        {!isComplete && (
          <div className="text-xs text-[color:var(--color-muted-foreground)]">running…</div>
        )}
      </Card>
    </Link>
  );
}

function StatusPill({ status }: { status: 'running' | 'complete' | 'failed' }) {
  if (status === 'complete') {
    return (
      <span className="inline-flex items-center gap-1 rounded-md border border-success/40 bg-success/10 px-2 py-0.5 text-xs font-medium text-success">
        <CheckCircle2 className="size-3" />
        complete
      </span>
    );
  }
  if (status === 'failed') {
    return (
      <span className="inline-flex items-center gap-1 rounded-md border border-danger/40 bg-danger/10 px-2 py-0.5 text-xs font-medium text-danger">
        failed
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-md border border-warning/40 bg-warning/10 px-2 py-0.5 text-xs font-medium text-warning">
      running
    </span>
  );
}
