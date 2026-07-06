import { createFileRoute } from '@tanstack/react-router';
import { FleetDetailPage } from '@/pages/FleetDetailPage';

export const Route = createFileRoute('/fleet/$id')({
  component: FleetDetailPage,
});
