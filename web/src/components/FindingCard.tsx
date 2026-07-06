import { useState } from 'react';
import {
  AlertOctagon,
  AlertTriangle,
  ChevronRight,
  FileText,
  Info,
  Lightbulb,
  ShieldAlert,
  ShieldCheck,
  Wrench,
  type LucideIcon,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { Markdown } from './Markdown';
import { CopyButton } from './CopyButton';
import type { Finding, Severity } from '@/types/api';

interface SeverityStyle {
  Icon: LucideIcon;
  // Chip background+text+border (rendered on the severity pill in the header)
  chip: string;
  // The 3px left rail on the card itself — the strongest visual hierarchy
  // signal in a long findings list. Apple HIG: rely on size/spacing/contrast,
  // not color alone — paired with the icon and label. `rail` is the solid
  // dot color; `railGrad` is the vertical gradient used for the edge rail so
  // it reads as a lit edge rather than a flat block.
  rail: string;
  railGrad: string;
  rank: number;
}

const severityStyles: Record<Severity, SeverityStyle> = {
  critical: { Icon: AlertOctagon,  chip: 'bg-severity-critical/15 text-severity-critical border-severity-critical/40', rail: 'bg-severity-critical', railGrad: 'from-severity-critical to-severity-critical/30', rank: 4 },
  high:     { Icon: AlertTriangle, chip: 'bg-severity-high/15 text-severity-high border-severity-high/40',             rail: 'bg-severity-high',     railGrad: 'from-severity-high to-severity-high/30',         rank: 3 },
  medium:   { Icon: ShieldAlert,   chip: 'bg-severity-medium/15 text-severity-medium border-severity-medium/40',       rail: 'bg-severity-medium',   railGrad: 'from-severity-medium to-severity-medium/30',     rank: 2 },
  low:      { Icon: Info,          chip: 'bg-severity-low/15 text-severity-low border-severity-low/40',                 rail: 'bg-severity-low',      railGrad: 'from-severity-low to-severity-low/30',           rank: 1 },
  info:     { Icon: ShieldCheck,   chip: 'bg-severity-info/15 text-severity-info border-severity-info/40',             rail: 'bg-severity-info',     railGrad: 'from-severity-info to-severity-info/30',         rank: 0 },
};

export function severityRank(s: Severity): number {
  return severityStyles[s].rank;
}

/**
 * FindingCard renders one Finding as a collapse-by-default summary row
 * with progressive disclosure of the full detail. With 11+ findings on
 * a report page, the all-expanded default produced an 11k-pixel-tall
 * unscannable wall — so the header row carries enough signal (severity,
 * title, category, threat-link, evidence count) to triage without
 * opening every card.
 *
 * Visual hierarchy is established three ways (Apple HIG / Material color-
 * not-only rule):
 *   1. 4-pixel coloured rail on the card's left edge — scans even when
 *      you're skimming at speed.
 *   2. Severity icon + chip in the header — colour + glyph.
 *   3. Card border-radius and elevation match the rest of the report.
 *
 * Expanded layout: at md+ widths Impact / Mitigation / Exploit live in
 * a three-column grid so the dense per-finding content takes ~60% less
 * vertical space than the v1 stacked layout.
 */
export function FindingCard({ finding }: { finding: Finding }) {
  const sev = severityStyles[finding.severity];
  const [open, setOpen] = useState(false);

  return (
    <article
      id={finding.id}
      className={cn(
        'group relative overflow-hidden rounded-xl border bg-[color:var(--color-card)] shadow-[var(--shadow-card)]',
        'border-[color:var(--color-border)] transition-all duration-150',
        'hover:border-[color:var(--color-primary)]/30 hover:shadow-[var(--shadow-elevated)]',
        open && 'border-[color:var(--color-primary)]/40 shadow-[var(--shadow-elevated)]',
      )}
    >
      {/* Severity rail — vertical gradient bar on the left edge. Visible at a
          glance when scanning a long list; the gradient reads as a lit edge
          rather than a flat-color block. */}
      <div
        aria-hidden="true"
        className={cn('absolute inset-y-0 left-0 w-[3px] bg-linear-to-b', sev.railGrad)}
      />
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-controls={`${finding.id}-body`}
        className="flex w-full items-start gap-3 pl-4 pr-3.5 py-3 text-left"
      >
        {/* Severity glyph — colored rounded chip, the left-side anchor. */}
        <span
          className={cn('mt-px grid size-8 shrink-0 place-items-center rounded-lg border', sev.chip)}
          aria-hidden="true"
        >
          <sev.Icon className="size-4" />
        </span>

        <div className="min-w-0 flex-1">
          {/* Top line — title with a single clean severity label + chevron on
              the right. One explicit severity signal (this badge) + the icon
              anchor; no redundant dot/word pile-up in the meta. */}
          <div className="flex items-start justify-between gap-3">
            <h3 className="font-semibold text-[15px] leading-snug">{finding.title}</h3>
            <div className="flex shrink-0 items-center gap-2 pt-px">
              <span
                className={cn(
                  'inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider',
                  sev.chip,
                )}
              >
                {finding.severity}
              </span>
              {finding.diff && <DiffChip status={finding.diff.status} />}
              {(finding.source === 'sca' || finding.source === 'poison') && (
                <span
                  className="inline-flex shrink-0 items-center rounded-full border border-[color:var(--color-border)] px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]"
                >
                  {finding.source === 'sca' ? 'SCA' : 'POISON'}
                </span>
              )}
              <span
                aria-hidden="true"
                className={cn(
                  'text-[color:var(--color-muted-foreground)] transition-transform duration-200',
                  'group-hover:text-[color:var(--color-foreground)]',
                  open && 'rotate-90',
                )}
              >
                <ChevronRight className="size-4" />
              </span>
            </div>
          </div>
          {/* Meta line — calm, single row: category · threat · evidence. */}
          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-[color:var(--color-muted-foreground)]">
            <span>{finding.category}</span>
            {finding.threat_id && (
              <>
                <Sep />
                <a
                  href={`#${finding.threat_id}`}
                  onClick={(e) => e.stopPropagation()}
                  className="hover:text-[color:var(--color-primary)] hover:underline"
                >
                  Maps to {finding.threat_id}
                </a>
              </>
            )}
            {finding.evidence && finding.evidence.length > 0 && (
              <>
                <Sep />
                <span className="inline-flex items-center gap-1">
                  <FileText className="size-3" />
                  {finding.evidence.length} evidence
                </span>
              </>
            )}
          </div>
        </div>
      </button>

      {open && (
        <div id={`${finding.id}-body`} className="space-y-4 border-t border-[color:var(--color-border)] px-4 py-4 text-sm">
          {finding.description && (
            <Field label="What's broken" markdown={finding.description} />
          )}

          {finding.context && (
            <Field
              label="Context (data-flow node)"
              icon={<Lightbulb className="size-3.5" />}
              markdown={finding.context}
              accent="sky"
            />
          )}

          {finding.evidence && finding.evidence.length > 0 && (
            <EvidenceList evidence={finding.evidence} />
          )}

          {/* Impact / Mitigation / Exploit scenario — three-column grid at
              md+ so a single finding takes one screen instead of three. */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {finding.impact && (
              <Field
                label="Impact"
                icon={<AlertOctagon className="size-3.5" />}
                markdown={finding.impact}
                accent="red"
              />
            )}
            {finding.mitigation && (
              <Field
                label="Mitigation"
                icon={<Wrench className="size-3.5" />}
                markdown={finding.mitigation}
                accent="emerald"
              />
            )}
          </div>

          {finding.exploit_scenario && (
            <Field
              label="How an attacker would use it"
              icon={<AlertTriangle className="size-3.5" />}
              markdown={finding.exploit_scenario}
              accent="amber"
            />
          )}

          {finding.recommended_action && (
            <div className="rounded-md border border-[color:var(--color-primary)]/40 bg-[color:var(--color-primary)]/5 p-3">
              <div className="mb-1 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-[color:var(--color-primary)]">
                <ShieldCheck className="size-3.5" />
                Do this now
              </div>
              <Markdown source={finding.recommended_action} className="text-sm" />
            </div>
          )}
        </div>
      )}
    </article>
  );
}

// Sep — a low-contrast middot separating meta items. Pulled out so the dots
// don't compete with the text they divide.
function Sep() {
  return <span aria-hidden="true" className="text-[color:var(--color-border-strong)]">·</span>;
}

/**
 * DiffChip renders the small new/changed/stable/resolved badge next to the
 * severity pill on a finding's header. Stable findings get no chip (visual
 * noise reduction — only the deltas matter to a reviewer).
 */
function DiffChip({ status }: { status: 'new' | 'changed' | 'stable' | 'resolved' }) {
  if (status === 'stable') return null;
  const styles: Record<'new' | 'changed' | 'resolved', string> = {
    new: 'border-success/40 bg-success/10 text-success',
    changed: 'border-warning/40 bg-warning/10 text-warning',
    resolved: 'border-[color:var(--color-border-strong)] bg-[color:var(--color-muted)] text-[color:var(--color-muted-foreground)] line-through',
  };
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center rounded-full border px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider',
        styles[status],
      )}
    >
      {status}
    </span>
  );
}

