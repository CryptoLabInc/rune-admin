// Shared domain types for the Rune Console mock server.
// These mirror the console API request/response shapes.

export type Role = "Admin" | "edit" | "write" | "read";

export type MemberStatus =
  "online" | "invite_pending" | "invite_expired" | "session_expired";

export type WorkspacePhase =
  | "provisioning"
  | "running"
  | "stopping"
  | "stopped"
  | "starting"
  | "deleting"
  | "error";

export type Team = {
  id: string;
  name: string;
  parentId: string | null;
  createdAt: string;
};

export type Membership = {
  userId: string;
  teamId: string;
  role: Role;
  joinedAt: string;
};

export type User = {
  id: string;
  account: string;
  status: MemberStatus;
  lastAccessAt: string | null;
  lastInvitedAt: string | null;
  sessionExpiredAt: string | null;
};

// One row per invitation-code issuance (SC-16). Multiple rows per account.
export type InvitationRow = {
  account: string;
  issuedAt: string;
  lastAccessAt: string | null;
};

export type Workspace = {
  exists: boolean;
  phase: WorkspacePhase;
  endpointUrl: string | null;
  rows: number | null;
  createdAt: string | null;
};

export type Principal = {
  email: string;
  avatar: string;
};

export type Session = {
  loggedIn: boolean;
  expiresAt: string | null;
  me: Principal | null;
};
