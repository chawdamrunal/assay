import { useEffect, useState, type MouseEvent } from 'react';
import { cn } from '@/lib/utils';

export interface NavSection {
  id: string;
  label: string;
}

/**
 * useScrollSpy tracks which report section is currently in view and returns a
 * `scrollTo` that smooth-scrolls to one.
 *
 * It observes each section against the viewport (`root: null`). That works even
 * though AppShell's <main> is the real scroll container: scrolling <main> moves
 * the sections through the viewport, which is exactly what IntersectionObserver
 * reports on. The rootMargin biases the "active" line to ~45% down the viewport
 * so a section lights up as its heading crosses that band, not only at the top.
 */
export function useScrollSpy(ids: string[]): {
  active: string | undefined;
  scrollTo: (id: string) => void;
} {
  const key = ids.join('|');
  const [active, setActive] = useState<string | undefined>(ids[0]);

  useEffect(() => {
    const els = ids
      .map((id) => document.getElementById(id))
      .filter((el): el is HTMLElement => el !== null);
    if (els.length === 0) return;

    const ratios = new Map<string, number>();
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          ratios.set(e.target.id, e.isIntersecting ? e.intersectionRatio : 0);
        }
        // First section in document order that is currently visible wins.
        const next = ids.find((id) => (ratios.get(id) ?? 0) > 0);
        if (next) setActive(next);
      },
      { rootMargin: '-88px 0px -55% 0px', threshold: [0, 0.25, 0.5, 1] },
    );
    els.forEach((el) => io.observe(el));
    return () => io.disconnect();
    // `key` is the joined id list, so the observer rebuilds when the set of
    // present sections changes (e.g. once the verdict loads).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  const scrollTo = (id: string) => {
    document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    setActive(id);
  };

  return { active, scrollTo };
}

/** Desktop sticky right rail — "On this page" with an active accent rail. */
export function SectionRail({
  sections,
  active,
  onNavigate,
}: {
  sections: NavSection[];
  active: string | undefined;
  onNavigate: (id: string) => void;
}) {
  return (
    <aside className="hidden w-44 shrink-0 lg:block">
      <nav className="sticky top-6 flex flex-col gap-0.5" aria-label="On this page">
        <p className="mb-1.5 px-3 text-[10px] font-medium uppercase tracking-[0.18em] text-[color:var(--color-muted-foreground)]">
          On this page
        </p>
        {sections.map((s) => (
          <NavItem key={s.id} section={s} active={active === s.id} onNavigate={onNavigate} />
        ))}
      </nav>
    </aside>
  );
}

function NavItem({
  section,
  active,
  onNavigate,
}: {
  section: NavSection;
  active: boolean;
  onNavigate: (id: string) => void;
}) {
  const handle = (e: MouseEvent) => {
    e.preventDefault();
    onNavigate(section.id);
  };
  return (
    <a href={`#${section.id}`} onClick={handle} className="group relative block">
      {/* Active accent rail — grows in place, never shifts layout (matches the
          app Sidebar's active affordance). */}
      <span
        aria-hidden="true"
        className={cn(
          'absolute left-0 top-1/2 h-4 -translate-y-1/2 rounded-r-full bg-[color:var(--color-primary)] transition-all duration-200',
          active ? 'w-[2px] opacity-100' : 'w-0 opacity-0',
        )}
      />
      <span
        className={cn(
          'block rounded-md px-3 py-1.5 text-sm transition-colors',
          active
            ? 'font-medium text-[color:var(--color-primary)]'
            : 'text-[color:var(--color-muted-foreground)] hover:bg-[color:var(--color-muted)] hover:text-[color:var(--color-foreground)]',
        )}
      >
        {section.label}
      </span>
    </a>
  );
}

/** Mobile/tablet sticky horizontal pill bar (shown below lg). */
export function SectionPills({
  sections,
  active,
  onNavigate,
}: {
  sections: NavSection[];
  active: string | undefined;
  onNavigate: (id: string) => void;
}) {
  return (
    <div className="sticky top-0 z-20 -mx-4 border-b border-[color:var(--color-border)] bg-[color:var(--color-background)]/85 px-4 py-2 backdrop-blur sm:-mx-6 sm:px-6 lg:hidden">
      <div className="flex items-center gap-1.5 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
        {sections.map((s) => (
          <a
            key={s.id}
            href={`#${s.id}`}
            onClick={(e) => {
              e.preventDefault();
              onNavigate(s.id);
            }}
            className={cn(
              'shrink-0 whitespace-nowrap rounded-full px-3 py-1 text-xs font-medium transition-colors',
              active === s.id
                ? 'bg-[color:var(--color-primary)] text-[color:var(--color-primary-foreground)]'
                : 'bg-[color:var(--color-muted)] text-[color:var(--color-muted-foreground)] hover:text-[color:var(--color-foreground)]',
            )}
          >
            {s.label}
          </a>
        ))}
      </div>
    </div>
  );
}
