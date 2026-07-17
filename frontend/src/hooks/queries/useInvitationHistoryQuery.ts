import { keepPreviousData, useQuery } from "@tanstack/react-query";

import { getInvitationHistory } from "@/api/invitationAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import type { TPage } from "@/types/teamTypes";
import type { TInvitationHistoryRow } from "@/types/userTypes";

/**
 * useInvitationHistoryQuery loads one page of the token issuance/access
 * history (GET /invitations?view=history — SC-16). Sort and paging run
 * server-side; the previous page is kept as placeholder data.
 */
export const useInvitationHistoryQuery = (
  sort: string,
  page: number,
  size: number,
) => {
  return useQuery<TPage<TInvitationHistoryRow>, Response>({
    queryKey: [QUERY_KEYS.invitations, sort, page, size],
    queryFn: async () => {
      const res = await getInvitationHistory(sort, page, size);
      if (!res.ok) throw res;
      return (await res.json()) as TPage<TInvitationHistoryRow>;
    },
    placeholderData: keepPreviousData,
  });
};
