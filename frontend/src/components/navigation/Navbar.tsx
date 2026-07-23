import { Link, useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";

import RuneMark from "@/components/elements/RuneMark";
import WorkspaceStatus from "@/components/elements/WorkspaceStatus";
import LanguageToggle from "@/components/navigation/LanguageToggle";
import ProfileMenu from "@/components/navigation/ProfileMenu";
import WorkspaceModal from "@/components/workspace/WorkspaceModal";
import { useSessionQuery } from "@/hooks/queries/useSessionQuery";
import { useWorkspaceQuery } from "@/hooks/queries/useWorkspaceQuery";
import { postLogout } from "@/api/authAPIs";
import {
  BRAND_WORDMARK,
  PATH_LIST,
  QUERY_KEYS,
} from "@/constants/commonConstants";
import { L } from "@/locales";
import { useWorkspaceStore } from "@/stores/workspaceStore";

/**
 * Navbar is the console top bar (SC-03). Its background and bottom border
 * span the full viewport width while the inner content stays within the
 * 1380px content width.
 *
 * The rune slot (SC-03 callout 2) tracks workspace state: a clickable
 * WorkspaceStatus pill that opens the management modal (SC-02 state D)
 * when a workspace exists, or a [워크스페이스 없음] indicator that routes
 * to the empty-workspace page (SC-02 state A) when none does.
 */
const Navbar = () => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const { data: session } = useSessionQuery();
  const me = session?.logged_in ? session.me : null;
  const { data: workspace } = useWorkspaceQuery();
  const modalOpen = useWorkspaceStore((s) => s.modalOpen);
  const openModal = useWorkspaceStore((s) => s.openModal);

  const handleSignOut = async () => {
    try {
      await postLogout(); // idempotent server-side
    } finally {
      queryClient.setQueryData([QUERY_KEYS.session], { logged_in: false });
      navigate(PATH_LIST.login);
    }
  };

  return (
    <header className="bg-background border-b">
      <div className="max-w-content mx-auto flex h-14 w-full items-center justify-between px-6">
        <Link
          to={PATH_LIST.home}
          className="flex items-center gap-2 text-lg font-semibold"
        >
          <RuneMark />
          {BRAND_WORDMARK}
        </Link>
        <div className="flex items-center gap-4">
          {/* rune status badge / [워크스페이스 없음] (SC-03 callout 2). While
              the query is still loading (workspace === undefined) the slot
              stays empty. */}
          <div
            className={`flex items-baseline gap-2 rounded px-3 py-1 ${
              workspace ? "cursor-pointer" : ""
            }`}
            onClick={workspace ? openModal : undefined}
          >
            <span className="text-foreground text-sm">
              {L.workspace.badgeLabel}
            </span>
            {workspace ? (
              workspace.orphaned ? (
                /* Reinstall detected: the workspace no longer matches this
                   console. Flag it so the badge doesn't read as healthy;
                   clicking opens the modal's 재생성 prompt. */
                <span className="border-negative text-negative rounded border px-2 py-1 text-xs">
                  {L.workspace.badgeRecreate}
                </span>
              ) : workspace.reconnectRequired ? (
                /* Data-plane credential expired: the cloud workspace is fine but
                   the local engine link is stale. Flag it (not the healthy pill);
                   clicking opens the modal's 재연결 prompt. */
                <span className="border-warning text-warning rounded border px-2 py-1 text-xs">
                  {L.workspace.badgeReconnect}
                </span>
              ) : (
                <WorkspaceStatus status={workspace.status} />
              )
            ) : workspace === null ? (
              <button
                type="button"
                className="text-muted-foreground hover:text-foreground cursor-pointer rounded border px-2 py-1 text-xs"
                onClick={() => navigate(PATH_LIST.workspace)}
              >
                {L.workspace.badgeNone}
              </button>
            ) : null}
          </div>
          <LanguageToggle />
          {me && <ProfileMenu me={me} onSignOut={handleSignOut} />}
        </div>
      </div>
      {modalOpen && <WorkspaceModal />}
    </header>
  );
};

export default Navbar;
