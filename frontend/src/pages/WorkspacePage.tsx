import { useEffect, useState } from "react";
import { Navigate } from "react-router";

import Button from "@/components/elements/Button";
import Feedback from "@/components/elements/Feedback";
import { useCreateWorkspaceMutation } from "@/hooks/mutations/useWorkspaceMutations";
import {
  isTransitionalStatus,
  useWorkspaceQuery,
} from "@/hooks/queries/useWorkspaceQuery";
import { PATH_LIST } from "@/constants/commonConstants";
import { useWorkspaceStore } from "@/stores/workspaceStore";

const panelClass =
  "m-6 flex min-h-[340px] flex-col items-center justify-center gap-3 text-center";

const createButton = (label: string, onClick: () => void) => (
  <Button
    btnText={label}
    btnSize="md"
    btnColor="mintFilled"
    className="w-fit"
    handleClick={onClick}
  />
);

/**
 * WorkspacePage is the empty-workspace landing (wireframe SC-02 states
 * A/B/C): 생성된 워크스페이스가 없습니다 with a create action, the create
 * spinner (provisioning), and the create-failed retry. It renders only while
 * no workspace exists; once one does it redirects to 팀 관리, and after a
 * create it started, it opens the management modal there (spec no.3).
 */
const WorkspacePage = () => {
  const { data: workspace, isLoading } = useWorkspaceQuery();
  const createMutation = useCreateWorkspaceMutation();
  const openModal = useWorkspaceStore((s) => s.openModal);
  const recreateHandoff = useWorkspaceStore((s) => s.recreateHandoff);
  const clearRecreateHandoff = useWorkspaceStore((s) => s.clearRecreateHandoff);
  /* Marks a create started here, so we auto-open the modal once it runs. */
  const [createdHere, setCreatedHere] = useState(false);

  useEffect(() => {
    if (workspace?.status === "running" && createdHere) {
      setCreatedHere(false);
      openModal();
    }
  }, [workspace?.status, createdHere, openModal]);

  /* 삭제 후 재생성 handoff (orphan remediation): the modal already tore the
     old workspace down, so pick up with a plain create — from here the flow
     is identical to a first-time create. mutate is referentially stable. */
  const { mutate: createWorkspace } = createMutation;
  useEffect(() => {
    if (recreateHandoff) {
      clearRecreateHandoff();
      setCreatedHere(true);
      createWorkspace();
    }
  }, [recreateHandoff, clearRecreateHandoff, createWorkspace]);

  const exists = workspace != null;
  const transitional = exists && isTransitionalStatus(workspace.status);

  if (isLoading) return <section aria-label="워크스페이스 관리" />;

  /* A workspace exists → go to the console. The one exception is our own
     create still provisioning: stay and keep the spinner until it runs. */
  if (exists && !(createdHere && transitional)) {
    return <Navigate to={PATH_LIST.teams} replace />;
  }

  const handleCreate = () => {
    setCreatedHere(true);
    createMutation.mutate();
  };

  /* recreateHandoff keeps the spinner up on the first paint, before the
     auto-create effect has fired. */
  const creating =
    recreateHandoff ||
    createMutation.isPending ||
    (createMutation.isSuccess && !exists) ||
    transitional;

  return (
    <section aria-label="워크스페이스 관리">
      {creating ? (
        <Feedback
          state="loading"
          title="워크스페이스를 생성하는 중입니다…"
          description="생성까지 약 3~5분 정도 소요됩니다."
          className={panelClass}
        />
      ) : createMutation.isError ? (
        <Feedback
          state="error"
          title="워크스페이스 생성 실패"
          description="워크스페이스를 생성할 수 없습니다. 다시 시도해 주세요."
          className={panelClass}
          action={createButton("워크스페이스 생성", handleCreate)}
        />
      ) : (
        <Feedback
          state="empty"
          title="생성된 워크스페이스가 없습니다."
          description="워크스페이스를 생성하면 기억(memory)을 저장할 수 있습니다."
          className={panelClass}
          action={createButton("워크스페이스 생성", handleCreate)}
        />
      )}
    </section>
  );
};

export default WorkspacePage;
