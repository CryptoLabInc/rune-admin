// Team-member endpoints (SC-06·10·14): the "one team × many users" surface of
// the membership data. List, add-existing-user, bulk role change, bulk remove.
import {
  HttpError,
  paginate,
  parseCsvQuery,
  parsePaging,
  sendJson,
} from "../http.ts";
import type { BatchResult } from "../http.ts";
import type { Ctx } from "../router.ts";
import { state } from "../store.ts";
import type { Membership, Role, User } from "../types.ts";

const GRANTABLE: Role[] = ["edit", "write", "read"];

const requireTeam = (teamId: string): void => {
  if (!state.teams.some((t) => t.id === teamId)) {
    throw new HttpError(404, "TEAM_NOT_FOUND", `team ${teamId} not found`);
  }
};

const userById = (userId: string): User | undefined =>
  state.users.find((u) => u.id === userId);

const memberItem = (m: Membership) => {
  const user = userById(m.userId);
  return {
    userId: m.userId,
    account: user?.account ?? "unknown",
    role: m.role,
    status: user?.status ?? "session_expired",
    joinedAt: m.joinedAt,
  };
};

export const listTeamMembers = (ctx: Ctx): void => {
  requireTeam(ctx.params.teamId);
  const { page, size } = parsePaging(ctx.query);
  const members = state.memberships
    .filter((m) => m.teamId === ctx.params.teamId)
    .map(memberItem);
  sendJson(ctx.res, 200, paginate(members, page, size));
};

export const addTeamMember = (ctx: Ctx): void => {
  const { teamId } = ctx.params;
  requireTeam(teamId);
  const body = (ctx.body ?? {}) as { account?: unknown; role?: unknown };
  const account = typeof body.account === "string" ? body.account.trim() : "";
  const role = body.role;
  if (account === "")
    throw new HttpError(400, "VALIDATION_ERROR", "account is required");
  if (typeof role !== "string" || !GRANTABLE.includes(role as Role)) {
    if (role === "Admin") {
      throw new HttpError(
        400,
        "VALIDATION_ERROR",
        "Admin role cannot be granted",
      );
    }
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "role must be edit|write|read",
    );
  }
  const user = state.users.find(
    (u) => u.account.toLowerCase() === account.toLowerCase(),
  );
  if (!user)
    throw new HttpError(
      404,
      "USER_NOT_FOUND",
      `no user with account ${account}`,
    );
  if (user.account.toLowerCase() === "admin@corp.com") {
    throw new HttpError(
      409,
      "CANNOT_INVITE_ADMIN",
      "cannot add the console Admin account",
    );
  }
  if (
    state.memberships.some((m) => m.teamId === teamId && m.userId === user.id)
  ) {
    throw new HttpError(
      409,
      "ALREADY_TEAM_MEMBER",
      "already a member of this team",
    );
  }
  const membership: Membership = {
    userId: user.id,
    teamId,
    role: role as Role,
    joinedAt: new Date().toISOString(),
  };
  state.memberships.push(membership);
  sendJson(ctx.res, 201, memberItem(membership));
};

export const bulkRoleChange = (ctx: Ctx): void => {
  const { teamId } = ctx.params;
  requireTeam(teamId);
  const body = (ctx.body ?? {}) as {
    updates?: Array<{ userId?: string; role?: string }>;
  };
  const updates = Array.isArray(body.updates) ? body.updates : [];
  const result: BatchResult = { succeeded: [], failed: [] };
  for (const u of updates) {
    const userId = String(u.userId ?? "");
    if (!userById(userId)) {
      result.failed.push({
        id: userId,
        code: "USER_NOT_FOUND",
        message: "user not found",
      });
      continue;
    }
    const m = state.memberships.find(
      (x) => x.teamId === teamId && x.userId === userId,
    );
    if (!m) {
      result.failed.push({
        id: userId,
        code: "NOT_TEAM_MEMBER",
        message: "not a team member",
      });
      continue;
    }
    if (typeof u.role !== "string" || !GRANTABLE.includes(u.role as Role)) {
      result.failed.push({
        id: userId,
        code: "VALIDATION_ERROR",
        message: "bad role",
      });
      continue;
    }
    m.role = u.role as Role;
    result.succeeded.push(userId);
  }
  sendJson(ctx.res, 200, result);
};

export const removeTeamMembers = (ctx: Ctx): void => {
  const { teamId } = ctx.params;
  requireTeam(teamId);
  const userIds = parseCsvQuery(ctx.query, "userIds");
  if (userIds.length === 0) {
    throw new HttpError(400, "VALIDATION_ERROR", "userIds query is required");
  }
  const result: BatchResult = { succeeded: [], failed: [] };
  for (const userId of userIds) {
    if (!userById(userId)) {
      result.failed.push({
        id: userId,
        code: "USER_NOT_FOUND",
        message: "user not found",
      });
      continue;
    }
    const idx = state.memberships.findIndex(
      (m) => m.teamId === teamId && m.userId === userId,
    );
    if (idx === -1) {
      result.failed.push({
        id: userId,
        code: "NOT_TEAM_MEMBER",
        message: "not a team member",
      });
      continue;
    }
    state.memberships.splice(idx, 1);
    result.succeeded.push(userId);
  }
  sendJson(ctx.res, 200, result);
};
