import { useState } from 'react';
import { Check, Copy } from 'lucide-react';
import { cn } from '@/lib/utils';

/**
 * CopyButton copies `text` to the clipboard and flips to a transient "copied"
 * check for ~1.5s. Deliberately self-contained (no global toast/provider) so
 * it drops in anywhere — evidence snippets, metadata values — without new
 * cross-route state.
 */
export function CopyButton({
  text,
  label = 'Copy',
  className,
}: {
  text: string;
  label?: string;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard unavailable (insecure context / denied) — no-op */
    }
  };

  return (
    <button
      type="button"
      onClick={onCopy}
      aria-label={copied ? 'Copied' : label}
      title={copied ? 'Copied' : label}
      className={cn(
        'inline-flex items-center justify-center rounded-md p-1.5 text-[color:var(--color-muted-foreground)] transition-colors hover:bg-[color:var(--color-muted)] hover:text-[color:var(--color-foreground)]',
        className,
      )}
    >
      {copied ? (
        <Check className="size-3.5 text-[color:var(--color-success)]" />
      ) : (
        <Copy className="size-3.5" />
      )}
    </button>
  );
}
