import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

/**
 * Utility function to merge Tailwind CSS classes
 * Handles conflicts and deduplicates classes
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
