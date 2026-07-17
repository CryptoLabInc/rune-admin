// User-membership endpoints (SC-13 drawer): the "one user × many teams" surface
// of the same membership data. Add team, bulk role change, bulk remove.
import { HttpError, parseCsvQuery, sendJson } from "../http.ts";
import type { BatchResult } from "../http.ts";
import type { Ctx } from "../router.ts";
import { state } from "../store.ts";
import type { Role } from "../types.ts";

const GRANTABLE: Role[] = ["edit", "write", "read"];

const requireUser = (userId: string): void => {
  if (!state.users.some((u) => u.id === userId)) {
    throw new HttpError(404, "USER_NOT_FOUND", `user ${userId} not found`);
  }
};

const teamName = (teamId: string): string | undefined =>
  state.teams.find((t) => t.id === teamId)?.name;

export const addMembershipTeam = (ctx: Ctx): void => {
  const { userId } = ctx.params;
  requireUser(userId);
  const body = (ctx.body ?? {}) as { teamId?: unknown; role?: unknown };
  const teamId = String(body.teamId ?? "");
  const role = body.role;
  const name = teamName(teamId);
  if (name === undefined)
    throw new HttpError(404, "TEAM_NOT_FOUND", "team not found");
  if (typeof role !== "string" || !GRANTABLE.includes(role as Role)) {
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "role must be edit|write|read",
    );
  }
  if (
    state.memberships.some((m) => m.userId === userId && m.teamId === teamId)
  ) {
    throw new HttpError(
      409,
      "ALREADY_TEAM_MEMBER",
      "already a member of this team",
    );
  }
  state.memberships.push({
    userId,
    teamId,
    role: role as Role,
    joinedAt: new Date().toISOString(),
  });
  sendJson(ctx.res, 201, { teamId, teamName: name, role });
};

export const bulkMembershipRoleChange = (ctx: Ctx): void => {
  const { userId } = ctx.params;
  requireUser(userId);
  const body = (ctx.body ?? {}) as {
    updates?: Array<{ teamId?: string; role?: string }>;
  };
  const updates = Array.isArray(body.updates) ? body.updates : [];
  const result: BatchResult = { succeeded: [], failed: [] };
  for (const u of updates) {
    const teamId = String(u.teamId ?? "");
    if (teamName(teamId) === undefined) {
      result.failed.push({
        id: teamId,
        code: "TEAM_NOT_FOUND",
        message: "team not found",
      });
      continue;
    }
    const m = state.memberships.find(
      (x) => x.userId === userId && x.teamId === teamId,
    );
    if (!m) {
      result.failed.push({
        id: teamId,
        code: "NOT_TEAM_MEMBER",
        message: "not a team member",
      });
      continue;
    }
    if (typeof u.role !== "string" || !GRANTABLE.includes(u.role as Role)) {
      result.failed.push({
        id: teamId,
        code: "VALIDATION_ERROR",
        message: "bad role",
      });
      continue;
    }
    m.role = u.role as Role;
    result.succeeded.push(teamId);
  }
  sendJson(ctx.res, 200, result);
};

export const removeMembershipTeams = (ctx: Ctx): void => {
  const { userId } = ctx.params;
  requireUser(userId);
  const teamIds = parseCsvQuery(ctx.query, "teamIds");
  if (teamIds.length === 0) {
    throw new HttpError(400, "VALIDATION_ERROR", "teamIds query is required");
  }
  const result: BatchResult = { succeeded: [], failed: [] };
  for (const teamId of teamIds) {
    if (teamName(teamId) === undefined) {
      result.failed.push({
        id: teamId,
        code: "TEAM_NOT_FOUND",
        message: "team not found",
      });
      continue;
    }
    const idx = state.memberships.findIndex(
      (m) => m.userId === userId && m.teamId === teamId,
    );
    if (idx === -1) {
      result.failed.push({
        id: teamId,
        code: "NOT_TEAM_MEMBER",
        message: "not a team member",
      });
      continue;
    }
    state.memberships.splice(idx, 1);
    result.succeeded.push(teamId);
  }
  sendJson(ctx.res, 200, result);
};
