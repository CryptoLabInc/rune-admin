import type { ReactNode } from "react";

import { cn } from "@/utils/cn";

const styles = {
  table: "w-full border-collapse text-sm",
  th: "text-tag border bg-muted-foreground/[4%] px-3 py-2 text-left font-mono font-medium tracking-[0.08em] whitespace-nowrap text-faint",
  td: "text-muted-foreground border px-3 py-2 align-middle",
};

interface ModalTableProps {
  head: string[];
  rows: ReactNode[][];
  className?: string;
}

/**
 * ModalTable is the compact bordered table used inside confirm modals
 * (wireframe table.wft): SC-06 E role changes, SC-12 invite preview,
 * SC-14 removal targets, SC-15 delete targets. Full grid lines — the
 * page-level Table shell (row rules only) stays for data tables.
 */
const ModalTable = ({ head, rows, className }: ModalTableProps) => {
  return (
    <table className={cn(styles.table, className)}>
      <thead>
        <tr>
          {head.map((label) => (
            <th key={label} scope="col" className={styles.th}>
              {label}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((cells, rowIndex) => (
          <tr key={rowIndex}>
            {cells.map((cell, cellIndex) => (
              <td key={cellIndex} className={styles.td}>
                {cell}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
};

export default ModalTable;
