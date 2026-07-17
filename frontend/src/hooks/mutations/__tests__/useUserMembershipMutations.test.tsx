import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useRemoveUserMemberships } from "@/hooks/mutations/useUserMembershipMutations";
import * as userAPIs from "@/api/userAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";

const jsonRes = (body: unknown) =>
  ({ ok: true, json: async () => body }) as unknown as Response;

describe("useRemoveUserMemberships", () => {
  afterEach(() => vi.restoreAllMocks());

  it("calls the API and invalidates user + users + usersStats keys on success", async () => {
    vi.spyOn(userAPIs, "removeUserMemberships").mockResolvedValue(
      jsonRes({ succeeded: ["t_1"], failed: [] }),
    );
    const client = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    });
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );

    const { result } = renderHook(() => useRemoveUserMemberships("u_1"), {
      wrapper,
    });
    result.current.mutate(["t_1"]);

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(userAPIs.removeUserMemberships).toHaveBeenCalledWith("u_1", ["t_1"]);
    const keys = invalidate.mock.calls.map((c) => c[0]?.queryKey?.[0]);
    expect(keys).toContain(QUERY_KEYS.user);
    expect(keys).toContain(QUERY_KEYS.users);
    expect(keys).toContain(QUERY_KEYS.usersStats);
  });
});
