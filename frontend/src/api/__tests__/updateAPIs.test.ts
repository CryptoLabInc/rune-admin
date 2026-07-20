import { afterEach, describe, expect, it, vi } from "vitest";

import { getSystemUpdate, postSystemUpdate } from "@/api/updateAPIs";

describe("updateAPIs", () => {
  afterEach(() => vi.restoreAllMocks());

  const spyFetch = (status = 200) =>
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: status >= 200 && status < 300,
      status,
    } as Response);

  it("reads update status from GET /api/v1/system/update", async () => {
    const fetchSpy = spyFetch();

    await getSystemUpdate();

    expect(fetchSpy).toHaveBeenCalledWith(
      "/api/v1/system/update",
      expect.objectContaining({ credentials: "include" }),
    );
    expect(fetchSpy.mock.calls[0][1]?.method).toBeUndefined();
  });

  it("queues the selected version as JSON", async () => {
    const fetchSpy = spyFetch(202);

    await postSystemUpdate("v1.1.0");

    expect(fetchSpy).toHaveBeenCalledWith(
      "/api/v1/system/update",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ version: "v1.1.0" }),
      }),
    );
  });
});
