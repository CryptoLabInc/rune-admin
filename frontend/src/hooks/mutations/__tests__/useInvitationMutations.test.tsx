import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useResendInvitation } from "@/hooks/mutations/useInvitationMutations";
import * as invitationAPIs from "@/api/invitationAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";

const jsonRes = (body: unknown) =>
  ({ ok: true, json: async () => body }) as unknown as Response;

describe("useResendInvitation", () => {
  afterEach(() => vi.restoreAllMocks());

  it("calls the API and invalidates users + usersStats + invitations + user keys on success", async () => {
    vi.spyOn(invitationAPIs, "resendInvitation").mockResolvedValue(
      jsonRes({ userId: "u_1", status: "invite_pending" }),
    );
    const client = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    });
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );

    const { result } = renderHook(() => useResendInvitation(), { wrapper });
    result.current.mutate("u_1");

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(invitationAPIs.resendInvitation).toHaveBeenCalledWith("u_1");

    const keys = invalidate.mock.calls.map((c) => c[0]?.queryKey?.[0]);
    expect(keys).toContain(QUERY_KEYS.users);
    expect(keys).toContain(QUERY_KEYS.usersStats);
    expect(keys).toContain(QUERY_KEYS.invitations);
    expect(invalidate.mock.calls).toContainEqual([
      expect.objectContaining({ queryKey: [QUERY_KEYS.user, "u_1"] }),
    ]);
  });
});
