// Contract-parity tests for the mock server against the real console API:
//  1. invite_redeemed member status (backend 2e527dc)
//  2. workspace `orphaned` flag + recreate flow (backend 484c179)
//  3. inherited-read rows in the team member list (backend 461faaa)
import type { ServerResponse } from "node:http";
import { afterEach, beforeEach, describe, expect, test } from "vitest";

import type { Ctx } from "../router.ts";
import { resetState, state } from "../store.ts";
import { listTeamMembers } from "../routes/teamMembers.ts";
import { deactivateSession, listUsers } from "../routes/users.ts";
import { createWorkspace, getWorkspace } from "../routes/workspace.ts";
import * as mockControl from "../routes/mockControl.ts";

type Sent = { status: number; body: any };

/** call invokes a route handler with a fake ServerResponse and captures the JSON reply. */
const call = (handler: (ctx: Ctx) => void, over: Partial<Ctx> = {}): Sent => {
  const sent: Sent = { status: 0, body: undefined };
  const res = {
    writeHead(status: number) {
      sent.status = status;
      return res;
    },
    end(payload?: string) {
      if (payload) sent.body = JSON.parse(payload);
    },
  } as unknown as ServerResponse;
  handler({
    req: {} as never,
    res,
    params: {},
    query: new URLSearchParams(),
    body: undefined,
    ...over,
  });
  return sent;
};

beforeEach(() => resetState());
afterEach(() => resetState()); // also clears scheduled phase-transition timers

describe("invite_redeemed member status", () => {
  test("seed contains an invite_redeemed member with no access stamp yet", () => {
    // Redeemed = token released but never used: invited stamp yes, access
    // stamp no (distinct from a redeemed member that has since logged in).
    const redeemed = state.users.filter(
      (u) => u.invitationStatus === "invite_redeemed" && u.lastAccessAt === null,
    );
    expect(redeemed.length).toBeGreaterThan(0);
    expect(redeemed[0].lastAccessAt).toBeNull();
    expect(redeemed[0].lastInvitedAt).not.toBeNull();
  });

  test("GET /users?status=online filters to the online members", () => {
    const sent = call(listUsers, {
      query: new URLSearchParams("status=online&page=1&size=50"),
    });
    expect(sent.status).toBe(200);
    expect(sent.body.total).toBeGreaterThan(0);
    for (const row of sent.body.items) expect(row.sessionStatus).toBe("online");
  });

  test("session deactivate accepts an invite_redeemed member (holds a live token)", () => {
    // A redeemed member currently online holds the live session token that
    // deactivation destroys.
    const redeemed = state.users.find(
      (u) =>
        u.invitationStatus === "invite_redeemed" && u.sessionStatus === "online",
    );
    expect(redeemed).toBeDefined();
    const sent = call(deactivateSession, { params: { userId: redeemed!.id } });
    expect(sent.status).toBe(200);
    // Invitation state is untouched by a session-only action; only the
    // session axis flips to offline.
    expect(sent.body.invitationStatus).toBe("invite_redeemed");
    expect(sent.body.sessionStatus).toBe("offline");
  });
});

describe("workspace orphaned flag", () => {
  test("GET /workspace serves orphaned=false on the healthy seed", () => {
    const sent = call(getWorkspace);
    expect(sent.status).toBe(200);
    expect(sent.body.orphaned).toBe(false);
  });

  test("POST /__mock/workspace/orphan marks the workspace orphaned", () => {
    const orphan = (mockControl as Record<string, unknown>)
      .mockWorkspaceOrphan as ((ctx: Ctx) => void) | undefined;
    expect(typeof orphan).toBe("function");
    call(orphan!);
    expect(call(getWorkspace).body.orphaned).toBe(true);
  });

  test("recreate clears the orphaned flag (delete → create returns orphaned=false)", () => {
    // Arrange: orphaned workspace whose teardown already completed (404 phase).
    (state.workspace as Record<string, unknown>).orphaned = true;
    state.workspace.exists = false;
    const created = call(createWorkspace);
    expect(created.status).toBe(202);
    expect(created.body.orphaned).toBe(false);
  });
});

describe("inherited-read rows in team members", () => {
  const listMembers = (teamId: string): Sent =>
    call(listTeamMembers, {
      params: { teamId },
      query: new URLSearchParams("page=1&size=50"),
    });

  test("an ancestor member appears on the child team as read with null joinedAt", () => {
    // Seed: jung@corp.com is write on t_1 (parent of t_2), not direct on t_2.
    const items = listMembers("t_2").body.items as Array<{
      account: string;
      role: string;
      joinedAt: string | null;
    }>;
    const jung = items.find((m) => m.account === "jung@corp.com");
    expect(jung).toBeDefined();
    expect(jung!.role).toBe("read");
    expect(jung!.joinedAt).toBeNull();
  });

  test("any ancestor membership inherits (read on the parent inherits too)", () => {
    // Seed: han@corp.com is read on t_1 → still an inheritor of t_2
    // (backend Inheritors walks memberships regardless of role).
    const items = listMembers("t_2").body.items as Array<{
      account: string;
      role: string;
      joinedAt: string | null;
    }>;
    const han = items.find((m) => m.account === "han@corp.com");
    expect(han).toBeDefined();
    expect(han!.role).toBe("read");
    expect(han!.joinedAt).toBeNull();
  });

  test("direct members keep their stored role and joinedAt", () => {
    const items = listMembers("t_2").body.items as Array<{
      account: string;
      role: string;
      joinedAt: string | null;
    }>;
    const kim = items.find((m) => m.account === "kim@corp.com");
    expect(kim!.role).toBe("write");
    expect(kim!.joinedAt).not.toBeNull();
  });

  test("rows are sorted by account like the real API", () => {
    const accounts = (
      listMembers("t_2").body.items as Array<{ account: string }>
    ).map((m) => m.account);
    expect(accounts).toEqual([...accounts].sort());
  });

  test("a root team lists only its direct members", () => {
    // t_1 has no ancestors, so no inherited rows can exist.
    const items = listMembers("t_1").body.items as Array<{
      joinedAt: string | null;
    }>;
    expect(items.length).toBeGreaterThan(0);
    for (const m of items) expect(m.joinedAt).not.toBeNull();
  });
});
