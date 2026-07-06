import { lazy, Suspense, useEffect, useState } from 'react';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { cn } from '@/lib/utils';

// Lazy-load ThreatDiagram so mermaid only enters the bundle when used.
const ThreatDiagram = lazy(() =>
  import('./ThreatDiagram').then((m) => ({ default: m.ThreatDiagram })),
);

interface ShikiHighlighter {
  codeToHtml: (
    code: string,
    options: { lang: string; themes: { light: string; dark: string } },
  ) => string;
}

/**
 * Markdown renders text with GFM, syntax-highlighted code (Shiki), and
 * Mermaid diagrams for ```mermaid fences. Shiki and Mermaid both lazy-load.
 */
export function Markdown({ source, className }: { source: string; className?: string }) {
  const components: Components = {
    code: ({ className: codeClass, children, ...rest }) => {
      const langMatch = /language-(\w+)/.exec(codeClass || '');
      const lang = langMatch?.[1] ?? '';
      const code = String(children).replace(/\n$/, '');

      // Inline code (no language fence) — render as a styled <code>.
      if (!langMatch) {
        return (
          <code
            className="rounded bg-[color:var(--color-muted)] px-1.5 py-0.5 text-[0.9em] font-mono"
            {...rest}
          >
            {children}
          </code>
        );
      }

      if (lang === 'mermaid') {
        return (
          <Suspense fallback={<div className="my-4 text-sm opacity-60">Loading diagram…</div>}>
            <ThreatDiagram source={code} />
          </Suspense>
        );
      }

      return <ShikiCode code={code} lang={lang} />;
    },
    table: ({ children }) => (
      <div className="my-4 overflow-x-auto">
        <table className="w-full border-collapse text-sm">{children}</table>
      </div>
    ),
    th: ({ children }) => (
      <th className="border-b border-[color:var(--color-border)] px-3 py-2 text-left font-semibold">
        {children}
      </th>
    ),
    td: ({ children }) => (
      <td className="border-b border-[color:var(--color-border)] px-3 py-2 align-top">
        {children}
      </td>
    ),
    blockquote: ({ children }) => (
      <blockquote className="my-4 border-l-4 border-[color:var(--color-primary)] bg-[color:var(--color-muted)] px-4 py-2 text-[color:var(--color-muted-foreground)]">
        {children}
      </blockquote>
    ),
    h1: ({ children }) => <h1 className="my-4 text-2xl font-semibold">{children}</h1>,
    h2: ({ children }) => <h2 className="mt-6 mb-3 text-xl font-semibold">{children}</h2>,
    h3: ({ children }) => <h3 className="mt-4 mb-2 text-lg font-semibold">{children}</h3>,
    ul: ({ children }) => <ul className="my-3 list-disc pl-6">{children}</ul>,
    ol: ({ children }) => <ol className="my-3 list-decimal pl-6">{children}</ol>,
    li: ({ children }) => <li className="my-1">{children}</li>,
    a: ({ children, href }) => (
      <a href={href} className="text-[color:var(--color-primary)] underline" target="_blank" rel="noreferrer">
        {children}
      </a>
    ),
    p: ({ children }) => <p className="my-3 leading-relaxed">{children}</p>,
  };

  return (
    <div className={cn('prose-assay text-[color:var(--color-foreground)]', className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {source}
      </ReactMarkdown>
    </div>
  );
}

/**
 * ShikiCode renders a syntax-highlighted code block. Shiki is dynamically
 * loaded; until it's ready, we show the raw code in a plain <pre> so the
 * UI doesn't flicker.
 */
function ShikiCode({ code, lang }: { code: string; lang: string }) {
  const [html, setHtml] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const { createHighlighter } = await import('shiki');
        const highlighter = await getOrCreateHighlighter(createHighlighter);
        const out = highlighter.codeToHtml(code, {
          lang: normalizeLang(lang),
          themes: { light: 'github-light', dark: 'github-dark' },
        });
        if (!cancelled) setHtml(out);
      } catch {
        if (!cancelled) setHtml(null);
      }
    })();
    return () => { cancelled = true; };
  }, [code, lang]);

  if (html) {
    return (
      <div
        className="shiki-block my-4 overflow-x-auto rounded-md text-sm"
        // eslint-disable-next-line react/no-danger
        dangerouslySetInnerHTML={{ __html: html }}
      />
    );
  }
  return (
    <pre className="my-4 overflow-x-auto rounded-md bg-[color:var(--color-card)] p-4 text-sm font-mono">
      <code>{code}</code>
    </pre>
  );
}

// Singleton Shiki highlighter — loading themes/langs is expensive; cache it.
let highlighterPromise: Promise<ShikiHighlighter> | null = null;
function getOrCreateHighlighter(
  factory: (opts: { themes: string[]; langs: string[] }) => Promise<ShikiHighlighter>,
): Promise<ShikiHighlighter> {
  if (!highlighterPromise) {
    highlighterPromise = factory({
      themes: ['github-light', 'github-dark'],
      langs: ['javascript', 'typescript', 'jsx', 'tsx', 'python', 'go', 'json', 'bash', 'shell', 'yaml', 'markdown'],
    });
  }
  return highlighterPromise;
}

function normalizeLang(lang: string): string {
  // Map common aliases to Shiki language names.
  const aliases: Record<string, string> = {
    js: 'javascript', ts: 'typescript', py: 'python', sh: 'bash',
    yml: 'yaml', md: 'markdown',
  };
  return aliases[lang] ?? lang;
}
