import { useEffect, useMemo, useRef, useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { AlertOctagon, Bot, Check, Loader2, ShieldCheck, Sparkles } from 'lucide-react';
import type { ScanProgressEvent } from '@/types/api';

/**
 * ChatScanThread renders the live scan as a conversation: each SSE event
 * arrives as a chat bubble from Assay with a typewriter reveal, while a
 * "thinking…" bubble pulses between events to make the wait feel intentional.
 *
 * Three guiding choices:
 *
 *  1. **Stage events are framed in human voice** — `start` messages become
 *     "I'm starting <stage>…", `complete` becomes "Done with <stage> (Nms)."
 *     so users see the scan as work, not log lines.
 *
 *  2. **Typewriter reveal at ~35 chars/sec** — fast enough to never feel
 *     blocking, slow enough that arriving messages feel authored, not
 *     dumped. The reveal respects prefers-reduced-motion.
 *
 *  3. **Single Assay avatar + alternating accent dot** — every message is
 *     from the same speaker, so we don't need user-vs-AI sides. The accent
 *     dot animates while the message is the "current" one.
 */
export function ChatScanThread({
  events,
  done,
  target,
}: {
  events: ScanProgressEvent[];
  done: boolean;
  target: string;
}) {
  const bottomRef = useRef<HTMLDivElement>(null);
  const introTime = useMemo(() => new Date(), []);

  // Auto-scroll the document (or AppShell main) so the latest event is in
  // view. We scroll a bottom anchor into view rather than scrolling a fixed
  // inner container — that lets the chat live inside the standard page
  // scroll model and inherit the same scrollbar / momentum / mobile
  // behavior the rest of the app uses.
  useEffect(() => {
    if (!bottomRef.current) return;
    bottomRef.current.scrollIntoView({ behavior: 'smooth', block: 'end' });
  }, [events.length, done]);

  const messages = useMemo<ChatMessage[]>(() => {
    const base: ChatMessage[] = [
      {
        id: 'intro',
        kind: 'intro',
        text: `Hi — I'm Assay. I'm about to walk the source of ${target} through a seven-stage security analysis. I'll narrate every step here as it happens.`,
        timestamp: introTime,
      },
    ];
    let prevAt = introTime.getTime();
    events.forEach((e, idx) => {
      const at = new Date(prevAt + 600 + Math.random() * 200);
      prevAt = at.getTime();
      base.push({
        id: `${idx}-${e.stage}-${e.status}`,
        kind: 'event',
        event: e,
        text: humanize(e),
        timestamp: at,
      });
    });
    return base;
  }, [events, introTime, target]);

  const showThinking = !done && events.length > 0 && (events[events.length - 1]?.status === 'complete' || events[events.length - 1]?.status === 'start');

  return (
    // No inner overflow / no viewport-relative height cap. The chat is a
    // plain flex column that grows with content; the parent page (LiveScan
    // or AssistantPage) inherits whatever scroll container its layout
    // provides. This keeps page scroll, mobile momentum, and viewport sizing
    // consistent across every surface that embeds this component.
    <div className="relative flex min-h-[280px] flex-col gap-4 px-1 py-2">
      <AnimatePresence initial={false}>
        {messages.map((m, idx) => (
          <ChatBubble key={m.id} msg={m} isLast={idx === messages.length - 1 && !showThinking} />
        ))}
        {showThinking && <ThinkingBubble key="thinking" />}
        {done && (
          <motion.div
            key="done"
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.3 }}
            className="self-center rounded-full border border-success/30 bg-success/10 px-3 py-1 text-xs font-medium text-success"
          >
            <ShieldCheck className="mr-1 inline size-3" />
            Analysis complete — assembling report
          </motion.div>
        )}
      </AnimatePresence>
      {/* Bottom anchor for auto-scroll. */}
      <div ref={bottomRef} aria-hidden="true" />
    </div>
  );
}

type ChatMessage =
  | { id: string; kind: 'intro'; text: string; timestamp: Date }
  | { id: string; kind: 'event'; event: ScanProgressEvent; text: string; timestamp: Date };

