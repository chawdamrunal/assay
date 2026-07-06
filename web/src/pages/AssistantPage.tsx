import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
} from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { AnimatePresence, motion } from 'framer-motion';
import {
  ArrowRight,
  Bot,
  Cable,
  CornerDownLeft,
  ListTree,
  Loader2,
  Package,
  Plus,
  Send,
  Server,
  ShieldCheck,
  Sparkles,
  User,
  Wand2,
} from 'lucide-react';
import { Card } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Markdown } from '@/components/Markdown';
import { ChatScanThread } from '@/components/ChatScanThread';
import { useScanStream } from '@/hooks/useScanStream';
import { api } from '@/lib/api';
import { cn } from '@/lib/utils';
import { useScanProgress } from '@/lib/scan-progress';
import { useAssistantConversation, type Turn } from '@/lib/assistant-conversation';
import type { AssistantCandidate, AssistantReply, Item } from '@/types/api';

/**
 * AssistantPage is the chat-driven entry point: the user asks "is X safe?",
 * Assay resolves candidates from inventory + marketplace cache, the user
 * picks one, and the same conversation embeds the live scan progress.
 *
 * Conversation state (turns + conversation_id) lives in the root-level
 * AssistantConversationProvider, not in this component — so it survives route
 * navigation (this page unmounts on nav) and page refreshes. The
 * conversation_id from the first response is threaded into every subsequent
 * POST so the server can resolve "yes" / "scan the second one" against the
 * prior proposal.
 */

