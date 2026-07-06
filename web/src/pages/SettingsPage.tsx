import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CheckCircle2, Cpu, DollarSign, Gauge, Github, KeyRound, Layers, Send } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { ConnectionsCard } from '@/components/ConnectionsCard';
import { api } from '@/lib/api';
import type { Config } from '@/types/api';

// Anthropic model catalog — populated from the user-confirmed list in
// CLAUDE.md (Opus 4.7, Sonnet 4.6, Haiku 4.5). When the OpenAI provider
// lands the dropdown groups by provider; for now it's Anthropic-only.
const ANTHROPIC_MODELS = [
  { id: '', label: 'Auto — Claude Code picks the model your plan allows (recommended)' },
  { id: 'claude-opus-4-7', label: 'Claude Opus 4.7 — deepest, slowest, most expensive' },
  { id: 'claude-sonnet-4-6', label: 'Claude Sonnet 4.6 — balanced' },
  { id: 'claude-haiku-4-5', label: 'Claude Haiku 4.5 — fastest, cheapest, shallower' },
];

// Direct-API providers whose keys can be set from the UI. Must match the
// AgentID -api ids the backend accepts at POST /api/keys.
const API_PROVIDERS = [
  { id: 'anthropic-api', label: 'Anthropic API', placeholder: 'sk-ant-…' },
  { id: 'gemini-api', label: 'Gemini (Google) API', placeholder: 'AIza…' },
  { id: 'openai-api', label: 'OpenAI API', placeholder: 'sk-…' },
] as const;

// The LLM "brain" that runs scans. claude-code (default) drives the MCP server
// on the user's subscription; the -api providers run in-process and need a key.
// (gemini-cli / codex-cli are added once their CLI adapters land.)
const PROVIDERS = [
  { id: 'claude-code', label: 'Claude Code — MCP via subscription (default, no key)' },
  { id: 'cursor-agent', label: 'Cursor (cursor-agent) — MCP, via Cursor login or CURSOR_API_KEY' },
  { id: 'anthropic-api', label: 'Anthropic API — direct in-process (no CLI needed), uses your key' },
];

/**
 * SettingsPage — v0.4 redesign.
 *
 * v0.3 dumped the raw config JSON which gave the page a stub-y feel. v0.4
 * presents the same config as labeled grouped cards (no form yet — editing
 * lands in v0.5 along with PATCH /api/config). Bottom of the page keeps the
 * raw JSON in a collapsed-by-default details element for power users.
 */
export function SettingsPage() {
  const { data, isLoading, error } = useQuery({ queryKey: ['config'], queryFn: api.getConfig });

  return (
    <div className="fade-in flex flex-col gap-6 max-w-3xl">
      <header className="flex flex-col gap-2">
        <h1 className="text-3xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-[color:var(--color-muted-foreground)]">
          Configuration for the local scanner. Changes are saved immediately and take effect on the next scan.
        </p>
      </header>

      {isLoading && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          <Skeleton className="h-32 w-full" />
          <Skeleton className="h-32 w-full" />
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
        </div>
      )}

      {error && (
        <div className="rounded-md border border-danger/40 bg-danger/10 p-4 text-sm text-danger">
          Failed to load config: {(error as Error).message}
        </div>
      )}

      <ConnectionsCard />

      <ProviderKeysCard />

      <GitHubTokenCard />

      {data && <SettingsCards config={data} />}
    </div>
  );
}

