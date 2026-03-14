# Frontend Improvement Plan

## Overview
Transform the current basic email client into a functional, modern email client using shadcn/ui components and React best practices.

---

## Phase 1: Foundation & Setup

### 1.1 Install Required Dependencies
```bash
cd frontend

# Install Tailwind CSS and shadcn/ui dependencies
npm install -D tailwindcss postcss autoprefixer
npx tailwindcss init -p

# Install shadcn/ui peer dependencies
npm install class-variance-authority clsx tailwind-merge lucide-react
npm install @radix-ui/react-slot @radix-ui/react-dialog @radix-ui/react-dropdown-menu
npm install @radix-ui/react-label @radix-ui/react-select @radix-ui/react-tabs
npm install @radix-ui/react-toast @radix-ui/react-tooltip @radix-ui/react-avatar
npm install @radix-ui/react-popover @radix-ui/react-scroll-area @radix-ui/react-separator

# Install additional utilities
npm install date-fns react-router-dom
```

### 1.2 Configure Tailwind CSS
- Update `tailwind.config.js` with shadcn/ui theming
- Update `postcss.config.js`
- Add Tailwind directives to `src/styles/index.css`

### 1.3 Initialize shadcn/ui
```bash
npx shadcn-ui@latest init
npx shadcn-ui@latest add button card input label badge toast
npx shadcn-ui@latest add dialog select tabs scroll-area separator
npx shadcn-ui@latest add avatar tooltip popover dropdown-menu
npx shadcn-ui@latest add table skeleton form
```

### 1.4 Set Up Global State Management
- Install Zustand: `npm install zustand`
- Create `src/store/useAppStore.ts` with:
  - Account state (list, selected account)
  - Message state (current folder, selected message)
  - UI state (sidebar collapsed, theme)
  - API configuration

### 1.5 Create Custom Hooks
Create `src/hooks/` directory with:
- `useAccounts.ts` - Account CRUD operations
- `useMessages.ts` - Message fetching, pagination
- `useMessage.ts` - Single message detail
- `useCompose.ts` - Compose form state
- `useToast.ts` - Toast notifications wrapper

---

## Phase 2: Core UI Components

### 2.1 Message List View Improvements
**File: `src/components/features/Messages.tsx`**

Current issues:
- No proper time formatting
- Missing sender column
- No click-to-view functionality
- No message preview

**Required changes:**
1. Use shadcn `Table` component for message list
2. Add columns: Checkbox, Sender, Subject, Preview, Date, Actions
3. Format dates using `date-fns` (e.g., "10:30 AM" or "Mar 14")
4. Add click handler to navigate to message detail
5. Add unread indicator
6. Add folder selector dropdown
7. Add refresh button
8. Add pagination or infinite scroll

### 2.2 Message Detail View (NEW)
**File: `src/components/features/MessageDetail.tsx`**

Create new component with:
- Full message headers (From, To, Cc, Date, Subject)
- Message body (HTML and plain text support)
- Attachment list with download buttons
- Reply/Reply All/Forward buttons
- Delete button
- Back to inbox button
- Loading skeleton state

### 2.3 Account Management Improvements
**File: `src/components/features/Accounts.tsx`**

Current issues:
- Edit button doesn't work
- No visual feedback for connection status

**Required changes:**
1. Implement Edit account dialog
2. Add connection status badge (Connected/Disconnected/Auth Error)
3. Add "Test Connection" button
4. Show last sync time
5. Add manual sync trigger button
6. Use shadcn Dialog for add/edit forms
7. Use shadcn Form components for validation

### 2.4 Compose View Improvements
**File: `src/components/features/Compose.tsx`**

**Required changes:**
1. Add To/Cc/Bcc fields with multi-select
2. Add attachment upload with drag-and-drop
3. Add rich text editor (optional: TipTap or Quill)
4. Add draft auto-save
5. Add send confirmation toast
6. Use shadcn Form for validation

---

## Phase 3: Layout & Navigation

### 3.1 Sidebar Improvements
**File: `src/components/layout/Sidebar.tsx`**

**Required changes:**
1. Add folder tree (Inbox, Sent, Drafts, etc.)
2. Show unread count badges
3. Add collapse/expand functionality
4. Add account switcher dropdown
5. Use shadcn ScrollArea for overflow

### 3.2 Header Improvements
**File: `src/components/layout/Header.tsx`**

**Required changes:**
1. Add global search bar
2. Add user/account avatar
3. Add theme toggle (light/dark)
4. Remove API URL input from production (move to settings only)

