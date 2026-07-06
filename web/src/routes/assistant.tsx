import { createFileRoute } from '@tanstack/react-router';
import { AssistantPage } from '@/pages/AssistantPage';

export const Route = createFileRoute('/assistant')({
  component: AssistantPage,
});
