import { useNavigate } from "react-router";

import Button from "@/components/elements/Button";
import Notice from "@/components/elements/Notice";
import TextButton from "@/components/elements/TextButton";
import WorkspaceStatus from "@/components/elements/WorkspaceStatus";
import ModalLayout from "@/components/layout/ModalLayout";
import {
  useDeleteWorkspaceMutation,
  useRecreateWorkspaceMutation,
  useStartWorkspaceMutation,
  useStopWorkspaceMutation,
} from "@/hooks/mutations/useWorkspaceMutations";
import {
  isTransitionalStatus,
  useWorkspaceQuery,
} from "@/hooks/queries/useWorkspaceQuery";
import {
  BTN_TEXT,
  MODAL_TITLES,
  PATH_LIST,
  WORKSPACE_MAX_MEMORIES,
} from "@/constants/commonConstants";
import { useWorkspaceStore } from "@/stores/workspaceStore";

const FAIL_COPY = {
  stop: "워크스페이스 중지에 실패했습니다. 다시 시도해 주세요.",
  restart: "워크스페이스 재실행에 실패했습니다. 다시 시도해 주세요.",
} as const;

const styles = {
  field: "flex items-center justify-between gap-4",
  label: "text-muted-foreground text-md",
  value: "font-mono text-md break-all",
};

/** 저장된 기억 개수 usage line — count / plan max (percent of the cap,
 * clamped to 100 even if the stored count overshoots the plan max). */
const memoryUsage = (rowCount: number): string => {
  const percent = Math.min(
    100,
    Math.round((rowCount / WORKSPACE_MAX_MEMORIES) * 100),
  );
  return `${rowCount.toLocaleString("en-US")} / ${WORKSPACE_MAX_MEMORIES.toLocaleString("en-US")} (${percent}%)`;
};

/**
 * WorkspaceModal is the rune 워크스페이스 관리 modal (wireframe SC-02
 * state D + variants). One shell, four bodies: detail (D), 삭제 확인 (D-1),
 * 삭제 실패 (D-1 실패), 불러오기 실패 (D-2). Stop/restart failures (D-3/D-4)
 * stay on the detail body with an inline message. The workspace record comes
 * from useWorkspaceQuery; lifecycle actions are async mutations. Mount
 * conditionally on the store's modalOpen so each open starts with fresh
 * mutation state.
 */
