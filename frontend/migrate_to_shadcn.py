#!/usr/bin/env python3
"""
Frontend Migration Script to shadcn/ui
======================================

This script guides you through migrating the email client frontend to use shadcn/ui components.

Usage:
    python migrate_to_shadcn.py [--dry-run]

Options:
    --dry-run    Show what would be done without making changes
"""

import os
import sys
import json
import subprocess
import shutil
from pathlib import Path
from typing import Optional

# Colors for terminal output
class Colors:
    HEADER = '\033[95m'
    OKBLUE = '\033[94m'
    OKCYAN = '\033[96m'
    OKGREEN = '\033[92m'
    WARNING = '\033[93m'
    FAIL = '\033[91m'
    ENDC = '\033[0m'
    BOLD = '\033[1m'
    UNDERLINE = '\033[4m'

def print_header(text: str):
    print(f"\n{Colors.HEADER}{Colors.BOLD}{'=' * 60}{Colors.ENDC}")
    print(f"{Colors.HEADER}{Colors.BOLD}{text.center(60)}{Colors.ENDC}")
    print(f"{Colors.HEADER}{Colors.BOLD}{'=' * 60}{Colors.ENDC}\n")

def print_step(step: int, text: str):
    print(f"\n{Colors.OKBLUE}{Colors.BOLD}Step {step}:{Colors.ENDC} {text}")

def print_success(text: str):
    print(f"{Colors.OKGREEN}✓ {text}{Colors.ENDC}")

def print_warning(text: str):
    print(f"{Colors.WARNING}⚠ {text}{Colors.ENDC}")

def print_error(text: str):
    print(f"{Colors.FAIL}✗ {text}{Colors.ENDC}")

def print_info(text: str):
    print(f"{Colors.OKCYAN}ℹ {text}{Colors.ENDC}")


