import type { ReactNode } from 'react';
import { AppShell } from '@/components/layout/AppShell';

export function App({ children }: { children: ReactNode }) {
  return <AppShell>{children}</AppShell>;
}
