import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Cable, Cog, Package, Server, Webhook } from 'lucide-react';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { Button } from '@/components/ui/button';
import { api } from '@/lib/api';
import type { Item, Kind } from '@/types/api';

/**
 * InventoryPage — v0.4 redesign.
 *
 * v0.3 rendered a single flat table mixing plugins / MCP servers / hooks /
 * settings. v0.4 groups by kind into separate sections with kind-specific
 * iconography + responsive card grid. Reads better on mobile and surfaces
 * the natural separation a security reviewer cares about (the plugin attack
 * surface is fundamentally different from the hook attack surface).
 */
export function InventoryPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['inventory'],
    queryFn: api.getInventory,
  });

  const groups = useMemo(() => groupByKind(data?.items ?? []), [data]);

  return (
    <div className="fade-in flex flex-col gap-6">
      <header className="flex flex-col gap-2">
        <h1 className="text-3xl font-semibold tracking-tight">Inventory</h1>
        <p className="text-sm text-[color:var(--color-muted-foreground)]">
          Everything Assay can see in <code className="font-mono text-xs">~/.claude</code> — plugins, MCP servers, connectors, hooks, and settings overrides.
        </p>
      </header>

      {isLoading && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-24 w-full" />
        </div>
      )}

      {error && (
        <div className="rounded-md border border-danger/40 bg-danger/10 p-4 text-sm text-danger">
          Failed to load inventory: {(error as Error).message}
        </div>
      )}

      {data && (data.items ?? []).length === 0 && (
        <Card className="p-12 text-center">
          <Package className="mx-auto size-10 text-[color:var(--color-muted-foreground)] mb-3" />
          <p className="text-[color:var(--color-muted-foreground)]">
            No plugins, MCP servers, hooks, or settings overrides found in <code className="font-mono text-xs">~/.claude</code>.
          </p>
        </Card>
      )}

      {groups.plugins.length > 0 && (
        <Section
          icon={<Package className="size-4" />}
          title="Claude Code plugins"
          count={groups.plugins.length}
          description="Installed via /plugin install."
        >
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
            {groups.plugins.map((it) => (
              <PluginCard key={`${it.kind}:${it.name}`} item={it} />
            ))}
          </div>
        </Section>
      )}

      {groups.mcpServers.length > 0 && (
        <Section
          icon={<Server className="size-4" />}
          title="MCP servers"
          count={groups.mcpServers.length}
          description="Declared in settings.json. Some don't have a local source dir (npx, uvx)."
        >
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {groups.mcpServers.map((it) => (
              <MCPCard key={`${it.kind}:${it.name}`} item={it} />
            ))}
          </div>
        </Section>
      )}

      {groups.connectors.length > 0 && (
        <Section
          icon={<Cable className="size-4" />}
          title="Connectors"
          count={groups.connectors.length}
          description="OAuth-scoped integrations. claude.ai connectors are hosted remotely; local ones ship a manifest."
        >
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
            {groups.connectors.map((it) => (
              <ConnectorCard key={`${it.kind}:${it.name}`} item={it} />
            ))}
          </div>
        </Section>
      )}

      {groups.hooks.length > 0 && (
        <Section
          icon={<Webhook className="size-4" />}
          title="Hooks"
          count={groups.hooks.length}
          description="Shell commands attached to Claude Code events. Worth a second look."
        >
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {groups.hooks.map((it) => (
              <HookCard key={`${it.kind}:${it.name}`} item={it} />
            ))}
          </div>
        </Section>
      )}

      {groups.settings.length > 0 && (
        <Section
          icon={<Cog className="size-4" />}
          title="Settings"
          count={groups.settings.length}
          description="Per-file settings sources Assay enumerated."
        >
          <ul className="flex flex-col gap-2">
            {groups.settings.map((it) => (
              <li key={`${it.kind}:${it.name}`}>
                <Card className="p-3 flex items-center justify-between gap-3">
                  <div className="font-mono text-xs truncate">{it.local_path ?? it.name}</div>
                  {it.metadata?.allow_count && (
                    <span className="text-xs text-[color:var(--color-muted-foreground)]">
                      {it.metadata.allow_count} allow rules
                    </span>
                  )}
                </Card>
              </li>
            ))}
          </ul>
        </Section>
      )}
    </div>
  );
}

interface KindGroups {
  plugins: Item[];
  mcpServers: Item[];
  connectors: Item[];
  hooks: Item[];
  settings: Item[];
}

function groupByKind(items: Item[]): KindGroups {
  const out: KindGroups = { plugins: [], mcpServers: [], connectors: [], hooks: [], settings: [] };
  for (const it of items) {
    const kind = it.kind as Kind;
    if (kind === 'claude-code-plugin') out.plugins.push(it);
    else if (kind === 'mcp-server') out.mcpServers.push(it);
    else if (kind === 'connector') out.connectors.push(it);
    else if (kind === 'hook') out.hooks.push(it);
    else if (kind === 'settings') out.settings.push(it);
  }
  return out;
}