type Accent = 'sky' | 'red' | 'emerald' | 'amber';

function Field({
  label,
  icon,
  markdown,
  accent,
}: {
  label: string;
  icon?: React.ReactNode;
  markdown: string;
  accent?: Accent;
}) {
  const accentBorder =
    accent === 'red'
      ? 'border-l-danger/60'
      : accent === 'emerald'
        ? 'border-l-success/60'
        : accent === 'sky'
          ? 'border-l-primary/60'
          : accent === 'amber'
            ? 'border-l-warning/60'
            : 'border-l-[color:var(--color-border)]';
  return (
    <section className={cn('border-l-2 pl-3', accentBorder)}>
      <h4 className="mb-1.5 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
        {icon}
        {label}
      </h4>
      <Markdown source={markdown} className="text-sm [&_p]:my-1" />
    </section>
  );
}

/**
 * EvidenceList renders all evidence rows with a compact file:line header
 * and the snippet code block. Each row has a copy-to-clipboard affordance
 * via the file:line which doubles as a click-to-select handle for the user
 * who wants to grep the snippet in their editor.
 */
function EvidenceList({ evidence }: { evidence: NonNullable<Finding['evidence']> }) {
  return (
    <section>
      <h4 className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
        <FileText className="size-3.5" />
        Evidence ({evidence.length})
      </h4>
      <ul className="space-y-2">
        {evidence.map((e, idx) => (
          <li key={idx} className="overflow-hidden rounded-md border border-[color:var(--color-border)]">
            <div className="flex items-center justify-between gap-2 border-b border-[color:var(--color-border)] bg-[color:var(--color-muted)]/40 py-1 pl-3 pr-1.5 font-mono text-[11px] text-[color:var(--color-muted-foreground)]">
              <span className="select-all truncate">{e.file}:{e.line}</span>
              <CopyButton text={e.snippet} label="Copy snippet" className="shrink-0" />
            </div>
            <pre className="overflow-x-auto bg-[color:var(--color-muted)]/20 p-3 font-mono text-xs leading-relaxed">
              <code>{e.snippet}</code>
            </pre>
          </li>
        ))}
      </ul>
    </section>
  );
}
