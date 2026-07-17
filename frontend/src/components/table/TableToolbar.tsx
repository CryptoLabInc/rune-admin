import type { ReactNode } from "react";

import Badge from "@/components/elements/Badge";
import { cn } from "@/utils/cn";

const styles = {
  wrap: "flex flex-wrap items-center justify-between gap-x-5 gap-y-3 px-4 py-4",
  title: "text-md font-semibold",
  selected:
    "text-tag ml-2 inline-flex items-center gap-1.5 font-mono tracking-[0.08em] text-muted-foreground",
  actions: "flex flex-wrap items-center gap-2",
};

interface TableToolbarProps {
  title: string;
  count?: number;
  selectedCount?: number;
  children?: ReactNode;
  className?: string;
}

/**
 * TableToolbar is the strip above a table: title (h3 role) with a
 * neutral count badge, a "N SELECTED" indicator while any row is
 * selected, and a right-side slot for search/filters/bulk actions.
 */
const TableToolbar = ({
  title,
  count,
  selectedCount = 0,
  children,
  className,
}: TableToolbarProps) => {
  return (
    <div className={cn(styles.wrap, className)}>
      <div className="flex items-center gap-2">
        <b className={styles.title}>{title}</b>
        {count !== undefined && (
          <Badge value={count} max={999} tone="neutral" />
        )}
        {selectedCount > 0 && (
          <span className={styles.selected}>{selectedCount} SELECTED</span>
        )}
      </div>
      {children && <div className={styles.actions}>{children}</div>}
    </div>
  );
};

export default TableToolbar;
