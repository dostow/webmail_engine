import { type ReactNode } from 'react';
import './Sidebar.css';

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
    <aside className="sidebar">
      <div className="logo">
        <svg className="nav-icon" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
        </svg>
        Webmail Engine
      </div>

      <nav>
        {sections.map((section) => (
          <div key={section.title} className="nav-section">
            <div className="nav-section-title">{section.title}</div>
            {section.items.map((item) => (
              <div
                key={item.id}
                className={`nav-item ${activeView === item.id ? 'active' : ''}`}
                onClick={() => onViewChange(item.id)}
              >
                <span className="nav-icon-wrapper">{item.icon}</span>
                {item.label}
              </div>
            ))}
          </div>
        ))}
      </nav>
    </aside>
  );
}
