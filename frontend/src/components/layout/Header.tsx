import { type ReactNode } from 'react';
import './Header.css';

export interface HeaderProps {
  title: string;
  apiUrl: string;
  onApiUrlChange: (url: string) => void;
  action?: ReactNode;
}

export function Header({ title, apiUrl, onApiUrlChange, action }: HeaderProps) {
  return (
    <header className="header">
      <h1 className="page-title">{title}</h1>
      <div className="header-actions">
        <div className="api-url-input">
          <svg width="16" height="16" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" />
          </svg>
          <input
            type="text"
            value={apiUrl}
            onChange={(e) => onApiUrlChange(e.target.value)}
            placeholder="API URL"
          />
        </div>
        {action && <div className="header-action">{action}</div>}
      </div>
    </header>
  );
}
