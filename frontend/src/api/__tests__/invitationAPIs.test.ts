import { afterEach, describe, expect, it, vi } from "vitest";

import {
  cancelInvitation,
  getInvitationHistory,
  postInvitation,
  resendInvitation,
} from "@/api/invitationAPIs";

describe("invitationAPIs", () => {
  afterEach(() => vi.restoreAllMocks());
  const spyFetch = () =>
    vi.spyOn(globalThis, "fetch").mockResolvedValue({ ok: true } as Response);

  it("posts an invitation with account and memberships", async () => {
    const f = spyFetch();
    await postInvitation({
      account: "user@example.com",
      memberships: [{ teamId: "t_1", role: "write" }],
    });
    const [url, opts] = f.mock.calls[0];
    expect(url).toBe("/api/v1/invitations");
    expect(opts).toMatchObject({ method: "POST" });
    expect(JSON.parse(opts!.body as string)).toEqual({
      account: "user@example.com",
      memberships: [{ teamId: "t_1", role: "write" }],
    });
  });

  it("resends an invitation for a user", async () => {
    const f = spyFetch();
    await resendInvitation("u_1");
    const [url, opts] = f.mock.calls[0];
    expect(url).toBe("/api/v1/invitations/resend");
    expect(opts).toMatchObject({ method: "POST" });
    expect(JSON.parse(opts!.body as string)).toEqual({ userId: "u_1" });
  });

  it("cancels an invitation for a user", async () => {
    const f = spyFetch();
    await cancelInvitation("u_1");
    const [url, opts] = f.mock.calls[0];
    expect(url).toBe("/api/v1/invitations/cancel");
    expect(opts).toMatchObject({ method: "POST" });
    expect(JSON.parse(opts!.body as string)).toEqual({ userId: "u_1" });
  });

  it("fetches invitation history with sort, page, and size", async () => {
    const f = spyFetch();
    await getInvitationHistory("last_access", 2, 10);
    const url = f.mock.calls[0][0] as string;
    expect(url).toBe(
      "/api/v1/invitations?view=history&sort=last_access&page=2&size=10",
    );
  });
});
