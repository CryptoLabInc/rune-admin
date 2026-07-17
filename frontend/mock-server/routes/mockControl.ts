// Dev-only control endpoints (prefix /__mock/). NOT part of the API contract —
// they exist so the frontend can force auth/session states on demand while
// developing (e.g. reproduce an expired session without waiting for the TTL).
import { HttpError, sendJson, sendNoContent } from "../http.ts";
import type { Ctx } from "../router.ts";
import {
  armWorkspaceFail,
  expireSessionNow,
  getSession,
  login,
  logout,
  resetState,
  state,
} from "../store.ts";

export const mockExpire = (ctx: Ctx): void => {
  expireSessionNow();
  sendJson(ctx.res, 200, {
    ok: true,
    note: "session expired; next /api call returns 401",
  });
};

export const mockLogin = (ctx: Ctx): void => {
  const s = login();
  sendJson(ctx.res, 200, { ok: true, expires_at: s.expiresAt });
};

export const mockLogout = (ctx: Ctx): void => {
  logout();
  sendNoContent(ctx.res);
};

export const mockReset = (ctx: Ctx): void => {
  resetState();
  sendJson(ctx.res, 200, { ok: true, note: "all state reset to seed" });
};

/**
 * mockWorkspaceFail arms a one-shot failure for the next workspace op so the
 * frontend can reproduce the SC-02 failure screens. Query params:
 *   op     get | create | stop | start | delete   (required)
 *   status HTTP status to return                   (default 502)
 *   code   error envelope code                     (default CLOUD_UPSTREAM_ERROR)
 * e.g. POST /__mock/workspace/fail?op=stop
 */
const WORKSPACE_OPS = ["get", "create", "stop", "start", "delete"];

export const mockWorkspaceFail = (ctx: Ctx): void => {
  const op = ctx.query.get("op") ?? "";
  if (!WORKSPACE_OPS.includes(op)) {
    throw new HttpError(
      400,
      "BAD_REQUEST",
      `op must be one of ${WORKSPACE_OPS.join(", ")}`,
    );
  }
  const status = Number(ctx.query.get("status")) || 502;
  const code = ctx.query.get("code") ?? "CLOUD_UPSTREAM_ERROR";
  armWorkspaceFail(op, status, code);
  sendJson(ctx.res, 200, { ok: true, armed: { op, status, code } });
};

/**
 * mockDump inspects the in-memory "database". Default = summary (session,
 * row counts, workspace). With ?full=1 it also returns every table verbatim
 * (teams / users / memberships / invitations) so you can see exactly what
 * the mock currently holds while testing.
 */
export const mockDump = (ctx: Ctx): void => {
  const s = getSession();
  const summary = {
    session: s,
    counts: {
      teams: state.teams.length,
      users: state.users.length,
      memberships: state.memberships.length,
      invitations: state.invitations.length,
    },
    workspace: state.workspace,
  };
  if (ctx.query.get("full") !== "1") {
    sendJson(ctx.res, 200, summary);
    return;
  }
  sendJson(ctx.res, 200, {
    ...summary,
    tables: {
      teams: state.teams,
      users: state.users,
      memberships: state.memberships,
      invitations: state.invitations,
    },
  });
};
