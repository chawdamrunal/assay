import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { Maximize2, Scan, X, ZoomIn, ZoomOut } from 'lucide-react';

/**
 * Renders a Mermaid diagram from the given source string.
 *
 * Mermaid is dynamically imported on first use to keep it out of the initial
 * bundle. The SVG shows inline (fit to the card width); a Fullscreen button —
 * or clicking the diagram — opens a zoom/pan lightbox so dense diagrams (a
 * data-flow graph with many nodes) stay legible instead of shrinking to an
 * unreadable thumbnail.
 */
export function ThreatDiagram({ source }: { source: string }) {
  const [svg, setSvg] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [fullscreen, setFullscreen] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const { default: mermaid } = await import('mermaid');
        mermaid.initialize({
          startOnLoad: false,
          theme: 'default',
          securityLevel: 'strict',
        });
        // mermaid.render needs a unique id per call
        const id = 'mermaid-' + Math.random().toString(36).slice(2, 10);
        const { svg: out } = await mermaid.render(id, source);
        if (cancelled) return;
        setSvg(out);
      } catch (e: unknown) {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [source]);

  if (error) {
    return (
      <div className="my-4 rounded-md border border-[color:var(--color-danger)]/40 bg-[color:var(--color-danger-soft)] p-3 text-sm text-[color:var(--color-danger)]">
        <div className="mb-1 font-semibold">Mermaid diagram failed to render</div>
        <pre className="whitespace-pre-wrap text-xs opacity-80">{error}</pre>
        <details className="mt-2">
          <summary className="cursor-pointer">Show source</summary>
          <pre className="mt-2 whitespace-pre-wrap text-xs opacity-60">{source}</pre>
        </details>
      </div>
    );
  }

  return (
    <>
      <div className="relative my-4 overflow-x-auto rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-card)] p-4">
        {svg && (
          <button
            type="button"
            onClick={() => setFullscreen(true)}
            className="absolute right-2 top-2 z-10 inline-flex items-center gap-1.5 rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-card)]/90 px-2 py-1 text-xs font-medium text-[color:var(--color-muted-foreground)] shadow-[var(--shadow-card)] backdrop-blur transition-colors hover:border-[color:var(--color-border-strong)] hover:text-[color:var(--color-foreground)]"
            aria-label="Open diagram fullscreen"
          >
            <Maximize2 className="size-3.5" />
            Fullscreen
          </button>
        )}
        {svg ? (
          <div
            role="button"
            tabIndex={0}
            title="Click to enlarge"
            onClick={() => setFullscreen(true)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                setFullscreen(true);
              }
            }}
            className="cursor-zoom-in [&_svg]:mx-auto [&_svg]:h-auto [&_svg]:max-w-full"
            dangerouslySetInnerHTML={{ __html: svg }}
          />
        ) : (
          <div className="text-sm text-[color:var(--color-muted-foreground)]">Loading diagram…</div>
        )}
      </div>

      {fullscreen && svg && <DiagramLightbox svg={svg} onClose={() => setFullscreen(false)} />}
    </>
  );
}

/** Pull the intrinsic size out of the Mermaid SVG's viewBox for exact scaling. */
function parseViewBox(svg: string): { w: number; h: number } | null {
  const m = /viewBox="[\d.eE+-]+\s+[\d.eE+-]+\s+([\d.eE+-]+)\s+([\d.eE+-]+)"/.exec(svg);
  if (!m) return null;
  const w = parseFloat(m[1]);
  const h = parseFloat(m[2]);
  if (!Number.isFinite(w) || !Number.isFinite(h) || w <= 0 || h <= 0) return null;
  return { w, h };
}

const clampScale = (v: number) => Math.min(8, Math.max(0.1, v));

/**
 * DiagramLightbox is a full-viewport zoom/pan viewer for a rendered Mermaid
 * SVG. Scroll (or the +/- buttons) zooms toward the cursor/centre, drag pans,
 * double-click / Fit re-centres, Esc closes. The live transform lives in a ref
 * (not state) so the non-passive wheel handler never reads a stale closure — a
 * bump to a tick counter forces the re-render.
 */
