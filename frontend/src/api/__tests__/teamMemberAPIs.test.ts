import { afterEach, describe, expect, it, vi } from "vitest";

import {
  addTeamMember,
  bulkRoleChange,
  listTeamMembers,
  removeTeamMembers,
} from "@/api/teamMemberAPIs";

describe("teamMemberAPIs", () => {
  afterEach(() => vi.restoreAllMocks());

  const spyFetch = () =>
    vi.spyOn(globalThis, "fetch").mockResolvedValue({ ok: true } as Response);

  it("lists members with page+size query", async () => {
    const f = spyFetch();
    await listTeamMembers("t_1", 2, 10);
    expect(f.mock.calls[0][0]).toBe("/api/v1/teams/t_1/members?page=2&size=10");
  });

  it("adds a member with a POST body", async () => {
    const f = spyFetch();
    await addTeamMember("t_1", { account: "k@x.com", role: "read" });
    const [url, opts] = f.mock.calls[0];
    expect(url).toBe("/api/v1/teams/t_1/members");
    expect(opts).toMatchObject({ method: "POST" });
    expect(JSON.parse(opts!.body as string)).toEqual({
      account: "k@x.com",
      role: "read",
    });
  });

  it("bulk role change PUTs updates", async () => {
    const f = spyFetch();
    await bulkRoleChange("t_1", {
      updates: [{ userId: "u_1", role: "write" }],
    });
    const [url, opts] = f.mock.calls[0];
    expect(url).toBe("/api/v1/teams/t_1/members/roles");
    expect(opts).toMatchObject({ method: "PUT" });
  });

  it("removes members with a CSV userIds query", async () => {
    const f = spyFetch();
    await removeTeamMembers("t_1", ["u_1", "u_2"]);
    const [url, opts] = f.mock.calls[0];
    expect(url).toBe("/api/v1/teams/t_1/members?userIds=u_1%2Cu_2");
    expect(opts).toMatchObject({ method: "DELETE" });
  });
});
