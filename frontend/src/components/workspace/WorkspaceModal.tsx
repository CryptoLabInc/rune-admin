import Button from "@/components/elements/Button";
import Notice from "@/components/elements/Notice";
import StorageStatus from "@/components/elements/StorageStatus";
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
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import { useWorkspaceStore } from "@/stores/workspaceStore";

const FAIL_COPY = {
  stop: "워크스페이스 중지에 실패했습니다. 다시 시도해 주세요.",
  restart: "워크스페이스 재실행에 실패했습니다. 다시 시도해 주세요.",
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

  /* Recreate (orphan remediation) in flight or failed. Checked before the live
     query is read: recreate deletes then creates, so mid-flight the workspace
     404s (→ query null) and the orphaned flag drops — this keeps the 재생성 중 /
     실패 body on screen regardless. Mount is fresh per open, so a failure clears
     on reopen. */
  if (recreateMutation.isPending) {
    return (
      <ModalLayout title={MODAL_TITLES.workspaceOrphaned} isOpen>
        <p className="text-center text-base">
          워크스페이스를 재생성하는 중입니다…
          <br />
          생성까지 약 3~5분 정도 소요됩니다.
        </p>
      </ModalLayout>
    );
  }
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
     over the detail body: an orphaned workspace is not usable. */
  if (workspace?.orphaned) {
    return (
      <ModalLayout title={MODAL_TITLES.workspaceOrphaned} isOpen>
        <p className="text-center text-base">
          콘솔이 재설치되어 이 워크스페이스와 연결할 수 없습니다.
          <br />
          기존에 저장된 기억(memory)은 이전 보안 키로 암호화되어 복구할 수 없습니다.
          <br />
          삭제 후 재생성하면 빈 워크스페이스로 다시 시작합니다.
        </p>
        <div className="flex w-full gap-2">
          <Button
            btnText={BTN_TEXT.close}
            btnSize="md"
            btnColor="grayOutline"
            handleClick={closeModal}
          />
          <Button
            btnText={BTN_TEXT.recreate}
            btnSize="md"
            btnColor="redFilled"
            handleClick={() =>
              recreateMutation.mutate(undefined, { onSuccess: closeModal })
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
