import * as React from 'react';
import { cn } from '@/lib/utils';

export const Card = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div
      ref={ref}
      className={cn(
        'rounded-xl border border-[color:var(--color-border)] bg-[color:var(--color-card)] text-[color:var(--color-card-foreground)]',
        'shadow-[var(--shadow-card)]',
        className,
      )}
      {...props}
    />
  ),
);
Card.displayName = 'Card';
