import { useMemo } from 'react';
import { Cell, Legend, Pie, PieChart, ResponsiveContainer, Tooltip } from 'recharts';
import type { Verdict } from '@/types/api';

/**
 * SeverityDonut sums the severity distribution across an array of verdicts.
 * Renders as a donut chart (Recharts) with a centered total + legend.
 *
 * Color sourcing: severity tokens from globals.css so the chart stays in
 * sync with FindingCard badges. No raw hex.
 */
export function SeverityDonut({ verdicts }: { verdicts: Verdict[] }) {
  const data = useMemo(() => {
    const counts = { critical: 0, high: 0, medium: 0, low: 0, info: 0 };
    for (const v of verdicts) {
      for (const f of v.findings ?? []) {
        const k = f.severity as keyof typeof counts;
        if (k in counts) counts[k]++;
      }
    }
    return [
      { name: 'Critical', value: counts.critical, color: 'oklch(0.55 0.18 25)' },
      { name: 'High', value: counts.high, color: 'oklch(0.65 0.16 50)' },
      { name: 'Medium', value: counts.medium, color: 'oklch(0.72 0.14 80)' },
      { name: 'Low', value: counts.low, color: 'oklch(0.62 0.15 230)' },
      { name: 'Info', value: counts.info, color: 'oklch(0.70 0.04 260)' },
    ].filter((d) => d.value > 0);
  }, [verdicts]);
  const total = data.reduce((acc, d) => acc + d.value, 0);

  if (total === 0) {
    return (
      <div className="flex h-[260px] items-center justify-center text-sm text-[color:var(--color-muted-foreground)]">
        No findings yet — run a scan to populate.
      </div>
    );
  }

  return (
    <div className="relative h-[260px] w-full">
      <ResponsiveContainer width="100%" height="100%">
        <PieChart>
          <Pie
            data={data}
            dataKey="value"
            nameKey="name"
            innerRadius={62}
            outerRadius={92}
            paddingAngle={2}
            stroke="var(--color-card)"
            strokeWidth={3}
          >
            {data.map((entry) => (
              <Cell key={entry.name} fill={entry.color} />
            ))}
          </Pie>
          <Tooltip
            cursor={false}
            contentStyle={{
              background: 'var(--color-card)',
              border: '1px solid var(--color-border)',
              borderRadius: 8,
              fontSize: 12,
              padding: '6px 10px',
            }}
            labelStyle={{ color: 'var(--color-foreground)' }}
            itemStyle={{ color: 'var(--color-foreground)' }}
          />
          <Legend
            verticalAlign="bottom"
            align="center"
            iconType="circle"
            wrapperStyle={{ paddingTop: 12, fontSize: 12 }}
            formatter={(value) => <span className="text-[color:var(--color-muted-foreground)]">{value}</span>}
          />
        </PieChart>
      </ResponsiveContainer>
      <div className="pointer-events-none absolute left-1/2 top-[42%] -translate-x-1/2 -translate-y-1/2 text-center">
        <div className="text-2xl font-semibold tabular-nums">{total}</div>
        <div className="text-[10px] uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
          findings
        </div>
      </div>
    </div>
  );
}
