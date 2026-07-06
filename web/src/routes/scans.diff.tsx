import { createFileRoute } from '@tanstack/react-router';
import { ScanDiffPage } from '@/pages/ScanDiffPage';

/**
 * Side-by-side diff between two scans of the same target.
 * URL: /scans/diff?a=<scan_id>&b=<scan_id>
 */
export const Route = createFileRoute('/scans/diff')({
  component: ScanDiffPage,
  validateSearch: (search: Record<string, unknown>) => ({
    a: typeof search.a === 'string' ? search.a : '',
    b: typeof search.b === 'string' ? search.b : '',
  }),
});
