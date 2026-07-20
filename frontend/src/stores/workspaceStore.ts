import { create } from "zustand";

/*
 * UI-only state for the rune 워크스페이스 관리 modal (SC-02). Server state
 * (the workspace record + its lifecycle) lives in react-query
 * (useWorkspaceQuery / useWorkspaceMutations); this store holds just what
 * the modal needs to remember across renders: whether it is open, whether
 * the 삭제 confirm dialog (state D-1) has replaced the detail body, and the
 * orphan-recreate handoff to the 생성 중 page.
 *
 * The other modal bodies are derived, not stored: 불러오기 실패 (D-2) from the
 * query error, and stop/restart/delete failures (D-3/D-4/D-1 실패) from the
 * per-mount mutation state.
 */
interface WorkspaceUIState {
  modalOpen: boolean;
  /** D-1 삭제 확인 다이얼로그가 detail 본문을 대체한 상태. */
  deleteConfirmOpen: boolean;
  /**
   * 삭제 후 재생성 handoff: the modal finished tearing the orphaned workspace
   * down (delete + 404 confirmed) and routed to WorkspacePage, which should
   * auto-start the create. In-memory only, so a refresh drops the handoff
   * instead of re-creating.
   */
  recreateHandoff: boolean;
  openModal: () => void;
  closeModal: () => void;
  openDeleteConfirm: () => void;
  /** Close the modal and flag WorkspacePage to auto-start the create. */
  beginRecreateHandoff: () => void;
  clearRecreateHandoff: () => void;
}

/** useWorkspaceStore holds the SC-02 management-modal UI state. */
export const useWorkspaceStore = create<WorkspaceUIState>((set) => ({
  modalOpen: false,
  deleteConfirmOpen: false,
  recreateHandoff: false,
  openModal: () => set({ modalOpen: true, deleteConfirmOpen: false }),
  closeModal: () => set({ modalOpen: false, deleteConfirmOpen: false }),
  openDeleteConfirm: () => set({ deleteConfirmOpen: true }),
  beginRecreateHandoff: () =>
    set({ recreateHandoff: true, modalOpen: false, deleteConfirmOpen: false }),
  clearRecreateHandoff: () => set({ recreateHandoff: false }),
}));
