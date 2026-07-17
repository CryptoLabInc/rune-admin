// Workspace endpoints (SC-02). A singular resource, one per account. Async
// operations return immediately with a transitional phase; a timer then flips
// the phase, simulating the runespace-cloud S2S round trip.
import { HttpError, sendJson } from "../http.ts";
import type { Ctx } from "../router.ts";
import {
  consumeWorkspaceFail,
  scheduleWorkspacePhase,
  state,
} from "../store.ts";
import type { Workspace } from "../types.ts";

const view = (w: Workspace) => ({
  phase: w.phase,
  endpointUrl: w.endpointUrl,
  rows: w.rows,
  createdAt: w.createdAt,
});

/**
 * failIfArmed throws the one-shot error armed for `op` via
 * POST /__mock/workspace/fail (drives the SC-02 failure screens). No-op when
 * nothing is armed.
 */
const failIfArmed = (op: string): void => {
  const armed = consumeWorkspaceFail(op);
  if (armed)
    throw new HttpError(armed.status, armed.code, `injected ${op} failure`);
};

const requireExists = (): Workspace => {
  if (!state.workspace.exists) {
    throw new HttpError(404, "WORKSPACE_NOT_FOUND", "no workspace exists");
  }
  return state.workspace;
};

export const getWorkspace = (ctx: Ctx): void => {
  failIfArmed("get");
  const w = requireExists();
  sendJson(ctx.res, 200, view(w));
};

export const createWorkspace = (ctx: Ctx): void => {
  failIfArmed("create");
  if (state.workspace.exists) {
    throw new HttpError(
      409,
      "WORKSPACE_ALREADY_EXISTS",
      "workspace already exists",
    );
  }
  state.workspace = {
    exists: true,
    phase: "provisioning",
    endpointUrl: null,
    rows: null,
    createdAt: new Date().toISOString(),
  };
  scheduleWorkspacePhase("running", () => {
    state.workspace.endpointUrl =
      "https://mock-new.workspace.runespace.cloud:443";
    state.workspace.rows = 0;
  });
  sendJson(ctx.res, 202, view(state.workspace));
};

const requirePhase = (allowed: Workspace["phase"][]): Workspace => {
  const w = requireExists();
  if (!allowed.includes(w.phase)) {
    throw new HttpError(
      409,
      "WORKSPACE_INVALID_PHASE",
      `cannot transition from ${w.phase}`,
    );
  }
  return w;
};

export const stopWorkspace = (ctx: Ctx): void => {
  failIfArmed("stop");
  const w = requirePhase(["running"]);
  w.phase = "stopping";
  scheduleWorkspacePhase("stopped");
  sendJson(ctx.res, 202, view(w));
};

export const startWorkspace = (ctx: Ctx): void => {
  failIfArmed("start");
  const w = requirePhase(["stopped"]);
  w.phase = "starting";
  scheduleWorkspacePhase("running");
  sendJson(ctx.res, 202, view(w));
};

export const deleteWorkspace = (ctx: Ctx): void => {
  failIfArmed("delete");
  const w = requirePhase(["running", "stopped", "error"]);
  w.phase = "deleting";
  scheduleWorkspacePhase("deleting", () => {
    // Once deletion "completes", GET /workspace returns 404 again.
    state.workspace = {
      exists: false,
      phase: "deleting",
      endpointUrl: null,
      rows: null,
      createdAt: null,
    };
  });
  sendJson(ctx.res, 202, view(w));
};
