import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useUpdateMutation } from "@/hooks/mutations/useUpdateMutation";
import * as updateAPIs from "@/api/updateAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import type { TSystemUpdateStatus } from "@/types/updateTypes";

const response = (status: number) =>
  ({ ok: status >= 200 && status < 300, status }) as Response;

const renderMutation = () => {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  client.setQueryData<TSystemUpdateStatus>([QUERY_KEYS.systemUpdate], {
    currentVersion: "v1.0.0",
    targetVersion: "v1.1.0",
    updateAvailable: true,
    capable: true,
    state: "idle",
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return { client, ...renderHook(() => useUpdateMutation(), { wrapper }) };
};

describe("useUpdateMutation", () => {
  afterEach(() => vi.restoreAllMocks());

  it("queues the target and updates the shared status", async () => {
    const post = vi
      .spyOn(updateAPIs, "postSystemUpdate")
      .mockResolvedValue(response(202));
    const { client, result } = renderMutation();

    result.current.mutate("v1.1.0");

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(post).toHaveBeenCalledWith("v1.1.0");
    expect(
      client.getQueryData<TSystemUpdateStatus>([QUERY_KEYS.systemUpdate]),
    ).toEqual(
      expect.objectContaining({
        targetVersion: "v1.1.0",
        state: "queued",
      }),
    );
  });

  it("rejects an unexpected successful status instead of assuming it queued", async () => {
    vi.spyOn(updateAPIs, "postSystemUpdate").mockResolvedValue(response(200));
    const { result } = renderMutation();

    result.current.mutate("v1.1.0");

    await waitFor(() => expect(result.current.isError).toBe(true));
  });

  it("refreshes server status immediately when queueing fails", async () => {
    vi.spyOn(updateAPIs, "postSystemUpdate").mockResolvedValue(response(409));
    const { client, result } = renderMutation();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    result.current.mutate("v1.1.0");

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: [QUERY_KEYS.systemUpdate],
    });
  });
});