export function AssistantPage() {
  const { register } = useScanProgress();
  // Transcript lives in the root provider so it survives navigation + refresh.
  const { turns, setTurns, convID, setConvID, reset } = useAssistantConversation();
  const [input, setInput] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  // --- Inventory typeahead -----------------------------------------------
  // Surface the machine's own askable artifacts (plugins, MCP servers,
  // connectors, skills) as the user types, so a stub like "is a…" pops the
  // full list to pick from. Inventory rides the same cached query the
  // Inventory/Dashboard pages already use, so this adds no extra fetch.
  const { data: inventory } = useQuery({ queryKey: ['inventory'], queryFn: api.getInventory });
  const [sugFocused, setSugFocused] = useState(false);
  const [sugDismissed, setSugDismissed] = useState(false);
  const [sugActive, setSugActive] = useState(-1);
  const suggestions = useMemo(
    () => computeSuggestions(inventory?.items ?? [], input),
    [inventory, input],
  );
  const sugOpen =
    sugFocused && !sugDismissed && input.trim().length > 0 && suggestions.length > 0 && !pending;

  const onComposerChange = useCallback((v: string) => {
    setInput(v);
    setSugDismissed(false);
    setSugActive(-1);
  }, []);

  // Auto-scroll on each new turn. We scroll the document (or the AppShell's
  // <main>) rather than a fixed-height inner container so chat works inside
  // the standard page-scroll model — that's what lets every other page on
  // the app scroll naturally too. scrollRef points to the bottom anchor.
  useEffect(() => {
    if (!scrollRef.current) return;
    scrollRef.current.scrollIntoView({ behavior: 'smooth', block: 'end' });
  }, [turns.length]);

  // Focus the input on mount.
  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const submit = useCallback(
    async (text: string) => {
      const trimmed = text.trim();
      if (!trimmed || pending) return;
      setError(null);
      const id = `u-${Date.now()}`;
      setTurns((prev) => [...prev, { role: 'user', text: trimmed, id }]);
      setInput('');
      setPending(true);
      try {
        const reply = await api.sendAssistantMessage(trimmed, convID);
        setConvID(reply.conversation_id);
        setTurns((prev) => {
          const next: Turn[] = [
            ...prev,
            { role: 'assistant', reply, id: `a-${Date.now()}` },
          ];
          if (reply.kind === 'scan_started' && reply.scan_id) {
            // Register with the global tracker so the TopBar indicator
            // shows progress and the scan keeps streaming if the user
            // navigates away from the assistant chat.
            register(reply.scan_id, reply.target ?? reply.scan_id.slice(0, 8));
            next.push({
              role: 'scan',
              scanID: reply.scan_id,
              target: reply.target ?? '',
              id: `s-${reply.scan_id}`,
            });
          }
          return next;
        });
      } catch (e: unknown) {
        setError(e instanceof Error ? e.message : String(e));
      } finally {
        setPending(false);
        // Re-focus input so the user can keep typing.
        requestAnimationFrame(() => inputRef.current?.focus());
      }
    },
    [convID, pending],
  );

  // Picking a suggestion launches resolution immediately ("check <name>") —
  // the server proposes the resolved artifact; no scan runs until the user
  // confirms that proposal. Matches the empty-state QuickChip flow.
  const pickInventory = useCallback(
    (name: string) => {
      setSugDismissed(true);
      setSugActive(-1);
      void submit(`check ${name}`);
    },
    [submit],
  );

  // Shared composer key handling for both the hero and the sticky composer.
  // The dropdown only intercepts keys while it's open AND an item is
  // highlighted, so a plain Enter always sends — the typeahead never hijacks
  // the primary action.
  const onComposerKeyDown = useCallback(
    (e: ReactKeyboardEvent<HTMLTextAreaElement>) => {
      if (sugOpen) {
        if (e.key === 'ArrowDown') {
          e.preventDefault();
          setSugActive((i) => Math.min(i + 1, suggestions.length - 1));
          return;
        }
        if (e.key === 'ArrowUp') {
          e.preventDefault();
          setSugActive((i) => Math.max(i - 1, -1));
          return;
        }
        if (e.key === 'Escape') {
          e.preventDefault();
          setSugDismissed(true);
          setSugActive(-1);
          return;
        }
        if (e.key === 'Enter' && !e.shiftKey && sugActive >= 0) {
          e.preventDefault();
          pickInventory(suggestions[sugActive].name);
          return;
        }
      }
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        void submit(input);
      }
    },
    [sugOpen, sugActive, suggestions, pickInventory, submit, input],
  );

  const pickCandidate = useCallback(
    async (cand: AssistantCandidate, index: number) => {
      // Use ordinal-confirm phrasing so the server's pattern matcher uses
      // the right pending-list index — this keeps the chat transcript
      // honest about what the user did.
      const ordinal = ['first', 'second', 'third', 'fourth', 'fifth'][index] ?? `${index + 1}`;
      await submit(`scan the ${ordinal} one (${cand.name})`);
    },
    [submit],
  );

  const pickSuggestion = useCallback(
    async (name: string) => {
      await submit(`check ${name}`);
    },
    [submit],
  );

  // Empty state (only the intro greeting) → a centered, modern hero entry.
  // Once the user sends anything it becomes the scrolling chat thread below.
  if (turns.length <= 1) {
    return (
      <div className="fade-in mx-auto flex min-h-[72vh] w-full max-w-2xl flex-col items-center justify-center gap-8 px-1">
        <div className="flex flex-col items-center gap-3 text-center">
          <span className="inline-flex items-center gap-1.5 rounded-full border border-[color:var(--color-border)] bg-[color:var(--color-card)] px-3 py-1 text-xs font-medium text-[color:var(--color-muted-foreground)] shadow-[var(--shadow-card)]">
            <Sparkles className="size-3.5 text-[color:var(--color-primary)]" />
            Ask Assay
          </span>
          <h1 className="text-4xl font-semibold tracking-tight sm:text-[2.75rem]">
            How can I help you scan?
          </h1>
          <p className="max-w-md text-sm leading-relaxed text-[color:var(--color-muted-foreground)]">
            Ask about any plugin, MCP server, or skill on this machine. I'll find
            the source, confirm with you, then run a full security scan.
          </p>
        </div>

        {error && (
          <div className="w-full rounded-lg border border-[color:var(--color-danger)]/40 bg-[color:var(--color-danger-soft)] px-3 py-2 text-xs text-[color:var(--color-danger)]">
            {error}
          </div>
        )}

        <form
          className="w-full"
          onSubmit={(e) => {
            e.preventDefault();
            void submit(input);
          }}
        >
          <div className="relative rounded-2xl border border-[color:var(--color-border)] bg-[color:var(--color-card)] p-2 shadow-[var(--shadow-elevated)] transition-colors focus-within:border-[color:var(--color-primary)]">
            {sugOpen && (
              <SuggestionDropdown
                items={suggestions}
                activeIndex={sugActive}
                onPick={pickInventory}
                placement="below"
              />
            )}
            <textarea
              ref={inputRef}
              value={input}
              onChange={(e) => onComposerChange(e.target.value)}
              onKeyDown={onComposerKeyDown}
              onFocus={() => setSugFocused(true)}
              onBlur={() => setSugFocused(false)}
              placeholder="Ask about a plugin… e.g. is vercel safe?"
              rows={3}
              className="w-full resize-none bg-transparent px-3 py-2.5 text-base leading-relaxed placeholder:text-[color:var(--color-muted-foreground)] focus:outline-none"
            />
            <div className="flex items-center justify-between gap-2 px-1.5 pb-0.5">
              <span className="flex items-center gap-1 px-1.5 text-[11px] text-[color:var(--color-muted-foreground)]">
                <CornerDownLeft className="size-3" />
                Enter to send · Shift+Enter for newline
              </span>
              <Button type="submit" disabled={pending || !input.trim()}>
                {pending ? <Loader2 className="size-4 animate-spin" /> : <Send className="size-4" />}
                Send
              </Button>
            </div>
          </div>
        </form>

        <div className="flex flex-wrap items-center justify-center gap-2">
          <QuickChip
            icon={<ListTree className="size-3.5" />}
            label="List my plugins"
            onClick={() => void submit('list my plugins')}
            disabled={pending}
          />
          <QuickChip
            icon={<ShieldCheck className="size-3.5" />}
            label="Is a plugin safe?"
            onClick={() => {
              setInput('is ');
              inputRef.current?.focus();
            }}
            disabled={pending}
          />
          <QuickChip
            icon={<Server className="size-3.5" />}
            label="Check an MCP server"
            onClick={() => {
              setInput('check ');
              inputRef.current?.focus();
            }}
            disabled={pending}
          />
        </div>
      </div>
    );
  }

  return (
    // The page uses the standard scroll model — no viewport-height lock,
    // no inner overflow container. The AppShell's <main> is the scroll
    // container; this page is just a long flex column. Padding-bottom
    // reserves space so the sticky composer never covers the last message.
    <div className="fade-in flex flex-col gap-4 max-w-3xl pb-32 sm:pb-28">
      <header className="flex items-center justify-between gap-2 pb-1">
        <div className="flex items-center gap-2 text-sm font-semibold tracking-tight">
          <Sparkles className="size-4 text-[color:var(--color-primary)]" />
          Ask Assay
        </div>
        <Button variant="outline" size="sm" onClick={reset} disabled={pending}>
          <Plus className="size-3.5" />
          New chat
        </Button>
      </header>

      {/* Thread — natural flow, no inner overflow. AppShell <main> scrolls. */}
      <div className="flex flex-col gap-2 px-1 sm:px-2">
        <AnimatePresence initial={false}>
          {turns.map((t) =>
            t.role === 'user' ? (
              <UserBubble key={t.id} text={t.text} />
            ) : t.role === 'assistant' ? (
              <AssistantBubble
                key={t.id}
                reply={t.reply}
                onPickCandidate={pickCandidate}
                onPickSuggestion={pickSuggestion}
                disabled={pending}
              />
            ) : (
              <EmbeddedScan key={t.id} scanID={t.scanID} target={t.target} />
            ),
          )}
          {pending && <ThinkingRow key="thinking" />}
        </AnimatePresence>
        {/* Bottom anchor used by auto-scroll on new turn. */}
        <div ref={scrollRef} aria-hidden="true" />
      </div>

      {/* Sticky composer — anchored to the bottom of the AppShell main
          viewport so the input is always reachable while scrollback works
          for the full thread. Hovers above the AppShell's bottom padding. */}
      <form
        className="sticky bottom-3 z-10 mt-auto rounded-xl border border-[color:var(--color-border)] bg-[color:var(--color-card)] p-3 shadow-lg sm:p-4"
        onSubmit={(e) => {
          e.preventDefault();
          void submit(input);
        }}
      >
        {error && (
          <div className="mb-2 rounded-md border border-danger/40 bg-danger/10 p-2 text-xs text-danger">
            {error}
          </div>
        )}
        {sugOpen && (
          <SuggestionDropdown
            items={suggestions}
            activeIndex={sugActive}
            onPick={pickInventory}
            placement="above"
          />
        )}
        <div className="flex items-end gap-2">
          <textarea
            ref={inputRef}
            value={input}
            onChange={(e) => onComposerChange(e.target.value)}
            onKeyDown={onComposerKeyDown}
            onFocus={() => setSugFocused(true)}
            onBlur={() => setSugFocused(false)}
            placeholder="Ask about a plugin… e.g. is vercel safe?"
            rows={1}
            className="min-h-[2.5rem] flex-1 resize-none rounded-md border border-[color:var(--color-border)] bg-[color:var(--color-background)] px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-[color:var(--color-primary)]"
          />
          <Button type="submit" disabled={pending || !input.trim()}>
            {pending ? <Loader2 className="size-4 animate-spin" /> : <Send className="size-4" />}
            Send
          </Button>
        </div>
        <div className="mt-1.5 flex items-center gap-1 text-[10px] text-[color:var(--color-muted-foreground)]">
          <CornerDownLeft className="size-3" />
          Enter to send · Shift+Enter for newline
        </div>
      </form>
    </div>
  );
}

