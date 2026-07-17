import { create } from "zustand";

/*
 * UI-only state for the rune 워크스페이스 관리 modal (SC-02). Server state
 * (the workspace record + its lifecycle) lives in react-query
 * (useWorkspaceQuery / useWorkspaceMutations); this store holds just what
 * the modal needs to remember across renders: whether it is open, and
 * whether the 삭제 confirm dialog (state D-1) has replaced the detail body.
 *
 * The other modal bodies are derived, not stored: 불러오기 실패 (D-2) from the
 * query error, and stop/restart/delete failures (D-3/D-4/D-1 실패) from the
 * per-mount mutation state.
 */
interface WorkspaceUIState {
  modalOpen: boolean;
  /** D-1 삭제 확인 다이얼로그가 detail 본문을 대체한 상태. */
  deleteConfirmOpen: boolean;
  openModal: () => void;
  closeModal: () => void;
  openDeleteConfirm: () => void;
}

/** useWorkspaceStore holds the SC-02 management-modal UI state. */
export const useWorkspaceStore = create<WorkspaceUIState>((set) => ({
  modalOpen: false,
  deleteConfirmOpen: false,
  openModal: () => set({ modalOpen: true, deleteConfirmOpen: false }),
  closeModal: () => set({ modalOpen: false, deleteConfirmOpen: false }),
  openDeleteConfirm: () => set({ deleteConfirmOpen: true }),
}));
