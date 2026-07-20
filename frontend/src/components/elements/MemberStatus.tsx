import StatusBadge from "@/components/elements/StatusBadge";
import { MEMBER_STATUS_VAR } from "@/constants/styleConstants";
import type { TMemberStatus } from "@/types/commonTypes";

interface MemberStatusProps {
  status: TMemberStatus;
  className?: string;
}

/**
 * MemberStatus is the member session-status chip (dot + Korean label).
 * A thin wrapper over StatusBadge keyed by the session status, so it
 * shares one badge style with the invitation-status chip.
 */
const MemberStatus = ({ status, className }: MemberStatusProps) => {
  return (
    <StatusBadge
      label={MEMBER_STATUS_VAR[status].label}
      color={MEMBER_STATUS_VAR[status].color}
      className={className}
    />
  );
};

export default MemberStatus;