function QuickChip({
  icon,
  label,
  onClick,
  disabled,
}: {
  icon: ReactNode;
  label: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className="inline-flex cursor-pointer items-center gap-2 rounded-xl border border-[color:var(--color-border)] bg-[color:var(--color-card)] px-3.5 py-2 text-sm font-medium text-[color:var(--color-foreground)] shadow-[var(--shadow-card)] transition-[transform,border-color,box-shadow] duration-150 hover:-translate-y-0.5 hover:border-[color:var(--color-border-strong)] hover:shadow-[var(--shadow-elevated)] disabled:pointer-events-none disabled:opacity-50"
    >
      <span className="text-[color:var(--color-muted-foreground)]">{icon}</span>
      {label}
    </button>
  );
}

function UserBubble({ text }: { text: string }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.2 }}
      className="mb-4 flex items-start justify-end gap-3"
    >
      <div className="max-w-[80%] rounded-2xl rounded-tr-sm border border-[color:var(--color-border)] bg-[color:var(--color-muted)] px-4 py-2.5 text-sm leading-relaxed">
        {text}
      </div>
      <div className="grid size-9 shrink-0 place-items-center rounded-full border border-[color:var(--color-border)] bg-[color:var(--color-background)] text-[color:var(--color-muted-foreground)]">
        <User className="size-4" />
      </div>
    </motion.div>
  );
}

