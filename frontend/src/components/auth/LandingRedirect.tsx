import { Navigate } from "react-router";

import { useWorkspaceQuery } from "@/hooks/queries/useWorkspaceQuery";
import { PATH_LIST } from "@/constants/commonConstants";

/**
 * LandingRedirect resolves the console's initial entry point (route index "/").
 * A freshly signed-in admin arrives here from the OAuth callback; if no
 * workspace exists yet (SC-02 state A) they land on the empty-workspace page so
 * provisioning is the first thing they see, otherwise on 팀 관리 (the default
 * landing). This decision fires only at the entry point — navigating to
 * teams/members afterwards is never redirected. While the workspace query is
 * still resolving it renders nothing; a query error falls back to 팀 관리.
 */
const LandingRedirect = () => {
  const { data: workspace, isPending } = useWorkspaceQuery();
  if (isPending) return null;
  return (
    <Navigate
      to={workspace === null ? PATH_LIST.workspace : PATH_LIST.teams}
      replace
    />
  );
};

export default LandingRedirect;
