import { createFileRoute } from '@tanstack/react-router';
import { ScanReportPage } from '@/pages/ScanReportPage';

export const Route = createFileRoute('/scans/$id')({
  component: ScanReportPage,
});