function AssistantBubble({
  reply,
  onPickCandidate,
  onPickSuggestion,
  disabled,
}: {
  reply: AssistantReply;
  onPickCandidate: (cand: AssistantCandidate, index: number) => void;
  onPickSuggestion: (name: string) => void;
  disabled: boolean;
}) {
  const isError = reply.kind === 'error';
  return (
    <motion.div
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.2 }}
      className="mb-4 flex items-start gap-3"
    >
      <div
        className={
          'grid size-9 shrink-0 place-items-center rounded-full border bg-[color:var(--color-background)] ' +
          (isError
            ? 'border-danger/50 text-danger'
            : 'border-[color:var(--color-border)] text-[color:var(--color-primary)]')
        }
      >
        <Bot className="size-4" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 text-xs">
          <span className="font-semibold text-[color:var(--color-foreground)]">Assay</span>
          {reply.kind === 'proposal' && (
            <span className="rounded-full border border-primary/40 bg-primary/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-primary">
              proposal
            </span>
          )}
          {reply.kind === 'scan_started' && (
            <span className="rounded-full border border-success/40 bg-success/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-success">
              scan started
            </span>
          )}
          {reply.kind === 'error' && (
            <span className="rounded-full border border-danger/40 bg-danger/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-danger">
              error
            </span>
          )}
        </div>
        <div
          className={
            'mt-1 rounded-2xl rounded-tl-sm border px-4 py-2.5 text-sm leading-relaxed text-[color:var(--color-foreground)] ' +
            (isError
              ? 'border-danger/40 bg-danger/5'
              : 'border-[color:var(--color-border)] bg-[color:var(--color-card)]')
          }
        >
          <Markdown source={reply.text} className="text-sm [&_p]:my-1 [&_ol]:my-1.5 [&_ul]:my-1.5" />
        </div>
        {reply.kind === 'proposal' && reply.candidates && reply.candidates.length > 0 && (
          <div className="mt-2 flex flex-col gap-2">
            {reply.candidates.map((c, i) => (
              <CandidateCard
                key={`${c.name}-${c.local_path}`}
                candidate={c}
                index={i}
                onPick={() => onPickCandidate(c, i)}
                disabled={disabled}
              />
            ))}
          </div>
        )}
        {reply.suggestions && reply.suggestions.length > 0 && (
          <div className="mt-2 flex flex-wrap items-center gap-2 text-xs">
            <span className="text-[color:var(--color-muted-foreground)]">Did you mean:</span>
            {reply.suggestions.map((s) => (
              <button
                key={s.name}
                type="button"
                disabled={disabled}
                onClick={() => onPickSuggestion(s.name)}
                className="inline-flex items-center gap-1 rounded-full border border-[color:var(--color-border)] bg-[color:var(--color-muted)] px-2 py-0.5 text-xs hover:border-[color:var(--color-primary)] hover:bg-[color:var(--color-card)] disabled:opacity-50"
              >
                <Package className="size-3" />
                {s.name}
              </button>
            ))}
          </div>
        )}
        {reply.github_url && (
          <div className="mt-2 text-xs text-[color:var(--color-muted-foreground)]">
            <a
              href={reply.github_url}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 text-[color:var(--color-primary)] hover:underline"
            >
              Open {reply.github_url} ↗
            </a>
          </div>
        )}
      </div>
    </motion.div>
  );
}

