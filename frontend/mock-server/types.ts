// Shared domain types for the Rune Console mock server.
// These mirror the console API request/response shapes.

export type Role = "Admin" | "edit" | "write" | "read";

export type TInvitationStatus =
  | "invite_pending"
  | "invite_expired"
  | "invite_redeemed";

export type TSessionStatus = "online" | "offline";

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
  // Display name (not an identifier — account stays unique, API 2026-07-20).
  username: string;
  invitationStatus: TInvitationStatus;
  sessionStatus: TSessionStatus;
  lastAccessAt: string | null;
  lastInvitedAt: string | null;
  sessionExpiredAt: string | null;
};

// One row per invitation-code issuance (SC-16). Multiple rows per account.
export type InvitationRow = {
  account: string;
  username: string;
  issuedAt: string;
  lastAccessAt: string | null;
};

export type Workspace = {
  exists: boolean;
  phase: WorkspacePhase;
  endpointUrl: string | null;
  rows: number | null;
  createdAt: string | null;
  // True when the console reinstall left the workspace pinned to a stale
  // team_secret (backend detects the fingerprint mismatch). The SPA then
  // offers the delete-and-recreate flow.
  orphaned: boolean;
};

export type Principal = {
  email: string;
  avatar: string;
};

export type Session = {
  loggedIn: boolean;
  expiresAt: string | null;
  plan: string;
  me: Principal | null;
};

export type SystemUpdateState =
  | "idle"
  | "queued"
  | "running"
  | "failed"
  | "succeeded";

// Owner-facing update view (GET /api/v1/system/update). Mirrors the backend
// UpdateStatus: updateAvailable = targetVersion strictly newer; capable = the
// privileged helper is installed.
export type SystemUpdate = {
  currentVersion: string;
  targetVersion?: string;
  updateAvailable: boolean;
  capable: boolean;
  state: SystemUpdateState;
};
