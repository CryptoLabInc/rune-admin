import type { ReactNode } from "react";

import { cn } from "@/utils/cn";

const styles = {
  default:
    "text-muted-foreground border-t px-3 py-2 text-sm align-middle whitespace-nowrap",
};

interface TableCellProps {
  children?: ReactNode;
  className?: string;
}

/**
 * TableCell is a body td in the secondary text role. Cells never wrap;
 * free-length columns (team names, account emails) opt into truncation
 * per screen via className (max-w-* truncate) plus a title attribute
 * on the content.
 */
const TableCell = ({ children, className }: TableCellProps) => {
  return <td className={cn(styles.default, className)}>{children}</td>;
};

export default TableCell;
