import { useMutation, useQueryClient } from "@tanstack/react-query";

import { postSystemUpdate } from "@/api/updateAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import type { TSystemUpdateStatus } from "@/types/updateTypes";

/** Queue the selected release and immediately move the shared status to queued. */
export const useUpdateMutation = () => {
  const queryClient = useQueryClient();

  return useMutation<void, Response, string>({
    mutationFn: async (version) => {
      const res = await postSystemUpdate(version);
      if (res.status !== 202) throw res;
    },
    onSuccess: (_, version) => {
      queryClient.setQueryData<TSystemUpdateStatus>(
        [QUERY_KEYS.systemUpdate],
        (current) =>
          current
            ? {
                ...current,
                targetVersion: version,
                state: "queued",
              }
            : current,
      );
    },
    onError: () => {
      // A 409 normally means the server's pinned latest version or job state
      // changed after this tab rendered. Refresh now instead of leaving a
      // stale retry card on the one-hour idle polling interval.
      void queryClient.invalidateQueries({
        queryKey: [QUERY_KEYS.systemUpdate],
      });
    },
  });
};
