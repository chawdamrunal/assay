// Parses the constrained Mermaid `flowchart` that Assay's methodology asks the
// LLM to emit (see internal/mcp/methodology.md Step 3) into a structured graph
// the React Flow renderer can lay out and style. This is deliberately a
// best-effort parser over a KNOWN subset — node defs, edges with labels,
// `subgraph` trust boundaries, and `classDef`/`class` kind tags. On anything it
// can't make sense of, the caller falls back to the raw Mermaid SVG render, so
// a parse miss is never worse than today.

export type FlowKind = 'external' | 'sink' | 'store' | 'process';

export interface FlowNode {
  id: string;
  label: string;
  kind: FlowKind;
}

export interface FlowEdge {
  id: string;
  source: string;
  target: string;
  label?: string;
  /** credential/exfil/prompt-injection flow — rendered as a red dashed edge. */
  danger?: boolean;
}

export interface FlowBoundary {
  id: string;
  label: string;
  members: string[];
}

export interface FlowGraph {
  nodes: FlowNode[];
  edges: FlowEdge[];
  boundaries: FlowBoundary[];
}

// Mermaid classDef names the methodology standardises on → our semantic kind.
function kindFromClass(cls: string): FlowKind | undefined {
  switch (cls) {
    case 'ext':
      return 'external';
    case 'sink':
      return 'sink';
    case 'store':
      return 'store';
    case 'trust':
      return 'process'; // trust-boundary marker sits on a decision node; render neutral
    default:
      return undefined;
  }
}

// Convert Mermaid line-break markers in a label to real newlines.
function normalizeLabel(s: string): string {
  return s
    .replace(/<br\s*\/?>/gi, '\n')
    .replace(/\\n/g, '\n')
    .trim();
}

// Heuristic kind for nodes that carry no explicit classDef — colour-coding is
// what makes the graph readable, and many scans omit the class tags. Order
// matters: strongest external/sink signals win before the softer store/process.
function inferKind(label: string): FlowKind {
  const l = label.toLowerCase();
  if (/https?:\/\/|\bpost\b|telemetry|egress|any host|:\d{2,5}\b|\bport\b|outbound|exfil|webhook|remote|\.com\b/.test(l))
    return 'external';
  if (/\brce\b|eval|exec|subprocess|\bshell\b|spawn|command|prompt injection|\binject|credential|secret|\.aws|\.ssh|password|\btoken\b|\bpii\b/.test(l))
    return 'sink';
  if (/filesystem|file system|\.env\b|documents|\blog\b|database|\bcache\b|\bfiles?\b|storage|\bdisk\b/.test(l))
    return 'store';
  return 'process';
}

