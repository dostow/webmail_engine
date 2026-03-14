import { format, formatDistanceToNow, isToday, isYesterday, isThisYear } from "date-fns"

/**
 * Format a date for display in message lists
 * - Today: "10:30 AM"
 * - Yesterday: "Yesterday"
 * - This year: "Mar 14, 10:30 AM"
 * - Older: "Mar 14, 2024, 10:30 AM"
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
    return format(d, "MMM d, h:mm a")
  }

  return format(d, "MMM d, yyyy, h:mm a")
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
