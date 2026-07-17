import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useUsersQuery } from "@/hooks/queries/useUsersQuery";
import * as userAPIs from "@/api/userAPIs";

const jsonRes = (body: unknown) =>
  ({ ok: true, json: async () => body }) as unknown as Response;

const wrapper = () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
};

describe("useUsersQuery", () => {
  afterEach(() => vi.restoreAllMocks());

  it("returns the parsed user page", async () => {
    const page = { total: 0, page: 1, size: 8, items: [] };
    vi.spyOn(userAPIs, "listUsers").mockResolvedValue(jsonRes(page));
    const { result } = renderHook(() => useUsersQuery({ page: 1, size: 8 }), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual(page);
  });
});
