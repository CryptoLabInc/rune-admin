import type { ReactNode } from "react";

import { cn } from "@/utils/cn";

const styles = {
  frame:
    "overflow-hidden rounded border bg-[color:color-mix(in_srgb,color-mix(in_srgb,var(--color-panel-solid)_60%,var(--color-well))_70%,transparent)]",
  scroll:
    "overflow-x-auto overscroll-x-contain [scrollbar-width:thin] [scrollbar-color:color-mix(in_srgb,var(--color-mint)_32%,transparent)_color-mix(in_srgb,var(--color-muted-foreground)_8%,transparent)]",
  table: "w-full min-w-[830px] border-collapse",
};

interface TableProps {
  toolbar?: ReactNode;
  foot?: ReactNode;
  /** Drop the 830px minimum — the table always fits its container and
      never scrolls; columns must handle the squeeze (truncation). */
  fluid?: boolean;
  /** Extra classes for the scroll area around the <table> — used to pin
      a fixed page height (min-h-*) so short pages, empty states, and
      loading keep the same frame height as a full page. */
  scrollClassName?: string;
  children: ReactNode;
  className?: string;
}

/**
 * Table is the framed data-table shell (ported from UIKIT AdminTable):
 * hairline frame + horizontal scroll area around the <table>, with the
 * toolbar/foot slots kept outside the scroll area. A minimum table
 * width prevents column collapse; the frame scrolls instead — unless
 * `fluid` opts out for tables narrow enough to always fit.
 */
const Table = ({
  toolbar,
  foot,
  fluid = false,
  scrollClassName,
  children,
  className,
}: TableProps) => {
  return (
    <div className={cn(styles.frame, className)}>
      {toolbar}
      <div className={cn(styles.scroll, scrollClassName)}>
        <table className={cn(styles.table, fluid && "min-w-0")}>
          {children}
        </table>
      </div>
      {foot}
    </div>
  );
};

export default Table;
