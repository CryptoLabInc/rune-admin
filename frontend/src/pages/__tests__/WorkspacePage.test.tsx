import { MemoryRouter } from "react-router";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import WorkspacePage from "@/pages/WorkspacePage";
import type { TWorkspace } from "@/types/commonTypes";
import { useWorkspaceStore } from "@/stores/workspaceStore";

/* Server state is mocked; the page renders only while no workspace exists
   (query → null), which is exactly the post-teardown handoff situation. */
let queryState: { data: TWorkspace | null | undefined; isLoading: boolean };
let createState: {
  isPending: boolean;
  isError: boolean;
  isSuccess: boolean;
  mutate: ReturnType<typeof vi.fn>;
};

vi.mock("@/hooks/queries/useWorkspaceQuery", () => ({
  useWorkspaceQuery: () => queryState,
  isTransitionalStatus: (s: string) =>
    ["provisioning", "stopping", "starting", "deleting"].includes(s),
}));
vi.mock("@/hooks/mutations/useWorkspaceMutations", () => ({
  useCreateWorkspaceMutation: () => createState,
}));

const renderPage = () =>
  render(
    <MemoryRouter>
      <WorkspacePage />
    </MemoryRouter>,
  );

beforeEach(() => {
  queryState = { data: null, isLoading: false };
  createState = {
    isPending: false,
    isError: false,
    isSuccess: false,
    mutate: vi.fn(),
  };
  useWorkspaceStore.setState({
    modalOpen: false,
    deleteConfirmOpen: false,
    recreateHandoff: false,
  });
});

describe("WorkspacePage", () => {
  it("shows the empty state with a create action when no workspace exists", () => {
    renderPage();
    expect(
      screen.getByText("생성된 워크스페이스가 없습니다."),
    ).toBeInTheDocument();
    expect(createState.mutate).not.toHaveBeenCalled();
  });

  it("recreate handoff auto-starts the create and clears the flag", () => {
    useWorkspaceStore.setState({ recreateHandoff: true });
    renderPage();
    expect(createState.mutate).toHaveBeenCalledTimes(1);
    expect(useWorkspaceStore.getState().recreateHandoff).toBe(false);
  });

  it("recreate handoff never flashes the empty create prompt", () => {
    useWorkspaceStore.setState({ recreateHandoff: true });
    createState.isPending = true; // the auto-create fires on mount
    renderPage();
    expect(
      screen.getByText(/워크스페이스를 생성하는 중입니다/),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("생성된 워크스페이스가 없습니다."),
    ).toBeNull();
  });
});