// id + opening bracket + label (+ closing) + optional `:::class`. Labels are
// either double-quoted (the sanitizer quotes anything with special chars) or a
// plain run without bracket/pipe chars.
const NODE_RE =
  /\b([A-Za-z0-9_-]+)(\[\[|\[\(|\(\[|\[|\(\(|\(|\{)\s*(?:"([^"]*)"|([^\]|)}\n]+?))\s*(?:\]\]|\]\)|\)\]|\)\)|\]|\)|\})(?:\s*:::\s*([A-Za-z0-9_-]+))?/g;

const EDGE_RE =
  /\b([A-Za-z0-9_-]+)\s*(?:-->|---|-\.->|-\.-|==>|===|--x|--o)\s*(?:\|\s*"?([^|"]*?)"?\s*\|)?\s*([A-Za-z0-9_-]+)\b/g;

const DANGER_RE =
  /\b(read|reads|cred|credential|exfil|token|secret|password|leak|inject|POST|PUT|\.aws|\.ssh|\.env|\.gnupg|keychain|egress)\b/i;

/**
 * parseMermaidFlow turns a Mermaid flowchart body (fence already stripped) into
 * a structured graph. Returns a graph that may be empty/sparse — callers should
 * check `isRenderable` before using it and fall back to the raw renderer otherwise.
 */
export function parseMermaidFlow(src: string): FlowGraph {
  const nodes = new Map<string, FlowNode>();
  const edges: FlowEdge[] = [];
  const boundaries: FlowBoundary[] = [];
  const boundaryStack: FlowBoundary[] = [];
  const explicit = new Set<string>(); // ids whose kind came from an explicit class/shape
  let edgeSeq = 0;

  const ensureNode = (id: string): FlowNode => {
    let n = nodes.get(id);
    if (!n) {
      n = { id, label: id, kind: 'process' };
      nodes.set(id, n);
    }
    // Every id referenced inside an open subgraph is a member of it.
    for (const b of boundaryStack) if (!b.members.includes(id)) b.members.push(id);
    return n;
  };

  const lines = src.split('\n');
  for (const raw of lines) {
    const line = raw.trim();
    if (!line) continue;
    // Skip directives that carry no node/edge/boundary info.
    if (/^(flowchart|graph|direction|linkStyle|style|classDef|%%)/.test(line)) continue;

    // Subgraph open: `subgraph id [Title]` | `subgraph "Title"` | `subgraph Title`
    const sg = /^subgraph\s+(?:([A-Za-z0-9_-]+)\s*)?(?:\[\s*"?([^"\]]*)"?\s*\]|"([^"]*)"|(.+))?$/.exec(line);
    if (sg) {
      const id = sg[1] ?? `boundary-${boundaries.length}`;
      const label = (sg[2] ?? sg[3] ?? sg[4] ?? sg[1] ?? 'Boundary').trim();
      const b: FlowBoundary = { id, label, members: [] };
      boundaries.push(b);
      boundaryStack.push(b);
      continue;
    }
    if (/^end\b/.test(line)) {
      boundaryStack.pop();
      continue;
    }

    // `class a,b,c clsName`
    const cls = /^class\s+([A-Za-z0-9_,\s-]+?)\s+([A-Za-z0-9_-]+)\s*$/.exec(line);
    if (cls) {
      const kind = kindFromClass(cls[2].trim());
      if (kind) {
        for (const id of cls[1].split(',').map((s) => s.trim())) {
          if (id) {
            ensureNode(id).kind = kind;
            explicit.add(id);
          }
        }
      }
      continue;
    }

    // Node definitions anywhere on the line (also inside an edge like `a[A] --> b[B]`).
    NODE_RE.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = NODE_RE.exec(line)) !== null) {
      const [, id, open, quoted, plain, inlineCls] = m;
      const node = ensureNode(id);
      const label = normalizeLabel(quoted ?? plain ?? '');
      if (label) node.label = label;
      if (open === '[(' || open === '([') {
        node.kind = 'store'; // cylinder shape → store, unless class overrides
        explicit.add(id);
      }
      const kind = inlineCls ? kindFromClass(inlineCls) : undefined;
      if (kind) {
        node.kind = kind;
        explicit.add(id);
      }
    }

    // Edges — strip node-def brackets to bare ids first so endpoints parse cleanly.
    const bare = line.replace(NODE_RE, '$1 ');
    EDGE_RE.lastIndex = 0;
    while ((m = EDGE_RE.exec(bare)) !== null) {
      const [, source, label, target] = m;
      ensureNode(source);
      ensureNode(target);
      const trimmedLabel = (label ?? '').trim();
      edges.push({
        id: `e${edgeSeq++}`,
        source,
        target,
        label: trimmedLabel || undefined,
      });
    }
  }

  // Redirect edges that point at a subgraph id to that boundary's first member —
  // Mermaid allows an edge to a subgraph, but we render boundaries as background
  // groups with nothing to attach to. Then drop any node that is really a boundary.
  const boundaryById = new Map(boundaries.map((b) => [b.id, b]));
  const redirect = (id: string): string => {
    const b = boundaryById.get(id);
    return b && b.members.length > 0 ? b.members[0] : id;
  };
  for (const e of edges) {
    e.source = redirect(e.source);
    e.target = redirect(e.target);
  }
  for (const id of boundaryById.keys()) nodes.delete(id);
  const cleanEdges = edges.filter(
    (e) => e.source !== e.target && nodes.has(e.source) && nodes.has(e.target),
  );

  // Infer a kind for nodes that weren't explicitly classed, from their label —
  // many scans omit classDef, and colour-coding is what makes the graph legible.
  for (const n of nodes.values()) {
    if (!explicit.has(n.id)) n.kind = inferKind(n.label);
  }

  // Flag danger edges from node kinds + label keywords.
  for (const e of cleanEdges) {
    const s = nodes.get(e.source);
    const t = nodes.get(e.target);
    const kindDanger =
      s?.kind === 'sink' || s?.kind === 'external' || t?.kind === 'sink' || t?.kind === 'external';
    const labelDanger = e.label ? DANGER_RE.test(e.label) : false;
    if (kindDanger || labelDanger) e.danger = true;
  }

  const liveBoundaries = boundaries.filter((b) => b.members.length > 0);
  return { nodes: [...nodes.values()], edges: cleanEdges, boundaries: liveBoundaries };
}

/**
 * isRenderable is the confidence gate: we only take over from the raw Mermaid
 * SVG when we parsed a graph substantial enough to be worth the custom render
 * (at least a couple of nodes and one edge). Anything thinner → fall back.
 */
export function isRenderable(g: FlowGraph): boolean {
  return g.nodes.length >= 2 && g.edges.length >= 1;
}
