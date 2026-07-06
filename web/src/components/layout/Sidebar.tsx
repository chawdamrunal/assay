import { Link } from '@tanstack/react-router';
import {
  History,
  Layers,
  LayoutDashboard,
  ListTree,
  Plus,
  ScrollText,
  Settings,
  Sparkles,
  type LucideIcon,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { ActiveScansIndicator } from '@/components/ActiveScansIndicator';

interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
}

// Primary navigation — the core surfaces a reviewer moves between.
const primary: NavItem[] = [
  { to: '/',           label: 'Dashboard',    icon: LayoutDashboard },
  { to: '/assistant',  label: 'Assistant',    icon: Sparkles },
  { to: '/inventory',  label: 'Inventory',    icon: ListTree },
  { to: '/fleet',      label: 'Fleet',        icon: Layers },
  { to: '/scans',      label: 'Scan Reports', icon: ScrollText },
  { to: '/history',    label: 'History',      icon: History },
];

// Utilities — visually separated from primary nav (nav-hierarchy rule), pinned
// to the bottom so the destructive/config surface sits apart from daily flow.
const utility: NavItem[] = [
  { to: '/settings',   label: 'Settings',     icon: Settings },
];

export function Sidebar({
  className,
  onNavigate,
}: {
  className?: string;
  onNavigate?: () => void;
}) {
  return (
    <aside
      className={cn(
        'w-60 shrink-0 flex-col border-r border-[color:var(--color-border)] bg-[color:var(--color-card)] px-3 py-4',
        'flex',
        className,
      )}
    >
      {/* Brand mark — text wordmark only (no icon). */}
      <div className="flex min-w-0 flex-col px-2 pb-5 pt-1 leading-none">
        <span className="text-lg font-bold tracking-tight text-[color:var(--color-foreground)]">
          Assay
        </span>
        <span className="mt-1 text-[10px] font-medium uppercase tracking-[0.2em] text-[color:var(--color-muted-foreground)]">
          Security Scanner
        </span>
      </div>

      {/* Primary action — sits above the nav so the most common task is first. */}
      <Link to="/scan/new" onClick={onNavigate} className="contents">
        <button
          type="button"
          className="mb-4 inline-flex w-full cursor-pointer items-center justify-center gap-2 rounded-lg bg-[color:var(--color-primary)] px-3 py-2 text-sm font-medium text-[color:var(--color-primary-foreground)] shadow-[var(--shadow-card)] transition-[filter,transform] duration-150 hover:brightness-110 active:scale-[0.98]"
        >
          <Plus className="size-4" />
          New scan
        </button>
      </Link>

      {/* Active-scans chip — now lives in the sidebar since the desktop top bar
          is gone. Renders only while a scan is in flight (null otherwise), so it
          adds no space when idle. Opens its dropdown rightward (align="left"). */}
      <ActiveScansIndicator align="left" className="mb-4" />

      <nav className="flex flex-col gap-0.5">
        {primary.map((item) => (
          <NavLink key={item.to} item={item} onNavigate={onNavigate} />
        ))}
      </nav>

      {/* Utilities pinned to the bottom, separated by a hairline divider. */}
      <div className="mt-auto flex flex-col gap-0.5 border-t border-[color:var(--color-border)] pt-3">
        {utility.map((item) => (
          <NavLink key={item.to} item={item} onNavigate={onNavigate} />
        ))}
      </div>
    </aside>
  );
}

/**
 * NavLink renders one navigation row. Uses TanStack Router's function-children
 * API so the active state can drive three signals at once (color-not-only
 * rule): an accent rail on the left edge, a tinted background, and an
 * accent-colored icon — not color alone.
 */
function NavLink({ item, onNavigate }: { item: NavItem; onNavigate?: () => void }) {
  const { to, label, icon: Icon } = item;
  return (
    <Link to={to} onClick={onNavigate} className="group relative block">
      {({ isActive }) => (
        <>
          {/* Active accent rail — animates width/opacity, never shifts layout. */}
          <span
            aria-hidden="true"
            className={cn(
              'absolute left-0 top-1/2 h-5 -translate-y-1/2 rounded-r-full bg-[color:var(--color-primary)] transition-all duration-200',
              isActive ? 'w-[3px] opacity-100' : 'w-0 opacity-0',
            )}
          />
          <span
            className={cn(
              'flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors duration-150',
              isActive
                ? 'bg-[color:var(--color-primary-soft)] font-medium text-[color:var(--color-foreground)]'
                : 'text-[color:var(--color-muted-foreground)] hover:bg-[color:var(--color-muted)] hover:text-[color:var(--color-foreground)]',
            )}
          >
            <Icon
              className={cn(
                'size-4 shrink-0 transition-colors duration-150',
                isActive
                  ? 'text-[color:var(--color-primary)]'
                  : 'text-[color:var(--color-muted-foreground)] group-hover:text-[color:var(--color-foreground)]',
              )}
            />
            <span>{label}</span>
          </span>
        </>
      )}
    </Link>
  );
}
