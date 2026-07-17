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

/** Member status on the wire (common contract). */
export type TTeamMemberStatus =
  "online" | "invite_pending" | "invite_expired" | "session_expired";

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
  role: TTeamMemberRole;
  status: TTeamMemberStatus;
  joinedAt: string;
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
