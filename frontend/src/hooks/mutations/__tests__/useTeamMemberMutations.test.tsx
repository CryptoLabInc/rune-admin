import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useRemoveTeamMembersMutation } from "@/hooks/mutations/useTeamMemberMutations";
import * as teamMemberAPIs from "@/api/teamMemberAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";

const jsonRes = (body: unknown) =>
  ({ ok: true, json: async () => body }) as unknown as Response;

describe("useRemoveTeamMembersMutation", () => {
  afterEach(() => vi.restoreAllMocks());

  it("calls the API and invalidates member + tree keys on success", async () => {
    vi.spyOn(teamMemberAPIs, "removeTeamMembers").mockResolvedValue(
      jsonRes({ succeeded: ["u_1"], failed: [] }),
    );
    const client = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    });
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );

    const { result } = renderHook(() => useRemoveTeamMembersMutation("t_1"), {
      wrapper,
    });
    result.current.mutate(["u_1"]);

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(teamMemberAPIs.removeTeamMembers).toHaveBeenCalledWith("t_1", [
      "u_1",
    ]);
    const keys = invalidate.mock.calls.map((c) => c[0]?.queryKey?.[0]);
    expect(keys).toContain(QUERY_KEYS.teamMembers);
    expect(keys).toContain(QUERY_KEYS.teamsTree);
  });

  it("patches cached member pages from succeeded[] before any refetch", async () => {
    vi.spyOn(teamMemberAPIs, "removeTeamMembers").mockResolvedValue(
      jsonRes({
        succeeded: ["u_1"],
        failed: [{ id: "u_9", code: "X", message: "x" }],
      }),
    );
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    /* Seed the visible page cache; no observer, so no refetch can land —
       any change to this entry comes from the mutation's cache patch. */
    const pageKey = [QUERY_KEYS.teamMembers, "t_1", 1, 10];
    client.setQueryData(pageKey, {
      total: 12,
      page: 1,
      size: 10,
      items: [
        { userId: "u_1", account: "kim@corp.com", role: "read" },
        { userId: "u_2", account: "lee@corp.com", role: "edit" },
      ],
    });
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );

    const { result } = renderHook(() => useRemoveTeamMembersMutation("t_1"), {
      wrapper,
    });
    result.current.mutate(["u_1", "u_9"]);
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const page = client.getQueryData<{
      total: number;
      items: { userId: string }[];
    }>(pageKey);
    /* Only the succeeded id is dropped; the failed one stays. */
    expect(page?.items.map((m) => m.userId)).toEqual(["u_2"]);
    expect(page?.total).toBe(11);
  });
});
