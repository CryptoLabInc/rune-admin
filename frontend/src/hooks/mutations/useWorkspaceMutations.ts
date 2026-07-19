import { useMutation, useQueryClient } from "@tanstack/react-query";

import {
  createWorkspace,
  deleteWorkspace,
  getWorkspace,
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

/** How often, and for how long, recreate polls for the old workspace to vanish. */
const RECREATE_POLL_MS = 3000;
const RECREATE_TIMEOUT_MS = 180000; // teardown (StatefulSet + volume) can take minutes

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

/**
 * waitForTeardown polls GET /workspace until it 404s — i.e. the cloud has fully
 * deprovisioned the runespace and dropped its row. Delete is async (202) and the
 * cloud 409s a create while the old row still exists, so recreate must wait here
 * before creating. Throws the last Response if teardown outruns the timeout.
 */
const waitForTeardown = async () => {
  const deadline = Date.now() + RECREATE_TIMEOUT_MS;
  for (;;) {
    const res = await getWorkspace();
    if (res.status === 404) return;
    if (Date.now() > deadline) throw res;
    await sleep(RECREATE_POLL_MS);
  }
};

/**
 * Recreate an orphaned workspace (console reinstalled → team_secret changed, so
 * the cloud-held runespace can no longer be decrypted). It can only be discarded
 * and rebuilt: delete, wait for the async teardown to finish (poll until 404),
 * then create — the create carries this console's new team_secret fingerprint, so
 * the rebuilt workspace matches. On success the query is invalidated and the new
 * (provisioning) workspace begins polling.
 */
export const useRecreateWorkspaceMutation = () => {
  const queryClient = useQueryClient();
  return useMutation<void, Response, void>({
    mutationFn: async () => {
      const del = await deleteWorkspace();
      if (!del.ok) throw del;
      await waitForTeardown();
      const created = await createWorkspace();
      if (!created.ok) throw created;
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.workspace] }),
  });
};
