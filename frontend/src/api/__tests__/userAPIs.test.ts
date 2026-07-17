import { afterEach, describe, expect, it, vi } from "vitest";

import {
  addUserMembership,
  bulkUserRoleChange,
  deactivateUserSession,
  deleteUsers,
  getUser,
  getUsersStats,
  listUsers,
  removeUserMemberships,
} from "@/api/userAPIs";

describe("userAPIs", () => {
  afterEach(() => vi.restoreAllMocks());
  const spyFetch = () =>
    vi.spyOn(globalThis, "fetch").mockResolvedValue({ ok: true } as Response);

  it("lists users with only the provided params (omits 'all'/empty)", async () => {
    const f = spyFetch();
    await listUsers({
      search: "kim",
      status: "all",
      teamId: "t_1",
      sort: "account",
      page: 2,
      size: 10,
    });
    const url = f.mock.calls[0][0] as string;
    expect(url).toContain("/api/v1/users?");
    expect(url).toContain("search=kim");
    expect(url).not.toContain("status="); // "all" omitted
    expect(url).toContain("teamId=t_1");
    expect(url).toContain("sort=account");
    expect(url).toContain("page=2");
    expect(url).toContain("size=10");
  });

  it("omits an empty search", async () => {
    const f = spyFetch();
    await listUsers({
      search: "",
      status: "all",
      teamId: "all",
      sort: "last_invited",
      page: 1,
      size: 8,
    });
    const url = f.mock.calls[0][0] as string;
    expect(url).not.toContain("search=");
    expect(url).not.toContain("teamId=");
  });

  it("gets stats", async () => {
    const f = spyFetch();
    await getUsersStats();
    expect(f.mock.calls[0][0]).toBe("/api/v1/users/stats");
  });

  it("gets one user", async () => {
    const f = spyFetch();
    await getUser("u_1");
    expect(f.mock.calls[0][0]).toBe("/api/v1/users/u_1");
  });

  it("adds a membership with a POST body", async () => {
    const f = spyFetch();
    await addUserMembership("u_1", { teamId: "t_9", role: "read" });
    const [url, opts] = f.mock.calls[0];
    expect(url).toBe("/api/v1/users/u_1/members/roles");
    expect(opts).toMatchObject({ method: "POST" });
    expect(JSON.parse(opts!.body as string)).toEqual({
      teamId: "t_9",
      role: "read",
    });
  });

  it("bulk role change PUTs updates", async () => {
    const f = spyFetch();
    await bulkUserRoleChange("u_1", {
      updates: [{ teamId: "t_1", role: "write" }],
    });
    const [url, opts] = f.mock.calls[0];
    expect(url).toBe("/api/v1/users/u_1/members/roles");
    expect(opts).toMatchObject({ method: "PUT" });
  });

  it("removes memberships with a CSV teamIds query and deactivates a session", async () => {
    const f = spyFetch();
    await removeUserMemberships("u_1", ["t_1", "t_2"]);
    expect(f.mock.calls[0][0]).toBe(
      "/api/v1/users/u_1/members/roles?teamIds=t_1%2Ct_2",
    );
    expect(f.mock.calls[0][1]).toMatchObject({ method: "DELETE" });
    await deactivateUserSession("u_1");
    expect(f.mock.calls[1][0]).toBe("/api/v1/users/u_1/session");
    expect(f.mock.calls[1][1]).toMatchObject({ method: "DELETE" });
  });

  it("deletes users in batch with a CSV userIds query", async () => {
    const f = spyFetch();
    await deleteUsers(["u_1", "u_2"]);
    expect(f.mock.calls[0][0]).toBe("/api/v1/users?userIds=u_1%2Cu_2");
    expect(f.mock.calls[0][1]).toMatchObject({ method: "DELETE" });
  });
});
