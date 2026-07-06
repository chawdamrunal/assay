/**
 * AssistantConversationProvider — a root-level context that holds the Ask-Assay
 * chat transcript so it survives route navigation. Mirrors ScanProgressProvider.
 *
 * The bug it fixes: the assistant page's `turns` used to live in component-local
 * useState, so navigating away unmounted the page and wiped the conversation —
 * the scan kept running (ScanProgressProvider tracks it globally) but the chat
 * around it vanished, leaving the user staring at a fresh greeting. Lifting
 * `turns` + `conversation_id` up here means the transcript is intact when the
 * user comes back. A localStorage mirror (12h expiry, same as ScanProgressProvider)
 * keeps it across a full page refresh too.
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type ReactNode,
  type SetStateAction,
} from 'react';
import type { AssistantReply } from '@/types/api';

export type Turn =
  | { role: 'user'; text: string; id: string }
  | { role: 'assistant'; reply: AssistantReply; id: string }
  | { role: 'scan'; scanID: string; target: string; id: string };

const INTRO_REPLY: AssistantReply = {
  kind: 'text',
  text:
    "Hi — I'm Assay. Ask me about any plugin or MCP server you have on this machine, like:\n\n" +
    '1. **"is vercel plugin safe?"**\n' +
    '2. **"check frontend-design"**\n' +
    '3. **"list my plugins"**\n\n' +
    "I'll find the source, confirm with you, then run a full scan.",
  conversation_id: '',
};

const INTRO_TURNS: Turn[] = [{ role: 'assistant', reply: INTRO_REPLY, id: 'intro' }];

interface AssistantConversationValue {
  turns: Turn[];
  setTurns: Dispatch<SetStateAction<Turn[]>>;
  convID: string | undefined;
  setConvID: (id: string | undefined) => void;
  /** Clear the transcript back to the intro greeting. */
  reset: () => void;
}

const AssistantConversationContext = createContext<AssistantConversationValue | null>(null);

// localStorage mirror so a refresh keeps the transcript. A chat is small; the
// 12h expiry (matching ScanProgressProvider) drops stale conversations.
const LS_KEY = 'assay.assistantConversation.v1';

interface Stored {
  turns: Turn[];
  convID?: string;
  savedAt: number;
}

function loadStored(): { turns: Turn[]; convID?: string } {
  if (typeof window === 'undefined') return { turns: INTRO_TURNS };
  try {
    const raw = window.localStorage.getItem(LS_KEY);
    if (!raw) return { turns: INTRO_TURNS };
    const s = JSON.parse(raw) as Stored;
    const cutoff = Date.now() - 12 * 60 * 60 * 1000;
    if (!s || !Array.isArray(s.turns) || s.turns.length === 0 || !s.savedAt || s.savedAt < cutoff) {
      return { turns: INTRO_TURNS };
    }
    return { turns: s.turns, convID: s.convID };
  } catch {
    return { turns: INTRO_TURNS };
  }
}

export function AssistantConversationProvider({ children }: { children: ReactNode }) {
  // Single lazy read from localStorage, shared by both state slices.
  const [initial] = useState(loadStored);
  const [turns, setTurns] = useState<Turn[]>(initial.turns);
  const [convID, setConvID] = useState<string | undefined>(initial.convID);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    try {
      const payload: Stored = { turns, convID, savedAt: Date.now() };
      window.localStorage.setItem(LS_KEY, JSON.stringify(payload));
    } catch {
      /* quota — ignore */
    }
  }, [turns, convID]);

  const reset = useCallback(() => {
    setTurns(INTRO_TURNS);
    setConvID(undefined);
  }, []);

  const value = useMemo<AssistantConversationValue>(
    () => ({ turns, setTurns, convID, setConvID, reset }),
    [turns, convID, reset],
  );

  return (
    <AssistantConversationContext.Provider value={value}>
      {children}
    </AssistantConversationContext.Provider>
  );
}

export function useAssistantConversation(): AssistantConversationValue {
  const ctx = useContext(AssistantConversationContext);
  if (!ctx) {
    throw new Error('useAssistantConversation must be used inside <AssistantConversationProvider>');
  }
  return ctx;
}
