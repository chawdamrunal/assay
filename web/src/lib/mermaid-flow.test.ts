import { describe, it, expect } from 'vitest';
import { parseMermaidFlow, isRenderable } from './mermaid-flow';

// The exact shape internal/mcp/methodology.md Step 3 asks the LLM to emit.
const EXAMPLE = `flowchart LR
  evt[Claude Code SessionStart] --> hooks
  subgraph hooks [Plugin hooks]
    profiler[profiler.mjs]
    inject[inject-claude-md.mjs]
  end
  profiler -->|reads project manifests| fs1[(project files)]
  profiler -->|daily POST telemetry| net[https://telemetry.vercel.com]
  inject -->|stdout additionalContext| host[Claude Code session]
  classDef ext fill:#ede9fe,stroke:#7c3aed,color:#1e1b4b
  classDef store fill:#dcfce7,stroke:#16a34a,color:#052e16
  class net ext
  class fs1 store`;

describe('parseMermaidFlow', () => {
  const g = parseMermaidFlow(EXAMPLE);

  it('is renderable', () => {
    expect(isRenderable(g)).toBe(true);
  });

  it('extracts nodes with labels', () => {
    expect(g.nodes.find((n) => n.id === 'net')?.label).toBe('https://telemetry.vercel.com');
    expect(g.nodes.find((n) => n.id === 'profiler')?.label).toBe('profiler.mjs');
  });

  it('maps classDef/class and cylinder shape to kinds', () => {
    expect(g.nodes.find((n) => n.id === 'net')?.kind).toBe('external'); // class ext
    expect(g.nodes.find((n) => n.id === 'fs1')?.kind).toBe('store'); // class store + [(...)]
  });

  it('captures subgraph boundaries and their members', () => {
    const b = g.boundaries.find((x) => x.label === 'Plugin hooks');
    expect(b).toBeTruthy();
    expect(b?.members).toEqual(expect.arrayContaining(['profiler', 'inject']));
    // the subgraph id must NOT survive as a node
    expect(g.nodes.find((n) => n.id === 'hooks')).toBeUndefined();
  });

  it('redirects an edge aimed at a subgraph to its first member', () => {
    // evt --> hooks (a subgraph) becomes evt --> <first member>
    const e = g.edges.find((x) => x.source === 'evt');
    expect(e).toBeTruthy();
    expect(['profiler', 'inject']).toContain(e?.target);
  });

  it('keeps edge labels and flags danger flows', () => {
    const e = g.edges.find((x) => x.source === 'profiler' && x.target === 'net');
    expect(e?.label).toBe('daily POST telemetry');
    expect(e?.danger).toBe(true); // external target + "POST" keyword
  });

  it('rejects non-diagram text', () => {
    expect(isRenderable(parseMermaidFlow('this is not a diagram'))).toBe(false);
  });
});
