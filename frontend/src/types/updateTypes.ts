/** Lifecycle reported by the privileged rune-console update agent. */
export type TSystemUpdateState =
  "idle" | "queued" | "running" | "failed" | "succeeded";

/** Wire contract for GET /api/v1/system/update. */
export type TSystemUpdateStatus = {
  currentVersion: string;
  targetVersion?: string;
  updateAvailable: boolean;
  capable: boolean;
  state: TSystemUpdateState;
};
