import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useSessionQuery } from "@/hooks/queries/useSessionQuery";
import * as authAPIs from "@/api/authAPIs";

const wrapper = () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
};

const jsonRes = (body: unknown) =>
  ({ ok: true, json: async () => body }) as unknown as Response;

describe("useSessionQuery", () => {
  afterEach(() => vi.restoreAllMocks());

  it("returns the parsed logged-in session", async () => {
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({
        logged_in: true,
        expires_at: "2026-07-16T21:00:00Z",
        me: { email: "admin@corp.com", avatar: "a.png" },
      }),
    );

    const { result } = renderHook(() => useSessionQuery(), {
      wrapper: wrapper(),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual({
      logged_in: true,
      expires_at: "2026-07-16T21:00:00Z",
      me: { email: "admin@corp.com", avatar: "a.png" },
    });
  });
});
