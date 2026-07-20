import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useUpdateQuery } from "@/hooks/queries/useUpdateQuery";
import * as updateAPIs from "@/api/updateAPIs";

const jsonRes = (body: unknown) =>
  ({ ok: true, status: 200, json: async () => body }) as unknown as Response;

const wrapper = () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
};

describe("useUpdateQuery", () => {
  afterEach(() => vi.restoreAllMocks());

  it("returns the server update status", async () => {
    vi.spyOn(updateAPIs, "getSystemUpdate").mockResolvedValue(
      jsonRes({
        currentVersion: "v1.0.0",
        targetVersion: "v1.1.0",
        updateAvailable: true,
        capable: true,
        state: "idle",
      }),
    );

    const { result } = renderHook(() => useUpdateQuery(), {
      wrapper: wrapper(),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual({
      currentVersion: "v1.0.0",
      targetVersion: "v1.1.0",
      updateAvailable: true,
      capable: true,
      state: "idle",
    });
  });

  it("keeps an unavailable status as normal data", async () => {
    vi.spyOn(updateAPIs, "getSystemUpdate").mockResolvedValue(
      jsonRes({
        currentVersion: "v1.1.0",
        updateAvailable: false,
        capable: true,
        state: "idle",
      }),
    );

    const { result } = renderHook(() => useUpdateQuery(), {
      wrapper: wrapper(),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.updateAvailable).toBe(false);
  });
});
