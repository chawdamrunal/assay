import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { AlertOctagon, CheckCircle2, Clock, Plus, ScrollText, ShieldCheck, Trash2 } from 'lucide-react';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { Button } from '@/components/ui/button';
import { VerdictBadge } from '@/components/VerdictBadge';
import { HeroGlow } from '@/components/HeroGlow';
import { api } from '@/lib/api';
import type { ScanListItem, ScanStatus } from '@/types/api';

export function ScansListPage() {
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ['scans'],
    queryFn: api.listScans,
  });
  const [pendingDelete, setPendingDelete] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const del = useMutation({
    mutationFn: (id: string) => api.deleteScan(id),
    onSuccess: () => {
      setPendingDelete(null);
      setDeleteError(null);
      void qc.invalidateQueries({ queryKey: ['scans'] });
    },
    onError: (err: Error) => {
      setDeleteError(err.message);
    },
  });

  if (isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Header />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col gap-4">
        <Header />
        <div className="rounded-md border border-danger/40 bg-danger/10 p-4 text-sm">
          Failed to load scans: {error.message}
        </div>
      </div>
    );
  }

  const items = data?.items ?? [];
  const unsafeCount = items.filter((i) => i.verdict === 'unsafe').length;
  const completeCount = items.filter((i) => i.status === 'complete').length;

  if (items.length === 0) {
    return (
      <div className="flex flex-col gap-4">
        <Header />
        <Card className="flex flex-col items-center gap-3 p-12 text-center">
          <div className="grid size-12 place-items-center rounded-2xl bg-[color:var(--color-primary-soft)] text-[color:var(--color-primary)]">
            <ScrollText className="size-6" />
          </div>
          <p className="max-w-sm text-[color:var(--color-muted-foreground)]">
            No scans yet. Run your first scan and its full report will land here.
          </p>
          <Link to="/scan/new">
            <Button>
              <Plus className="size-4" />
              New scan
            </Button>
          </Link>
        </Card>
      </div>
    );
  }

  return (
    <div className="fade-in flex flex-col gap-6">
      <Header stats={{ total: items.length, unsafe: unsafeCount, complete: completeCount }} />

      {deleteError && (
        <div className="rounded-md border border-danger/40 bg-danger/10 p-3 text-sm text-danger">
          Delete failed: {deleteError}
        </div>
      )}

      <ul className="grid grid-cols-1 lg:grid-cols-2 gap-3">
        {items.map((item) => (
          <li key={item.scan_id}>
            <ScanCard
              item={item}
              isPendingDelete={pendingDelete === item.scan_id}
              onAskDelete={() => {
                setDeleteError(null);
                setPendingDelete(item.scan_id);
              }}
              onCancelDelete={() => setPendingDelete(null)}
              onConfirmDelete={() => del.mutate(item.scan_id)}
              deleting={del.isPending && pendingDelete === item.scan_id}
            />
          </li>
        ))}
      </ul>
    </div>
  );
}