function DiagramLightbox({ svg, onClose }: { svg: string; onClose: () => void }) {
  const vpRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const tRef = useRef({ scale: 1, tx: 0, ty: 0 });
  const [, setTick] = useState(0);
  const dragRef = useRef<{ x: number; y: number; tx: number; ty: number } | null>(null);
  const [grabbing, setGrabbing] = useState(false);

  // Intrinsic size from the viewBox — memoised so `fit`'s identity is stable
  // (recomputing per render would make the fit-on-mount effect loop forever).
  const nat = useMemo(() => parseViewBox(svg), [svg]);

  const apply = (next: { scale: number; tx: number; ty: number }) => {
    tRef.current = next;
    setTick((t) => t + 1);
  };

  const fit = useCallback(() => {
    const vp = vpRef.current;
    if (!vp) return;
    let w = nat?.w ?? 0;
    let h = nat?.h ?? 0;
    if (!w || !h) {
      // Fallback: measure the live SVG and undo the current scale.
      const el = contentRef.current?.querySelector('svg');
      if (el) {
        const cur = tRef.current.scale || 1;
        const r = el.getBoundingClientRect();
        w = r.width / cur;
        h = r.height / cur;
      }
    }
    if (!w || !h) return;
    const s = clampScale(Math.min(vp.clientWidth / w, vp.clientHeight / h) * 0.92);
    apply({ scale: s, tx: (vp.clientWidth - w * s) / 2, ty: (vp.clientHeight - h * s) / 2 });
  }, [nat]);

  const zoomAt = (cx: number, cy: number, factor: number) => {
    const { scale, tx, ty } = tRef.current;
    const next = clampScale(scale * factor);
    const px = (cx - tx) / scale;
    const py = (cy - ty) / scale;
    apply({ scale: next, tx: cx - px * next, ty: cy - py * next });
  };

  const zoomCentre = (factor: number) => {
    const vp = vpRef.current;
    if (!vp) return;
    zoomAt(vp.clientWidth / 2, vp.clientHeight / 2, factor);
  };

  // Fit once the SVG is in the DOM.
  useEffect(() => {
    const t = setTimeout(fit, 40);
    return () => clearTimeout(t);
  }, [fit]);

  // Non-passive wheel listener so preventDefault works — React's onWheel is
  // passive and would let the zoom gesture scroll the page behind the modal.
  useEffect(() => {
    const vp = vpRef.current;
    if (!vp) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const rect = vp.getBoundingClientRect();
      zoomAt(e.clientX - rect.left, e.clientY - rect.top, e.deltaY < 0 ? 1.12 : 1 / 1.12);
    };
    vp.addEventListener('wheel', onWheel, { passive: false });
    return () => vp.removeEventListener('wheel', onWheel);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Esc closes; lock body scroll while the lightbox is open.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      window.removeEventListener('keydown', onKey);
      document.body.style.overflow = prev;
    };
  }, [onClose]);

  const onMouseDown = (e: React.MouseEvent) => {
    dragRef.current = { x: e.clientX, y: e.clientY, tx: tRef.current.tx, ty: tRef.current.ty };
    setGrabbing(true);
  };
  const onMouseMove = (e: React.MouseEvent) => {
    const d = dragRef.current;
    if (!d) return;
    apply({ scale: tRef.current.scale, tx: d.tx + (e.clientX - d.x), ty: d.ty + (e.clientY - d.y) });
  };
  const endDrag = () => {
    dragRef.current = null;
    setGrabbing(false);
  };

  const { scale, tx, ty } = tRef.current;

  return (
    <div className="fixed inset-0 z-[80] flex flex-col bg-[color:var(--color-background)]">
      <div className="flex items-center justify-between gap-2 border-b border-[color:var(--color-border)] px-4 py-2.5">
        <span className="text-sm font-medium">Diagram</span>
        <div className="flex items-center gap-1">
          <ToolBtn onClick={() => zoomCentre(1 / 1.2)} label="Zoom out">
            <ZoomOut className="size-4" />
          </ToolBtn>
          <button
            type="button"
            onClick={fit}
            className="min-w-[3.25rem] rounded-md px-2 py-1 text-xs font-medium tabular-nums text-[color:var(--color-muted-foreground)] transition-colors hover:bg-[color:var(--color-muted)] hover:text-[color:var(--color-foreground)]"
            aria-label="Reset zoom to fit"
          >
            {Math.round(scale * 100)}%
          </button>
          <ToolBtn onClick={() => zoomCentre(1.2)} label="Zoom in">
            <ZoomIn className="size-4" />
          </ToolBtn>
          <ToolBtn onClick={fit} label="Fit to screen">
            <Scan className="size-4" />
          </ToolBtn>
          <ToolBtn onClick={onClose} label="Close (Esc)">
            <X className="size-4" />
          </ToolBtn>
        </div>
      </div>

      <div
        ref={vpRef}
        className={`relative flex-1 overflow-hidden ${grabbing ? 'cursor-grabbing' : 'cursor-grab'}`}
        onMouseDown={onMouseDown}
        onMouseMove={onMouseMove}
        onMouseUp={endDrag}
        onMouseLeave={endDrag}
        onDoubleClick={fit}
      >
        <div
          ref={contentRef}
          className={`absolute left-0 top-0 origin-top-left select-none [&_svg]:!max-w-none ${
            nat ? '[&_svg]:!h-full [&_svg]:!w-full' : '[&_svg]:!h-auto'
          }`}
          style={{
            ...(nat ? { width: nat.w, height: nat.h } : {}),
            transform: `translate(${tx}px, ${ty}px) scale(${scale})`,
          }}
          dangerouslySetInnerHTML={{ __html: svg }}
        />
      </div>

      <div className="border-t border-[color:var(--color-border)] px-4 py-1.5 text-center text-[11px] text-[color:var(--color-muted-foreground)]">
        Scroll to zoom · drag to pan · double-click to fit · Esc to close
      </div>
    </div>
  );
}

function ToolBtn({
  onClick,
  label,
  children,
}: {
  onClick: () => void;
  label: string;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      className="inline-grid size-8 place-items-center rounded-md text-[color:var(--color-muted-foreground)] transition-colors hover:bg-[color:var(--color-muted)] hover:text-[color:var(--color-foreground)]"
    >
      {children}
    </button>
  );
}
