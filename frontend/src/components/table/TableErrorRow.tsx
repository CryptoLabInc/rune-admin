import { cn } from "@/utils/cn";

const styles = {
  cell: "text-xs bg-negative/[3%] text-negative border-t px-3 pt-[6px] pb-2",
  icon: "text-tag mr-1.5 inline-flex size-4 items-center justify-center rounded-full border border-current align-middle font-mono",
};

interface TableErrorRowProps {
  message: string;
  colSpan: number;
  className?: string;
}

/**
 * TableErrorRow renders a row-level failure line inserted under the
 * failed row: red message on a faint red wash spanning the full table
 * width.
 */
const TableErrorRow = ({ message, colSpan, className }: TableErrorRowProps) => {
  return (
    <tr>
      <td colSpan={colSpan} role="alert" className={cn(styles.cell, className)}>
        <span aria-hidden="true" className={styles.icon}>
          !
        </span>
        {message}
      </td>
    </tr>
  );
};

export default TableErrorRow;
