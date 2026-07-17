import type { ReactNode } from "react";

import { cn } from "@/utils/cn";

const styles = {
  default:
    "text-tag bg-muted-foreground/[2%] px-3 py-2.5 text-left font-mono font-medium tracking-[0.08em] whitespace-nowrap text-faint",
};

interface TableHeaderCellProps {
  children?: ReactNode;
  className?: string;
}

/**
 * TableHeaderCell is a th in the table-header text role (mono tag,
 * faint). The select-all checkbox column narrows itself via
 * className="w-8 pr-1".
 */
const TableHeaderCell = ({ children, className }: TableHeaderCellProps) => {
  return (
    <th scope="col" className={cn(styles.default, className)}>
      {children}
    </th>
  );
};

export default TableHeaderCell;