function SettingsCards({ config }: { config: Config }) {
  const qc = useQueryClient();
  const save = useMutation({
    mutationFn: (next: Config) => api.putConfig(next),
    onSuccess: (saved) => qc.setQueryData(['config'], saved),
  });

  const updateModel = (key: 'default' | 'investigation') => (value: string) => {
    save.mutate({
      ...config,
      models: { ...config.models, [key]: value },
    });
  };

  const updateProvider = (value: string) => {
    save.mutate({ ...config, models: { ...config.models, provider: value } });
  };

  const toggleDeepScan = () => {
    save.mutate({
      ...config,
      scan: { ...config.scan, deep_scan: !config.scan.deep_scan },
    });
  };

  return (
    <>
      <ProviderSelectCard
        value={config.models.provider || 'claude-code'}
        onChange={updateProvider}
        saving={save.isPending}
      />
      <section className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <ModelSelectCard
          icon={<Cpu className="size-4" />}
          label="Default model"
          description="Pinned strictly via claude -p --model. Used for all stages except investigation (triage, claims, threat model, exploitability, synthesis)."
          value={config.models.default}
          onChange={updateModel('default')}
          saving={save.isPending}
        />
        <ModelSelectCard
          icon={<Cpu className="size-4" />}
          label="Investigation model"
          description="Used for stage-3 sub-agents only. Haiku here trades depth for speed/cost."
          value={config.models.investigation}
          onChange={updateModel('investigation')}
          saving={save.isPending}
        />
      </section>
      {save.isError && (
        <div className="rounded-md border border-danger/40 bg-danger/10 p-2 text-xs text-danger">
          Save failed: {(save.error as Error).message}
        </div>
      )}

      <section className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <FieldCard
          icon={<Gauge className="size-4" />}
          label="Sub-agent concurrency"
          description="How many stage-3 investigations run in parallel. Lower for subscription-bearer quotas; raise for API-key users."
          value={String(config.scan.subagent_concurrency)}
          cmd={`assay config set scan.subagent_concurrency <n>`}
        />
        <FieldCard
          icon={<DollarSign className="size-4" />}
          label="Per-scan budget cap (USD)"
          description="Soft cap on Anthropic spend per scan. The scanner stops gracefully and returns partial findings on overrun."
          value={`$${config.scan.budget_usd.toFixed(2)}`}
          cmd={`assay config set scan.budget_usd <amount>`}
        />
      </section>

      <ToggleCard
        icon={<Layers className="size-4" />}
        label="Deep scan (parallel investigation)"
        description="Investigate each threat in its own parallel Claude Code sub-agent instead of one sequential pass — deeper, less context dilution, but spends more of your subscription quota. Off by default."
        enabled={config.scan.deep_scan}
        onToggle={toggleDeepScan}
        saving={save.isPending}
      />

      <Card className="p-4 flex flex-col gap-2">
        <header className="flex items-center gap-2 text-xs uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
          <Send className="size-3.5" />
          Telemetry
        </header>
        <div className="flex items-center justify-between">
          <div className="text-sm">
            {config.telemetry.enabled ? 'Anonymous usage telemetry enabled' : 'Anonymous usage telemetry disabled'}
          </div>
          <code className="font-mono text-xs px-2 py-1 rounded bg-[color:var(--color-muted)] text-[color:var(--color-muted-foreground)]">
            telemetry.enabled = {String(config.telemetry.enabled)}
          </code>
        </div>
      </Card>

      <details className="rounded-md border border-[color:var(--color-border)]">
        <summary className="cursor-pointer p-3 text-xs font-medium text-[color:var(--color-muted-foreground)] hover:text-[color:var(--color-foreground)] select-none">
          Raw config JSON
        </summary>
        <pre className="border-t border-[color:var(--color-border)] p-4 text-xs font-mono overflow-auto bg-[color:var(--color-muted)]/40">
          {JSON.stringify(config, null, 2)}
        </pre>
      </details>
    </>
  );
}

/**
 * ToggleCard is an editable boolean setting persisted via PUT /api/config.
 * Used for deep-scan; the next scan picks up the change (serve re-reads config
 * per scan).
 */
