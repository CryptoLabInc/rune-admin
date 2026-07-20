import { keepPreviousData, useQuery } from "@tanstack/react-query";

import { listUsers } from "@/api/userAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import { type TPage } from "@/types/teamTypes";
import { type TUserListItem, type TUsersQueryParams } from "@/types/userTypes";

/**
 * useUsersQuery loads one page of the cross-team user list (GET /users).
 * The whole params object is in the key, so any filter/sort/page change
 * refetches; keepPreviousData avoids an empty-table flash between pages.
 */
export const useUsersQuery = (params: TUsersQueryParams) =>
  useQuery<TPage<TUserListItem>, Response>({
    queryKey: [QUERY_KEYS.users, params],
    placeholderData: keepPreviousData,
    queryFn: async () => {
      const res = await listUsers(params);
      if (!res.ok) throw res;
      return (await res.json()) as TPage<TUserListItem>;
    },
  });
