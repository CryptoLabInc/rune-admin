import { useQuery } from "@tanstack/react-query";

import { getTeam } from "@/api/teamAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import { type TTeamDetail } from "@/types/teamTypes";

/** useTeamQuery loads one team's detail (GET /teams/{id}). */
export const useTeamQuery = (teamId: string) =>
  useQuery<TTeamDetail, Response>({
    queryKey: [QUERY_KEYS.team, teamId],
    enabled: !!teamId,
    queryFn: async () => {
      const res = await getTeam(teamId);
      if (!res.ok) throw res;
      return (await res.json()) as TTeamDetail;
    },
  });
