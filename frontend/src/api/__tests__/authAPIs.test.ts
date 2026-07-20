import { afterEach, describe, expect, it, vi } from "vitest";

import { getConsoleSession, postAuthStart, postLogout } from "@/api/authAPIs";

describe("authAPIs", () => {
  afterEach(() => vi.restoreAllMocks());

  const spyFetch = () => {
    const res = { ok: true } as Response;
    return vi.spyOn(globalThis, "fetch").mockResolvedValue(res);
  };

  it("starts login with POST to the non-/api/v1 console path", async () => {
    const fetchSpy = spyFetch();
    await postAuthStart();
    expect(fetchSpy).toHaveBeenCalledWith(
      "/console/auth/start",
      expect.objectContaining({ method: "POST", credentials: "include" }),
    );
  });

  it("reads the session with GET /console/session", async () => {
    const fetchSpy = spyFetch();
    await getConsoleSession();
    const [url, options] = fetchSpy.mock.calls[0];
    expect(url).toBe("/console/session");
    expect(options?.method).toBeUndefined(); // GET default
  });

  it("logs out with POST /console/auth/logout", async () => {
    const fetchSpy = spyFetch();
    await postLogout();
    expect(fetchSpy).toHaveBeenCalledWith(
      "/console/auth/logout",
      expect.objectContaining({ method: "POST" }),
    );
  });
});
