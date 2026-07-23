import { useNavigate } from "react-router";

import Button from "@/components/elements/Button";
import Notice from "@/components/elements/Notice";
import TextButton from "@/components/elements/TextButton";
import WorkspaceStatus from "@/components/elements/WorkspaceStatus";
import ModalLayout from "@/components/layout/ModalLayout";
import {
  useCreateWorkspaceMutation,
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
import { L } from "@/locales";
import { useWorkspaceStore } from "@/stores/workspaceStore";

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
  // POST /workspace re-bootstraps the expired data-plane credential — the same
  // connect endpoint the empty-state create uses.
  const reconnectMutation = useCreateWorkspaceMutation();

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
          {L.workspace.recreateFailed}
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
            {L.workspace.deleteFailed}
          </p>
          {closeButton}
        </ModalLayout>
      );
    }
    return (
      <ModalLayout title={MODAL_TITLES.workspaceDelete} isOpen>
        <p className="text-center text-base">
          {L.workspace.deleteConfirm}
          <br />
          {L.workspace.deleteIrreversible}
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
            {L.workspace.tearingDown1}
            <br />
            {L.workspace.tearingDown2}
          </p>
        ) : (
          <p className="text-center text-base">
            {L.workspace.orphaned1}
            <br />
            {L.workspace.orphaned2}
            <br />
            {L.workspace.orphaned3}
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

  /* Reconnect — the data-plane credential expired. The cloud workspace is
     healthy; POST /workspace re-bootstraps the local engine link. Takes
     precedence over the detail body (the workspace can't serve until the link
     is restored) and is mutually exclusive with orphaned — the backend returns
     the orphaned flag first, so both are never set at once. */
  if (workspace?.reconnectRequired) {
    return (
      <ModalLayout title={MODAL_TITLES.workspaceReconnect} isOpen>
        <p className="text-center text-base">
          {L.workspace.reconnectExpired}
          <br />
          {L.workspace.reconnectPrompt}
        </p>
        {reconnectMutation.isError && (
          <Notice tone="error">{L.workspace.reconnectFailed}</Notice>
        )}
        <div className="flex w-full gap-2">
          <Button
            btnText={BTN_TEXT.close}
            btnSize="md"
            btnColor="grayOutline"
            disabled={reconnectMutation.isPending}
            handleClick={closeModal}
          />
          <Button
            btnText={BTN_TEXT.reconnect}
            btnSize="md"
            btnColor="mintFilled"
            disabled={reconnectMutation.isPending}
            handleClick={() =>
              reconnectMutation.mutate(undefined, { onSuccess: closeModal })
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
          {L.workspace.loadFailed}
          <br />
          {L.common.tryAgainLater}
        </p>
        {closeButton}
      </ModalLayout>
    );
  }

  const actionError = stopMutation.isError
    ? L.workspace.stopFailed
    : startMutation.isError
      ? L.workspace.restartFailed
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
          <span className={styles.label}>{L.workspace.plan}</span>
          <span className={styles.label}>Free</span>
        </div>
        <div className={styles.field}>
          <span className={styles.label}>{L.workspace.statusLabel}</span>
          <WorkspaceStatus status={status} className="cursor-default" />
        </div>
        <div className={styles.field}>
          <span className={styles.label}>{L.workspace.storedMemories}</span>
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
