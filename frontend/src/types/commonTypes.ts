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

/** Session chip state — the only status a list view renders. */
export type TMemberStatus = "online" | "offline";

/**
 * rune workspace lifecycle phase (wireframe SC-03 badge; console API
 * `phase`). `provisioning` is the transient state right after create,
 * before the endpoint/row count exist.
 */
export type TWorkspaceStatus =
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
  status: TWorkspaceStatus;
  endpoint: string | null;
  rowCount: number | null;
  /**
   * The workspace exists in the cloud but was created by a different console
   * install than this one (a reinstall minted a fresh team_secret), so its
   * stored data is encrypted under a key we no longer hold and it can only be
   * deleted + recreated. Absent/false on a healthy workspace.
   */
  orphaned: boolean;
  /**
   * The data-plane credential expired and a background reconnect cannot
   * re-bootstrap it — the user must drive a reconnect (POST /workspace). The
   * cloud workspace itself is healthy; only the local engine link is stale.
   * Mutually exclusive with orphaned (recreate supersedes reconnect).
   */
  reconnectRequired: boolean;
};

/** Wire shape of `GET /workspace` (console API design 2026-07-13, §Workspace). */
export type TWorkspaceWire = {
  phase: TWorkspaceStatus;
  endpointUrl: string | null;
  rows: number | null;
  /** true when the workspace no longer matches this console (reinstall). */
  orphaned?: boolean;
  /** true when the data-plane credential expired and needs a user-driven reconnect. */
  reconnect?: boolean;
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
