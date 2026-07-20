import type { ReactNode } from "react";

import { cn } from "@/utils/cn";

interface TableHeadProps {
  children: ReactNode;
  className?: string;
}

/**
 * TableHead is the header-row wrapper (thead > tr) — compose it with
 * TableHeaderCell children.
 */
const TableHead = ({ children, className }: TableHeadProps) => {
  return (
    <thead>
      <tr className={cn(className)}>{children}</tr>
    </thead>
  );
};

export default TableHead;
