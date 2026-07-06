import { Menu } from 'lucide-react';
import { ActiveScansIndicator } from '@/components/ActiveScansIndicator';

export function TopBar({ onMenuClick }: { onMenuClick?: () => void }) {
  return (
    <header className="sticky top-0 z-30 flex h-14 items-center gap-3 border-b border-[color:var(--color-border)] bg-[color:var(--color-background)]/80 px-4 backdrop-blur-xl sm:px-6 md:hidden">
      {onMenuClick && (
        <button
          type="button"
          aria-label="Open navigation"
          onClick={onMenuClick}
          className="md:hidden inline-flex items-center justify-center rounded-md p-2 text-[color:var(--color-muted-foreground)] transition-colors hover:bg-[color:var(--color-muted)] hover:text-[color:var(--color-foreground)]"
        >
          <Menu className="size-5" />
        </button>
      )}
      <div className="md:hidden flex items-center font-bold tracking-tight text-base">
        Assay
      </div>
      <div className="ml-auto flex items-center gap-2">
        {/* Active-scans chip — only renders when at least one scan is in
            flight; reads from the ScanProgressProvider context so it
            updates without re-mounting on navigation. */}
        <ActiveScansIndicator />
      </div>
    </header>
  );
}
