import Button from "@/components/elements/Button";
import Notice from "@/components/elements/Notice";
import StorageStatus from "@/components/elements/StorageStatus";
import ModalLayout from "@/components/layout/ModalLayout";
import {
  useDeleteWorkspaceMutation,
  useStartWorkspaceMutation,
  useStopWorkspaceMutation,
} from "@/hooks/mutations/useWorkspaceMutations";
import {
  isTransitionalStatus,
  useWorkspaceQuery,
} from "@/hooks/queries/useWorkspaceQuery";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import { useWorkspaceStore } from "@/stores/workspaceStore";

const FAIL_COPY = {
  stop: "워크스페이스 중지에 실패했습니다. 다시 시도해주세요.",
  restart: "워크스페이스 재실행에 실패했습니다. 다시 시도해주세요.",
} as const;

const styles = {
  field: "flex items-center justify-between gap-4",
  label: "text-muted-foreground text-sm",
  value: "font-mono text-sm break-all",
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
  const { data: workspace, isError } = useWorkspaceQuery();

  const deleteConfirmOpen = useWorkspaceStore((s) => s.deleteConfirmOpen);
  const closeModal = useWorkspaceStore((s) => s.closeModal);
  const openDeleteConfirm = useWorkspaceStore((s) => s.openDeleteConfirm);

  const stopMutation = useStopWorkspaceMutation();
  const startMutation = useStartWorkspaceMutation();
  const deleteMutation = useDeleteWorkspaceMutation();

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

  /* D-1 / D-1 실패 — the 삭제 confirm dialog (replaces the detail body). */
  if (deleteConfirmOpen) {
    if (deleteMutation.isError) {
      return (
        <ModalLayout title={MODAL_TITLES.workspaceDelete} isOpen>
          <p className="text-negative text-center text-base">
            워크스페이스 삭제에 실패했습니다. 다시 시도해주세요.
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

  /* D-2 — workspace info failed to load: message + [닫기] only. */
  if (isError && !workspace) {
    return (
      <ModalLayout title={MODAL_TITLES.workspaceManage} isOpen>
        <p className="text-center text-base">
          워크스페이스 정보를 불러올 수 없습니다.
          <br />
          잠시 후 다시 시도해주세요.
        </p>
        {closeButton}
      </ModalLayout>
    );
  }

  /* D — detail view. [중지]/[재실행] and [삭제] share the bottom-right action row
     under the info box (spec 2026-07-17): [중지] shows unless stopped, [재실행]
     replaces it when stopped (spec no.6–7); [삭제] is the red-filled action
     (spec no.8). A successful stop/restart closes the modal; a failure keeps it
     open with an inline message (D-3/D-4). */
  const actionError = stopMutation.isError
    ? FAIL_COPY.stop
    : startMutation.isError
      ? FAIL_COPY.restart
      : null;

  return (
    <ModalLayout title={MODAL_TITLES.workspaceManage} isOpen>
      <div className="flex w-full flex-col gap-3">
        <div className="border-border flex flex-col gap-2.5 rounded-md border p-4">
          <div className={styles.field}>
            <span className={styles.label}>Endpoint URL</span>
            <span className={styles.value}>{workspace?.endpoint ?? "—"}</span>
          </div>
          <div className={styles.field}>
            <span className={styles.label}>상태</span>
            <StorageStatus status={status} className="cursor-default" />
          </div>
          <div className={styles.field}>
            <span className={styles.label}>Row count</span>
            <span className={styles.value}>
              {workspace?.rowCount != null
                ? workspace.rowCount.toLocaleString("en-US")
                : "—"}
            </span>
          </div>
        </div>

        {actionError && <Notice tone="error">{actionError}</Notice>}

        <div className="flex justify-end gap-4">
          {status === "stopped" ? (
            <Button
              btnText={BTN_TEXT.restart}
              btnSize="sm"
              btnColor="grayOutline"
              className="w-20"
              disabled={busy || startMutation.isPending}
              handleClick={() =>
                startMutation.mutate(undefined, { onSuccess: closeModal })
              }
            />
          ) : (
            <Button
              btnText={BTN_TEXT.stop}
              btnSize="sm"
              btnColor="grayOutline"
              className="w-20"
              disabled={busy || stopMutation.isPending}
              handleClick={() =>
                stopMutation.mutate(undefined, { onSuccess: closeModal })
              }
            />
          )}
          <Button
            btnText={BTN_TEXT.delete}
            btnSize="sm"
            btnColor="redFilled"
            className="w-20"
            disabled={busy}
            handleClick={openDeleteConfirm}
          />
        </div>
      </div>

      {closeButton}
    </ModalLayout>
  );
};

export default WorkspaceModal;
