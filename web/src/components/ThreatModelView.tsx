import { useMemo } from 'react';
import { Shield, Target, Users } from 'lucide-react';
import { Markdown } from './Markdown';
import { cn } from '@/lib/utils';

/**
 * ThreatModelView parses the LLM's threat_model markdown into structured
 * cards. The methodology asks for `## Tn — Title` sections with optional
 * `**Affected servers:**`, `**Affected files:**`, `**Findings:**`, and
 * `**Asset at risk:** / **Attacker:** / **Surface:** / **Why it matters:**`
 * metadata. We extract those fields and render them as pills/sidebars
 * around the body prose.
 *
 * Falls back to plain Markdown rendering when the document doesn't match
 * the expected shape — never worse than v1's render, often dramatically
 * better.
 */
export function ThreatModelView({ markdown }: { markdown: string }) {
  const threats = useMemo(() => parseThreats(markdown), [markdown]);
  if (threats.length === 0) {
    return <Markdown source={markdown} />;
  }
  return (
    <ol className="flex flex-col gap-3">
      {threats.map((t) => (
        <li key={t.id}>
          <ThreatCard threat={t} />
        </li>
      ))}
    </ol>
  );
}

interface Threat {
  id: string;        // "T1"
  title: string;     // "Arbitrary Code Execution via Tool Parameters"
  body: string;      // remaining paragraphs (un-parsed Markdown)
  findings: string[];      // ["FIND-001", "FIND-002"]
  affectedServers: string[];
  affectedFiles: string[];
  metadata: Record<string, string>; // Asset at risk / Attacker / Surface / Severity / etc
}

/**
 * parseThreats splits the document by `## T<digit> — <title>` headers and
 * extracts per-threat metadata. Robust to absent metadata lines.
 */
