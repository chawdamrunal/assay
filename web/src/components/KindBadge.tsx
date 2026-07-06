import { Box, Cable, Plug, Webhook, Settings } from 'lucide-react';
import type { Kind } from '@/types/api';

// Kind is a category, not a status — so every kind shares one neutral chip and
// the icon + label carry the distinction. Four decorative hues would either
// misuse the semantic tokens (mcp is not "success", a hook is not "warning") or
// fail contrast on white; the icon gets a subtle primary tint for a brand cue.
const kindMap: Record<Kind, { label: string; icon: typeof Box }> = {
  'claude-code-plugin': { label: 'plugin',   icon: Box },
  'mcp-server':         { label: 'mcp',      icon: Plug },
  'connector':          { label: 'connector', icon: Cable },
  'hook':               { label: 'hook',     icon: Webhook },
  'settings':           { label: 'settings', icon: Settings },
};

export function KindBadge({ kind }: { kind: Kind }) {
  const { label, icon: Icon } = kindMap[kind];
  return (
    <span className="inline-flex items-center gap-1.5 rounded border border-[color:var(--color-border)] bg-[color:var(--color-muted)] px-2 py-0.5 text-xs font-medium text-[color:var(--color-muted-foreground)]">
      <Icon className="size-3 text-[color:var(--color-primary)]" />
      {label}
    </span>
  );
}
