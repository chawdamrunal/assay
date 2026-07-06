import { createFileRoute } from '@tanstack/react-router';
import { FleetListPage } from '@/pages/FleetListPage';

export const Route = createFileRoute('/fleet/')({
  component: FleetListPage,
});