function CandidateCard({
  candidate,
  index,
  onPick,
  disabled,
}: {
  candidate: AssistantCandidate;
  index: number;
  onPick: () => void;
  disabled: boolean;
}) {
  return (
    <Card className="flex flex-wrap items-center gap-3 p-3">
      <div className="grid size-8 shrink-0 place-items-center rounded-md border border-[color:var(--color-border)] text-[color:var(--color-primary)]">
        <Package className="size-4" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-2">
          <span className="font-medium">{candidate.name}</span>
          {candidate.version && (
            <span className="text-xs text-[color:var(--color-muted-foreground)] tabular-nums">v{candidate.version}</span>
          )}
          <KindBadge kind={candidate.kind} />
          {candidate.marketplace && (
            <span className="text-[11px] text-[color:var(--color-muted-foreground)]">
              from {candidate.marketplace}
            </span>
          )}
        </div>
        <div className="mt-0.5 truncate font-mono text-[11px] text-[color:var(--color-muted-foreground)]">
          {candidate.local_path}
        </div>
        {candidate.description && (
          <div className="mt-0.5 text-xs text-[color:var(--color-muted-foreground)] line-clamp-2">
            {candidate.description}
          </div>
        )}
      </div>
      <Button onClick={onPick} disabled={disabled} size="sm">
        {`Scan #${index + 1}`}
        <ArrowRight className="size-3.5" />
      </Button>
    </Card>
  );
}

