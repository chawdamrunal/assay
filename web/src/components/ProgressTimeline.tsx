import { Check, Circle, Loader2, X, type LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';

const stages = [
  { key: 'prepass',         label: 'Pre-pass' },
  { key: 'triage',          label: 'Triage' },
  { key: 'claims',          label: 'Claim extraction' },
  { key: 'threat_model',    label: 'Threat model' },
  { key: 'investigation',   label: 'Investigation' },
  { key: 'exploitability',  label: 'Exploitability' },
  { key: 'synthesis',       label: 'Synthesis' },
  { key: 'done',            label: 'Done' },
];

type StageState = 'pending' | 'active' | 'complete' | 'error';

function stateFor(status: string | undefined): StageState {
  if (!status) return 'pending';
  if (status === 'error') return 'error';
  if (status === 'complete') return 'complete';
  if (status === 'start') return 'active';
  return 'pending';
}

/**
 * Renders a vertical 8-step timeline of the scanner stages.
 * stageStatus maps stage key -> latest status string ("start" | "complete" | "error").
 */
export function ProgressTimeline({
  stageStatus,
  className,
}: {
  stageStatus: Record<string, string>;
  className?: string;
}) {
  return (
    <ol className={cn('flex flex-col gap-1', className)}>
      {stages.map((s, idx) => {
        const state = stateFor(stageStatus[s.key]);
        const Icon: LucideIcon = state === 'complete' ? Check : state === 'active' ? Loader2 : state === 'error' ? X : Circle;
        const iconClass = cn(
          'size-4 shrink-0',
          state === 'complete' && 'text-success',
          state === 'active' && 'text-primary animate-spin',
          state === 'error' && 'text-danger',
          state === 'pending' && 'text-[color:var(--color-muted-foreground)] opacity-50',
        );
        const labelClass = cn(
          'text-sm',
          state === 'complete' && 'text-[color:var(--color-foreground)]',
          state === 'active' && 'text-[color:var(--color-foreground)] font-medium',
          state === 'error' && 'text-danger font-medium',
          state === 'pending' && 'text-[color:var(--color-muted-foreground)]',
        );
        return (
          <li key={s.key} className="flex items-center gap-3 py-1.5">
            <Icon className={iconClass} />
            <span className={labelClass}>{s.label}</span>
            {idx === stages.length - 1 && state === 'complete' && (
              <span className="ml-2 inline-flex items-center gap-1 rounded-full bg-success/15 px-2 py-0.5 text-xs font-medium uppercase tracking-wide text-success">
                <Check className="size-3" />
                done
              </span>
            )}
          </li>
        );
      })}
    </ol>
  );
}
