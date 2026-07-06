import { createFileRoute } from '@tanstack/react-router';
import { LiveScanPage } from '@/pages/LiveScanPage';

export const Route = createFileRoute('/scans/live/$id')({
  component: LiveScanPage,
  // Optional ?target= search param lets the originating page (NewScan,
  // ScansList) prime the chat thread with the human plugin name so Assay
  // doesn't have to address the user with the bare scan UUID.
  validateSearch: (search: Record<string, unknown>): { target?: string } => ({
    target: typeof search.target === 'string' ? search.target : undefined,
  }),
});
