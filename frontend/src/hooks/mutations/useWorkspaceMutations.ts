import { useMutation, useQueryClient } from "@tanstack/react-query";

import {
  createWorkspace,
  deleteWorkspace,
  startWorkspace,
  stopWorkspace,
} from "@/api/workspaceAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";

/*
 * Workspace lifecycle mutations (SC-02). Each op is async (202) — on success
 * we invalidate the workspace query, which refetches and (because the phase
 * is now transitional) begins polling until it settles. Errors reject with
 * the raw Response so callers can render the matching failure screen
 * (D-1 실패 / D-3 / D-4).
 */
const useWorkspaceOp = (op: () => Promise<Response>) => {
  const queryClient = useQueryClient();
  return useMutation<void, Response, void>({
    mutationFn: async () => {
      const res = await op();
      if (!res.ok) throw res;
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.workspace] }),
  });
};

/** POST /workspace — provision (state A [워크스페이스 생성]). */
export const useCreateWorkspaceMutation = () => useWorkspaceOp(createWorkspace);

/** POST /workspace/stop — [중지]. */
export const useStopWorkspaceMutation = () => useWorkspaceOp(stopWorkspace);

/** POST /workspace/start — [재실행]. */
export const useStartWorkspaceMutation = () => useWorkspaceOp(startWorkspace);

/** DELETE /workspace — [삭제] confirm. */
export const useDeleteWorkspaceMutation = () => useWorkspaceOp(deleteWorkspace);
