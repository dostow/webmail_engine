import { type ReactNode } from 'react';

export interface HeaderProps {
  title: string;
  apiUrl: string;
  onApiUrlChange: (url: string) => void;
  action?: ReactNode;
}

export function Header({ title, apiUrl, onApiUrlChange, action }: HeaderProps) {
  return (
    <header className="mb-8 flex items-center justify-between">
      <h1 className="text-2xl font-bold">{title}</h1>
      <div className="flex items-center gap-4">
        <div className="flex items-center gap-2 rounded-lg border bg-background px-4 py-2">
          <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" />
          </svg>
          <input
            type="text"
            value={apiUrl}
            onChange={(e) => onApiUrlChange(e.target.value)}
            placeholder="API URL"
            className="w-[200px] border-none bg-transparent text-sm outline-none"
          />
        </div>
        {action && <div>{action}</div>}
      </div>
    </header>
  );
}
