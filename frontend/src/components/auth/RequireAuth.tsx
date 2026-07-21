import { Navigate, Outlet } from "react-router";

import OwnerLockedNotice from "@/components/auth/OwnerLockedNotice";
import { useSessionQuery } from "@/hooks/queries/useSessionQuery";
import { PATH_LIST } from "@/constants/commonConstants";

/**
 * RequireAuth gates the console app shell. SC-01 rule: internal pages are
 * unreachable while logged out, so an absent session redirects to the sign-in
 * screen. While the session is still resolving it renders nothing (a shared
 * loading state can slot in here later).
 *
 * Single-admin soft block: a signed-in account that is not the console owner
 * (is_owner === false) reaches the app but cannot use it — show the owner-locked
 * notice instead of the shell. is_owner is only ever explicitly false here; an
 * older backend that omits it (undefined) is treated as the owner (no gate).
 */
const RequireAuth = () => {
  const { data, isPending } = useSessionQuery();
  if (isPending) return null;
  if (!data?.logged_in) return <Navigate to={PATH_LIST.login} replace />;
  if (data.is_owner === false)
    return <OwnerLockedNotice owner={data.owner_email} />;
  return <Outlet />;
};

export default RequireAuth;
