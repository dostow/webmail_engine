# Webmail Engine - Frontend

## New Vite-based Frontend

The frontend has been migrated to a modern React + TypeScript application built with Vite.

### Quick Start

**Development Mode:**

1. Start the Vite dev server:
   ```bash
   cd frontend
   bun install
   bun run dev
   ```

2. Start the Go backend with the `-dev` flag:
   ```bash
   go run ./cmd/main.go -dev -config config.json
   ```

3. Open http://localhost:5173 in your browser

**Production Mode:**

1. Build the frontend:
   ```bash
   cd frontend
   bun install
   bun run build
   ```

2. Run the Go backend (production mode embeds the built assets):
   ```bash
   go run ./cmd/main.go -config config.json
   ```

3. Open http://localhost:8080 in your browser

### Documentation

See [frontend/README.md](frontend/README.md) for detailed documentation.

### Old Frontend

The old `web/index.html` single-file frontend has been deprecated in favor of the new component-based architecture.
