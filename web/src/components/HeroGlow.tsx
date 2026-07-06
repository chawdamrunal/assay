import { cn } from '@/lib/utils';

/**
 * HeroGlow — the whisper-faint indigo radial that anchors a page hero, giving
 * the section ambient depth over the otherwise-flat off-white canvas.
 *
 * Decorative and absolutely positioned: the parent must be `relative isolate`
 * so the `-z-10` sits behind the hero text but above the page background. The
 * `.hero-glow` class (globals.css) adds a barely-perceptible slow drift, which
 * the prefers-reduced-motion block auto-disables.
 */
export function HeroGlow({ className }: { className?: string }) {
  return (
    <div
      aria-hidden="true"
      className={cn(
        'hero-glow pointer-events-none absolute -left-8 -top-10 -z-10 h-44 w-[42rem] max-w-[115%] rounded-full blur-2xl',
        'bg-[radial-gradient(50%_100%_at_28%_0%,var(--color-primary-soft),transparent_72%)]',
        className,
      )}
    />
  );
}
