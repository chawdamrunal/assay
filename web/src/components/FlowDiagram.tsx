import { useEffect, useRef, useState } from 'react';
import { Lock, Maximize2, Minimize2, RefreshCw, Unlock } from 'lucide-react';
import { Excalidraw, convertToExcalidrawElements } from '@excalidraw/excalidraw';
import { parseMermaidToExcalidraw } from '@excalidraw/mermaid-to-excalidraw';
import '@excalidraw/excalidraw/index.css';
import { Button } from '@/components/ui/button';

type ExcalidrawAPI = {
  scrollToContent: (target?: unknown, opts?: { fitToContent?: boolean; animate?: boolean }) => void;
  updateScene: (scene: unknown) => void;
};

/**
 * FlowDiagram renders a Mermaid `flowchart` definition as a zoomable,
 * pan-able Excalidraw canvas. Drag to pan, scroll/pinch to zoom, double-click
 * for fit-to-screen. A fullscreen toggle expands the canvas to the viewport.
 *
 * The viewer is strict read-only (viewModeEnabled + zenModeEnabled + most
 * canvas actions disabled) so users can inspect freely without accidentally
 * editing the scene.
 *
 * This component is heavy (Excalidraw ships its own font + canvas runtime),
 * so callers should lazy-load it (React.lazy) and wrap with <Suspense>.
 */
