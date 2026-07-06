import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Clock, Plus } from 'lucide-react';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { Button } from '@/components/ui/button';
import { api } from '@/lib/api';

interface TimelineEntry {
  scanID: string;
  target: string;
  dir: string;
  date: Date | null;
}

interface DateGroup {
  label: string;
  entries: TimelineEntry[];
}

function parseScanIDDate(id: string): Date | null {
  // Format: 20260515T100000.000Z  (YYYYMMDDTHHMMSS.mmmZ) — possibly with -NNN suffix
  const m = id.match(/^(\d{8})T(\d{6})\.(\d{3})Z/);
  if (!m) return null;
  const [, ymd, hms, ms] = m;
  const iso = `${ymd.slice(0, 4)}-${ymd.slice(4, 6)}-${ymd.slice(6, 8)}T${hms.slice(0, 2)}:${hms.slice(2, 4)}:${hms.slice(4, 6)}.${ms}Z`;
  const d = new Date(iso);
  return isNaN(d.getTime()) ? null : d;
}

function parseISODate(s: string | undefined): Date | null {
  if (!s) return null;
  const d = new Date(s);
  return isNaN(d.getTime()) ? null : d;
}

function dateGroupLabel(d: Date | null): string {
  if (!d) return 'Unknown';
  const now = new Date();
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const startOfYesterday = new Date(startOfToday);
  startOfYesterday.setDate(startOfYesterday.getDate() - 1);
  const dDate = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  if (dDate.getTime() === startOfToday.getTime()) return 'Today';
  if (dDate.getTime() === startOfYesterday.getTime()) return 'Yesterday';
  return d.toLocaleDateString(undefined, { weekday: 'long', month: 'short', day: 'numeric', year: 'numeric' });
}

export function HistoryPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['scans'],
    queryFn: api.listScans,
  });

  const groups = useMemo<DateGroup[]>(() => {
    if (!data?.items) return [];
    const entries: TimelineEntry[] = data.items.map((it) => ({
      scanID: it.scan_id,
      target: it.target,
      dir: it.dir,
      date: parseScanIDDate(it.scan_id) ?? parseISODate(it.created_at),
    }));
    entries.sort((a, b) => (b.date?.getTime() ?? 0) - (a.date?.getTime() ?? 0));

    const byLabel = new Map<string, TimelineEntry[]>();
    for (const e of entries) {
      const label = dateGroupLabel(e.date);
      const arr = byLabel.get(label) ?? [];
      arr.push(e);
      byLabel.set(label, arr);
    }
    return Array.from(byLabel.entries()).map(([label, items]) => ({ label, entries: items }));
  }, [data]);

  return (
    <div className="fade-in flex flex-col gap-6 max-w-3xl">
      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-3xl font-semibold tracking-tight">History</h1>
          <p className="text-sm text-[color:var(--color-muted-foreground)]">
            Scans grouped by day. Click any row for the full audit.
          </p>
        </div>
        <Link to="/scan/new">
          <Button variant="outline" size="sm">
            <Plus className="size-4" />
            New scan
          </Button>
        </Link>
      </header>

      {isLoading && (
        <div className="flex flex-col gap-3">
          <Skeleton className="h-8 w-40" />
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      )}

      {error && (
        <div className="rounded-md border border-danger/40 bg-danger/10 p-4 text-sm">
          Failed to load history: {error.message}
        </div>
      )}

      {!isLoading && !error && groups.length === 0 && (
        <Card className="p-12 text-center">
          <p className="text-[color:var(--color-muted-foreground)] mb-4">
            No scan history yet.
          </p>
          <Link to="/scan/new">
            <Button>
              <Plus className="size-4" />
              Run your first scan
            </Button>
          </Link>
        </Card>
      )}

      {groups.map((group) => (
        <section key={group.label} className="flex flex-col gap-2">
          <h2 className="text-xs font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
            {group.label}
          </h2>
          <ol className="flex flex-col gap-2">
            {group.entries.map((e) => (
              <li key={e.scanID}>
                <Link
                  to="/scans/$id"
                  params={{ id: e.scanID }}
                  className="block"
                >
                  <Card className="flex items-center gap-4 p-4 transition-colors hover:bg-[color:var(--color-muted)]">
                    <Clock className="size-4 shrink-0 text-[color:var(--color-muted-foreground)]" />
                    <div className="flex-1 min-w-0">
                      <div className="font-medium truncate">{e.target}</div>
                      <div className="font-mono text-xs text-[color:var(--color-muted-foreground)] truncate">
                        {e.scanID}
                      </div>
                    </div>
                    <div className="hidden sm:block text-xs text-[color:var(--color-muted-foreground)]">
                      {e.date ? e.date.toLocaleTimeString() : ''}
                    </div>
                  </Card>
                </Link>
              </li>
            ))}
          </ol>
        </section>
      ))}
    </div>
  );
}
