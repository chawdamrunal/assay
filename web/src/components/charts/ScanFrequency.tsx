import { useMemo } from 'react';
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import type { ScanListItem } from '@/types/api';

/**
 * ScanFrequency shows the scan-count-per-day for the last 14 days as a
 * smooth area chart. Filled-area emphasizes recent activity vs a flat line
 * while staying readable at small sizes (we render at 200px tall).
 */
export function ScanFrequency({ scans }: { scans: ScanListItem[] }) {
  const data = useMemo(() => buildLast14Days(scans), [scans]);
  const hasAny = data.some((d) => d.count > 0);

  if (!hasAny) {
    return (
      <div className="flex h-[200px] items-center justify-center text-sm text-[color:var(--color-muted-foreground)]">
        No scans in the last 14 days.
      </div>
    );
  }

  return (
    <div className="h-[200px] w-full">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 8, right: 8, left: -16, bottom: 0 }}>
          <defs>
            <linearGradient id="scan-area" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--color-primary)" stopOpacity={0.5} />
              <stop offset="100%" stopColor="var(--color-primary)" stopOpacity={0.05} />
            </linearGradient>
          </defs>
          <CartesianGrid stroke="var(--color-border)" strokeDasharray="3 3" vertical={false} />
          <XAxis
            dataKey="day"
            tick={{ fill: 'var(--color-muted-foreground)', fontSize: 10 }}
            tickLine={false}
            axisLine={{ stroke: 'var(--color-border)' }}
            interval={1}
          />
          <YAxis
            allowDecimals={false}
            tick={{ fill: 'var(--color-muted-foreground)', fontSize: 10 }}
            tickLine={false}
            axisLine={false}
            width={28}
          />
          <Tooltip
            cursor={{ fill: 'var(--color-muted)', opacity: 0.3 }}
            contentStyle={{
              background: 'var(--color-card)',
              border: '1px solid var(--color-border)',
              borderRadius: 8,
              fontSize: 12,
              padding: '6px 10px',
            }}
            labelStyle={{ color: 'var(--color-foreground)' }}
            itemStyle={{ color: 'var(--color-foreground)' }}
            labelFormatter={(_, payload) => {
              const dataPoint = payload?.[0]?.payload as { fullDate: string } | undefined;
              return dataPoint?.fullDate ?? '';
            }}
          />
          <Area
            type="monotone"
            dataKey="count"
            stroke="var(--color-primary)"
            strokeWidth={2}
            fill="url(#scan-area)"
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}

interface DayBucket {
  day: string;       // e.g. "Mon 15"
  fullDate: string;  // ISO date for tooltip
  count: number;
}

function buildLast14Days(scans: ScanListItem[]): DayBucket[] {
  const now = new Date();
  const buckets: DayBucket[] = [];
  for (let i = 13; i >= 0; i--) {
    const d = new Date(now);
    d.setDate(d.getDate() - i);
    d.setHours(0, 0, 0, 0);
    buckets.push({
      day: d.toLocaleDateString(undefined, { weekday: 'short' }) + ' ' + d.getDate(),
      fullDate: d.toLocaleDateString(undefined, { weekday: 'long', month: 'short', day: 'numeric' }),
      count: 0,
    });
  }
  for (const s of scans) {
    if (!s.created_at) continue;
    const t = new Date(s.created_at).getTime();
    if (Number.isNaN(t)) continue;
    const startOfToday = new Date();
    startOfToday.setHours(0, 0, 0, 0);
    const diffDays = Math.floor((startOfToday.getTime() - new Date(t).setHours(0, 0, 0, 0)) / 86400000);
    if (diffDays < 0 || diffDays > 13) continue;
    const idx = 13 - diffDays;
    if (buckets[idx]) buckets[idx].count++;
  }
  return buckets;
}
