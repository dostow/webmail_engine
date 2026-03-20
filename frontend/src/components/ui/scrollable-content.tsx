import { ScrollArea, ScrollBar } from './scroll-area';
import { cn } from '@/lib/utils';

interface ScrollableContentProps {
  children: React.ReactNode;
  className?: string;
  /**
   * Height strategy:
   * - 'flex': Use flex-1 min-h-0 (requires parent with constrained height)
   * - 'calc': Use h-[calc(100vh-offset)] for standalone containers
   * - 'fixed': Use a fixed height like h-[600px]
   */
  heightStrategy?: 'flex' | 'calc' | 'fixed';
  /** Offset in pixels for calc() strategy (default: 280) */
  offset?: number;
  /** Fixed height value for 'fixed' strategy (e.g., '600px') */
  fixedHeight?: string;
  /** Show horizontal scrollbar (default: false) */
  showHorizontalScrollbar?: boolean;
}

/**
 * Reusable scrollable content wrapper using Radix UI ScrollArea.
 * Ensures consistent scrolling behavior across all pages.
 */
export function ScrollableContent({
  children,
  className,
  heightStrategy = 'flex',
  offset = 280,
  fixedHeight,
  showHorizontalScrollbar = false,
}: ScrollableContentProps) {
  const heightClass =
    heightStrategy === 'flex'
      ? 'flex-1 min-h-0'
      : heightStrategy === 'calc'
        ? `h-[calc(100vh-${offset}px)]`
        : fixedHeight || 'h-[600px]';

  return (
    <ScrollArea className={cn(heightClass, className)}>
      {children}
      <ScrollBar orientation="vertical" />
      {showHorizontalScrollbar && <ScrollBar orientation="horizontal" />}
    </ScrollArea>
  );
}
