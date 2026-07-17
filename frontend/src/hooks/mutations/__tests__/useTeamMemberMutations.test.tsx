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
});
