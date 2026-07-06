import { createFileRoute } from '@tanstack/react-router';
import { ScansListPage } from '@/pages/ScansListPage';

export const Route = createFileRoute('/scans/')({
  component: ScansListPage,
});