function ScanCard({
  item,
  isPendingDelete,
  onAskDelete,
  onCancelDelete,
  onConfirmDelete,
  deleting,
}: {
  item: ScanListItem;
  isPendingDelete: boolean;
  onAskDelete: () => void;
  onCancelDelete: () => void;
  onConfirmDelete: () => void;
  deleting: boolean;
}) {
  // Pending scans don't have an audit.json yet — link them to the live
  // page so the user sees progress, not a 404 on the report page.
  const isPending = item.status === 'pending';
  return (
    <Card className="p-4 flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          {isPending ? (
            <Link
              to="/scans/live/$id"
              params={{ id: item.scan_id }}
              search={{ target: item.target }}
              className="font-medium hover:underline truncate block"
            >
              {item.target}
            </Link>
          ) : (
            <Link
              to="/scans/$id"
              params={{ id: item.scan_id }}
              className="font-medium hover:underline truncate block"
            >
              {item.target}
            </Link>
          )}
          <div className="mt-0.5 font-mono text-xs text-[color:var(--color-muted-foreground)] truncate">
            {item.scan_id}
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {item.verdict && <VerdictBadge verdict={item.verdict} size="sm" />}
          <StatusBadge status={item.status} />
        </div>
      </div>

      {item.created_at && (
        <div className="text-xs text-[color:var(--color-muted-foreground)] flex items-center gap-1.5">
          <Clock className="size-3.5" />
          {new Date(item.created_at).toLocaleString()}
        </div>
      )}

      <div className="text-xs text-[color:var(--color-muted-foreground)] truncate font-mono">
        {item.dir}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2 pt-1">
        {isPending ? (
          <Link
            to="/scans/live/$id"
            params={{ id: item.scan_id }}
            search={{ target: item.target }}
          >
            <Button variant="outline" size="sm">View live progress</Button>
          </Link>
        ) : (
          <Link to="/scans/$id" params={{ id: item.scan_id }}>
            <Button variant="outline" size="sm">View report</Button>
          </Link>
        )}
        {isPendingDelete ? (
          <div className="flex items-center gap-2">
            <span className="text-xs text-[color:var(--color-muted-foreground)]">
              Delete this scan?
            </span>
            <Button variant="ghost" size="sm" onClick={onCancelDelete} disabled={deleting}>
              Cancel
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="border-danger/40 text-danger hover:bg-danger/10"
              onClick={onConfirmDelete}
              disabled={deleting}
            >
              {deleting ? 'Deleting…' : 'Confirm'}
            </Button>
          </div>
        ) : (
          <Button variant="ghost" size="sm" onClick={onAskDelete} className="text-[color:var(--color-muted-foreground)]">
            <Trash2 className="size-3.5" />
            Delete
          </Button>
        )}
      </div>
    </Card>
  );
}

function StatusBadge({ status }: { status: ScanStatus | undefined }) {
  if (status === 'failed') {
    return (
      <span className="inline-flex items-center gap-1 rounded-md border border-danger/40 bg-danger/10 px-2 py-0.5 text-xs font-medium text-danger shrink-0">
        <AlertOctagon className="size-3" />
        Failed
      </span>
    );
  }
  if (status === 'pending') {
    return (
      <span className="inline-flex items-center gap-1 rounded-md border border-warning/40 bg-warning/10 px-2 py-0.5 text-xs font-medium text-warning shrink-0">
        <Clock className="size-3" />
        Pending
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-md border border-success/40 bg-success/10 px-2 py-0.5 text-xs font-medium text-success shrink-0">
      <CheckCircle2 className="size-3" />
      Complete
    </span>
  );
}

/**
 * Header doubles as the page's posture hero when `stats` are available (the
 * populated list). It mirrors the Dashboard's hero-band pattern — an eyebrow,
 * a big headline that surfaces the one number that matters (unsafe count), and
 * a HeroGlow anchor. Loading / error / empty states render the plain title.
 */
function Header({ stats }: { stats?: { total: number; unsafe: number; complete: number } }) {
  return (
    <header className="relative isolate flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      {stats && <HeroGlow />}
      <div className="flex min-w-0 flex-col gap-1.5">
        {stats && (
          <p className="text-[11px] font-medium uppercase tracking-[0.2em] text-[color:var(--color-muted-foreground)]">
            Scan reports
          </p>
        )}
        <h1 className="text-[1.75rem] font-semibold leading-tight tracking-tight sm:text-[2.1rem]">
          {stats ? (
            <>
              {stats.total} report{stats.total === 1 ? '' : 's'}
              {stats.unsafe > 0 ? (
                <>
                  <span className="mx-2 font-normal text-[color:var(--color-muted-foreground)]">·</span>
                  <span className="text-danger">{stats.unsafe} unsafe</span>
                </>
              ) : stats.complete > 0 ? (
                <>
                  <span className="mx-2 font-normal text-[color:var(--color-muted-foreground)]">·</span>
                  <span className="inline-flex items-baseline gap-1.5 text-success">
                    <ShieldCheck className="size-5 translate-y-0.5" />
                    all clear
                  </span>
                </>
              ) : null}
            </>
          ) : (
            'Scan reports'
          )}
        </h1>
        <p className="max-w-xl text-sm text-[color:var(--color-muted-foreground)]">
          Every scan Assay has produced for this machine. Drill in for the full audit, or delete a stale one.
        </p>
      </div>
      <Link to="/scan/new">
        <Button variant="outline" size="sm">
          <Plus className="size-4" />
          New scan
        </Button>
      </Link>
    </header>
  );
}
