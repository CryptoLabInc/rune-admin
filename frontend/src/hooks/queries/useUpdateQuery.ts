import { useQuery } from "@tanstack/react-query";

import { getSystemUpdate } from "@/api/updateAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import type { TSystemUpdateStatus } from "@/types/updateTypes";

const ACTIVE_POLL_MS = 2000;
const IDLE_POLL_MS = 60 * 60 * 1000;

export const isSystemUpdateActive = (state: TSystemUpdateStatus["state"]) =>
  state === "queued" || state === "running";

/**
 * Checks for a release without disturbing the app when GitHub or the local
 * update agent is unavailable. Once an update is accepted, polling speeds up
 * so the floating card survives the expected daemon restart window.
 */
export const useUpdateQuery = () =>
  useQuery<TSystemUpdateStatus, Response>({
    queryKey: [QUERY_KEYS.systemUpdate],
    queryFn: async () => {
      const res = await getSystemUpdate();
      if (!res.ok) throw res;
      return (await res.json()) as TSystemUpdateStatus;
    },
    retry: false,
    refetchInterval: (query) =>
      query.state.data && isSystemUpdateActive(query.state.data.state)
        ? ACTIVE_POLL_MS
        : IDLE_POLL_MS,
    refetchIntervalInBackground: true,
  });
