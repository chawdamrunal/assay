import { useState, type ReactNode } from 'react';
import { Sidebar } from './Sidebar';
import { TopBar } from './TopBar';
import { ScanProgressProvider } from '@/lib/scan-progress';
import { AssistantConversationProvider } from '@/lib/assistant-conversation';

export function AppShell({ children }: { children: ReactNode }) {
  const [mobileNavOpen, setMobileNavOpen] = useState(false);

  // ScanProgressProvider lives at the AppShell root so every scan the
  // user kicks off keeps its SSE subscription open while they navigate
  // between routes. Without this wrapper the LiveScanPage's hook
  // unmounts on nav and the user loses live feedback even though the
  // scan continues server-side. The TopBar reads from this context to
  // render the ActiveScansIndicator.
  //
  // h-dvh (dynamic viewport height) keeps the shell sized correctly when
  // mobile browser chrome collapses on scroll — h-screen (100vh) is baked
  // at load and goes stale when the URL bar hides.
  return (
    <ScanProgressProvider>
      <AssistantConversationProvider>
      <div className="flex h-dvh w-screen overflow-hidden">
        {/* Desktop sidebar (always visible at md+) */}
        <Sidebar className="hidden md:flex" />

        {/* Mobile drawer + scrim */}
        {mobileNavOpen && (
          <div className="fixed inset-0 z-40 md:hidden">
            <button
              type="button"
              aria-label="Close navigation"
              className="absolute inset-0 bg-black/60"
              onClick={() => setMobileNavOpen(false)}
            />
            <Sidebar
              className="relative z-50 h-full w-56 shadow-2xl"
              onNavigate={() => setMobileNavOpen(false)}
            />
          </div>
        )}

        <div className="flex min-w-0 flex-1 flex-col">
          <TopBar onMenuClick={() => setMobileNavOpen(true)} />
          <main
            className="flex-1 overflow-y-auto px-4 py-4 sm:px-6 sm:py-6 md:px-8"
            style={{
              // iOS Safari needs the legacy webkit property for momentum
              // scroll; harmless in Chrome (it ignores unknown properties).
              WebkitOverflowScrolling: 'touch',
              // NOTE: do NOT set touch-action or overscroll-behavior here.
              // Earlier versions set 'pan-y' + 'contain' to fix a mobile
              // touch issue, but Chrome desktop interprets trackpad input as
              // touch and the combination blocked wheel/trackpad scroll in
              // Chrome (worked in Safari because Safari's trackpad handling
              // is different). The default values are correct for all of
              // mobile-touch, desktop-trackpad, and desktop-mouse.
            }}
          >
            {children}
          </main>
        </div>
      </div>
      </AssistantConversationProvider>
    </ScanProgressProvider>
  );
}
