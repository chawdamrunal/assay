import { createFileRoute } from '@tanstack/react-router';
import { NewScanPage } from '@/pages/NewScanPage';

export const Route = createFileRoute('/scan/new')({
  component: NewScanPage,
});