class FrontendMigrator:
    def __init__(self, frontend_dir: str, dry_run: bool = False):
        self.frontend_dir = Path(frontend_dir)
        self.dry_run = dry_run
        self.package_json_path = self.frontend_dir / "package.json"

    def check_prerequisites(self) -> bool:
        """Check if Bun is available"""
        print_header("Checking Prerequisites")

        try:
            result = subprocess.run(["bun", "--version"], capture_output=True, text=True, check=True)
            print_success(f"Bun found: {result.stdout.strip()}")
        except (subprocess.CalledProcessError, FileNotFoundError):
            print_error("Bun not found. Please install Bun first: https://bun.sh")
            return False

        if not self.package_json_path.exists():
            print_error(f"package.json not found at {self.package_json_path}")
            return False

        print_success("package.json found")
        return True

    def read_package_json(self) -> dict:
        """Read current package.json"""
        with open(self.package_json_path, 'r') as f:
            return json.load(f)

    def update_package_json(self, package_data: dict):
        """Update package.json with new dependencies"""
        print_step(1, "Updating package.json")

        if self.dry_run:
            print_info("[DRY RUN] Would update package.json with:")
            print(json.dumps(package_data, indent=2))
            return

        with open(self.package_json_path, 'w') as f:
            json.dump(package_data, f, indent=2)
            f.write('\n')

        print_success("package.json updated")

    def add_dependencies(self):
        """Add required dependencies to package.json"""
        print_step(2, "Adding shadcn/ui and Tailwind CSS dependencies")

        package_data = self.read_package_json()

        # Dependencies to add
        new_deps = {
            "@radix-ui/react-slot": "^1.1.0",
            "@radix-ui/react-dialog": "^1.1.4",
            "@radix-ui/react-dropdown-menu": "^2.1.4",
            "@radix-ui/react-label": "^2.1.1",
            "@radix-ui/react-select": "^2.1.4",
            "@radix-ui/react-tabs": "^1.1.2",
            "@radix-ui/react-toast": "^1.2.4",
            "@radix-ui/react-tooltip": "^1.1.6",
            "@radix-ui/react-avatar": "^1.1.2",
            "@radix-ui/react-popover": "^1.1.4",
            "@radix-ui/react-scroll-area": "^1.2.2",
            "@radix-ui/react-separator": "^1.1.1",
            "class-variance-authority": "^0.7.1",
            "clsx": "^2.1.1",
            "tailwind-merge": "^2.6.0",
            "lucide-react": "^0.468.0",
            "tailwindcss-animate": "^1.0.7",
            "date-fns": "^4.1.0",
            "zustand": "^5.0.3"
        }

        # Dev dependencies to add
        new_dev_deps = {
            "tailwindcss": "^3.4.17",
            "postcss": "^8.4.49",
            "autoprefixer": "^10.4.20"
        }

        # Merge dependencies
        if "dependencies" not in package_data:
            package_data["dependencies"] = {}
        if "devDependencies" not in package_data:
            package_data["devDependencies"] = {}

        package_data["dependencies"].update(new_deps)
        package_data["devDependencies"].update(new_dev_deps)

        # Sort dependencies alphabetically
        package_data["dependencies"] = dict(sorted(package_data["dependencies"].items()))
        package_data["devDependencies"] = dict(sorted(package_data["devDependencies"].items()))

        self.update_package_json(package_data)
        print_success("Dependencies added to package.json")

    def create_tailwind_config(self):
        """Create Tailwind CSS configuration"""
        print_step(3, "Creating Tailwind CSS configuration")

        tailwind_config = """/** @type {import('tailwindcss').Config} */
export default {
  darkMode: ["class"],
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    container: {
      center: true,
      padding: "2rem",
      screens: {
        "2xl": "1400px",
      },
    },
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
      keyframes: {
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
      },
      animation: {
        "accordion-down": "accordion-down 0.2s ease-out",
        "accordion-up": "accordion-up 0.2s ease-out",
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
}
"""

        if self.dry_run:
            print_info("[DRY RUN] Would create tailwind.config.js")
            return

        config_path = self.frontend_dir / "tailwind.config.js"
        with open(config_path, 'w') as f:
            f.write(tailwind_config)

        print_success("tailwind.config.js created")

    def create_postcss_config(self):
        """Create PostCSS configuration"""
        print_step(4, "Creating PostCSS configuration")

        postcss_config = """export default {
  plugins: {
    tailwindcss: {},
    autoprefixer: {},
  },
}
"""

        if self.dry_run:
            print_info("[DRY RUN] Would create postcss.config.js")
            return

        config_path = self.frontend_dir / "postcss.config.js"
        with open(config_path, 'w') as f:
            f.write(postcss_config)

        print_success("postcss.config.js created")

    def update_global_styles(self):
        """Update global CSS file with Tailwind directives"""
        print_step(5, "Updating global styles")

        styles_path = self.frontend_dir / "src" / "styles" / "index.css"

        new_styles = """@tailwind base;
@tailwind components;
@tailwind utilities;

@layer base {
  :root {
    --background: 222.2 84% 4.9%;
    --foreground: 210 40% 98%;
    --card: 222.2 84% 4.9%;
    --card-foreground: 210 40% 98%;
    --popover: 222.2 84% 4.9%;
    --popover-foreground: 210 40% 98%;
    --primary: 217.2 91.2% 59.8%;
    --primary-foreground: 222.2 47.4% 11.2%;
    --secondary: 217.2 32.6% 17.5%;
    --secondary-foreground: 210 40% 98%;
    --muted: 217.2 32.6% 17.5%;
    --muted-foreground: 215 20.2% 65.1%;
    --accent: 217.2 32.6% 17.5%;
    --accent-foreground: 210 40% 98%;
    --destructive: 0 62.8% 30.6%;
    --destructive-foreground: 210 40% 98%;
    --border: 217.2 32.6% 17.5%;
    --input: 217.2 32.6% 17.5%;
    --ring: 224.3 76.3% 48%;
    --radius: 0.5rem;
  }
}

@layer base {
  * {
    @apply border-border;
  }
  body {
    @apply bg-background text-foreground;
  }
}

/* Legacy CSS custom properties - keep for backward compatibility */
:root {
  --color-primary: hsl(217.2 91.2% 59.8%);
  --color-secondary: hsl(217.2 32.6% 17.5%);
  --color-success: hsl(142.1 76.2% 36.3%);
  --color-error: hsl(0 84.2% 60.2%);
  --color-warning: hsl(38 92% 50%);
  --color-info: hsl(217.2 91.2% 59.8%);

  --font-sans: system-ui, -apple-system, sans-serif;
  --font-mono: ui-monospace, monospace;

  --spacing-xs: 0.25rem;
  --spacing-sm: 0.5rem;
  --spacing-md: 1rem;
  --spacing-lg: 1.5rem;
  --spacing-xl: 2rem;

  --radius-sm: 0.25rem;
  --radius-md: 0.5rem;
  --radius-lg: 0.75rem;

  --shadow-sm: 0 1px 2px 0 rgb(0 0 0 / 0.05);
  --shadow-md: 0 4px 6px -1px rgb(0 0 0 / 0.1);
  --shadow-lg: 0 10px 15px -3px rgb(0 0 0 / 0.1);
}

* {
  box-sizing: border-box;
  margin: 0;
  padding: 0;
}

html,
body,
#root {
  height: 100%;
  width: 100%;
}

body {
  font-family: var(--font-sans);
  background-color: hsl(var(--background));
  color: hsl(var(--foreground));
  line-height: 1.5;
}
"""

        if self.dry_run:
            print_info("[DRY RUN] Would update src/styles/index.css")
            return

        with open(styles_path, 'w') as f:
            f.write(new_styles)

        print_success("Global styles updated")

    def create_utils(self):
        """Create utility files"""
        print_step(6, "Creating utility files")

        utils_dir = self.frontend_dir / "src" / "utils"
        utils_dir.mkdir(exist_ok=True)

        # cn.ts - className utility
        cn_ts = """import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

/**
 * Utility function to merge Tailwind CSS classes
 * Handles conflicts and deduplicates classes
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
"""

        # format.ts - Date and number formatters
        format_ts = """import { format, formatDistanceToNow, isToday, isYesterday, isThisYear } from "date-fns"

/**
 * Format a date for display in message lists
 * - Today: "10:30 AM"
 * - Yesterday: "Yesterday"
 * - This year: "Mar 14"
 * - Older: "Mar 14, 2024"
 */
export function formatMessageDate(date: Date | string): string {
  const d = typeof date === "string" ? new Date(date) : date

  if (isToday(d)) {
    return format(d, "h:mm a")
  }

  if (isYesterday(d)) {
    return "Yesterday"
  }

  if (isThisYear(d)) {
    return format(d, "MMM d")
  }

  return format(d, "MMM d, yyyy")
}

/**
 * Format a date for display in message detail header
 */
export function formatFullDate(date: Date | string): string {
  const d = typeof date === "string" ? new Date(date) : date
  return format(d, "EEEE, MMMM d, yyyy 'at' h:mm a")
}

/**
 * Format relative time (e.g., "2 hours ago")
 */
export function formatRelativeTime(date: Date | string): string {
  const d = typeof date === "string" ? new Date(date) : date
  return formatDistanceToNow(d, { addSuffix: true })
}

/**
 * Format email address with name
 */
export function formatEmailContact(name: string | null | undefined, email: string): string {
  if (!name || name === email) {
    return email
  }
  return `${name} <${email}>`
}

/**
 * Truncate text to specified length
 */
export function truncate(text: string, length: number, suffix: string = "..."): string {
  if (text.length <= length) return text
  return text.slice(0, length) + suffix
}
"""

        if self.dry_run:
            print_info("[DRY RUN] Would create src/utils/cn.ts and src/utils/format.ts")
            return

        (utils_dir / "cn.ts").write_text(cn_ts)
        (utils_dir / "format.ts").write_text(format_ts)

        # Create utils index
        index_ts = """export * from "./cn"
export * from "./format"
"""
        (utils_dir / "index.ts").write_text(index_ts)

        print_success("Utility files created")

    def create_store(self):
        """Create Zustand store"""
        print_step(7, "Creating global state store")

        store_dir = self.frontend_dir / "src" / "store"
        store_dir.mkdir(exist_ok=True)

        use_app_store_ts = """import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export interface Account {
  id: string
  email: string
  status: string
  connectionLimit: number
  createdAt: string
  updatedAt: string
}

export interface Message {
  uid: string
  subject: string
  from: Array<{ name: string; address: string }>
  to: Array<{ name: string; address: string }>
  date: string
  flags: string[]
  size: number
}

interface AppState {
  // Account state
  accounts: Account[]
  selectedAccountId: string | null
  setSelectedAccountId: (id: string | null) => void
  setAccounts: (accounts: Account[]) => void
  addAccount: (account: Account) => void
  removeAccount: (id: string) => void

  // Message state
  selectedMessage: Message | null
  setSelectedMessage: (message: Message | null) => void
  currentFolder: string
  setCurrentFolder: (folder: string) => void

  // UI state
  sidebarCollapsed: boolean
  setSidebarCollapsed: (collapsed: boolean) => void
  theme: 'dark' | 'light'
  setTheme: (theme: 'dark' | 'light') => void

  // API configuration
  apiUrl: string
  setApiUrl: (url: string) => void
}

export const useAppStore = create<AppState>()(
  persist(
    (set) => ({
      // Account state
      accounts: [],
      selectedAccountId: null,
      setSelectedAccountId: (id) => set({ selectedAccountId: id }),
      setAccounts: (accounts) => set({ accounts }),
      addAccount: (account) =>
        set((state) => ({ accounts: [...state.accounts, account] })),
      removeAccount: (id) =>
        set((state) => ({ accounts: state.accounts.filter((a) => a.id !== id) })),

      // Message state
      selectedMessage: null,
      setSelectedMessage: (message) => set({ selectedMessage: message }),
      currentFolder: 'INBOX',
      setCurrentFolder: (folder) => set({ currentFolder: folder }),

      // UI state
      sidebarCollapsed: false,
      setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
      theme: 'dark',
      setTheme: (theme) => set({ theme }),

      // API configuration
      apiUrl: 'http://localhost:8080',
      setApiUrl: (url) => set({ apiUrl: url }),
    }),
    {
      name: 'webmail-storage',
      partialize: (state) => ({
        apiUrl: state.apiUrl,
        theme: state.theme,
        sidebarCollapsed: state.sidebarCollapsed,
      }),
    }
  )
)
"""

        if self.dry_run:
            print_info("[DRY RUN] Would create src/store/useAppStore.ts")
            return

        (store_dir / "useAppStore.ts").write_text(use_app_store_ts)

        print_success("Global store created")

    def create_hooks(self):
        """Create custom React hooks"""
        print_step(8, "Creating custom hooks")

        hooks_dir = self.frontend_dir / "src" / "hooks"
        hooks_dir.mkdir(exist_ok=True)

        use_toast_ts = """import { useToast } from "@/components/ui/use-toast"

export function useEmailToast() {
  const { toast } = useToast()

  const showSuccess = (message: string) => {
    toast({
      title: "Success",
      description: message,
      variant: "default",
    })
  }

  const showError = (message: string) => {
    toast({
      title: "Error",
      description: message,
      variant: "destructive",
    })
  }

  const showInfo = (message: string) => {
    toast({
      title: "Info",
      description: message,
    })
  }

  return { showSuccess, showError, showInfo }
}
"""

        use_accounts_ts = """import { useState, useCallback } from 'react'
import api from '@/services/api'
import { useAppStore } from '@/store/useAppStore'
import { useEmailToast } from '@/hooks/useToast'

export function useAccounts() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { accounts, setAccounts, addAccount, removeAccount } = useAppStore()
  const { showSuccess, showError } = useEmailToast()

  const fetchAccounts = useCallback(async () => {
    try {
      setLoading(true)
      const data = await api.listAccounts()
      setAccounts(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load accounts')
      showError('Failed to load accounts')
    } finally {
      setLoading(false)
    }
  }, [setAccounts, showError])

  const createAccount = useCallback(async (requestData: any) => {
    try {
      setLoading(true)
      const account = await api.createAccount(requestData)
      addAccount(account)
      showSuccess('Account added successfully')
      return account
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to add account'
      showError(message)
      throw err
    } finally {
      setLoading(false)
    }
  }, [addAccount, showSuccess, showError])

  const deleteAccount = useCallback(async (id: string) => {
    try {
      setLoading(true)
      await api.deleteAccount(id)
      removeAccount(id)
      showSuccess('Account deleted successfully')
    } catch (err) {
      showError('Failed to delete account')
      throw err
    } finally {
      setLoading(false)
    }
  }, [removeAccount, showSuccess, showError])

  return {
    accounts,
    loading,
    error,
    fetchAccounts,
    createAccount,
    deleteAccount,
  }
}
"""

        use_messages_ts = """import { useState, useCallback } from 'react'
import api from '@/services/api'
import { useAppStore } from '@/store/useAppStore'
import { useEmailToast } from '@/hooks/useToast'

export interface MessageListResponse {
  messages: any[]
  folder: string
  cursor: string
  hasMore: boolean
  freshness: string
}

export function useMessages() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const selectedAccountId = useAppStore((state) => state.selectedAccountId)
  const { showError } = useEmailToast()

  const fetchMessages = useCallback(async (
    folder: string = 'INBOX',
    limit: number = 50,
    cursor: string = ''
  ): Promise<MessageListResponse | null> => {
    if (!selectedAccountId) {
      setError('No account selected')
      return null
    }

    try {
      setLoading(true)
      const data = await api.getMessages(selectedAccountId, folder, limit, cursor)
      setError(null)
      return data
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load messages'
      setError(message)
      showError(message)
      return null
    } finally {
      setLoading(false)
    }
  }, [selectedAccountId, showError])

  const fetchMessage = useCallback(async (
    uid: string,
    folder: string = 'INBOX'
  ): Promise<any | null> => {
    if (!selectedAccountId) {
      setError('No account selected')
      return null
    }

    try {
      setLoading(true)
      const data = await api.getMessage(selectedAccountId, uid, folder)
      setError(null)
      return data
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load message'
      setError(message)
      showError(message)
      return null
    } finally {
      setLoading(false)
    }
  }, [selectedAccountId, showError])

  return {
    loading,
    error,
    fetchMessages,
    fetchMessage,
  }
}
"""

        if self.dry_run:
            print_info("[DRY RUN] Would create hooks files")
            return

        (hooks_dir / "useToast.ts").write_text(use_toast_ts)
        (hooks_dir / "useAccounts.ts").write_text(use_accounts_ts)
        (hooks_dir / "useMessages.ts").write_text(use_messages_ts)

        # Create hooks index
        index_ts = """export * from './useAccounts'
export * from './useMessages'
export * from './useToast'
"""
        (hooks_dir / "index.ts").write_text(index_ts)

        print_success("Custom hooks created")

    def install_dependencies(self):
        """Run bun install"""
        print_step(9, "Installing dependencies")

        if self.dry_run:
            print_info("[DRY RUN] Would run: bun install")
            return

        print_info("Running bun install... This may take a minute.")
        try:
            subprocess.run(
                ["bun", "install"],
                cwd=self.frontend_dir,
                check=True,
                capture_output=False
            )
            print_success("Dependencies installed successfully")
        except subprocess.CalledProcessError as e:
            print_error(f"Failed to install dependencies: {e}")
            return False
        return True

    def print_next_steps(self):
        """Print next steps for the user"""
        print_header("Next Steps")

        print_info("""
The migration setup is complete! Here's what to do next:

1. Run the shadcn CLI to add components:

   cd frontend
   bunx --bun shadcn@latest init

   # Add the components you need:
   bunx --bun shadcn@latest add button card input label
   bunx --bun shadcn@latest add table dialog select tabs
   bunx --bun shadcn@latest add toast tooltip avatar popover
   bunx --bun shadcn@latest add scroll-area separator dropdown-menu
   bunx --bun shadcn@latest add skeleton form

2. Update your components to use shadcn/ui:

   - Replace custom Button with shadcn Button
   - Replace custom Card with shadcn Card
   - Replace custom Input with shadcn Input
   - Add shadcn Table to Messages.tsx

3. Create the MessageDetail component:

   See the improvement plan for details.

4. Update the router in App.tsx to add message detail view.

5. Test the application:

   bun run dev
""")

    def run(self):
        """Run the complete migration"""
        print_header("Frontend Migration to shadcn/ui")

        if self.dry_run:
            print_warning("DRY RUN MODE - No changes will be made\n")

        if not self.check_prerequisites():
            print_error("Prerequisites check failed. Exiting.")
            sys.exit(1)

        self.add_dependencies()
        self.create_tailwind_config()
        self.create_postcss_config()
        self.update_global_styles()
        self.create_utils()
        self.create_store()
        self.create_hooks()

        if not self.dry_run:
            if not self.install_dependencies():
                print_error("Dependency installation failed.")
                sys.exit(1)

        self.print_next_steps()

        print_header("Migration Complete!")


def main():
    import argparse

    parser = argparse.ArgumentParser(
        description="Migrate frontend to shadcn/ui"
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Show what would be done without making changes"
    )
    parser.add_argument(
        "--frontend-dir",
        default=".",
        help="Path to frontend directory (default: current directory)"
    )

    args = parser.parse_args()

    frontend_dir = Path(args.frontend_dir).absolute()
    if not frontend_dir.exists():
        print_error(f"Frontend directory not found: {frontend_dir}")
        sys.exit(1)

    migrator = FrontendMigrator(str(frontend_dir), dry_run=args.dry_run)
    migrator.run()


if __name__ == "__main__":
    main()
