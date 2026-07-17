// Auth & session endpoints. These live OUTSIDE the /api/v1 base path and are
// exempt from the session guard (see docs: start/callback/session exceptions).
//
// Redirect-shaped flow (mirrors the documented OAuth dance):
//   POST /console/auth/start   -> { authorize_url } pointing at /mock-authorize
//   GET  /mock-authorize       -> auto-approve page (stands in for cloud /signin)
//   GET  /auth/callback        -> establish server-held session, 302 to the app
//   GET  /console/session      -> single source of truth (always 200)
//   POST /console/auth/logout  -> destroy session, 204
import { randomUUID } from "node:crypto";
import type { ServerResponse } from "node:http";

import { config } from "../config.ts";
import { sendJson, sendNoContent } from "../http.ts";
import type { Ctx } from "../router.ts";
import { getSession, login, logout, state } from "../store.ts";

export const authStart = (ctx: Ctx): void => {
  const authState = randomUUID();
  state.pendingAuthStates.add(authState);
  const authorizeUrl = `http://localhost:${config.port}/mock-authorize?state=${authState}`;
  sendJson(ctx.res, 200, { authorize_url: authorizeUrl });
};

/** mockAuthorize stands in for the cloud web sign-in page; it auto-approves. */
export const mockAuthorize = (ctx: Ctx): void => {
  const authState = ctx.query.get("state") ?? "";
  const callback = `/auth/callback?code=mock_code&state=${encodeURIComponent(authState)}`;
  const html = `<!doctype html>
<meta charset="utf-8">
<title>Mock IdP — Rune Console</title>
<meta http-equiv="refresh" content="1;url=${callback}">
<style>body{font-family:system-ui,sans-serif;display:grid;place-items:center;height:100vh;margin:0;background:#f6f7f9;color:#1c1e26}
.card{text-align:center}.sp{font:600 13px ui-monospace,monospace;color:#575d68}</style>
<div class="card">
  <h2>Mock Identity Provider</h2>
  <p class="sp">Approving sign-in&hellip; redirecting to callback</p>
  <p><a href="${callback}">Continue now</a></p>
</div>`;
  ctx.res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
  ctx.res.end(html);
};

const redirect = (res: ServerResponse, location: string): void => {
  res.writeHead(302, { Location: location });
  res.end();
};

export const authCallback = (ctx: Ctx): void => {
  const code = ctx.query.get("code");
  const authState = ctx.query.get("state") ?? "";
  if (authState === "" || !state.pendingAuthStates.has(authState)) {
    redirect(ctx.res, `${config.appOrigin}/login?error=invalid_state`);
    return;
  }
  state.pendingAuthStates.delete(authState); // single-use
  if (!code) {
    redirect(ctx.res, `${config.appOrigin}/login?error=exchange_failed`);
    return;
  }
  login(); // establish the server-held session (no Set-Cookie)
  redirect(ctx.res, `${config.appOrigin}/`);
};

export const sessionCheck = (ctx: Ctx): void => {
  const s = getSession();
  // The one endpoint that never returns 401 — it decides logged-in state.
  if (!s.loggedIn) {
    sendJson(ctx.res, 200, { logged_in: false });
    return;
  }
  sendJson(ctx.res, 200, {
    logged_in: true,
    expires_at: s.expiresAt,
    me: s.me,
  });
};

export const authLogout = (ctx: Ctx): void => {
  logout(); // idempotent: 204 even if no session existed
  sendNoContent(ctx.res);
};
