import { Badge } from './badge';

export interface StatusBadgeProps {
  status: 'success' | 'error' | 'warning' | 'info';
  label: string;
  showDot?: boolean;
}

const statusVariants: Record<StatusBadgeProps['status'], string> = {
  success: 'bg-green-500/20 text-green-500 border-green-500/30',
  error: 'bg-red-500/20 text-red-500 border-red-500/30',
  warning: 'bg-yellow-500/20 text-yellow-500 border-yellow-500/30',
  info: 'bg-blue-500/20 text-blue-500 border-blue-500/30',
};

export function StatusBadge({ status, label, showDot = true }: StatusBadgeProps) {
  return (
    <Badge className={statusVariants[status]} variant="outline">
      {showDot && <span className="size-1.5 rounded-full bg-current" />}
      {label}
    </Badge>
  );
}
