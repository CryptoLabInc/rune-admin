import type { TTeamMemberRole, TTeamMemberStatus } from "@/types/teamTypes";

/** A row of GET /users (cross-team user list, SC-11). */
export type TUserListItem = {
  userId: string;
  account: string;
  status: TTeamMemberStatus;
  memberships: { teamId: string; teamName: string; role: TTeamMemberRole }[];
  lastAccessAt: string | null;
  lastInvitedAt: string | null;
  sessionExpiredAt: string | null;
};

/** GET /users/stats — the sidebar Members badge count. */
export type TUsersStats = { invitePending: number };

/** GET /users query params (status/teamId "all" are omitted from the URL). */
export type TUsersQueryParams = {
  search?: string;
  status?: string;
  teamId?: string;
  sort?: string;
  page: number;
  size: number;
};

/** One team/role pair staged in the invite modal (SC-12 no.2). */
export type TInviteSet = {
  teamId: string;
  role: string;
};

/** Invite request body (SC-12 [초대 전송]). */
export type TInvitePayload = {
  email: string;
  sets: TInviteSet[];
};

/**
 * Server verdict for an invite request — duplicate detection is
 * server-side only (SC-12 no.1, confirmed 2026-07-09).
 */
export type TInviteResult = "success" | "duplicate-account" | "error";

/** One membership scheduled for removal (SC-14 target list). */
export type TMembershipRemoveTarget = {
  account: string;
  teamId: string;
  teamName: string;
  role: string;
};

/** One account section of the delete confirm (SC-15 — D20). */
export type TMemberDeleteTarget = {
  account: string;
  memberships: { teamName: string; role: string }[];
};

/** One staged role change shown in the confirm modal (SC-06 E / SC-13). */
export type TRoleChange = {
  /** First-column value — account (SC-06) or team name (SC-13). */
  label: string;
  from: string;
  to: string;
};

/** Response to POST /invitations (invite creation). */
export type TInvitationResponse = {
  userId: string;
  account: string;
  status: TTeamMemberStatus;
  codeSent: boolean;
};

/** One row of GET /invitations?view=history (SC-16). */
export type TInvitationHistoryRow = {
  account: string;
  issuedAt: string;
  lastAccessAt: string | null;
};