function Section({
  icon,
  title,
  count,
  description,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  count: number;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <section className="flex flex-col gap-3">
      <header className="flex items-baseline gap-3 flex-wrap">
        <div className="flex items-center gap-2">
          {icon}
          <h2 className="text-sm font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
            {title}
          </h2>
          <span className="text-xs text-[color:var(--color-muted-foreground)]">({count})</span>
        </div>
        <p className="text-xs text-[color:var(--color-muted-foreground)]">{description}</p>
      </header>
      {children}
    </section>
  );
}

function PluginCard({ item }: { item: Item }) {
  return (
    <Card className="group p-4 flex flex-col gap-2 transition-colors hover:bg-[color:var(--color-muted)]/30">
      <div className="flex items-baseline justify-between gap-2">
        <div className="font-medium truncate">{item.name}</div>
        {item.version && (
          <span className="text-xs text-[color:var(--color-muted-foreground)] tabular-nums shrink-0 truncate">
            v{item.version}
          </span>
        )}
      </div>
      {item.metadata?.marketplace && (
        <div className="text-xs text-[color:var(--color-muted-foreground)] truncate">
          {item.metadata.marketplace}
        </div>
      )}
      {item.local_path && (
        <div className="text-[10px] text-[color:var(--color-muted-foreground)] font-mono truncate" title={item.local_path}>
          {item.local_path}
        </div>
      )}
      <div className="pt-1">
        <Link to="/scan/new">
          <Button variant="ghost" size="sm" className="text-[color:var(--color-primary)] -ml-2">
            Scan →
          </Button>
        </Link>
      </div>
    </Card>
  );
}

function MCPCard({ item }: { item: Item }) {
  // Project-scoped servers (declared under ~/.claude.json projects[*].mcpServers)
  // carry a scope tag so a reviewer can tell a per-workspace server from a global.
  const project = item.metadata?.scope === 'project' ? item.metadata?.project : undefined;
  return (
    <Card className="p-4 flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <div className="font-medium truncate">{item.name}</div>
        {project && (
          <span
            className="shrink-0 rounded bg-[color:var(--color-muted)] px-1.5 py-0.5 text-[10px] text-[color:var(--color-muted-foreground)]"
            title={project}
          >
            project
          </span>
        )}
      </div>
      {item.metadata?.commandLine && (
        <code className="font-mono text-xs text-[color:var(--color-muted-foreground)] truncate" title={item.metadata.commandLine}>
          {item.metadata.commandLine}
        </code>
      )}
      {item.metadata?.envKeys && item.metadata.envKeys !== '' && (
        <div className="text-[10px] text-[color:var(--color-muted-foreground)]">
          env: {item.metadata.envKeys}
        </div>
      )}
    </Card>
  );
}

function ConnectorCard({ item }: { item: Item }) {
  const provider = item.metadata?.provider;
  const remote = item.metadata?.scope === 'remote';
  // Hide the provider subtitle when it's already embedded in the name
  // (e.g. "claude.ai Gmail" has provider "claude.ai").
  const showProvider = provider && !item.name.toLowerCase().includes(provider.toLowerCase());
  return (
    <Card className="p-4 flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <div className="font-medium truncate">{item.name}</div>
        <span className="shrink-0 text-[10px] uppercase tracking-wide text-[color:var(--color-muted-foreground)]">
          {remote ? 'remote' : 'local'}
        </span>
      </div>
      {showProvider && (
        <div className="text-xs text-[color:var(--color-muted-foreground)] truncate">{provider}</div>
      )}
      {item.permissions && item.permissions.length > 0 && (
        <div className="flex flex-wrap gap-1 pt-0.5">
          {item.permissions.map((p) => (
            <span
              key={p}
              className="rounded bg-[color:var(--color-muted)] px-1.5 py-0.5 text-[10px] font-mono text-[color:var(--color-muted-foreground)]"
            >
              {p}
            </span>
          ))}
        </div>
      )}
      {item.local_path && (
        <div
          className="text-[10px] text-[color:var(--color-muted-foreground)] font-mono truncate"
          title={item.local_path}
        >
          {item.local_path}
        </div>
      )}
    </Card>
  );
}

function HookCard({ item }: { item: Item }) {
  return (
    <Card className="p-4 flex flex-col gap-1.5">
      <div className="font-medium font-mono text-sm truncate">{item.name}</div>
      {item.metadata?.commands && (
        <code className="font-mono text-[11px] text-[color:var(--color-muted-foreground)] truncate" title={item.metadata.commands}>
          {item.metadata.commands}
        </code>
      )}
    </Card>
  );
}
