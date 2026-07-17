import { keepPreviousData, useQuery } from "@tanstack/react-query";

import { listTeamMembers } from "@/api/teamMemberAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import { type TPage, type TTeamMember } from "@/types/teamTypes";

/**
 * useTeamMembersQuery loads one page of a team's members. Uses
 * keepPreviousData so paging doesn't flash an empty table between pages.
 */
export const useTeamMembersQuery = (
  teamId: string,
  page: number,
  size: number,
) =>
  useQuery<TPage<TTeamMember>, Response>({
    queryKey: [QUERY_KEYS.teamMembers, teamId, page, size],
    enabled: !!teamId,
    placeholderData: keepPreviousData,
    queryFn: async () => {
      const res = await listTeamMembers(teamId, page, size);
      if (!res.ok) throw res;
      return (await res.json()) as TPage<TTeamMember>;
    },
  });
