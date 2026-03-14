import { type ReactNode } from 'react';

export interface NavItem {
  id: string;
  label: string;
  icon: ReactNode;
}

export interface NavSection {
  title: string;
  items: NavItem[];
}

export interface SidebarProps {
  sections: NavSection[];
  activeView: string;
  onViewChange: (viewId: string) => void;
}

export function Sidebar({ sections, activeView, onViewChange }: SidebarProps) {
  return (
    <aside className="flex w-[250px] flex-col fixed h-screen overflow-y-auto border-r bg-sidebar">
      <div className="flex items-center gap-2 px-6 py-6 text-xl font-bold text-primary">
        <svg className="size-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
        </svg>
        Webmail Engine
      </div>

      <nav className="flex-1 px-3">
        {sections.map((section) => (
          <div key={section.title} className="mb-6">
            <div className="mb-2 px-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              {section.title}
            </div>
            {section.items.map((item) => (
              <div
                key={item.id}
                className={`flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2.5 text-sm transition-all ${activeView === item.id
                    ? 'bg-primary text-primary-foreground'
                    : 'text-muted-foreground hover:bg-muted'
                  }`}
                onClick={() => onViewChange(item.id)}
              >
                <span className="flex size-5 items-center justify-center">{item.icon}</span>
                {item.label}
              </div>
            ))}
          </div>
        ))}
      </nav>
    </aside>
  );
}
