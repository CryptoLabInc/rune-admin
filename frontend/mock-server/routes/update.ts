// System-update endpoints (owner-facing): read update availability/state and
// queue an update to a server-selected release. Mirrors the backend contract
// (GET → UpdateStatus, POST {version} → 202 {state,version}) so the SC update
// card has an intended-contract reference against the mock.
import { HttpError, sendJson } from "../http.ts";
import type { Ctx } from "../router.ts";
import { state } from "../store.ts";

export const getSystemUpdate = (ctx: Ctx): void => {
  sendJson(ctx.res, 200, state.systemUpdate);
};

export const postSystemUpdate = (ctx: Ctx): void => {
  const body = (ctx.body ?? {}) as { version?: unknown };
  const version = typeof body.version === "string" ? body.version.trim() : "";
  if (version === "") {
    throw new HttpError(400, "UPDATE_REQUEST_INVALID", "version is required");
  }
  if (!state.systemUpdate.updateAvailable) {
    throw new HttpError(
      409,
      "UPDATE_NOT_AVAILABLE",
      "the requested update is no longer available",
    );
  }
  if (
    state.systemUpdate.state === "queued" ||
    state.systemUpdate.state === "running"
  ) {
    throw new HttpError(
      409,
      "UPDATE_ALREADY_PENDING",
      "an update is already pending",
    );
  }
  state.systemUpdate.state = "queued";
  state.systemUpdate.targetVersion = version;
  sendJson(ctx.res, 202, { state: "queued", version });
};
