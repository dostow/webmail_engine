#!/usr/bin/env python3
"""
Setup script to create Vite + React + TypeScript frontend structure
"""

import os
import json

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
FRONTEND_DIR = os.path.join(BASE_DIR, 'frontend')

def create_dir(path):
    """Create directory if it doesn't exist"""
    os.makedirs(path, exist_ok=True)
    print(f"Created directory: {path}")

def write_file(path, content):
    """Write content to file"""
    with open(path, 'w', encoding='utf-8') as f:
        f.write(content)
    print(f"Created file: {path}")

def main():
    # Create directory structure
    dirs = [
        FRONTEND_DIR,
        os.path.join(FRONTEND_DIR, 'src'),
        os.path.join(FRONTEND_DIR, 'src', 'components'),
        os.path.join(FRONTEND_DIR, 'src', 'components', 'ui'),
        os.path.join(FRONTEND_DIR, 'src', 'components', 'layout'),
        os.path.join(FRONTEND_DIR, 'src', 'components', 'features'),
        os.path.join(FRONTEND_DIR, 'src', 'hooks'),
        os.path.join(FRONTEND_DIR, 'src', 'services'),
        os.path.join(FRONTEND_DIR, 'src', 'types'),
        os.path.join(FRONTEND_DIR, 'src', 'styles'),
        os.path.join(FRONTEND_DIR, 'public'),
    ]

    for d in dirs:
        create_dir(d)

    # Create package.json
    package_json = {
        "name": "webmail-engine-frontend",
        "private": True,
        "version": "1.0.0",
        "type": "module",
        "scripts": {
            "dev": "vite",
            "build": "tsc && vite build",
            "preview": "vite preview",
            "lint": "eslint . --ext ts,tsx --report-unused-disable-directives --max-warnings 0"
        },
        "dependencies": {
            "react": "^19.0.0",
            "react-dom": "^19.0.0"
        },
        "devDependencies": {
            "@types/react": "^19.0.0",
            "@types/react-dom": "^19.0.0",
            "@typescript-eslint/eslint-plugin": "^8.0.0",
            "@typescript-eslint/parser": "^8.0.0",
            "@vitejs/plugin-react": "^4.3.0",
            "eslint": "^9.0.0",
            "eslint-plugin-react-hooks": "^5.0.0",
            "eslint-plugin-react-refresh": "^0.4.5",
            "typescript": "^5.6.0",
            "vite": "^6.0.0"
        }
    }
    write_file(os.path.join(FRONTEND_DIR, 'package.json'), json.dumps(package_json, indent=2))

    # Create bunfig.toml for bun compatibility
    bunfig = '''[install]
# Use exact versions for lockfile
exact = true
'''
    write_file(os.path.join(FRONTEND_DIR, 'bunfig.toml'), bunfig)

    # Create vite.config.ts
    vite_config = '''import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ['react', 'react-dom'],
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
  },
})
'''
    write_file(os.path.join(FRONTEND_DIR, 'vite.config.ts'), vite_config)

    # Create tsconfig.json
    tsconfig = '''{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,

    /* Bundler mode */
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true,
    "jsx": "react-jsx",

    /* Linting */
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,

    /* Path aliases */
    "baseUrl": ".",
    "paths": {
      "@/*": ["./src/*"]
    }
  },
  "include": ["src"],
  "references": [{ "path": "./tsconfig.node.json" }]
}
'''
    write_file(os.path.join(FRONTEND_DIR, 'tsconfig.json'), tsconfig)

    # Create tsconfig.node.json
    tsconfig_node = '''{
  "compilerOptions": {
    "composite": true,
    "skipLibCheck": true,
    "module": "ESNext",
    "moduleResolution": "bundler",
    "allowSyntheticDefaultImports": true
  },
  "include": ["vite.config.ts"]
}
'''
    write_file(os.path.join(FRONTEND_DIR, 'tsconfig.node.json'), tsconfig_node)

    # Create index.html
    index_html = '''<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <link rel="icon" type="image/svg+xml" href="/vite.svg" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Webmail Engine</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
'''
    write_file(os.path.join(FRONTEND_DIR, 'index.html'), index_html)

    # Create .env.example
    env_example = '''# Vite Configuration
VITE_API_BASE_URL=http://localhost:8080
'''
    write_file(os.path.join(FRONTEND_DIR, '.env.example'), env_example)

    # Create .gitignore
    gitignore = '''# Logs
logs
*.log
npm-debug.log*
yarn-debug.log*
yarn-error.log*
pnpm-debug.log*
lerna-debug.log*

node_modules
dist
dist-ssr
*.local

# Editor directories and files
.vscode/*
!.vscode/extensions.json
.idea
.DS_Store
*.suo
*.ntvs*
*.njsproj
*.sln
*.sw?
'''
    write_file(os.path.join(FRONTEND_DIR, '.gitignore'), gitignore)

    print("\n✅ Frontend structure created successfully!")
    print(f"\nNext steps:")
    print(f"1. cd {FRONTEND_DIR}")
    print(f"2. npm install")
    print(f"3. npm run dev")

if __name__ == '__main__':
    main()
