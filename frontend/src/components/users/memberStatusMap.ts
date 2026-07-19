import type { TMemberStatus } from "@/types/commonTypes";
import type { TTeamMemberStatus } from "@/types/teamTypes";

/** API wire status (snake_case) → MemberStatus chip state (kebab-case). */
export const CHIP_STATUS: Record<TTeamMemberStatus, TMemberStatus> = {
  online: "online",
  invite_redeemed: "redeemed",
  invite_pending: "pending",
  invite_expired: "invite-expired",
  session_expired: "session-expired",
};
