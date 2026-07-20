import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import WorkspaceModal from "@/components/workspace/WorkspaceModal";
import { BTN_TEXT, PATH_LIST } from "@/constants/commonConstants";
import type { TWorkspaceStatus, TWorkspace } from "@/types/commonTypes";
import { useWorkspaceStore } from "@/stores/workspaceStore";

/* The modal's data + mutations are mocked so each render variant (detail,
   busy, D-1/D-1 실패, D-2, D-3) can be driven directly. */
let queryState: { data: TWorkspace | null | undefined; isError: boolean };
let stopState: { isPending: boolean; isError: boolean };
let startState: { isPending: boolean; isError: boolean };
let deleteState: { isPending: boolean; isError: boolean };
let recreateState: { isPending: boolean; isError: boolean };
const mutate = vi.fn();
const navigateMock = vi.fn();

vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => navigateMock,
}));

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
  rowCount: 431,
  orphaned: false,
};

const setStatus = (status: TWorkspaceStatus) => {
  queryState = { data: { ...RUNNING, status }, isError: false };
};

beforeEach(() => {
  queryState = { data: { ...RUNNING }, isError: false };
  stopState = { isPending: false, isError: false };
  startState = { isPending: false, isError: false };
  deleteState = { isPending: false, isError: false };
  recreateState = { isPending: false, isError: false };
  mutate.mockReset();
  navigateMock.mockReset();
  useWorkspaceStore.setState({
    modalOpen: true,
    deleteConfirmOpen: false,
    recreateHandoff: false,
  });
  document.body.innerHTML = "";
});

describe("WorkspaceModal", () => {
  it("detail (running) shows [중지], the delete trigger, and workspace info", () => {
    render(<WorkspaceModal />);
    expect(screen.getByRole("button", { name: BTN_TEXT.stop })).toBeEnabled();
    expect(screen.getByRole("button", { name: BTN_TEXT.delete })).toBeEnabled();
    expect(screen.queryByRole("button", { name: BTN_TEXT.restart })).toBeNull();
    expect(screen.getByText("실행 중")).toBeInTheDocument();
    // Memory usage is computed against the plan max, not hardcoded.
    expect(screen.getByText("431 / 1,000 (43%)")).toBeInTheDocument();
  });

  it("caps the memory usage percent at 100 when the count exceeds the max", () => {
    queryState = { data: { ...RUNNING, rowCount: 1204 }, isError: false };
    render(<WorkspaceModal />);
    expect(screen.getByText("1,204 / 1,000 (100%)")).toBeInTheDocument();
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

  it("recreate in flight shows the 삭제 중 message with both buttons disabled", () => {
    queryState = { data: { ...RUNNING, orphaned: true }, isError: false };
    recreateState = { isPending: true, isError: false };
    render(<WorkspaceModal />);
    expect(
      screen.getByText(/기존 워크스페이스를 삭제하는 중입니다/),
    ).toBeInTheDocument();
    // The progress copy replaces the orphaned explanation.
    expect(screen.queryByText(/콘솔이 재설치되어/)).toBeNull();
    // Buttons stay (disabled) so the modal doesn't look emptied out.
    expect(screen.getByRole("button", { name: BTN_TEXT.close })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: BTN_TEXT.recreate }),
    ).toBeDisabled();
  });

  it("keeps the 삭제 중 body even after the workspace 404s mid-teardown", () => {
    // Teardown drops the workspace row (query → null, orphaned flag gone).
    // The pending body must survive that.
    queryState = { data: null, isError: false };
    recreateState = { isPending: true, isError: false };
    render(<WorkspaceModal />);
    expect(
      screen.getByText(/기존 워크스페이스를 삭제하는 중입니다/),
    ).toBeInTheDocument();
  });

  it("recreate success hands off to the 생성 중 page (SC-02 A/B)", () => {
    queryState = { data: { ...RUNNING, orphaned: true }, isError: false };
    render(<WorkspaceModal />);
    fireEvent.click(screen.getByRole("button", { name: BTN_TEXT.recreate }));
    expect(mutate).toHaveBeenCalledTimes(1);

    // Fire the mutation's onSuccess: the teardown finished (404 confirmed),
    // so the modal closes, flags the handoff, and routes to the create page.
    const options = mutate.mock.calls[0][1] as { onSuccess: () => void };
    act(() => options.onSuccess());
    expect(useWorkspaceStore.getState().modalOpen).toBe(false);
    expect(useWorkspaceStore.getState().recreateHandoff).toBe(true);
    expect(navigateMock).toHaveBeenCalledWith(PATH_LIST.workspace);
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
