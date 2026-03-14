# Webmail Engine Frontend

A modern React-based frontend for the Webmail Engine API, built with Vite and TypeScript.

## Tech Stack

- **React 19** - UI library
- **TypeScript** - Type safety
- **Vite** - Build tool and dev server
- **Bun** - Package manager (npm/yarn also work)

## Project Structure

```
frontend/
├── src/
│   ├── components/
│   │   ├── ui/          # Reusable UI components (Button, Input, Card, etc.)
│   │   ├── layout/      # Layout components (Sidebar, Header)
│   │   └── features/    # Feature-specific components (Accounts, Messages, etc.)
│   ├── hooks/           # Custom React hooks
│   ├── services/        # API service layer
│   ├── types/           # TypeScript type definitions
│   ├── styles/          # Global styles
│   ├── App.tsx          # Main App component
│   └── main.tsx         # Entry point
├── public/              # Static assets
├── index.html           # HTML template
├── vite.config.ts       # Vite configuration
├── tsconfig.json        # TypeScript configuration
└── package.json         # Dependencies
```

## Getting Started

### Prerequisites

- Node.js 18+ or Bun 1.0+
- Webmail Engine backend running

### Installation

```bash
# Using Bun (recommended)
cd frontend
bun install

# Or using npm
npm install
```

### Development

1. Start the Vite dev server:

```bash
# Using Bun
bun run dev

# Or using npm
npm run dev
```

2. Start the Go backend in development mode:

```bash
cd ..
go run ./cmd/main.go -dev -config config.json
```

The frontend will be available at `http://localhost:5173` and will proxy API requests to the backend.

### Production Build

```bash
# Using Bun
bun run build

# Or using npm
npm run build
```

This creates an optimized build in the `dist/` directory, which is embedded into the Go binary.

### Preview Production Build

```bash
bun run preview
```

## Configuration

Create a `.env` file based on `.env.example`:

```env
VITE_API_BASE_URL=http://localhost:8080
```

The API URL can also be changed in the Settings page of the application.

## Features

- **Accounts Management**: Add, view, and delete email accounts
- **Message Viewing**: Browse messages in inbox folders
- **Compose Email**: Send new emails with HTML or plain text
- **System Health**: Monitor system and account status
- **Settings**: Configure API connection

## Component Architecture

### UI Components (`src/components/ui/`)

Reusable, presentational components:
- `Button` - Action buttons with variants
- `Input` - Form input fields
- `Card` - Content containers
- `StatusBadge` - Status indicators

### Layout Components (`src/components/layout/`)

Structural components:
- `Sidebar` - Navigation sidebar
- `Header` - Page header with API URL input

### Feature Components (`src/components/features/`)

Page-level components:
- `AccountsView` - Account management
- `MessagesView` - Message listing
- `ComposeView` - Email composition
- `HealthView` - System health monitoring
- `SettingsView` - Application settings

## API Integration

The `src/services/api.ts` module provides typed API methods for all backend endpoints. Types are defined in `src/types/index.ts`.

## Styling

Components use CSS modules for scoped styling. Global styles are in `src/styles/index.css`.

Theme variables are defined as CSS custom properties for consistent theming.
