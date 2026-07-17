import { useQuery } from "@tanstack/react-query";

import { getUsersStats } from "@/api/userAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import { type TUsersStats } from "@/types/userTypes";

/**
 * useUsersStatsQuery loads the sidebar Members badge count (GET /users/stats).
 * Polls every 60s — a client refetch policy, not a server contract.
 */
export const useUsersStatsQuery = () =>
  useQuery<TUsersStats, Response>({
    queryKey: [QUERY_KEYS.usersStats],
    refetchInterval: 60_000,
    queryFn: async () => {
      const res = await getUsersStats();
      if (!res.ok) throw res;
      return (await res.json()) as TUsersStats;
    },
  });
