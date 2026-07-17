import { Link, useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";

import RuneMark from "@/components/elements/RuneMark";
import StorageStatus from "@/components/elements/StorageStatus";
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
import { useWorkspaceStore } from "@/stores/workspaceStore";

/**
 * Navbar is the console top bar (SC-03). Its background and bottom border
 * span the full viewport width while the inner content stays within the
 * 1380px content width.
 *
 * The rune slot (SC-03 callout 2) tracks workspace state: a clickable
 * StorageStatus pill that opens the management modal (SC-02 state D)
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
        <div className="flex items-center gap-3">
          {/* rune status badge / [워크스페이스 없음] (SC-03 callout 2). While
              the query is still loading (workspace === undefined) the slot
              stays empty. */}
          {workspace ? (
            <StorageStatus status={workspace.status} onClick={openModal} />
          ) : workspace === null ? (
            <button
              type="button"
              className="text-muted-foreground hover:text-foreground rounded border px-2 py-0.5 text-xs"
              onClick={() => navigate(PATH_LIST.workspace)}
            >
              워크스페이스 없음
            </button>
          ) : null}
          {me && <ProfileMenu me={me} onSignOut={handleSignOut} />}
        </div>
      </div>
      {modalOpen && <WorkspaceModal />}
    </header>
  );
};

export default Navbar;
