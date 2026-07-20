// Invitation endpoints (SC-11·12·13·16): invite, resend (single-target),
// cancel, and the issuance-history list.
import {
  HttpError,
  paginate,
  parsePaging,
  parseUsername,
  sendJson,
} from "../http.ts";
import type { Ctx } from "../router.ts";
import { nextId, state } from "../store.ts";
import type { Role, User } from "../types.ts";

const GRANTABLE: Role[] = ["edit", "write", "read"];

export const invite = (ctx: Ctx): void => {
  const body = (ctx.body ?? {}) as {
    account?: unknown;
    username?: unknown;
    memberships?: Array<{ teamId?: string; role?: string }>;
  };
  const account = typeof body.account === "string" ? body.account.trim() : "";
  const memberships = Array.isArray(body.memberships) ? body.memberships : [];
  if (account === "" || !/.+@.+\..+/.test(account)) {
    throw new HttpError(400, "VALIDATION_ERROR", "valid account is required");
  }
  const username = parseUsername(body.username);
  if (memberships.length === 0) {
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "memberships must not be empty",
    );
  }
  if (account.toLowerCase() === "admin@corp.com") {
    throw new HttpError(
      409,
      "CANNOT_INVITE_ADMIN",
      "cannot invite the console Admin account",
    );
  }
  for (const m of memberships) {
    const teamId = String(m.teamId ?? "");
    if (!state.teams.some((t) => t.id === teamId)) {
      throw new HttpError(404, "TEAM_NOT_FOUND", `team ${teamId} not found`);
    }
    if (typeof m.role !== "string" || !GRANTABLE.includes(m.role as Role)) {
      throw new HttpError(
        400,
        "VALIDATION_ERROR",
        "role must be edit|write|read",
      );
    }
  }

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

  for (const m of memberships) {
    const teamId = String(m.teamId);
    if (
      state.memberships.some(
        (x) => x.userId === user!.id && x.teamId === teamId,
      )
    ) {
      throw new HttpError(
        409,
        "ALREADY_TEAM_MEMBER",
        "already a member of a target team",
      );
    }
  }
  for (const m of memberships) {
    state.memberships.push({
      userId: user.id,
      teamId: String(m.teamId),
      role: m.role as Role,
      joinedAt: new Date().toISOString(),
    });
  }

  // Send a fresh code when the member cannot currently get in: no live pending
  // code AND not online. (invite_pending has a live code; online is already in.)
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
  sendJson(ctx.res, 201, {
    userId: user.id,
    account: user.account,
    username: user.username,
    invitationStatus: user.invitationStatus,
    sessionStatus: user.sessionStatus,
    codeSent,
  });
};

const requireUserById = (userId: string): User => {
  const user = state.users.find((u) => u.id === userId);
  if (!user) throw new HttpError(404, "USER_NOT_FOUND", "user not found");
  return user;
};

export const resend = (ctx: Ctx): void => {
  const body = (ctx.body ?? {}) as { userId?: unknown };
  const user = requireUserById(String(body.userId ?? ""));
  // Resend = issue a new code. The latest code is now a fresh pending one,
  // so invitationStatus always becomes invite_pending; sessionStatus is
  // untouched. A new history row is always added.
  user.invitationStatus = "invite_pending";
  user.lastInvitedAt = new Date().toISOString();
  state.invitations.push({
    account: user.account,
    username: user.username,
    issuedAt: new Date().toISOString(),
    lastAccessAt: null,
  });
  sendJson(ctx.res, 200, {
    userId: user.id,
    invitationStatus: user.invitationStatus,
    sessionStatus: user.sessionStatus,
  });
};

export const cancel = (ctx: Ctx): void => {
  const body = (ctx.body ?? {}) as { userId?: unknown };
  const user = requireUserById(String(body.userId ?? ""));
  if (user.invitationStatus !== "invite_pending") {
    throw new HttpError(
      409,
      "INVITATION_NOT_PENDING",
      "user is not in invite_pending",
    );
  }
  user.invitationStatus = "invite_expired";
  sendJson(ctx.res, 200, {
    userId: user.id,
    invitationStatus: user.invitationStatus,
    sessionStatus: user.sessionStatus,
  });
};

export const invitationHistory = (ctx: Ctx): void => {
  const { page, size } = parsePaging(ctx.query);
  const sort = ctx.query.get("sort") ?? "last_access";
  const rows = state.invitations.slice();
  if (sort === "account") {
    rows.sort((a, b) => a.account.localeCompare(b.account));
  } else if (sort === "issued_at") {
    rows.sort((a, b) => Date.parse(b.issuedAt) - Date.parse(a.issuedAt));
  } else {
    rows.sort(
      (a, b) =>
        (b.lastAccessAt ? Date.parse(b.lastAccessAt) : 0) -
        (a.lastAccessAt ? Date.parse(a.lastAccessAt) : 0),
    );
  }
  sendJson(ctx.res, 200, paginate(rows, page, size));
};
