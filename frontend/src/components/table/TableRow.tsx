import type { ReactNode } from "react";

import { cn } from "@/utils/cn";

const styles = {
  default: "transition-[background-color] duration-[160ms]",
  hover: "hover:bg-muted-foreground/[3%]",
  selected: "bg-mint/[3%]",
  changed: "shadow-[inset_2px_0] shadow-accent-blue",
};

interface TableRowProps {
  selected?: boolean;
  changed?: boolean;
  /** false for read-only tables (e.g. session history) — no mouseover feedback */
  hoverable?: boolean;
  /** Whole-row click target (SC-11 no.8 — opens the member drawer).
      Interactive cells (checkboxes) must stop propagation themselves. */
  onClick?: () => void;
  children: ReactNode;
  className?: string;
}

/**
 * TableRow is a body row with the contract states: muted hover wash by
 * default, mint wash while selected (replaces hover), and a 2px blue
 * left accent while the row holds an unsaved role change.
 */
const TableRow = ({
  selected = false,
  changed = false,
  hoverable = true,
  onClick,
  children,
  className,
}: TableRowProps) => {
  return (
    <tr
      className={cn(
        styles.default,
        selected ? styles.selected : hoverable && styles.hover,
        changed && styles.changed,
        onClick && "cursor-pointer",
        className,
      )}
      onClick={onClick}
    >
      {children}
    </tr>
  );
};

export default TableRow;
