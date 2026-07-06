import { lazy, Suspense, useEffect, useMemo, useState } from 'react';
import {
  Background,
  Controls,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react';
import dagre from '@dagrejs/dagre';
import { Maximize2, Minimize2 } from 'lucide-react';
import '@xyflow/react/dist/style.css';
import { Button } from '@/components/ui/button';
import { parseMermaidFlow, isRenderable, type FlowGraph as Graph, type FlowKind } from '@/lib/mermaid-flow';

// Fallback to the raw Mermaid SVG renderer when we can't parse the diagram into
// a structured graph — a parse miss is never worse than today.
const ThreatDiagram = lazy(() =>
  import('./ThreatDiagram').then((m) => ({ default: m.ThreatDiagram })),
);

const NODE_W = 190;
const NODE_H = 58;
const BOUNDARY_PAD_X = 26;
const BOUNDARY_PAD_TOP = 30; // extra room for the boundary label
const BOUNDARY_PAD_BOT = 22;

// Per-kind node styling, driven by the app's design tokens so it themes in
// light + dark. Mirrors the legend below and the methodology's classDef colours.
const KIND_STYLE: Record<FlowKind, string> = {
  process:
    'border-[color:var(--color-primary)]/45 bg-[color:var(--color-primary-soft)] text-[color:var(--color-primary)]',
  sink: 'border-[color:var(--color-danger)] bg-[color:var(--color-danger-soft)] text-[color:var(--color-danger)]',
  external:
    'border-[color:var(--color-warning)] bg-[color:var(--color-warning-soft)] text-[color:var(--color-warning)]',
  store:
    'border-[color:var(--color-border-strong)] bg-[color:var(--color-card)] text-[color:var(--color-foreground)]',
};

const HIDDEN_HANDLE = {
  opacity: 0,
  width: 1,
  height: 1,
  minWidth: 0,
  minHeight: 0,
  border: 'none',
  background: 'transparent',
} as const;

function FlowNode({ data }: NodeProps) {
  const d = data as { label: string; kind: FlowKind };
  return (
    <div
      style={{ width: NODE_W }}
      className={`rounded-xl border px-3 py-2.5 text-center shadow-[var(--shadow-card)] ${KIND_STYLE[d.kind]}`}
    >
      <Handle type="target" position={Position.Left} style={HIDDEN_HANDLE} />
      <div className="whitespace-pre-line break-words text-[13px] font-semibold leading-tight">{d.label}</div>
      <Handle type="source" position={Position.Right} style={HIDDEN_HANDLE} />
    </div>
  );
}

function BoundaryNode({ data }: NodeProps) {
  const d = data as { label: string };
  return (
    <div className="pointer-events-none size-full rounded-2xl border-2 border-dashed border-[color:var(--color-border-strong)] bg-[color:var(--color-muted)]/25">
      <span className="absolute left-3 top-2 font-mono text-[10px] font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
        {d.label}
      </span>
    </div>
  );
}

const nodeTypes = { flow: FlowNode, boundary: BoundaryNode };

// Resolve token colours to concrete values — SVG edge strokes + arrow markers
// don't reliably inherit CSS custom properties, so we read the computed values.
function resolveColors() {
  const cs = typeof document !== 'undefined' ? getComputedStyle(document.documentElement) : null;
  const read = (name: string, fallback: string) => cs?.getPropertyValue(name).trim() || fallback;
  return {
    danger: read('--color-danger', '#dc2626'),
    muted: read('--color-muted-foreground', '#64748b'),
    border: read('--color-border', '#e5e7eb'),
  };
}

function layout(graph: Graph, colors: ReturnType<typeof resolveColors>): { nodes: Node[]; edges: Edge[] } {
  const g = new dagre.graphlib.Graph({ compound: true });
  g.setGraph({ rankdir: 'LR', nodesep: 42, ranksep: 96, marginx: 24, marginy: 24 });
  g.setDefaultEdgeLabel(() => ({}));

  for (const b of graph.boundaries) g.setNode(b.id, { label: b.label });
  for (const n of graph.nodes) g.setNode(n.id, { width: NODE_W, height: NODE_H });
  for (const b of graph.boundaries) {
    for (const mid of b.members) {
      if (graph.nodes.some((n) => n.id === mid)) g.setParent(mid, b.id);
    }
  }
  for (const e of graph.edges) g.setEdge(e.source, e.target);

  dagre.layout(g);

  const nodes: Node[] = [];

  // Boundary groups first (rendered behind, non-interactive).
  for (const b of graph.boundaries) {
    const gn = g.node(b.id);
    if (!gn) continue;
    const w = gn.width + BOUNDARY_PAD_X * 2;
    const h = gn.height + BOUNDARY_PAD_TOP + BOUNDARY_PAD_BOT;
    nodes.push({
      id: `boundary:${b.id}`,
      type: 'boundary',
      position: { x: gn.x - gn.width / 2 - BOUNDARY_PAD_X, y: gn.y - gn.height / 2 - BOUNDARY_PAD_TOP },
      data: { label: b.label },
      draggable: false,
      selectable: false,
      zIndex: 0,
      style: { width: w, height: h },
    });
  }

  for (const n of graph.nodes) {
    const gn = g.node(n.id);
    if (!gn) continue;
    nodes.push({
      id: n.id,
      type: 'flow',
      position: { x: gn.x - NODE_W / 2, y: gn.y - NODE_H / 2 },
      data: { label: n.label, kind: n.kind },
      draggable: false,
      zIndex: 1,
    });
  }

  const edges: Edge[] = graph.edges.map((e) => ({
    id: e.id,
    source: e.source,
    target: e.target,
    label: e.label,
    animated: e.danger,
    markerEnd: {
      type: MarkerType.ArrowClosed,
      width: 16,
      height: 16,
      color: e.danger ? colors.danger : colors.muted,
    },
    style: {
      stroke: e.danger ? colors.danger : colors.muted,
      strokeWidth: e.danger ? 2 : 1.5,
    },
    labelStyle: { fill: colors.muted, fontSize: 11, fontFamily: 'var(--font-mono)' },
    labelBgStyle: { fill: 'var(--color-card)', fillOpacity: 0.9 },
    labelBgPadding: [4, 2] as [number, number],
    labelBgBorderRadius: 4,
  }));

  return { nodes, edges };
}

const LEGEND: { label: string; kind?: FlowKind; boundary?: boolean }[] = [
  { label: 'Trust boundary', boundary: true },
  { label: 'Sensitive sink', kind: 'sink' },
  { label: 'External egress', kind: 'external' },
  { label: 'Internal process', kind: 'process' },
  { label: 'Local store', kind: 'store' },
];

const SWATCH: Record<FlowKind, string> = {
  sink: 'bg-[color:var(--color-danger)]',
  external: 'bg-[color:var(--color-warning)]',
  process: 'bg-[color:var(--color-primary)]',
  store: 'border border-[color:var(--color-border-strong)] bg-[color:var(--color-card)]',
};

/**
 * FlowGraph renders an Assay data-flow diagram as a laid-out, zoom/pan-able
 * React Flow graph — trust boundaries as dashed groups, kind-coloured nodes,
 * and red animated edges for credential / exfil / injection flows. It parses
 * the scan's own Mermaid (no backend change); anything it can't parse falls
 * back to the raw Mermaid SVG so it is never worse than the previous render.
 */
export function FlowGraph({ mermaid }: { mermaid: string }) {
  const [fullscreen, setFullscreen] = useState(false);
  const graph = useMemo(() => parseMermaidFlow(mermaid), [mermaid]);
  const renderable = useMemo(() => isRenderable(graph), [graph]);
  const { nodes, edges } = useMemo(
    () => (renderable ? layout(graph, resolveColors()) : { nodes: [], edges: [] }),
    [graph, renderable],
  );

  useEffect(() => {
    if (!fullscreen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setFullscreen(false);
    };
    window.addEventListener('keydown', onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      window.removeEventListener('keydown', onKey);
      document.body.style.overflow = prev;
    };
  }, [fullscreen]);

  if (!renderable) {
    return (
      <Suspense
        fallback={<div className="my-4 text-sm text-[color:var(--color-muted-foreground)]">Loading diagram…</div>}
      >
        <ThreatDiagram source={mermaid} />
      </Suspense>
    );
  }

  const wrapperClass = fullscreen
    ? 'fixed inset-0 z-[80] flex flex-col bg-[color:var(--color-background)]'
    : 'relative my-4 flex flex-col overflow-hidden rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-card)]';

  return (
    <div className={wrapperClass}>
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[color:var(--color-border)] px-3 py-2">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-[color:var(--color-muted-foreground)]">
          {LEGEND.map((it) => (
            <span key={it.label} className="inline-flex items-center gap-1.5">
              {it.boundary ? (
                <span className="inline-block size-3 rounded-[3px] border border-dashed border-[color:var(--color-border-strong)]" />
              ) : (
                <span className={`inline-block size-3 rounded-[3px] ${SWATCH[it.kind!]}`} />
              )}
              {it.label}
            </span>
          ))}
        </div>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => setFullscreen((v) => !v)}
          className="h-7 px-2 text-xs"
          aria-label={fullscreen ? 'Exit fullscreen' : 'Enter fullscreen'}
        >
          {fullscreen ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
          {fullscreen ? 'Exit' : 'Fullscreen'}
        </Button>
      </div>
      <div className={fullscreen ? 'flex-1' : 'h-[480px]'}>
        <ReactFlow
          key={fullscreen ? 'fs' : 'inline'}
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.18 }}
          minZoom={0.2}
          maxZoom={2.5}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={false}
          proOptions={{ hideAttribution: true }}
        >
          <Background gap={16} color="var(--color-border)" />
          <Controls showInteractive={false} position="bottom-right" />
        </ReactFlow>
      </div>
    </div>
  );
}
