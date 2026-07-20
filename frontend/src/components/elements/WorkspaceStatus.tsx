import { cn } from "@/utils/cn";
import { WORKSPACE_STATUS_VAR } from "@/constants/styleConstants";
import type { TWorkspaceStatus } from "@/types/commonTypes";

interface WorkspaceStatusProps {
  status: TWorkspaceStatus;
  /** When set the pill becomes a button (navbar badge → SC-02 modal). */
  onClick?: () => void;
  className?: string;
}

/**
 * WorkspaceStatus is the rune workspace lifecycle pill (한글 label, system
 * voice), ported from UIKIT StorageStatus. Display-only by default;
 * pass onClick to render it as an interactive button.
 */
const WorkspaceStatus = ({ status, onClick, className }: WorkspaceStatusProps) => {
  const classes = cn(
    "text-tag inline-flex h-[26px] w-fit cursor-pointer items-center rounded-full border border-current px-2 whitespace-nowrap",
    WORKSPACE_STATUS_VAR[status].color,
    className,
  );

  const label = <span>{WORKSPACE_STATUS_VAR[status].label}</span>;

  if (onClick) {
    return (
      <button type="button" className={classes} onClick={onClick}>
        {label}
      </button>
    );
  }

  return <span className={classes}>{label}</span>;
};

export default WorkspaceStatus;