function ToggleCard({
  icon,
  label,
  description,
  enabled,
  onToggle,
  saving,
}: {
  icon: React.ReactNode;
  label: string;
  description: string;
  enabled: boolean;
  onToggle: () => void;
  saving: boolean;
}) {
  return (
    <Card className="p-4 flex flex-col gap-2">
      <div className="flex items-center justify-between gap-3">
        <header className="flex items-center gap-2 text-xs uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
          {icon}
          {label}
        </header>
        <button
          type="button"
          role="switch"
          aria-checked={enabled}
          aria-label={label}
          disabled={saving}
          onClick={onToggle}
          className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors disabled:opacity-60 ${
            enabled ? 'bg-[color:var(--color-primary)]' : 'bg-[color:var(--color-muted)]'
          }`}
        >
          <span
            className={`inline-block size-4 transform rounded-full bg-white transition-transform ${
              enabled ? 'translate-x-4' : 'translate-x-0.5'
            }`}
          />
        </button>
      </div>
      <p className="text-xs text-[color:var(--color-muted-foreground)] leading-relaxed">
        {description}
      </p>
      {saving && (
        <div className="text-[10px] text-[color:var(--color-muted-foreground)]">Saving…</div>
      )}
    </Card>
  );
}

function FieldCard({
  icon,
  label,
  description,
  value,
  cmd,
}: {
  icon: React.ReactNode;
  label: string;
  description: string;
  value: string;
  cmd: string;
}) {
  return (
    <Card className="p-4 flex flex-col gap-2">
      <header className="flex items-center gap-2 text-xs uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
        {icon}
        {label}
      </header>
      <div className="font-mono text-base font-medium">{value}</div>
      <p className="text-xs text-[color:var(--color-muted-foreground)] leading-relaxed">
        {description}
      </p>
      <code className="font-mono text-[11px] px-2 py-1.5 rounded bg-[color:var(--color-muted)]/60 text-[color:var(--color-muted-foreground)] break-all">
        {cmd}
      </code>
    </Card>
  );
}

/**
 * ModelSelectCard renders a dropdown over the Anthropic model catalog and
 * persists the choice via PUT /api/config. The next scan picks up the new
 * model automatically (cmd_serve.go re-reads config per scan).
 *
 * Strict-honor: the backend passes the chosen model to `claude -p --model`,
 * so picking Sonnet here truly runs Sonnet — Claude Code can't silently
 * downgrade to its own default.
 */
function ModelSelectCard({
  icon,
  label,
  description,
  value,
  onChange,
  saving,
}: {
  icon: React.ReactNode;
  label: string;
  description: string;
  value: string;
  onChange: (next: string) => void;
  saving: boolean;
}) {
  // Local "dirty" state so the select feels instant even before the PUT
  // round-trips. React-query's optimistic update via setQueryData syncs
  // the canonical state after the save resolves.
  const [draft, setDraft] = useState(value);
  // Keep draft synced when the canonical value changes from outside.
  if (draft !== value && !saving) {
    // Two-render correction is fine here — saves a useEffect.
    setDraft(value);
  }
  return (
    <Card className="p-4 flex flex-col gap-2">
      <header className="flex items-center gap-2 text-xs uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
        {icon}
        {label}
      </header>
      <select
        className="w-full rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-background)] px-2 py-1.5 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-[color:var(--color-primary)] disabled:opacity-60"
        value={draft}
        disabled={saving}
        onChange={(e) => {
          setDraft(e.target.value);
          onChange(e.target.value);
        }}
      >
        {ANTHROPIC_MODELS.map((m) => (
          <option key={m.id} value={m.id}>
            {m.label}
          </option>
        ))}
        {/* Surface any custom model the user set via the CLI even if it's
            not in our curated list, so the dropdown doesn't pretend to lose
            their config. */}
        {!ANTHROPIC_MODELS.some((m) => m.id === draft) && draft && (
          <option value={draft}>{draft} (custom)</option>
        )}
      </select>
      <p className="text-xs text-[color:var(--color-muted-foreground)] leading-relaxed">
        {description}
      </p>
      {saving && (
        <div className="text-[10px] text-[color:var(--color-muted-foreground)]">Saving…</div>
      )}
    </Card>
  );
}

/**
 * ProviderSelectCard picks the LLM brain (PUT /api/config → models.provider).
 * The next scan picks up the change (serve re-reads config per scan).
 */
function ProviderSelectCard({
  value,
  onChange,
  saving,
}: {
  value: string;
  onChange: (next: string) => void;
  saving: boolean;
}) {
  const [draft, setDraft] = useState(value);
  if (draft !== value && !saving) setDraft(value);
  return (
    <Card className="p-4 flex flex-col gap-2">
      <header className="flex items-center gap-2 text-xs uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
        <Cpu className="size-4" />
        LLM provider
      </header>
      <select
        className="w-full rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-background)] px-2 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-[color:var(--color-primary)] disabled:opacity-60"
        value={draft}
        disabled={saving}
        onChange={(e) => {
          setDraft(e.target.value);
          onChange(e.target.value);
        }}
      >
        {PROVIDERS.map((p) => (
          <option key={p.id} value={p.id}>
            {p.label}
          </option>
        ))}
      </select>
      <p className="text-xs text-[color:var(--color-muted-foreground)] leading-relaxed">
        Which LLM runs scans. Claude Code (default) drives the MCP server on your subscription — no key
        needed. Direct-API providers run in-process and require the matching key in “Provider API keys” below.
      </p>
      {saving && (
        <div className="text-[10px] text-[color:var(--color-muted-foreground)]">Saving…</div>
      )}
    </Card>
  );
}

/**
 * ProviderKeysCard lets the user store a direct-API provider key (Anthropic,
 * Gemini, OpenAI) from the browser. Keys go straight to the OS keychain via
 * POST /api/keys and are NEVER read back — the card only knows whether each
 * provider is configured (GET /api/keys/status), never the value.
 */
function ProviderKeysCard() {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ['keyStatus'], queryFn: api.getKeyStatus });
  const onSaved = () => {
    qc.invalidateQueries({ queryKey: ['keyStatus'] });
    qc.invalidateQueries({ queryKey: ['status'] }); // refresh ConnectionsCard rows
  };
  return (
    <Card className="p-5 flex flex-col gap-3">
      <header className="flex flex-col gap-0.5">
        <h2 className="text-sm font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)] flex items-center gap-2">
          <KeyRound className="size-4" /> Provider API keys
        </h2>
        <p className="text-xs text-[color:var(--color-muted-foreground)]">
          Needed only to scan with a direct-API provider — the default Claude Code path needs no key.
          Keys are stored in your OS keychain, sent only to the provider, and never shown again.
        </p>
      </header>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        {API_PROVIDERS.map((p) => (
          <ApiKeyField
            key={p.id}
            provider={p.id}
            label={p.label}
            placeholder={p.placeholder}
            configured={Boolean(data?.providers[p.id])}
            onSaved={onSaved}
          />
        ))}
      </div>
    </Card>
  );
}

function ApiKeyField({
  provider,
  label,
  placeholder,
  configured,
  onSaved,
}: {
  provider: string;
  label: string;
  placeholder: string;
  configured: boolean;
  onSaved: () => void;
}) {
  const [value, setValue] = useState('');
  const [editing, setEditing] = useState(false);
  const save = useMutation({
    mutationFn: (key: string) => api.setApiKey(provider, key),
    onSuccess: () => {
      setValue('');
      setEditing(false);
      onSaved();
    },
  });
  const showForm = editing || !configured;
  return (
    <div className="rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-card)] p-3 flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium">{label}</span>
        {configured && !editing && (
          <span className="inline-flex items-center gap-1 text-[10px] font-semibold uppercase tracking-wider text-success">
            <CheckCircle2 className="size-3" /> set
          </span>
        )}
      </div>
      {showForm ? (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (value.trim()) save.mutate(value.trim());
          }}
          className="flex flex-col gap-2"
        >
          <input
            type="password"
            autoComplete="new-password"
            spellCheck={false}
            placeholder={placeholder}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            className="rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-background)] px-2 py-1.5 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-[color:var(--color-primary)]"
          />
          <div className="flex gap-2">
            <Button type="submit" size="sm" disabled={!value.trim() || save.isPending}>
              {save.isPending ? 'Saving…' : 'Save key'}
            </Button>
            {configured && (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => {
                  setEditing(false);
                  setValue('');
                }}
              >
                Cancel
              </Button>
            )}
          </div>
          {save.isError && (
            <div className="text-[11px] text-danger">{(save.error as Error).message}</div>
          )}
        </form>
      ) : (
        <Button type="button" variant="outline" size="sm" onClick={() => setEditing(true)}>
          Replace key
        </Button>
      )}
    </div>
  );
}

/**
 * GitHubTokenCard stores a GitHub PAT (for cloning + scanning PRIVATE repos)
 * in the OS keychain via POST /api/github-token — write-only, never read back.
 * Status (configured + source: keychain / env / gh CLI) is read from the
 * "GitHub fetch" row of /api/status, so an auto-detected gh-CLI or env token
 * shows here even with no manual entry. Remove is offered only for a token we
 * actually stored (source: keychain) — we can't unset an ambient gh/env token.
 */
function GitHubTokenCard() {
  const qc = useQueryClient();
  const [value, setValue] = useState('');
  const { data } = useQuery({ queryKey: ['status'], queryFn: api.getStatus });
  const gh = data?.checks.find((c) => c.kind === 'github');
  const hasToken = gh ? /token via/.test(gh.detail) : false;
  const removable = gh ? /via keychain/.test(gh.detail) : false;

  const refresh = () => qc.invalidateQueries({ queryKey: ['status'] });
  const save = useMutation({
    mutationFn: (token: string) => api.setGitHubToken(token),
    onSuccess: () => {
      setValue('');
      refresh();
    },
  });
  const remove = useMutation({ mutationFn: () => api.deleteGitHubToken(), onSuccess: refresh });

  return (
    <Card className="p-5 flex flex-col gap-3">
      <header className="flex flex-col gap-0.5">
        <h2 className="text-sm font-semibold uppercase tracking-wider text-[color:var(--color-muted-foreground)] flex items-center gap-2">
          <Github className="size-4" /> GitHub access (private repos)
        </h2>
        <p className="text-xs text-[color:var(--color-muted-foreground)]">
          Needed only to scan a <span className="font-medium">private</span> GitHub repo. Assay auto-detects a token from{' '}
          <code className="font-mono">GITHUB_TOKEN</code>/<code className="font-mono">GH_TOKEN</code> or the{' '}
          <code className="font-mono">gh</code> CLI; set one here to override. Stored in your OS keychain, sent only to
          github.com, never shown again. A fine-grained read-only <em>Contents</em> token is recommended.
        </p>
      </header>

      {gh && (
        <div
          className={`flex items-center gap-1.5 text-xs ${
            gh.level === 'warn' ? 'text-[color:var(--color-warning)]' : 'text-[color:var(--color-muted-foreground)]'
          }`}
        >
          {hasToken && <CheckCircle2 className="size-3.5 text-[color:var(--color-success)]" />}
          {gh.detail}
        </div>
      )}

      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (value.trim()) save.mutate(value.trim());
        }}
        className="flex flex-col gap-2 sm:flex-row"
      >
        <input
          type="password"
          autoComplete="new-password"
          spellCheck={false}
          placeholder="github_pat_… or ghp_…"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          className="flex-1 rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-background)] px-2 py-1.5 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-[color:var(--color-primary)]"
        />
        <div className="flex gap-2">
          <Button type="submit" size="sm" disabled={!value.trim() || save.isPending}>
            {save.isPending ? 'Saving…' : hasToken ? 'Replace token' : 'Save token'}
          </Button>
          {removable && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={remove.isPending}
              onClick={() => remove.mutate()}
            >
              {remove.isPending ? 'Removing…' : 'Remove'}
            </Button>
          )}
        </div>
      </form>
      {(save.isError || remove.isError) && (
        <div className="text-[11px] text-[color:var(--color-danger)]">
          {((save.error || remove.error) as Error)?.message}
        </div>
      )}
    </Card>
  );
}
