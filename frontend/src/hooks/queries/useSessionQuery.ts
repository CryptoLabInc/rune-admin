import { useQuery } from "@tanstack/react-query";

import { getConsoleSession } from "@/api/authAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import { type TSession } from "@/types/authTypes";

/**
 * useSessionQuery reads /console/session — the route guard's single source of
 * truth. The endpoint always returns 200, so the error branch is defensive.
 */
export const useSessionQuery = () =>
  useQuery<TSession, Response>({
    queryKey: [QUERY_KEYS.session],
    queryFn: async () => {
      const res = await getConsoleSession();
      if (!res.ok) throw res;
      return (await res.json()) as TSession;
    },
    retry: false,
  });
