import { beforeEach, describe, expect, it } from "vitest";

import { useWorkspaceStore } from "@/stores/workspaceStore";

const reset = () =>
  useWorkspaceStore.setState({ modalOpen: false, deleteConfirmOpen: false });

describe("workspaceStore", () => {
  beforeEach(reset);

  it("openModal opens the modal on the detail body", () => {
    useWorkspaceStore.setState({ deleteConfirmOpen: true });
    useWorkspaceStore.getState().openModal();

    const state = useWorkspaceStore.getState();
    expect(state.modalOpen).toBe(true);
    expect(state.deleteConfirmOpen).toBe(false);
  });

  it("openDeleteConfirm swaps to the delete-confirm body", () => {
    useWorkspaceStore.getState().openModal();
    useWorkspaceStore.getState().openDeleteConfirm();
    expect(useWorkspaceStore.getState().deleteConfirmOpen).toBe(true);
  });

  it("closeModal resets both flags", () => {
    useWorkspaceStore.setState({ modalOpen: true, deleteConfirmOpen: true });
    useWorkspaceStore.getState().closeModal();

    const state = useWorkspaceStore.getState();
    expect(state.modalOpen).toBe(false);
    expect(state.deleteConfirmOpen).toBe(false);
  });
});