function KindBadge({ kind }: { kind: string }) {
  const label =
    kind === 'installed-plugin'
      ? 'installed'
      : kind === 'marketplace-plugin'
        ? 'marketplace'
        : kind === 'mcp-server'
          ? 'MCP server'
          : kind;
  return (
    <span className="rounded-full border border-[color:var(--color-border)] bg-[color:var(--color-muted)] px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-[color:var(--color-muted-foreground)]">
      {label}
    </span>
  );
}

function EmbeddedScan({ scanID, target }: { scanID: string; target: string }) {
  const { events, done } = useScanStream(scanID);
  return (
    <motion.div
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25 }}
      className="mb-4 ml-12 rounded-xl border border-[color:var(--color-border)] bg-[color:var(--color-background)] p-3"
    >
      <div className="mb-2 flex items-center justify-between gap-2">
        <div className="text-[10px] uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
          Live scan · {target || scanID.slice(0, 8)}
        </div>
        {done && (
          <Link to="/scans/$id" params={{ id: scanID }}>
            <Button variant="outline" size="sm" className="h-7 px-2 text-xs">
              Open full report
              <ArrowRight className="size-3" />
            </Button>
          </Link>
        )}
      </div>
      <ChatScanThread events={events} done={done} target={target || scanID.slice(0, 8)} />
    </motion.div>
  );
}

function ThinkingRow() {
  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      className="mb-2 flex items-center gap-3 px-12 text-xs text-[color:var(--color-muted-foreground)]"
    >
      <Loader2 className="size-3 animate-spin" />
      Assay is thinking…
    </motion.div>
  );
}

// ---------------------------------------------------------------------------
// Inventory typeahead
// ---------------------------------------------------------------------------

interface Suggestion {
  name: string;
  kind: string;
}

// The artifact kinds a user can meaningfully ask "is X safe?" about. Hooks and
// settings are inventoried too but aren't scan/ask targets, so they're excluded.
const ASKABLE_KINDS = new Set(['claude-code-plugin', 'mcp-server', 'connector', 'skill']);

// Lead-in / filler words stripped before matching, so "is vercel safe?" and
// "check playwright" resolve to the artifact name ("vercel", "playwright").
const STOP_WORDS = new Set([
  'is', 'are', 'was', 'the', 'a', 'an', 'check', 'scan', 'how', 'safe', 'does', 'do',
  'can', 'could', 'you', 'tell', 'me', 'about', 'it', 'this', 'that', 'my', 'of',
  'please', 'run', 'on', 'plugin', 'mcp', 'server', 'connector', 'skill',
]);

/**
 * computeSuggestions builds the typeahead list from the inventory. It dedupes
 * askable artifacts by name, then either returns the full list (when the input
 * is only lead-in words like "is a…", so the user can browse everything) or a
 * ranked match set (exact → prefix → substring → all-words-present).
 */
function computeSuggestions(items: Item[], input: string): Suggestion[] {
  const seen = new Set<string>();
  const all: Suggestion[] = [];
  for (const it of items) {
    if (!ASKABLE_KINDS.has(it.kind) || seen.has(it.name)) continue;
    seen.add(it.name);
    all.push({ name: it.name, kind: it.kind });
  }
  all.sort((a, b) => a.name.localeCompare(b.name));

  const words = input.toLowerCase().replace(/[?.,!]/g, ' ').split(/\s+/).filter(Boolean);
  const termWords = words.filter((w) => !STOP_WORDS.has(w));
  const term = termWords.join(' ').trim();
  if (!term) return all.slice(0, 30); // stub query → browse the full list

  return all
    .map((s) => ({ s, score: scoreName(s.name, term, termWords) }))
    .filter((x) => x.score > 0)
    .sort((a, b) => b.score - a.score || a.s.name.localeCompare(b.s.name))
    .slice(0, 12)
    .map((x) => x.s);
}