### 3.3 App Layout
**File: `src/App.tsx`**

**Required changes:**
1. Add message detail route
2. Implement split-view mode (list + detail side by side)
3. Add responsive mobile layout
4. Add loading states between route transitions

---

## Phase 4: Additional Features

### 4.1 Search Functionality
**File: `src/components/features/Search.tsx` (NEW)**

Create search view with:
- Advanced search filters (from, to, subject, date range)
- Search results list
- Save search functionality

### 4.2 Toast Notifications
- Replace all `alert()` and inline errors with shadcn Toast
- Create toast helper functions
- Add success toasts for actions (sent, deleted, etc.)

### 4.3 Loading States
- Add skeleton loaders for all views
- Use shadcn Skeleton component
- Add optimistic updates where appropriate

### 4.4 Error Handling
- Add error boundary component
- Add retry mechanisms for failed requests
- Add user-friendly error messages

---

## Phase 5: Polish & Optimization

### 5.1 Performance
- Implement React Query for caching
- Add virtual scrolling for large message lists
- Lazy load message details

### 5.2 Accessibility
- Add keyboard navigation
- Add ARIA labels
- Ensure color contrast compliance

### 5.3 Responsive Design
- Mobile-first approach
- Hamburger menu for mobile
- Touch-friendly interactions

---

## Implementation Order

### Sprint 1: Foundation (Days 1-2)
1. Install dependencies
2. Configure Tailwind and shadcn/ui
3. Set up Zustand store
4. Create basic custom hooks

### Sprint 2: Message List (Days 3-4)
1. Refactor Messages.tsx with shadcn Table
2. Add proper date formatting
3. Add click-to-view navigation
4. Create MessageDetail.tsx component

### Sprint 3: Account Management (Days 5-6)
1. Implement Edit account dialog
2. Add connection status indicators
3. Add manual sync trigger
4. Improve form validation

### Sprint 4: Compose & Layout (Days 7-8)
1. Improve Compose view
2. Update Sidebar with folder tree
3. Update Header with search
4. Add responsive layout

### Sprint 5: Polish (Days 9-10)
1. Add toast notifications
2. Add loading skeletons
3. Add error boundaries
4. Test and fix bugs

---

## File Structure After Refactor

```
frontend/
├── src/
│   ├── components/
│   │   ├── features/
│   │   │   ├── Accounts.tsx
│   │   │   ├── MessageList.tsx       (renamed from Messages.tsx)
│   │   │   ├── MessageDetail.tsx     (NEW)
│   │   │   ├── Compose.tsx
│   │   │   ├── Search.tsx            (NEW)
│   │   │   ├── Health.tsx
│   │   │   └── Settings.tsx
│   │   ├── layout/
│   │   │   ├── Sidebar.tsx
│   │   │   ├── Header.tsx
│   │   │   └── AppLayout.tsx         (NEW)
│   │   └── ui/                       (shadcn components)
│   │       ├── button.tsx
│   │       ├── card.tsx
│   │       ├── input.tsx
│   │       ├── table.tsx
│   │       ├── dialog.tsx
│   │       ├── toast.tsx
│   │       └── ...
│   ├── hooks/
│   │   ├── useAccounts.ts
│   │   ├── useMessages.ts
│   │   ├── useMessage.ts
│   │   ├── useCompose.ts
│   │   └── useToast.ts
│   ├── store/
│   │   ├── useAppStore.ts
│   │   └── slices/
│   │       ├── accountSlice.ts
│   │       └── messageSlice.ts
│   ├── services/
│   │   └── api.ts
│   ├── styles/
│   │   └── index.css
│   ├── types/
│   │   └── index.ts
│   ├── utils/
│   │   ├── cn.ts                     (NEW - className utility)
│   │   └── format.ts                 (NEW - date/number formatters)
│   ├── App.tsx
│   └── main.tsx
```

---

## Python Script for Guided Implementation

A Python script (`migrate_to_shadcn.py`) will be created to:
1. Check current dependencies
2. Generate package.json updates
3. Create configuration files
4. Generate starter component templates
5. Provide step-by-step migration instructions

---

## Success Criteria

- [ ] Message list displays properly with sender, subject, date columns
- [ ] Clicking a message opens detail view
- [ ] Can reply/forward/delete messages
- [ ] Account edit functionality works
- [ ] Toast notifications for all actions
- [ ] Loading states during async operations
- [ ] Responsive design works on mobile
- [ ] All forms have proper validation
- [ ] No console errors in production build
