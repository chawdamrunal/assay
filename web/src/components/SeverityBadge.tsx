import { AlertOctagon, AlertTriangle, Info, ShieldCheck, ShieldAlert } from 'lucide-react';
import { cn } from '@/lib/utils';

const severityStyles = {
  critical: { bg: 'bg-severity-critical/20 text-severity-critical border-severity-critical/40', icon: AlertOctagon },
  high:     { bg: 'bg-severity-high/20 text-severity-high border-severity-high/40', icon: AlertTriangle },
  medium:   { bg: 'bg-severity-medium/20 text-severity-medium border-severity-medium/40', icon: ShieldAlert },
  low:      { bg: 'bg-severity-low/20 text-severity-low border-severity-low/40', icon: Info },
  info:     { bg: 'bg-severity-info/20 text-severity-info border-severity-info/40', icon: ShieldCheck },
} as const;

export type Severity = keyof typeof severityStyles;

export function SeverityBadge({ severity }: { severity: Severity }) {
  const { bg, icon: Icon } = severityStyles[severity];
  return (
    <span className={cn(
      'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-medium uppercase tracking-wide',
      bg,
    )}>
      <Icon className="size-3" />
      {severity}
    </span>
  );
}
