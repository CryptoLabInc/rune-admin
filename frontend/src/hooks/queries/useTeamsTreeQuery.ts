import { useQuery } from "@tanstack/react-query";

import { getTeamsTree } from "@/api/teamAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import { type TTeamTree } from "@/types/teamTypes";

/**
 * useTeamsTreeQuery loads the flat team-node list; the tree is built on the
 * client (team CRUD API design, 2026-07-10).
 */
export const useTeamsTreeQuery = () => {
  return useQuery<TTeamTree, Response>({
    queryKey: [QUERY_KEYS.teamsTree],
    queryFn: async () => {
      const res = await getTeamsTree();
      if (!res.ok) throw res;
      const data = await res.json();
      return data as TTeamTree;
    },
  });
};
