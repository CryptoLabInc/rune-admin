export type TAlert = {
  title: string;
  content: string;
};

/** Option shape for the shared Dropdown element (UIKIT AdminOption). */
export type TDropdownOption = {
  value: string;
  label: string;
  /** Indent level for tree-shaped option lists (team tree). */
  depth?: number;
  disabled?: boolean;
};

/** Member connection status (wireframe v0.4 token model, 4 badges). */
export type TMemberStatus =
  "online" | "pending" | "invite-expired" | "session-expired";

/**
 * rune workspace lifecycle phase (wireframe SC-03 badge; console API
 * `phase`). `provisioning` is the transient state right after create,
 * before the endpoint/row count exist.
 */
export type TStorageStatus =
  | "provisioning"
  | "running"
  | "stopping"
  | "stopped"
  | "starting"
  | "deleting"
  | "error";

/**
 * rune workspace record surfaced in the console (wireframe SC-02 state D),
 * mapped from the API `GET /workspace` body. The workspace name is never
 * exposed — it is a hash-like random value stored DB-side only. endpoint and
 * rowCount are null until the workspace finishes provisioning.
 */
export type TWorkspace = {
  status: TStorageStatus;
  endpoint: string | null;
  rowCount: number | null;
};

/** Wire shape of `GET /workspace` (console API design 2026-07-13, §Workspace). */
export type TWorkspaceWire = {
  phase: TStorageStatus;
  endpointUrl: string | null;
  rows: number | null;
};

/** Recursive team-tree node (UIKIT AdminTeamNode, wireframe SC-06). */
export type TTeamNode = {
  id: string;
  name: string;
  members: number;
  children?: TTeamNode[];
};

/** Toast tone — semantic colors are state, not decoration. */
export type TToastTone = "info" | "success" | "error";
