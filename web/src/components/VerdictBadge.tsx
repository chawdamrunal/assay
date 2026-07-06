import { ShieldCheck, ShieldAlert, ShieldX, type LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { VerdictLabel } from '@/types/api';

interface VerdictStyle {
  Icon: LucideIcon;
  label: string;
  bg: string;
}

const styles: Record<VerdictLabel, VerdictStyle> = {
  safe: {
    Icon: ShieldCheck,
    label: 'SAFE',
    bg: 'bg-success/15 text-success border-success/40',
  },
  caution: {
    Icon: ShieldAlert,
    label: 'CAUTION',
    bg: 'bg-warning/15 text-warning border-warning/40',
  },
  unsafe: {
    Icon: ShieldX,
    label: 'UNSAFE',
    bg: 'bg-danger/15 text-danger border-danger/40',
  },
};

/**
 * Large, color-coded verdict pill for the scan report header.
 * Use size="sm" for inline use (e.g., in the scans list table).
 */
export function VerdictBadge({
  verdict,
  size = 'lg',
  className,
}: {
  verdict: VerdictLabel;
  size?: 'sm' | 'lg';
  className?: string;
}) {
  const { Icon, label, bg } = styles[verdict];
  const sizing =
    size === 'lg'
      ? 'px-4 py-2 text-base gap-2'
      : 'px-2.5 py-0.5 text-xs gap-1.5';
  const iconSize = size === 'lg' ? 'size-5' : 'size-3.5';
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full border font-semibold tracking-wider uppercase',
        sizing,
        bg,
        className,
      )}
    >
      <Icon className={iconSize} />
      {label}
    </span>
  );
}