function scoreName(name: string, term: string, termWords: string[]): number {
  const n = name.toLowerCase();
  if (n === term) return 4;
  if (n.startsWith(term)) return 3;
  if (n.includes(term)) return 2;
  if (termWords.length > 0 && termWords.every((w) => n.includes(w))) return 1;
  return 0;
}

function SuggestionDropdown({
  items,
  activeIndex,
  onPick,
  placement,
}: {
  items: Suggestion[];
  activeIndex: number;
  onPick: (name: string) => void;
  placement: 'above' | 'below';
}) {
  const listRef = useRef<HTMLDivElement>(null);
  // Keep the highlighted option visible when arrowing past the fold.
  useEffect(() => {
    if (activeIndex < 0) return;
    const el = listRef.current?.children[activeIndex] as HTMLElement | undefined;
    el?.scrollIntoView({ block: 'nearest' });
  }, [activeIndex]);

  return (
    <div
      // preventDefault on mousedown keeps the textarea focused so the click's
      // onPick fires before a blur would tear the dropdown down.
      onMouseDown={(e) => e.preventDefault()}
      role="listbox"
      className={cn(
        'absolute left-0 right-0 z-40 overflow-hidden rounded-xl border border-[color:var(--color-border)] bg-[color:var(--color-card)] shadow-[var(--shadow-elevated)]',
        placement === 'above' ? 'bottom-full mb-2' : 'top-full mt-2',
      )}
    >
      <div className="px-3 pt-2 pb-1 text-[10px] uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
        Your plugins, MCP servers, connectors &amp; skills
      </div>
      <div ref={listRef} className="max-h-64 overflow-y-auto px-1 pb-1">
        {items.map((s, i) => (
          <button
            key={`${s.kind}:${s.name}`}
            type="button"
            role="option"
            aria-selected={i === activeIndex}
            onClick={() => onPick(s.name)}
            className={cn(
              'flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-sm text-[color:var(--color-foreground)] transition-colors',
              i === activeIndex
                ? 'bg-[color:var(--color-primary-soft)]'
                : 'hover:bg-[color:var(--color-muted)]',
            )}
          >
            <SuggestionIcon kind={s.kind} />
            <span className="min-w-0 flex-1 truncate">{s.name}</span>
            <span className="shrink-0 text-[10px] uppercase tracking-wide text-[color:var(--color-muted-foreground)]">
              {kindShort(s.kind)}
            </span>
          </button>
        ))}
      </div>
      <div className="flex items-center gap-2 border-t border-[color:var(--color-border)] px-3 py-1.5 text-[10px] text-[color:var(--color-muted-foreground)]">
        <span className="tabular-nums">↑↓</span> navigate
        <span aria-hidden="true">·</span> Enter to check
        <span aria-hidden="true">·</span> Esc to dismiss
      </div>
    </div>
  );
}

function SuggestionIcon({ kind }: { kind: string }) {
  const Icon =
    kind === 'mcp-server'
      ? Server
      : kind === 'connector'
        ? Cable
        : kind === 'skill'
          ? Wand2
          : Package;
  return (
    <span className="grid size-6 shrink-0 place-items-center rounded-md border border-[color:var(--color-border)] text-[color:var(--color-primary)]">
      <Icon className="size-3.5" />
    </span>
  );
}

function kindShort(kind: string): string {
  switch (kind) {
    case 'claude-code-plugin':
      return 'plugin';
    case 'mcp-server':
      return 'mcp';
    case 'connector':
      return 'connector';
    case 'skill':
      return 'skill';
    default:
      return kind;
  }
}