const WorkspaceModal = () => {
  const navigate = useNavigate();
  const { data: workspace, isError } = useWorkspaceQuery();

  const deleteConfirmOpen = useWorkspaceStore((s) => s.deleteConfirmOpen);
  const closeModal = useWorkspaceStore((s) => s.closeModal);
  const openDeleteConfirm = useWorkspaceStore((s) => s.openDeleteConfirm);
  const beginRecreateHandoff = useWorkspaceStore((s) => s.beginRecreateHandoff);

  const stopMutation = useStopWorkspaceMutation();
  const startMutation = useStartWorkspaceMutation();
  const deleteMutation = useDeleteWorkspaceMutation();
  const recreateMutation = useRecreateWorkspaceMutation();

  const status = workspace?.status ?? "error";
  /* Transitional phases (+ any request in flight) lock the actions. */
  const busy = workspace ? isTransitionalStatus(status) : false;

  const closeButton = (
    <Button
      btnText={BTN_TEXT.close}
      btnSize="md"
      btnColor="grayOutline"
      handleClick={closeModal}
    />
  );

  /* Recreate teardown (orphan remediation) failed — delete rejected or the
     404 poll timed out. Mount is fresh per open, so the failure clears on
     reopen (and reopening offers the retry). */
  if (recreateMutation.isError) {
    return (
      <ModalLayout title={MODAL_TITLES.workspaceOrphaned} isOpen>
        <p className="text-negative text-center text-base">
          워크스페이스 재생성에 실패했습니다. 다시 시도해 주세요.
        </p>
        {closeButton}
      </ModalLayout>
    );
  }

  /* D-1 / D-1 실패 — the 삭제 confirm dialog (replaces the detail body). */
  if (deleteConfirmOpen) {
    if (deleteMutation.isError) {
      return (
        <ModalLayout title={MODAL_TITLES.workspaceDelete} isOpen>
          <p className="text-negative text-center text-base">
            워크스페이스 삭제에 실패했습니다. 다시 시도해 주세요.
          </p>
          {closeButton}
        </ModalLayout>
      );
    }
    return (
      <ModalLayout title={MODAL_TITLES.workspaceDelete} isOpen>
        <p className="text-center text-base">
          워크스페이스를 삭제하시겠습니까?
          <br />
          삭제 후에는 되돌릴 수 없습니다.
        </p>
        <div className="flex w-full gap-2">
          <Button
            btnText={BTN_TEXT.close}
            btnSize="md"
            btnColor="grayOutline"
            handleClick={closeModal}
          />
          <Button
            btnText={BTN_TEXT.delete}
            btnSize="md"
            btnColor="redFilled"
            disabled={deleteMutation.isPending}
            handleClick={() =>
              deleteMutation.mutate(undefined, { onSuccess: closeModal })
            }
          />
        </div>
      </ModalLayout>
    );
  }

  /* Orphaned — the cloud-held workspace no longer matches this console (a
     reinstall minted a fresh team_secret), so its stored data is encrypted under
     a key we no longer have. It cannot be adopted, only deleted + recreated.
     Derived from the query data (like D-2), not a store flag. Takes precedence
     over the detail body: an orphaned workspace is not usable.

     Also rendered while the teardown (delete + 404 poll) is in flight — mid-
     teardown the workspace 404s and the orphaned flag drops, so the pending
     check can't rely on the query. The buttons stay visible but disabled so
     the modal keeps its shape; once the teardown confirms the 404, the modal
     hands off to WorkspacePage, which auto-starts the create and owns the
     생성 중 UI (same as a first-time create). */
  if (recreateMutation.isPending || workspace?.orphaned) {
    const tearingDown = recreateMutation.isPending;
    return (
      <ModalLayout title={MODAL_TITLES.workspaceOrphaned} isOpen>
        {tearingDown ? (
          <p className="text-center text-base">
            기존 워크스페이스를 삭제하는 중입니다…
            <br />
            삭제가 완료되면 워크스페이스 생성을 시작합니다.
          </p>
        ) : (
          <p className="text-center text-base">
            콘솔이 재설치되어 이 워크스페이스와 연결할 수 없습니다.
            <br />
            기존에 저장된 데이터는 이전 보안 키로 암호화되어 복구할 수 없습니다.
            <br />
            삭제 후 재생성하면 빈 워크스페이스로 다시 시작합니다.
          </p>
        )}
        <div className="flex w-full gap-2">
          <Button
            btnText={BTN_TEXT.close}
            btnSize="md"
            btnColor="grayOutline"
            disabled={tearingDown}
            handleClick={closeModal}
          />
          <Button
            btnText={BTN_TEXT.recreate}
            btnSize="md"
            btnColor="redFilled"
            disabled={tearingDown}
            handleClick={() =>
              recreateMutation.mutate(undefined, {
                onSuccess: () => {
                  beginRecreateHandoff();
                  navigate(PATH_LIST.workspace);
                },
              })
            }
          />
        </div>
      </ModalLayout>
    );
  }

  /* D-2 — workspace info failed to load */
  if (isError && !workspace) {
    return (
      <ModalLayout title={MODAL_TITLES.workspaceManage} isOpen>
        <p className="text-center text-base">
          워크스페이스 정보를 불러올 수 없습니다.
          <br />
          잠시 후 다시 시도해 주세요.
        </p>
        {closeButton}
      </ModalLayout>
    );
  }

  const actionError = stopMutation.isError
    ? FAIL_COPY.stop
    : startMutation.isError
      ? FAIL_COPY.restart
      : null;

  return (
    <ModalLayout title={MODAL_TITLES.workspaceManage} isOpen>
      <div className="flex w-full flex-col gap-4">
        {/* Lifecycle actions sit at the content's top-right as quiet
            TextButtons — the info fields carry the primary reading weight. */}
        <div className="flex justify-end gap-2">
          {status === "stopped" ? (
            <TextButton
              btnText={BTN_TEXT.restart}
              disabled={busy || startMutation.isPending}
              handleClick={() =>
                startMutation.mutate(undefined, { onSuccess: closeModal })
              }
            />
          ) : (
            <TextButton
              btnText={BTN_TEXT.stop}
              disabled={busy || stopMutation.isPending}
              handleClick={() =>
                stopMutation.mutate(undefined, { onSuccess: closeModal })
              }
            />
          )}
          <TextButton
            btnText={BTN_TEXT.delete}
            tone="red"
            disabled={busy}
            handleClick={openDeleteConfirm}
          />
        </div>

        <div className={styles.field}>
          <span className={styles.label}>플랜</span>
          <span className={styles.label}>Free</span>
        </div>
        <div className={styles.field}>
          <span className={styles.label}>상태</span>
          <WorkspaceStatus status={status} className="cursor-default" />
        </div>
        <div className={styles.field}>
          <span className={styles.label}>저장된 기억 개수</span>
          <span className={styles.value}>
            {workspace?.rowCount != null
              ? memoryUsage(workspace.rowCount)
              : "—"}
          </span>
        </div>

        {actionError && <Notice tone="error">{actionError}</Notice>}
      </div>

      {closeButton}
    </ModalLayout>
  );
};

export default WorkspaceModal;
