import { Navigate, Outlet } from "react-router";

import { useSessionQuery } from "@/hooks/queries/useSessionQuery";
import { PATH_LIST } from "@/constants/commonConstants";

/**
 * RequireAuth gates the console app shell. SC-01 rule: internal pages are
 * unreachable while logged out, so an absent session redirects to the sign-in
 * screen. While the session is still resolving it renders nothing (a shared
 * loading state can slot in here later).
 */
const RequireAuth = () => {
  const { data, isPending } = useSessionQuery();
  if (isPending) return null;
  if (!data?.logged_in) return <Navigate to={PATH_LIST.login} replace />;
  return <Outlet />;
};

export default RequireAuth;
