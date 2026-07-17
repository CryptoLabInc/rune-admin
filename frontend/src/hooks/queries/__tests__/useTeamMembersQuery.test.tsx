import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useTeamMembersQuery } from "@/hooks/queries/useTeamMembersQuery";
import * as teamMemberAPIs from "@/api/teamMemberAPIs";

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

describe("useTeamMembersQuery", () => {
  afterEach(() => vi.restoreAllMocks());

  it("returns the parsed member page", async () => {
    const page = {
      total: 1,
      page: 1,
      size: 8,
      items: [
        {
          userId: "u_1",
          account: "k@x.com",
          role: "edit",
          status: "online",
          joinedAt: "2026-07-02T00:00:00Z",
        },
      ],
    };
    vi.spyOn(teamMemberAPIs, "listTeamMembers").mockResolvedValue(
      jsonRes(page),
    );

    const { result } = renderHook(() => useTeamMembersQuery("t_1", 1, 8), {
      wrapper: wrapper(),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual(page);
  });
});
