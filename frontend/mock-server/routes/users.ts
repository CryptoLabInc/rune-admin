// Global user endpoints (SC-11·13·15): list, detail, bulk delete, session
// deactivation, and the Members badge count.
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
import type { User } from "../types.ts";

const teamName = (teamId: string): string =>
  state.teams.find((t) => t.id === teamId)?.name ?? "unknown";

const userView = (u: User) => ({
  userId: u.id,
  account: u.account,
  username: u.username,
  invitationStatus: u.invitationStatus,
  sessionStatus: u.sessionStatus,
  memberships: state.memberships
    .filter((m) => m.userId === u.id)
    .map((m) => ({
      teamId: m.teamId,
      teamName: teamName(m.teamId),
      role: m.role,
    })),
  lastAccessAt: u.lastAccessAt,
  lastInvitedAt: u.lastInvitedAt,
  sessionExpiredAt: u.sessionExpiredAt,
});

const sortValue = (u: User, sort: string): number => {
  if (sort === "account") return 0; // handled separately (string sort)
  const ts = sort === "last_access" ? u.lastAccessAt : u.lastInvitedAt; // last_invited default
  return ts ? Date.parse(ts) : 0;
};

export const listUsers = (ctx: Ctx): void => {
  const { page, size } = parsePaging(ctx.query);
  const search = (ctx.query.get("search") ?? "").toLowerCase();
  const status = ctx.query.get("status");
  const teamId = ctx.query.get("teamId");
  const sort = ctx.query.get("sort") ?? "last_invited";

  let rows = state.users.slice();
  if (search)
    rows = rows.filter(
      (u) =>
        u.account.toLowerCase().includes(search) ||
        u.username.toLowerCase().includes(search),
    );
  if (status) rows = rows.filter((u) => u.sessionStatus === status);
  if (teamId) {
    const ids = new Set(
      state.memberships.filter((m) => m.teamId === teamId).map((m) => m.userId),
    );
    rows = rows.filter((u) => ids.has(u.id));
  }
  if (sort === "account") {
    rows.sort((a, b) => a.account.localeCompare(b.account));
  } else {
    rows.sort((a, b) => sortValue(b, sort) - sortValue(a, sort)); // most recent first
  }
  sendJson(ctx.res, 200, paginate(rows.map(userView), page, size));
};

export const getUser = (ctx: Ctx): void => {
  const user = state.users.find((u) => u.id === ctx.params.userId);
  if (!user) throw new HttpError(404, "USER_NOT_FOUND", "user not found");
  sendJson(ctx.res, 200, userView(user));
};

export const deleteUsers = (ctx: Ctx): void => {
  const userIds = parseCsvQuery(ctx.query, "userIds");
  if (userIds.length === 0) {
    throw new HttpError(400, "VALIDATION_ERROR", "userIds query is required");
  }
  const result: BatchResult = { succeeded: [], failed: [] };
  for (const userId of userIds) {
    const user = state.users.find((u) => u.id === userId);
    if (!user) {
      result.failed.push({
        id: userId,
        code: "USER_NOT_FOUND",
        message: "user not found",
      });
      continue;
    }
    // Server behavior: remove all memberships + destroy session token & codes.
    state.users = state.users.filter((u) => u.id !== userId);
    state.memberships = state.memberships.filter((m) => m.userId !== userId);
    state.invitations = state.invitations.filter(
      (r) => r.account !== user.account,
    );
    result.succeeded.push(userId);
  }
  sendJson(ctx.res, 200, result);
};

export const deactivateSession = (ctx: Ctx): void => {
  const user = state.users.find((u) => u.id === ctx.params.userId);
  if (!user) throw new HttpError(404, "USER_NOT_FOUND", "user not found");
  // Deactivation destroys the live session token — only meaningful while
  // online (the visible session state maps 1:1 to the button).
  if (user.sessionStatus !== "online") {
    throw new HttpError(
      409,
      "SESSION_NOT_ACTIVE",
      "no active session to destroy",
    );
  }
  user.sessionStatus = "offline";
  user.sessionExpiredAt = new Date().toISOString();
  sendJson(ctx.res, 200, {
    userId: user.id,
    invitationStatus: user.invitationStatus,
    sessionStatus: user.sessionStatus,
  });
};

export const usersStats = (ctx: Ctx): void => {
  const invitePending = state.users.filter(
    (u) => u.invitationStatus === "invite_pending",
  ).length;
  sendJson(ctx.res, 200, { invitePending });
};