function ChatBubble({ msg, isLast }: { msg: ChatMessage; isLast: boolean }) {
  const text = msg.text;
  const reveal = useTypewriter(text, isLast ? 28 : 0);
  const status: 'ok' | 'err' | 'running' | 'intro' =
    msg.kind === 'intro'
      ? 'intro'
      : msg.event.status === 'error'
        ? 'err'
        : msg.event.status === 'complete'
          ? 'ok'
          : 'running';

  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, ease: 'easeOut' }}
      className="flex items-start gap-3"
    >
      <Avatar status={status} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold text-[color:var(--color-foreground)]">Assay</span>
          <StatusPill status={status} stage={msg.kind === 'event' ? msg.event.stage : undefined} />
          <span className="text-[10px] uppercase tracking-wider text-[color:var(--color-muted-foreground)]">
            {msg.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
          </span>
        </div>
        <div className="mt-1 rounded-2xl rounded-tl-sm border border-[color:var(--color-border)] bg-[color:var(--color-card)] px-4 py-2.5 text-sm leading-relaxed text-[color:var(--color-foreground)] shadow-sm">
          {reveal}
          {isLast && reveal.length < text.length && <Caret />}
        </div>
      </div>
    </motion.div>
  );
}

function Avatar({ status }: { status: 'ok' | 'err' | 'running' | 'intro' }) {
  const ring =
    status === 'ok'
      ? 'border-success/50 text-success'
      : status === 'err'
        ? 'border-danger/50 text-danger'
        : status === 'running'
          ? 'border-primary/50 text-primary'
          : 'border-[color:var(--color-border)] text-[color:var(--color-primary)]';
  return (
    <div
      className={`relative grid size-9 shrink-0 place-items-center rounded-full border bg-[color:var(--color-background)] ${ring}`}
    >
      <Bot className="size-4" />
      {status === 'running' && (
        <span className="absolute -bottom-0.5 -right-0.5 inline-flex size-2.5 items-center justify-center rounded-full bg-primary ring-2 ring-[color:var(--color-background)]">
          <span className="size-1.5 animate-ping rounded-full bg-primary" />
        </span>
      )}
    </div>
  );
}

