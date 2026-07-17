import { afterEach, describe, expect, it, vi } from "vitest";

import { createTeam, deleteTeam, renameTeam } from "@/api/teamAPIs";

describe("teamAPIs", () => {
  afterEach(() => vi.restoreAllMocks());

  const spyFetch = () =>
    vi.spyOn(globalThis, "fetch").mockResolvedValue({ ok: true } as Response);

  it("deleteTeam with transfer action includes targetTeamId query param", async () => {
    const f = spyFetch();
    await deleteTeam("t_1", "transfer", "t_2");
    const [url, opts] = f.mock.calls[0];
    expect(url).toBe(
      "/api/v1/teams/t_1?memoryAction=transfer&targetTeamId=t_2",
    );
    expect(opts).toMatchObject({ method: "DELETE" });
  });

  it("deleteTeam with purge action omits targetTeamId query param", async () => {
    const f = spyFetch();
    await deleteTeam("t_1", "purge");
    const [url, opts] = f.mock.calls[0];
    expect(url).toBe("/api/v1/teams/t_1?memoryAction=purge");
    expect(opts).toMatchObject({ method: "DELETE" });
  });

  it("createTeam POSTs with name and parentId body", async () => {
    const f = spyFetch();
    await createTeam({ name: "New", parentId: null });
    const [url, opts] = f.mock.calls[0];
    expect(url).toBe("/api/v1/teams");
    expect(opts).toMatchObject({ method: "POST" });
    expect(JSON.parse(opts!.body as string)).toEqual({
      name: "New",
      parentId: null,
    });
  });

  it("renameTeam PUTs name update", async () => {
    const f = spyFetch();
    await renameTeam("t_1", { name: "X" });
    const [url, opts] = f.mock.calls[0];
    expect(url).toBe("/api/v1/teams/t_1");
    expect(opts).toMatchObject({ method: "PUT" });
    expect(JSON.parse(opts!.body as string)).toEqual({ name: "X" });
  });
});
