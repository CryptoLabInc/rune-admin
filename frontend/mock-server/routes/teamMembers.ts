// Team-member endpoints (SC-06·10·14): the "one team × many users" surface of
// the membership data. List, add member (server-judged, may create the user
// and send a code), bulk role change, bulk remove.
import {
  HttpError,
  paginate,
  parseCsvQuery,
  parsePaging,
  parseUsername,
  sendJson,
} from "../http.ts";
import type { BatchResult } from "../http.ts";
import type { Ctx } from "../router.ts";
import { nextId, state } from "../store.ts";
import type { Membership, Role, User } from "../types.ts";

const GRANTABLE: Role[] = ["edit", "write", "read"];

const requireTeam = (teamId: string): void => {
  if (!state.teams.some((t) => t.id === teamId)) {
    throw new HttpError(404, "TEAM_NOT_FOUND", `team ${teamId} not found`);
  }
};

const userById = (userId: string): User | undefined =>
  state.users.find((u) => u.id === userId);

type MemberItem = {
  userId: string;
  account: string;
  username: string;
  role: Role;
  invitationStatus: User["invitationStatus"];
  sessionStatus: User["sessionStatus"];
  // null for an inherited-read row: no stored membership, no join timestamp
  // (mirrors the real API's nullable joinedAt).
  joinedAt: string | null;
};

const memberItem = (m: Membership): MemberItem => {
  const user = userById(m.userId);
  return {
    userId: m.userId,
    account: user?.account ?? "unknown",
    username: user?.username ?? "unknown",
    role: m.role,
    invitationStatus: user?.invitationStatus ?? "invite_redeemed",
    sessionStatus: user?.sessionStatus ?? "offline",
    joinedAt: m.joinedAt,
  };
};

/** Proper ancestors of a team (walk the parentId chain up to the root). */
const ancestorIds = (teamId: string): Set<string> => {
  const out = new Set<string>();
  let cur = state.teams.find((t) => t.id === teamId)?.parentId ?? null;
  while (cur !== null && !out.has(cur)) {
    out.add(cur);
    cur = state.teams.find((t) => t.id === cur)?.parentId ?? null;
  }
  return out;
};

/**
 * inheritedMemberItems mirrors the real API: every user with a membership on
 * a PROPER ancestor of the team (any role), minus direct members, appears as
 * an inherited-read row — role "read", joinedAt null.
 */
const inheritedMemberItems = (teamId: string): MemberItem[] => {
  const ancestors = ancestorIds(teamId);
  const direct = new Set(
    state.memberships.filter((m) => m.teamId === teamId).map((m) => m.userId),
  );
  const inheritorIds = new Set(
    state.memberships
      .filter((m) => ancestors.has(m.teamId) && !direct.has(m.userId))
      .map((m) => m.userId),
  );
  return [...inheritorIds].map((userId) => {
    const user = userById(userId);
    return {
      userId,
      account: user?.account ?? "unknown",
      username: user?.username ?? "unknown",
      role: "read" as Role,
      invitationStatus: user?.invitationStatus ?? "invite_redeemed",
      sessionStatus: user?.sessionStatus ?? "offline",
      joinedAt: null,
    };
  });
};

export const listTeamMembers = (ctx: Ctx): void => {
  requireTeam(ctx.params.teamId);
  const { page, size } = parsePaging(ctx.query);
  const members = state.memberships
    .filter((m) => m.teamId === ctx.params.teamId)
    .map(memberItem)
    .concat(inheritedMemberItems(ctx.params.teamId))
    // The real API sorts the combined list by account (console_api.go).
    .sort((a, b) => a.account.localeCompare(b.account));
  sendJson(ctx.res, 200, paginate(members, page, size));
};

export const addTeamMember = (ctx: Ctx): void => {
  const { teamId } = ctx.params;
  requireTeam(teamId);
  const body = (ctx.body ?? {}) as {
    account?: unknown;
    role?: unknown;
    username?: unknown;
  };
  const account = typeof body.account === "string" ? body.account.trim() : "";
  const role = body.role;
  if (account === "")
    throw new HttpError(400, "VALIDATION_ERROR", "account is required");
  const username = parseUsername(body.username);
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
  if (account.toLowerCase() === "admin@corp.com") {
    throw new HttpError(
      409,
      "CANNOT_INVITE_ADMIN",
      "cannot add the console Admin account",
    );
  }

  /* Per-target-status judgment is the server's call (SC-10, API design):
     new (unregistered) account → create the user (invite_pending) + add
     membership + send a code · expired code/session → add membership +
     fresh code (→ invite_pending) · online / pending in another team →
     membership only, no code · already in THIS team → 409. */
  let user = state.users.find(
    (u) => u.account.toLowerCase() === account.toLowerCase(),
  );
  const isNew = !user;
  if (!user) {
    // username applies at creation; an existing user keeps the stored name
    // (account is the identifier — the body's username is not an update).
    user = {
      id: nextId("u"),
      account,
      username,
      invitationStatus: "invite_pending",
      sessionStatus: "offline",
      lastAccessAt: null,
      lastInvitedAt: new Date().toISOString(),
      sessionExpiredAt: null,
    };
    state.users.push(user);
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
  const codeSent =
    isNew ||
    (user.invitationStatus !== "invite_pending" &&
      user.sessionStatus !== "online");
  if (codeSent) {
    user.invitationStatus = "invite_pending";
    user.lastInvitedAt = new Date().toISOString();
    state.invitations.push({
      account: user.account,
      username: user.username,
      issuedAt: new Date().toISOString(),
      lastAccessAt: null,
    });
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
