import { cn } from "@/utils/cn";
import { STORAGE_STATUS_VAR } from "@/constants/styleConstants";
import type { TStorageStatus } from "@/types/commonTypes";

interface StorageStatusProps {
  status: TStorageStatus;
  /** When set the pill becomes a button (navbar badge → SC-02 modal). */
  onClick?: () => void;
  className?: string;
}

/**
 * StorageStatus is the rune storage lifecycle pill (mono EN label,
 * system voice), ported from UIKIT StorageStatus. Display-only by
 * default; pass onClick to render it as an interactive button.
 */
const StorageStatus = ({ status, onClick, className }: StorageStatusProps) => {
  const classes = cn(
    "text-tag inline-flex h-7 w-fit cursor-pointer items-center rounded-full border border-current px-2 font-mono tracking-[0.06em] whitespace-nowrap",
    STORAGE_STATUS_VAR[status],
    className,
  );

  if (onClick) {
    return (
      <button type="button" className={classes} onClick={onClick}>
        {status}
      </button>
    );
  }

  return <span className={classes}>{status}</span>;
};

export default StorageStatus;
