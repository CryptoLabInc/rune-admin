import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useRecreateWorkspaceMutation } from "@/hooks/mutations/useWorkspaceMutations";
import * as workspaceAPIs from "@/api/workspaceAPIs";

const res = (status: number) =>
  ({ ok: status < 400, status }) as unknown as Response;

const renderMutation = () => {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return renderHook(() => useRecreateWorkspaceMutation(), { wrapper });
};

describe("useRecreateWorkspaceMutation", () => {
  afterEach(() => vi.restoreAllMocks());

  it("deletes and waits for teardown — the create is handed off to the page", async () => {
    vi.spyOn(workspaceAPIs, "deleteWorkspace").mockResolvedValue(res(202));
    vi.spyOn(workspaceAPIs, "getWorkspace").mockResolvedValue(res(404));
    const create = vi.spyOn(workspaceAPIs, "createWorkspace");

    const { result } = renderMutation();
    result.current.mutate();

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(workspaceAPIs.deleteWorkspace).toHaveBeenCalledTimes(1);
    expect(create).not.toHaveBeenCalled();
  });

  it("surfaces a delete failure without polling for teardown", async () => {
    vi.spyOn(workspaceAPIs, "deleteWorkspace").mockResolvedValue(res(500));
    const get = vi.spyOn(workspaceAPIs, "getWorkspace");

    const { result } = renderMutation();
    result.current.mutate();

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(get).not.toHaveBeenCalled();
  });
});