function parseThreats(markdown: string): Threat[] {
  // Split on lines beginning with "## T" + digits + optional separator
  const sections = markdown.split(/(?=^##\s+T\d+\b)/m).filter((s) => s.trim());
  const threats: Threat[] = [];
  for (const sec of sections) {
    const headerMatch = sec.match(/^##\s+(T\d+)\s*[—\-:]?\s*(.+?)\s*$/m);
    if (!headerMatch) continue;
    const [, id, title] = headerMatch;

    // Strip the header from the body so prose render doesn't show it.
    const body = sec.replace(/^##\s+T\d+.*$/m, '').trim();

    const findings = extractList(body, /\*\*\s*(?:Mapped\s+)?findings?\s*:?\s*\*\*\s*(.+?)$/im);
    const affectedServers = extractList(body, /\*\*\s*affected\s+servers?\s*:?\s*\*\*\s*(.+?)$/im);
    const affectedFiles = extractList(body, /\*\*\s*affected\s+files?\s*:?\s*\*\*\s*(.+?)$/im);

    const metadata: Record<string, string> = {};
    for (const key of ['Asset at risk', 'Attacker', 'Counterparty', 'Surface', 'Severity', 'Why it matters', 'Class']) {
      const re = new RegExp(`\\*\\*\\s*${key}\\s*:?\\s*\\*\\*\\s*([^\\n]+)`, 'i');
      const m = body.match(re);
      if (m) metadata[key] = m[1].trim();
    }

    // Strip out the metadata lines from body so they don't double up.
    let prose = body;
    prose = prose.replace(/\*\*\s*(?:Mapped\s+)?findings?\s*:?\s*\*\*[^\n]+\n?/gi, '');
    prose = prose.replace(/\*\*\s*affected\s+(?:servers?|files?)\s*:?\s*\*\*[^\n]+\n?/gi, '');
    for (const key of Object.keys(metadata)) {
      prose = prose.replace(new RegExp(`\\*\\*\\s*${key}\\s*:?\\s*\\*\\*[^\\n]+\\n?`, 'gi'), '');
    }
    prose = prose.replace(/\n{3,}/g, '\n\n').trim();

    threats.push({ id, title, body: prose, findings, affectedServers, affectedFiles, metadata });
  }
  return threats;
}

function extractList(body: string, re: RegExp): string[] {
  const m = body.match(re);
  if (!m) return [];
  return m[1]
    .replace(/`/g, '')
    .split(/[,;]/)
    .map((s) => s.trim())
    .filter(Boolean);
}

function ThreatCard({ threat }: { threat: Threat }) {
  return (
    <article
      id={threat.id}
      className={cn(
        'overflow-hidden rounded-xl border border-[color:var(--color-border)] bg-[color:var(--color-card)]',
        'transition-all hover:border-[color:var(--color-primary)]/30 hover:shadow-md hover:shadow-black/20',
      )}
    >
      <header className="flex flex-wrap items-baseline justify-between gap-3 border-b border-[color:var(--color-border)] bg-[color:var(--color-muted)]/30 px-5 py-3">
        <div className="flex items-baseline gap-3">
          <span className="inline-flex items-center gap-1.5 rounded-md border border-[color:var(--color-primary)]/40 bg-[color:var(--color-primary)]/10 px-2 py-0.5 text-xs font-bold uppercase tracking-wider text-[color:var(--color-primary)]">
            <Shield className="size-3" />
            {threat.id}
          </span>
          <h3 className="text-base font-semibold tracking-tight">{threat.title}</h3>
        </div>
        {threat.findings.length > 0 && (
          <div className="flex flex-wrap items-center gap-1.5 text-[11px] text-[color:var(--color-muted-foreground)]">
            <span className="uppercase tracking-wider">Maps to</span>
            {threat.findings.map((f) => (
              <a
                key={f}
                href={`#${f}`}
                className="rounded-full border border-[color:var(--color-border)] bg-[color:var(--color-card)] px-2 py-0.5 font-mono text-[10px] text-[color:var(--color-foreground)] hover:border-[color:var(--color-primary)] hover:text-[color:var(--color-primary)]"
              >
                {f}
              </a>
            ))}
          </div>
        )}
      </header>

      <div className="grid grid-cols-1 gap-4 p-5 md:grid-cols-[1fr_220px]">
        <div className="min-w-0">
          {Object.keys(threat.metadata).length > 0 && (
            <dl className="mb-3 grid grid-cols-1 gap-1 text-xs sm:grid-cols-2">
              {Object.entries(threat.metadata).map(([k, v]) => (
                <div key={k} className="flex flex-col">
                  <dt className="uppercase tracking-wider text-[color:var(--color-muted-foreground)]">{k}</dt>
                  <dd className="text-[color:var(--color-foreground)]">{v}</dd>
                </div>
              ))}
            </dl>
          )}
          {threat.body && (
            <Markdown source={threat.body} className="text-sm [&_p]:my-1.5" />
          )}
        </div>

        {/* Sidebar pills — affected servers + files. Hidden on mobile (md:block)
            because the inline metadata above already shows enough at narrow widths. */}
        {(threat.affectedServers.length > 0 || threat.affectedFiles.length > 0) && (
          <aside className="hidden md:flex md:flex-col md:gap-3 md:border-l md:border-[color:var(--color-border)] md:pl-4">
            {threat.affectedServers.length > 0 && (
              <PillList
                icon={<Target className="size-3" />}
                label="Affected servers"
                items={threat.affectedServers}
              />
            )}
            {threat.affectedFiles.length > 0 && (
              <PillList
                icon={<Users className="size-3" />}
                label="Affected files"
                items={threat.affectedFiles}
              />
            )}
          </aside>
        )}
      </div>
    </article>
  );
}

function PillList({ icon, label, items }: { icon: React.ReactNode; label: string; items: string[] }) {
  return (
    <section>
      <h4 className="mb-1.5 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
        {icon}
        {label}
      </h4>
      <ul className="flex flex-wrap gap-1">
        {items.map((it) => (
          <li
            key={it}
            className="rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-muted)]/50 px-2 py-0.5 font-mono text-[10px] text-[color:var(--color-muted-foreground)]"
          >
            {it}
          </li>
        ))}
      </ul>
    </section>
  );
}