export function FlowDiagram({ mermaid }: { mermaid: string }) {
  const apiRef = useRef<ExcalidrawAPI | null>(null);
  const [elements, setElements] = useState<unknown[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [fullscreen, setFullscreen] = useState(false);
  // The Excalidraw canvas captures wheel + touch events for its own zoom
  // and pan. That hijacks page scroll the moment the user's cursor crosses
  // the diagram, which makes a long report feel broken. Interactive mode is
  // off by default — the diagram still renders and the toolbar Fit/Expand
  // still work, but the canvas surface is locked from event capture so page
  // scroll passes straight through. The user clicks the Unlock chip (or hits
  // Expand for fullscreen) when they actually want to zoom/pan.
  const [interactive, setInteractive] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setError(null);
    setElements(null);
    (async () => {
      try {
        // Two-step preprocess: strip the markdown fence the LLM sometimes
        // wraps the diagram in, then sanitize node labels containing
        // Mermaid-special characters (slashes, parens, etc.). The LLM
        // commonly produces labels like T[/v2/libs/search] which Mermaid
        // misreads as a parallelogram shape.
        const cleaned = sanitizeMermaid(stripFence(mermaid));
        // 20px keeps node labels readable in the dark-mode card at default
        // zoom. mermaid-to-excalidraw scales the entire scene from this base.
        const parsed = await parseMermaidToExcalidraw(cleaned, {
          themeVariables: { fontSize: '20px' },
        });
        const converted = convertToExcalidrawElements(parsed.elements);
        if (cancelled) return;
        setElements(converted);
      } catch (e: unknown) {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [mermaid]);

  // Once Excalidraw has elements + an API handle, frame them in view but
  // never below 70% zoom — fitting to a card height shrinks dense diagrams
  // into illegible thumbnails. The "Fit" button overrides this.
  useEffect(() => {
    if (!apiRef.current || !elements) return;
    const t = setTimeout(() => {
      const api = apiRef.current as unknown as {
        scrollToContent: (target?: unknown, opts?: { fitToContent?: boolean; animate?: boolean }) => void;
        getAppState?: () => { zoom?: { value?: number } };
        updateScene: (scene: { appState: { zoom: { value: number } } }) => void;
      };
      api.scrollToContent(elements, { fitToContent: true, animate: false });
      // Clamp the auto-fit zoom up to at least 0.7 so dense diagrams stay readable.
      requestAnimationFrame(() => {
        try {
          const state = api.getAppState?.();
          const current = state?.zoom?.value ?? 1;
          if (current < 0.7) {
            api.updateScene({ appState: { zoom: { value: 0.85 } } });
          }
        } catch {
          /* swallow — Excalidraw API shape can drift between versions */
        }
      });
    }, 80);
    return () => clearTimeout(t);
  }, [elements, fullscreen]);

  useEffect(() => {
    if (!fullscreen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setFullscreen(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [fullscreen]);

  if (error) {
    return (
      <div className="rounded border border-danger/40 bg-danger/10 p-3 text-sm text-danger">
        <div className="font-semibold mb-1">Flow diagram failed to render</div>
        <pre className="whitespace-pre-wrap text-xs opacity-80">{error}</pre>
        <details className="mt-2">
          <summary className="cursor-pointer">Show Mermaid source</summary>
          <pre className="mt-2 whitespace-pre-wrap text-xs opacity-60">{mermaid}</pre>
        </details>
      </div>
    );
  }

  // Fullscreen forces interactive (you opened the modal to interact); the
  // locked default only applies to the inline embed where page scroll matters.
  const canvasInteractive = interactive || fullscreen;

  const heightClass = fullscreen ? 'flex-1' : 'h-[420px]';
  const wrapperClass = fullscreen
    ? 'fixed inset-0 z-50 bg-[color:var(--color-background)] flex flex-col'
    : 'relative my-4 overflow-hidden rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-card)]';

  return (
    <div className={wrapperClass}>
      <div className="flex items-center justify-between gap-2 border-b border-[color:var(--color-border)] bg-[color:var(--color-muted)]/40 px-3 py-1.5">
        <span className="text-xs font-medium text-[color:var(--color-muted-foreground)]">
          {canvasInteractive
            ? 'Flow diagram · drag to pan · scroll to zoom'
            : 'Flow diagram · scroll-locked so page scroll works · click Interact to enable zoom'}
        </span>
        <div className="flex items-center gap-1">
          {!fullscreen && (
            <Button
              variant={interactive ? 'outline' : 'ghost'}
              size="sm"
              onClick={() => setInteractive((v) => !v)}
              className="h-7 px-2 text-xs"
              aria-label={interactive ? 'Lock diagram to release page scroll' : 'Unlock diagram for zoom/pan'}
            >
              {interactive ? <Unlock className="size-3" /> : <Lock className="size-3" />}
              {interactive ? 'Interact' : 'Locked'}
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() =>
              apiRef.current?.scrollToContent(elements ?? undefined, { fitToContent: true, animate: true })
            }
            className="h-7 px-2 text-xs"
            aria-label="Fit diagram to view"
          >
            <RefreshCw className="size-3" />
            Fit
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setFullscreen((v) => !v)}
            className="h-7 px-2 text-xs"
            aria-label={fullscreen ? 'Exit fullscreen' : 'Enter fullscreen'}
          >
            {fullscreen ? <Minimize2 className="size-3" /> : <Maximize2 className="size-3" />}
            {fullscreen ? 'Exit' : 'Expand'}
          </Button>
        </div>
      </div>
      <Legend />
      <div
        className={`relative w-full ${heightClass}`}
        // When locked, kill pointer events on the canvas so wheel + touch
        // events fall through to the page's scroll container. Excalidraw
        // still renders, the user still sees it; they just can't interact.
        style={canvasInteractive ? undefined : { pointerEvents: 'none' }}
      >
        {!elements ? (
          <div className="absolute inset-0 grid place-items-center text-sm text-[color:var(--color-muted-foreground)]">
            Loading diagram…
          </div>
        ) : (
          <Excalidraw
            initialData={{
              elements: elements as never,
              appState: {
                viewBackgroundColor: 'transparent',
                zenModeEnabled: true,
              },
              scrollToContent: true,
            }}
            viewModeEnabled
            zenModeEnabled
            theme={isDarkMode() ? 'dark' : 'light'}
            excalidrawAPI={(api) => {
              apiRef.current = api as unknown as ExcalidrawAPI;
            }}
            UIOptions={{
              canvasActions: {
                changeViewBackgroundColor: false,
                clearCanvas: false,
                export: false,
                loadScene: false,
                saveAsImage: false,
                saveToActiveFile: false,
                toggleTheme: false,
              },
              tools: { image: false },
            }}
          />
        )}
      </div>
    </div>
  );
}

/**
 * Legend renders a compact chip strip explaining the classDef colour key the
 * methodology prompt asks the LLM to apply. The colours here MUST match
 * methodology.md Step 3's `classDef ext/sink/store` declarations so the
 * legend lines up with what the diagram actually paints.
 */
function Legend() {
  const items = [
    { label: 'External net', fill: '#ede9fe', stroke: '#7c3aed' },
    { label: 'Sensitive sink', fill: '#fee2e2', stroke: '#dc2626' },
    { label: 'Local FS', fill: '#dcfce7', stroke: '#16a34a' },
    { label: 'Trust boundary', stroke: 'currentColor', dashed: true },
  ];
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-[color:var(--color-border)] bg-[color:var(--color-card)] px-3 py-1.5 text-[11px] text-[color:var(--color-muted-foreground)]">
      <span className="font-medium uppercase tracking-wider opacity-70">Legend</span>
      {items.map((it) => (
        <span key={it.label} className="inline-flex items-center gap-1.5">
          <span
            className="inline-block size-3 rounded-sm"
            style={{
              backgroundColor: it.fill ?? 'transparent',
              border: `1.5px ${it.dashed ? 'dashed' : 'solid'} ${it.stroke}`,
            }}
          />
          {it.label}
        </span>
      ))}
    </div>
  );
}

function isDarkMode(): boolean {
  if (typeof document === 'undefined') return true;
  return !document.documentElement.classList.contains('light');
}

function stripFence(src: string): string {
  const trimmed = src.trim();
  if (trimmed.startsWith('```')) {
    const lines = trimmed.split('\n');
    lines.shift();
    if (lines[lines.length - 1]?.trim() === '```') lines.pop();
    return lines.join('\n');
  }
  return trimmed;
}

/**
 * sanitizeMermaid wraps every node-label that contains Mermaid-special
 * characters in double quotes so the parser doesn't misinterpret them.
 *
 * Specifically catches the common LLM failure mode where a label like
 *   T[/v2/libs/search]
 * is parsed as a parallelogram (`[/.../]`) and chokes on the extra
 * slashes inside. Quoting forces the label to be a literal string:
 *   T["/v2/libs/search"]
 *
 * Also fixes labels containing `(`, `)`, `&`, `'`, `:`, which Mermaid
 * sometimes treats specially. Idempotent — already-quoted labels are left
 * alone.
 */
export function sanitizeMermaid(src: string): string {
  // Match: identifier[<label>] OR identifier{<label>} OR identifier(<label>)
  // where the label is NOT already quoted and contains a special char.
  return src.replace(
    /([A-Za-z0-9_]+)(\[|\{|\()([^"\]\)}\n][^\]\)}\n]*)(\]|\}|\))/g,
    (full, id, open, label, close) => {
      // Already-quoted, or doesn't need quoting → leave alone.
      const trimmedLabel = label.trim();
      if (trimmedLabel.startsWith('"') && trimmedLabel.endsWith('"')) return full;
      // Quote when the label contains a Mermaid-fragile character.
      if (!/[/()&':,]/.test(trimmedLabel)) return full;
      const escaped = trimmedLabel.replace(/"/g, '\\"');
      return `${id}${open}"${escaped}"${close}`;
    },
  );
}
