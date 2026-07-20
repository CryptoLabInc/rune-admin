export type TTeamNode = {
  id: string;
  name: string;
  parentId: string | null;
  childrenIds: string[];
  childCount: number;
  memberCount: number;
};

export type TTeamTree = TTeamNode[];

/** Grantable member role (Admin is console-account only — API §0). */
export type TTeamMemberRole = "edit" | "write" | "read";

/** Invitation-code lifecycle status on the wire (common contract). */
export type TInvitationStatus =
  "invite_pending" | "invite_expired" | "invite_redeemed";

/** Session-token liveness on the wire (common contract). */
export type TSessionStatus = "online" | "offline";

/** GET /teams/{id} detail. */
export type TTeamDetail = {
  id: string;
  name: string;
  parentId: string | null;
  children: string[];
  memberCount: number;
  createdAt: string;
};

/** A row of GET /teams/{id}/members. */
export type TTeamMember = {
  userId: string;
  account: string;
  /** Display name (not an identifier — account stays unique, API 2026-07-20). */
  username: string;
  role: TTeamMemberRole;
  invitationStatus: TInvitationStatus;
  sessionStatus: TSessionStatus;
  /**
   * Granted-at of the stored membership. Null for an inherited-read row —
   * the member reaches this team by downward inheritance from an ancestor,
   * so there is no stored grant and no join timestamp (API memberDTO).
   */
  joinedAt: string | null;
};

/** Paginated list envelope (common contract). */
export type TPage<T> = {
  total: number;
  page: number;
  size: number;
  items: T[];
};

/** Batch endpoint result (partial success — API §0). */
export type TBatchResult = {
  succeeded: string[];
  failed: { id: string; code: string; message: string }[];
};
