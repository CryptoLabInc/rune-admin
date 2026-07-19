import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import WorkspaceModal from "@/components/workspace/WorkspaceModal";
import { BTN_TEXT } from "@/constants/commonConstants";
import type { TStorageStatus, TWorkspace } from "@/types/commonTypes";
import { useWorkspaceStore } from "@/stores/workspaceStore";

/* The modal's data + mutations are mocked so each render variant (detail,
   busy, D-1/D-1 실패, D-2, D-3) can be driven directly. */
let queryState: { data: TWorkspace | null | undefined; isError: boolean };
let stopState: { isPending: boolean; isError: boolean };
let startState: { isPending: boolean; isError: boolean };
let deleteState: { isPending: boolean; isError: boolean };
let recreateState: { isPending: boolean; isError: boolean };
const mutate = vi.fn();

vi.mock("@/hooks/queries/useWorkspaceQuery", () => ({
  useWorkspaceQuery: () => queryState,
  isTransitionalStatus: (s: string) =>
    ["provisioning", "stopping", "starting", "deleting"].includes(s),
}));
vi.mock("@/hooks/mutations/useWorkspaceMutations", () => ({
  useStopWorkspaceMutation: () => ({ ...stopState, mutate }),
  useStartWorkspaceMutation: () => ({ ...startState, mutate }),
  useDeleteWorkspaceMutation: () => ({ ...deleteState, mutate }),
  useRecreateWorkspaceMutation: () => ({ ...recreateState, mutate }),
  useCreateWorkspaceMutation: () => ({
    isPending: false,
    isError: false,
    isSuccess: false,
    mutate,
  }),
}));

const RUNNING: TWorkspace = {
  status: "running",
  endpoint: "https://a1b2c3d4.rune.example.com",
  rowCount: 12431,
  orphaned: false,
};

const setStatus = (status: TStorageStatus) => {
  queryState = { data: { ...RUNNING, status }, isError: false };
};

beforeEach(() => {
  queryState = { data: { ...RUNNING }, isError: false };
  stopState = { isPending: false, isError: false };
  startState = { isPending: false, isError: false };
  deleteState = { isPending: false, isError: false };
  recreateState = { isPending: false, isError: false };
  useWorkspaceStore.setState({ modalOpen: true, deleteConfirmOpen: false });
  document.body.innerHTML = "";
});

describe("WorkspaceModal", () => {
  it("detail (running) shows [중지], the delete trigger, and workspace info", () => {
    render(<WorkspaceModal />);
    expect(screen.getByRole("button", { name: BTN_TEXT.stop })).toBeEnabled();
    expect(screen.getByRole("button", { name: BTN_TEXT.delete })).toBeEnabled();
    expect(screen.queryByRole("button", { name: BTN_TEXT.restart })).toBeNull();
    expect(screen.getByText(RUNNING.endpoint as string)).toBeInTheDocument();
    expect(screen.getByText("running")).toBeInTheDocument();
    expect(screen.getByText("12,431")).toBeInTheDocument();
  });

  it("detail (stopped) replaces [중지] with [재실행]", () => {
    setStatus("stopped");
    render(<WorkspaceModal />);
    expect(
      screen.getByRole("button", { name: BTN_TEXT.restart }),
    ).toBeEnabled();
    expect(screen.queryByRole("button", { name: BTN_TEXT.stop })).toBeNull();
  });

  it("transitional status disables the lifecycle actions (spec no.6/8)", () => {
    setStatus("stopping");
    render(<WorkspaceModal />);
    expect(screen.getByRole("button", { name: BTN_TEXT.stop })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: BTN_TEXT.delete }),
    ).toBeDisabled();
  });

  it("detail surfaces the stop-failure message (state D-3)", () => {
    stopState = { isPending: false, isError: true };
    render(<WorkspaceModal />);
    expect(
      screen.getByText("워크스페이스 중지에 실패했습니다. 다시 시도해 주세요."),
    ).toBeInTheDocument();
  });

  it("confirm dialog (D-1) shows the confirm copy and both buttons", () => {
    useWorkspaceStore.setState({ deleteConfirmOpen: true });
    render(<WorkspaceModal />);
    expect(
      screen.getByText(/워크스페이스를 삭제하시겠습니까/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: BTN_TEXT.close }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: BTN_TEXT.delete }),
    ).toBeInTheDocument();
  });

  it("delete failure (D-1 실패) swaps to the fail copy with only [닫기]", () => {
    useWorkspaceStore.setState({ deleteConfirmOpen: true });
    deleteState = { isPending: false, isError: true };
    render(<WorkspaceModal />);
    expect(
      screen.getByText("워크스페이스 삭제에 실패했습니다. 다시 시도해 주세요."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: BTN_TEXT.delete })).toBeNull();
    expect(
      screen.getByRole("button", { name: BTN_TEXT.close }),
    ).toBeInTheDocument();
  });

  it("orphaned workspace shows the 재생성 prompt instead of the detail body", () => {
    queryState = { data: { ...RUNNING, orphaned: true }, isError: false };
    render(<WorkspaceModal />);
    expect(
      screen.getByText(/콘솔이 재설치되어 이 워크스페이스와 연결할 수 없습니다/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: BTN_TEXT.recreate }),
    ).toBeEnabled();
    // The normal lifecycle actions are gone — an orphaned workspace is unusable.
    expect(screen.queryByRole("button", { name: BTN_TEXT.stop })).toBeNull();
  });

  it("recreate in flight shows the 재생성 중 body with no action buttons", () => {
    queryState = { data: { ...RUNNING, orphaned: true }, isError: false };
    recreateState = { isPending: true, isError: false };
    render(<WorkspaceModal />);
    expect(
      screen.getByText(/워크스페이스를 재생성하는 중입니다/),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: BTN_TEXT.recreate })).toBeNull();
  });

  it("keeps the 재생성 중 body even after the workspace 404s mid-teardown", () => {
    // Recreate deletes then creates; between the two the workspace vanishes
    // (query → null, orphaned flag gone). The pending body must survive that.
    queryState = { data: null, isError: false };
    recreateState = { isPending: true, isError: false };
    render(<WorkspaceModal />);
    expect(
      screen.getByText(/워크스페이스를 재생성하는 중입니다/),
    ).toBeInTheDocument();
  });

  it("recreate failure swaps to the fail copy with only [닫기]", () => {
    queryState = { data: { ...RUNNING, orphaned: true }, isError: false };
    recreateState = { isPending: false, isError: true };
    render(<WorkspaceModal />);
    expect(
      screen.getByText("워크스페이스 재생성에 실패했습니다. 다시 시도해 주세요."),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: BTN_TEXT.recreate }),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: BTN_TEXT.close }),
    ).toBeInTheDocument();
  });

  it("load error (D-2) shows the load-failure copy and [닫기] only", () => {
    queryState = { data: undefined, isError: true };
    render(<WorkspaceModal />);
    expect(
      screen.getByText(/워크스페이스 정보를 불러올 수 없습니다/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: BTN_TEXT.close }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: BTN_TEXT.delete })).toBeNull();
  });
});
