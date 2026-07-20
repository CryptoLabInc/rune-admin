import { useQuery } from "@tanstack/react-query";

import { getUser } from "@/api/userAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import { type TUserListItem } from "@/types/userTypes";

/** useUserQuery loads one user's detail for the member drawer (GET /users/{id}). */
export const useUserQuery = (userId: string) =>
  useQuery<TUserListItem, Response>({
    queryKey: [QUERY_KEYS.user, userId],
    enabled: !!userId,
    queryFn: async () => {
      const res = await getUser(userId);
      if (!res.ok) throw res;
      return (await res.json()) as TUserListItem;
    },
  });
