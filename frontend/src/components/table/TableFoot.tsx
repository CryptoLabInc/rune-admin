import type { ReactNode } from "react";

import { cn } from "@/utils/cn";

const styles = {
  wrap: "flex flex-wrap flex-col items-center justify-between gap-3 border-t px-3 py-3",
  info: "text-xs text-faint",
};

interface TableFootProps {
  info?: string;
  children?: ReactNode;
  className?: string;
}

/**
 * TableFoot is the strip below a table: a caption-role info text on
 * the left ("총 N명 · 10명/페이지", or the session-history disclaimer)
 * and pagination on the right.
 */
const TableFoot = ({ info, children, className }: TableFootProps) => {
  return (
    <div className={cn(styles.wrap, className)}>
      {info && <span className={styles.info}>{info}</span>}
      {children}
    </div>
  );
};

export default TableFoot;