function StatusPill({ status, stage }: { status: 'ok' | 'err' | 'running' | 'intro'; stage?: string }) {
  if (status === 'intro') {
    return (
      <span className="inline-flex items-center gap-1 rounded-full border border-[color:var(--color-border)] bg-[color:var(--color-muted)] px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-[color:var(--color-muted-foreground)]">
        <Sparkles className="size-2.5" />
        intro
      </span>
    );
  }
  if (status === 'ok') {
    return (
      <span className="inline-flex items-center gap-1 rounded-full border border-success/40 bg-success/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-success">
        <Check className="size-2.5" />
        {stage ?? 'done'}
      </span>
    );
  }
  if (status === 'err') {
    return (
      <span className="inline-flex items-center gap-1 rounded-full border border-danger/40 bg-danger/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-danger">
        <AlertOctagon className="size-2.5" />
        {stage ?? 'error'}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-full border border-primary/40 bg-primary/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-primary">
      <Loader2 className="size-2.5 animate-spin" />
      {stage ?? 'running'}
    </span>
  );
}

function ThinkingBubble() {
  return (
    <motion.div
      initial={{ opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -4 }}
      transition={{ duration: 0.2 }}
      className="flex items-start gap-3"
    >
      <Avatar status="running" />
      <div className="rounded-2xl rounded-tl-sm border border-[color:var(--color-border)] bg-[color:var(--color-card)] px-4 py-3 shadow-sm">
        <div className="flex items-center gap-1.5">
          <Dot delay={0} />
          <Dot delay={150} />
          <Dot delay={300} />
          <span className="ml-2 text-xs text-[color:var(--color-muted-foreground)]">Assay is thinking…</span>
        </div>
      </div>
    </motion.div>
  );
}

function Dot({ delay }: { delay: number }) {
  return (
    <span
      className="inline-block size-1.5 animate-pulse rounded-full bg-[color:var(--color-muted-foreground)]"
      style={{ animationDelay: `${delay}ms` }}
    />
  );
}

function Caret() {
  return <span className="ml-0.5 inline-block h-3 w-1.5 translate-y-0.5 animate-pulse bg-[color:var(--color-primary)]" />;
}

/**
 * useTypewriter reveals `text` at `charsPerStep` per 30ms tick. Setting
 * `charsPerStep` to 0 disables the reveal (used for old messages so we
 * don't re-animate history every time a new event arrives).
 */
function useTypewriter(text: string, charsPerStep: number): string {
  const [out, setOut] = useState(() => (charsPerStep === 0 ? text : ''));
  const reducedMotion = useReducedMotion();
  useEffect(() => {
    if (charsPerStep === 0 || reducedMotion) {
      setOut(text);
      return;
    }
    setOut('');
    let i = 0;
    const id = setInterval(() => {
      i = Math.min(text.length, i + Math.max(1, Math.round(charsPerStep / 8)));
      setOut(text.slice(0, i));
      if (i >= text.length) clearInterval(id);
    }, 35);
    return () => clearInterval(id);
  }, [text, charsPerStep, reducedMotion]);
  return out;
}

function useReducedMotion(): boolean {
  const [reduce, setReduce] = useState(() => {
    if (typeof window === 'undefined') return false;
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  });
  useEffect(() => {
    const m = window.matchMedia('(prefers-reduced-motion: reduce)');
    const onChange = () => setReduce(m.matches);
    m.addEventListener('change', onChange);
    return () => m.removeEventListener('change', onChange);
  }, []);
  return reduce;
}

const STAGE_LABELS: Record<string, string> = {
  prepass: 'pre-pass',
  triage: 'triage',
  claims: 'claim extraction',
  threat_model: 'threat modeling',
  investigation: 'investigation',
  exploitability: 'exploitability analysis',
  synthesis: 'synthesis',
  done: 'wrap-up',
};

const STAGE_START_NARRATION: Record<string, string> = {
  prepass: "Scanning the source with fast static heuristics — looking for secrets, suspicious shell-outs, and high-risk imports.",
  triage: "Reading the manifest, declared permissions, and any docs. I want to know what this thing *claims* to do.",
  claims: "Extracting structured security-relevant claims from the source so I can compare them against the actual behavior.",
  threat_model: "Building a data-flow diagram and drafting a threat model — where does data enter, where does it leave, and where could trust break?",
  investigation: "Spawning targeted investigations against each threat. Each one looks at specific code paths to confirm or refute risk.",
  exploitability: "For every confirmed issue, weighing the actual exploitability — preconditions, attacker positioning, and impact ceiling.",
  synthesis: "Pulling everything together into a verdict and writing the final findings, with citations back to the source.",
  done: "Wrapping up — the audit JSON and the report are about to be ready.",
};

const STAGE_COMPLETE_NARRATION: Record<string, string> = {
  prepass: "Pre-pass done. I have a baseline risk picture before any LLM cost.",
  triage: "Triage done. I now know what the plugin's authors say it does.",
  claims: "Claim extraction done. I have a list of testable assertions about behavior.",
  threat_model: "Threat model done. I have the data-flow diagram and a prioritized list of threats to investigate.",
  investigation: "Investigations done — each threat has been chased to a verdict against the real source.",
  exploitability: "Exploitability analysis done. Severity ratings now reflect realistic blast radius, not just code shape.",
  synthesis: "Synthesis done. Verdict and findings are written.",
  done: "All done.",
};

function humanize(e: ScanProgressEvent): string {
  const stage = STAGE_LABELS[e.stage] ?? e.stage;
  if (e.status === 'start') {
    return STAGE_START_NARRATION[e.stage] ?? `Starting ${stage}…`;
  }
  if (e.status === 'complete') {
    const base = STAGE_COMPLETE_NARRATION[e.stage] ?? `Finished ${stage}.`;
    return e.message ? `${base} ${e.message}` : base;
  }
  if (e.status === 'error') {
    return e.message
      ? `Hit an error during ${stage}: ${e.message}`
      : `Hit an error during ${stage}.`;
  }
  return e.message ?? `${stage}: ${e.status}`;
}
