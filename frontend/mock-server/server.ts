// Rune Console mock server. Run with:
//   node --experimental-strip-types mock-server/server.ts
// (or `pnpm mock` from the frontend/ directory).
import http from "node:http";
import type { IncomingMessage, ServerResponse } from "node:http";

import { config } from "./config.ts";
import { HttpError, readBody, sendError } from "./http.ts";
import { Router } from "./router.ts";
import type { Ctx } from "./router.ts";
import * as auth from "./routes/auth.ts";
import * as invitations from "./routes/invitations.ts";
import * as memberships from "./routes/memberships.ts";
import * as mock from "./routes/mockControl.ts";
import * as teamMembers from "./routes/teamMembers.ts";
import * as teams from "./routes/teams.ts";
import * as users from "./routes/users.ts";
import * as workspace from "./routes/workspace.ts";
import { getSession, touchSession } from "./store.ts";

const router = new Router();

// ---- Auth & session (outside /api/v1, exempt from the session guard) -------
router.post("/console/auth/start", auth.authStart);
router.get("/mock-authorize", auth.mockAuthorize);
router.get("/auth/callback", auth.authCallback);
router.get("/console/session", auth.sessionCheck);
router.post("/console/auth/logout", auth.authLogout);

// ---- Dev-only control (outside the API contract) ---------------------------
router.post("/__mock/session/expire", mock.mockExpire);
router.post("/__mock/session/login", mock.mockLogin);
router.post("/__mock/session/logout", mock.mockLogout);
router.post("/__mock/reset", mock.mockReset);
router.post("/__mock/workspace/fail", mock.mockWorkspaceFail);
router.post("/__mock/workspace/orphan", mock.mockWorkspaceOrphan);
router.get("/__mock/state", mock.mockDump);

// ---- Workspace -------------------------------------------------------------
router.post("/api/v1/workspace", workspace.createWorkspace);
router.get("/api/v1/workspace", workspace.getWorkspace);
router.post("/api/v1/workspace/stop", workspace.stopWorkspace);
router.post("/api/v1/workspace/start", workspace.startWorkspace);
router.delete("/api/v1/workspace", workspace.deleteWorkspace);

// ---- Teams -----------------------------------------------------------------
router.get("/api/v1/teams/tree", teams.getTeamsTree);
router.get("/api/v1/teams/:teamId", teams.getTeam);
router.post("/api/v1/teams", teams.createTeam);
router.put("/api/v1/teams/:teamId", teams.renameTeam);
router.delete("/api/v1/teams/:teamId", teams.deleteTeam);

// ---- Team members ----------------------------------------------------------
router.get("/api/v1/teams/:teamId/members", teamMembers.listTeamMembers);
router.post("/api/v1/teams/:teamId/members", teamMembers.addTeamMember);
router.put("/api/v1/teams/:teamId/members/roles", teamMembers.bulkRoleChange);
router.delete("/api/v1/teams/:teamId/members", teamMembers.removeTeamMembers);

// ---- Users -----------------------------------------------------------------
router.get("/api/v1/users/stats", users.usersStats);
router.get("/api/v1/users", users.listUsers);
router.delete("/api/v1/users", users.deleteUsers);
router.get("/api/v1/users/:userId", users.getUser);
router.delete("/api/v1/users/:userId/session", users.deactivateSession);

// ---- User memberships (drawer) ---------------------------------------------
router.post(
  "/api/v1/users/:userId/members/roles",
  memberships.addMembershipTeam,
);
router.put(
  "/api/v1/users/:userId/members/roles",
  memberships.bulkMembershipRoleChange,
);
router.delete(
  "/api/v1/users/:userId/members/roles",
  memberships.removeMembershipTeams,
);

// ---- Invitations -----------------------------------------------------------
router.post("/api/v1/invitations", invitations.invite);
router.post("/api/v1/invitations/resend", invitations.resend);
router.post("/api/v1/invitations/cancel", invitations.cancel);
router.get("/api/v1/invitations", invitations.invitationHistory);

const applyCors = (req: IncomingMessage, res: ServerResponse): void => {
  const origin = req.headers.origin;
  // Reflect the origin so credentialed cross-origin requests (direct-to-mock
  // usage) work; harmless when the frontend goes through the Vite proxy.
  res.setHeader("Access-Control-Allow-Origin", origin ?? "*");
  res.setHeader("Access-Control-Allow-Credentials", "true");
  res.setHeader("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS");
  res.setHeader("Access-Control-Allow-Headers", "Content-Type");
  res.setHeader("Vary", "Origin");
};

const isApiPath = (pathname: string): boolean => pathname.startsWith("/api/v1");

const originGuardBlocks = (req: IncomingMessage): boolean => {
  if (!config.originGuard) return false;
  return req.headers["sec-fetch-site"] === "cross-site";
};

const server = http.createServer(async (req, res) => {
  applyCors(req, res);
  if (req.method === "OPTIONS") {
    res.writeHead(204);
    res.end();
    return;
  }

  const url = new URL(req.url ?? "/", `http://localhost:${config.port}`);
  const pathname = url.pathname;
  const method = req.method ?? "GET";

  try {
    if (originGuardBlocks(req)) {
      throw new HttpError(
        403,
        "ORIGIN_FORBIDDEN",
        "cross-site request rejected",
      );
    }

    const matched = router.match(method, pathname);
    if (!matched) {
      sendError(res, 404, "NOT_FOUND", `no route for ${method} ${pathname}`);
      return;
    }

    // Session guard: every /api/v1 endpoint requires a live console session.
    if (isApiPath(pathname)) {
      if (!getSession().loggedIn) {
        throw new HttpError(
          401,
          "SESSION_INVALID",
          "console session absent or expired",
        );
      }
      touchSession(); // sliding expiry (no-op unless MOCK_SLIDING=1)
    }

    const body =
      method === "POST" || method === "PUT" ? await readBody(req) : undefined;
    const ctx: Ctx = {
      req,
      res,
      params: matched.params,
      query: url.searchParams,
      body,
    };
    await matched.handler(ctx);
  } catch (err) {
    if (err instanceof HttpError) {
      sendError(res, err.status, err.code, err.message);
      return;
    }
    console.error("[mock] unhandled error:", err);
    if (!res.headersSent)
      sendError(res, 500, "INTERNAL_ERROR", "internal server error");
  }
});

server.listen(config.port, () => {
  console.log(`Rune Console mock server → http://localhost:${config.port}`);
  console.log(`  base path   : /api/v1`);
  console.log(
    `  auth        : POST /console/auth/start · GET /console/session · POST /console/auth/logout`,
  );
  console.log(
    `  dev control : POST /__mock/session/{expire,login,logout} · POST /__mock/reset · POST /__mock/workspace/fail?op=… · POST /__mock/workspace/orphan · GET /__mock/state`,
  );
  console.log(
    `  session     : startLoggedIn=${config.startLoggedIn} ttl=${config.sessionTtlMs}ms sliding=${config.sliding} → callback redirects to ${config.appOrigin}`,
  );
});
