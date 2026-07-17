import { cn } from "@/utils/cn";
import { MEMBER_STATUS_VAR } from "@/constants/styleConstants";
import type { TMemberStatus } from "@/types/commonTypes";

interface MemberStatusProps {
  status: TMemberStatus;
  className?: string;
}

/**
 * MemberStatus is the member connection status chip (dot + Korean
 * label), ported from UIKIT MemberStatus.
 */
const MemberStatus = ({ status, className }: MemberStatusProps) => {
  return (
    <span
      className={cn(
        "inline-flex h-8 w-fit cursor-pointer items-center gap-2 p-1 text-sm whitespace-nowrap",
        MEMBER_STATUS_VAR[status].color,
        className,
      )}
    >
      <span aria-hidden="true" className="size-1 rounded-full bg-current" />
      {MEMBER_STATUS_VAR[status].label}
    </span>
  );
};

export default MemberStatus;
