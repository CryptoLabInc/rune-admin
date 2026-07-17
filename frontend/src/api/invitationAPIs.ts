import { serviceFetch } from "@/api/createFetch";
import type { TTeamMemberRole } from "@/types/teamTypes";

/** postInvitation invites an account with team/role memberships
    (POST /invitations). */
export const postInvitation = async (body: {
  account: string;
  memberships: { teamId: string; role: TTeamMemberRole }[];
}) =>
  await serviceFetch("invitations", {
    method: "POST",
    body: JSON.stringify(body),
  });

/** resendInvitation issues a new invite code for one user
    (POST /invitations/resend). */
export const resendInvitation = async (userId: string) =>
  await serviceFetch("invitations/resend", {
    method: "POST",
    body: JSON.stringify({ userId }),
  });

/** cancelInvitation force-expires a pending invite code
    (POST /invitations/cancel). */
export const cancelInvitation = async (userId: string) =>
  await serviceFetch("invitations/cancel", {
    method: "POST",
    body: JSON.stringify({ userId }),
  });

/** getInvitationHistory lists per-issuance rows, paginated
    (GET /invitations?view=history). */
export const getInvitationHistory = async (
  sort: string,
  page: number,
  size: number,
) =>
  await serviceFetch(
    `invitations?view=history&sort=${encodeURIComponent(sort)}&page=${page}&size=${size}`,
  );
