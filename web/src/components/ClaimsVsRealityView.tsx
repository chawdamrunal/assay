import { useMemo } from 'react';
import { Quote, ScanSearch } from 'lucide-react';
import { Markdown } from './Markdown';

/**
 * ClaimsVsRealityView parses the `**Claim: ...**` followed by a reality
 * paragraph pattern the methodology asks for. Renders each pair as a
 * two-column comparison row: the marketing copy on the left ("Claim"),
 * what the code actually does on the right ("Reality").
 *
 * The "README lied" attack class is one of Assay's most distinctive
 * outputs — it deserves a presentation that lets a reviewer scan
 * mismatches at a glance, not a wall of italic markdown.
 *
 * Falls back to plain Markdown when the document doesn't match the
 * expected pattern.
 */
export function ClaimsVsRealityView({ markdown }: { markdown: string }) {
  const pairs = useMemo(() => parsePairs(markdown), [markdown]);
  if (pairs.length === 0) {
    return <Markdown source={markdown} />;
  }
  return (
    <div className="flex flex-col gap-3">
      {pairs.map((p, i) => (
        <ClaimRow key={i} pair={p} />
      ))}
    </div>
  );
}

interface ClaimPair {
  claim: string;
  // Optional parenthetical "(server-name)" that the methodology often
  // attaches to the claim line; we show it as a tag.
  tag?: string;
  reality: string;
}

/**
 * parsePairs splits on lines beginning with `**Claim`. For each section
 * it extracts the bold claim text, an optional parenthetical tag, and the
 * remaining text as the reality.
 */
function parsePairs(markdown: string): ClaimPair[] {
  // Drop the section header (## Claims vs. Reality) if present.
  const body = markdown.replace(/^##[^\n]*\n/, '');

  const blocks = body.split(/(?=^\s*\*\*\s*claim\b)/im).filter((b) => b.trim());
  const pairs: ClaimPair[] = [];
  for (const block of blocks) {
    // Match: **Claim: "text" (optional tag)** then everything after.
    const m = block.match(/^\s*\*\*\s*claim\s*:?\s*([^*]*?)\s*\*\*\s*([\s\S]*)$/i);
    if (!m) continue;
    let claimText = m[1].trim();
    let tag: string | undefined;

    // Pull out a trailing "(server-name)" from the claim line.
    const tagMatch = claimText.match(/^(.+?)\s*\(([^)]+)\)\s*$/);
    if (tagMatch) {
      claimText = tagMatch[1].trim();
      tag = tagMatch[2].trim();
    }
    // Strip surrounding quotes from the claim, if any.
    claimText = claimText.replace(/^["“]|["”]$/g, '');

    const reality = m[2].trim();
    if (claimText && reality) {
      pairs.push({ claim: claimText, tag, reality });
    }
  }
  return pairs;
}

function ClaimRow({ pair }: { pair: ClaimPair }) {
  return (
    <article className="grid grid-cols-1 gap-px overflow-hidden rounded-xl border border-[color:var(--color-border)] bg-[color:var(--color-border)] md:grid-cols-2">
      {/* Claim panel — slightly subdued background; this is the "marketing" side */}
      <div className="flex flex-col gap-2 bg-[color:var(--color-card)] p-4">
        <header className="flex items-center justify-between gap-2">
          <h4 className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
            <Quote className="size-3" />
            What the plugin claims
          </h4>
          {pair.tag && (
            <span className="rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-muted)]/50 px-1.5 py-0.5 font-mono text-[10px] text-[color:var(--color-muted-foreground)]">
              {pair.tag}
            </span>
          )}
        </header>
        <p className="text-sm leading-relaxed italic text-[color:var(--color-muted-foreground)]">
          "{pair.claim}"
        </p>
      </div>

      {/* Reality panel — slightly warmer accent on the border to call attention */}
      <div className="flex flex-col gap-2 bg-[color:var(--color-card)] p-4">
        <h4 className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-warning">
          <ScanSearch className="size-3" />
          What Assay found
        </h4>
        <div className="text-sm leading-relaxed">
          <Markdown source={pair.reality} className="[&_p]:my-1" />
        </div>
      </div>
    </article>
  );
}
