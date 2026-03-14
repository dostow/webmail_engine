import './StatusBadge.css';

export interface StatusBadgeProps {
  status: 'success' | 'error' | 'warning' | 'info';
  label: string;
  showDot?: boolean;
}

export function StatusBadge({ status, label, showDot = true }: StatusBadgeProps) {
  return (
    <span className={`status-badge ${status}`}>
      {showDot && <span className="status-dot" />}
      {label}
    </span>
  );
}
