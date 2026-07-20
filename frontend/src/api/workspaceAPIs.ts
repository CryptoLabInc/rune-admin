import { serviceFetch } from "@/api/createFetch";

/*
 * Workspace endpoints (console API design 2026-07-13, §Workspace / SC-02).
 * A singular resource, one per account — no id in the path. Create/stop/
 * start/delete are async (202): they return a transitional phase and the
 * caller polls GET /workspace until it settles.
 */

/** getWorkspace reads the workspace (GET /workspace; 404 when none exists). */
export const getWorkspace = async () => await serviceFetch("workspace");

/** createWorkspace provisions the workspace (POST /workspace → 202). */
export const createWorkspace = async () =>
  await serviceFetch("workspace", { method: "POST" });

/** stopWorkspace stops a running workspace (POST /workspace/stop → 202). */
export const stopWorkspace = async () =>
  await serviceFetch("workspace/stop", { method: "POST" });

/** startWorkspace restarts a stopped workspace (POST /workspace/start → 202). */
export const startWorkspace = async () =>
  await serviceFetch("workspace/start", { method: "POST" });

/** deleteWorkspace deletes the workspace (DELETE /workspace → 202). */
export const deleteWorkspace = async () =>
  await serviceFetch("workspace", { method: "DELETE" });
