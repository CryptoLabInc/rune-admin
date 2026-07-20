import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import UpdateFloatingCard from "@/components/update/UpdateFloatingCard";
import { BTN_TEXT } from "@/constants/commonConstants";
import type { TSystemUpdateStatus } from "@/types/updateTypes";

const hooks = vi.hoisted(() => ({
  query: vi.fn(),
  mutation: vi.fn(),
  mutate: vi.fn(),
  reload: vi.fn(),
}));

vi.mock("@/hooks/queries/useUpdateQuery", () => ({
  isSystemUpdateActive: (state: string) =>
    state === "queued" || state === "running",
  useUpdateQuery: hooks.query,
}));

vi.mock("@/hooks/mutations/useUpdateMutation", () => ({
  useUpdateMutation: hooks.mutation,
}));

vi.mock("@/utils/reloadPage", () => ({
  reloadPage: hooks.reload,
}));

const available: TSystemUpdateStatus = {
  currentVersion: "v1.0.0",
  targetVersion: "v1.1.0",
  updateAvailable: true,
  capable: true,
  state: "idle",
};

const setQuery = (data?: TSystemUpdateStatus) => {
  hooks.query.mockReturnValue({ data });
};

const setMutation = (overrides = {}) => {
  hooks.mutation.mockReturnValue({
    mutate: hooks.mutate,
    isPending: false,
    isError: false,
    ...overrides,
  });
};

describe("UpdateFloatingCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.sessionStorage.clear();
    setMutation();
  });

  it.each([
    ["loading", undefined],
    ["no update", { ...available, updateAvailable: false }],
    ["incapable", { ...available, capable: false }],
  ])("renders no DOM while %s", (_, status) => {
    setQuery(status as TSystemUpdateStatus | undefined);

    const { container } = render(<UpdateFloatingCard />);

    expect(container).toBeEmptyDOMElement();
  });

  it("renders no DOM when the initial check fails", () => {
    hooks.query.mockReturnValue({ data: undefined, isError: true });

    const { container } = render(<UpdateFloatingCard />);

    expect(container).toBeEmptyDOMElement();
  });

  it("shows a fixed top-right prompt only when an update is available", () => {
    setQuery(available);

    render(<UpdateFloatingCard />);

    const card = screen.getByRole("dialog", { name: "새 버전이 있습니다" });
    expect(card).toHaveClass("fixed", "top-20", "right-6");
    expect(screen.getByText("v1.0.0 → v1.1.0")).toBeInTheDocument();
    expect(screen.getByText(/SQLite DB와 설정을 백업/)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: BTN_TEXT.update }),
    ).toBeInTheDocument();
  });

  it("queues the exact target selected by the server", async () => {
    const user = userEvent.setup();
    setQuery(available);
    render(<UpdateFloatingCard />);

    await user.click(screen.getByRole("button", { name: BTN_TEXT.update }));

    expect(hooks.mutate).toHaveBeenCalledWith(
      "v1.1.0",
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("remembers later per target for the browser session", async () => {
    const user = userEvent.setup();
    setQuery(available);
    const view = render(<UpdateFloatingCard />);

    await user.click(screen.getByRole("button", { name: BTN_TEXT.later }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    view.unmount();
    const sameTarget = render(<UpdateFloatingCard />);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    sameTarget.unmount();

    setQuery({ ...available, targetVersion: "v1.2.0" });
    render(<UpdateFloatingCard />);
    expect(
      screen.getByRole("dialog", { name: "새 버전이 있습니다" }),
    ).toBeInTheDocument();
    expect(screen.getByText("v1.0.0 → v1.2.0")).toBeInTheDocument();
  });

  it("continues showing queued and running updates even after dismissal", async () => {
    const user = userEvent.setup();
    setQuery(available);
    const view = render(<UpdateFloatingCard />);
    await user.click(screen.getByRole("button", { name: BTN_TEXT.later }));

    setQuery({
      ...available,
      capable: false,
      updateAvailable: false,
      state: "running",
    });
    view.rerender(<UpdateFloatingCard />);

    expect(
      screen.getByRole("dialog", { name: "콘솔을 업데이트하는 중입니다" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent(
      "백업 및 업데이트를 진행하고 있습니다",
    );
    expect(
      screen.queryByRole("button", { name: BTN_TEXT.later }),
    ).not.toBeInTheDocument();
  });

  it("keeps progress visible when a status refetch loses the daemon", () => {
    hooks.query.mockReturnValue({
      data: { ...available, state: "queued" },
      isError: true,
    });

    render(<UpdateFloatingCard />);

    expect(
      screen.getByRole("dialog", { name: "콘솔을 업데이트하는 중입니다" }),
    ).toBeInTheDocument();
  });

  it("hides a stale failure when no update is available", () => {
    setQuery({ ...available, updateAvailable: false, state: "failed" });

    const { container } = render(<UpdateFloatingCard />);

    expect(container).toBeEmptyDOMElement();
  });

  it("offers a retry after an available update fails", async () => {
    const user = userEvent.setup();
    setQuery({ ...available, state: "failed" });
    render(<UpdateFloatingCard />);

    expect(
      screen.getByRole("dialog", { name: "업데이트에 실패했습니다" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: BTN_TEXT.retry }));
    expect(hooks.mutate).toHaveBeenCalledWith(
      "v1.1.0",
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it.each([
    [
      "the queued target becomes current",
      { currentVersion: "v1.1.0", state: "idle" as const },
    ],
    [
      "the helper reports success",
      { currentVersion: "v1.0.0", state: "succeeded" as const },
    ],
  ])("reloads once when %s", async (_, completion) => {
    const user = userEvent.setup();
    setQuery(available);
    const view = render(<UpdateFloatingCard />);

    await user.click(screen.getByRole("button", { name: BTN_TEXT.update }));
    const options = hooks.mutate.mock.calls[0][1] as {
      onSuccess: () => void;
    };
    act(() => options.onSuccess());

    setQuery({
      ...available,
      ...completion,
      updateAvailable: false,
    });
    view.rerender(<UpdateFloatingCard />);

    await waitFor(() => expect(hooks.reload).toHaveBeenCalledOnce());
    view.rerender(<UpdateFloatingCard />);
    expect(hooks.reload).toHaveBeenCalledOnce();
  });

  it("reloads when a job queued by another tab succeeds", async () => {
    setQuery({ ...available, state: "running" });
    const view = render(<UpdateFloatingCard />);

    await waitFor(() =>
      expect(
        window.sessionStorage.getItem(
          "runeconsole.system-update.queued-target",
        ),
      ).toBe("v1.1.0"),
    );

    setQuery({
      ...available,
      currentVersion: "v1.1.0",
      updateAvailable: false,
      state: "succeeded",
    });
    view.rerender(<UpdateFloatingCard />);

    await waitFor(() => expect(hooks.reload).toHaveBeenCalledOnce());
  });

  it("does not treat another job's succeeded state as this tab's completion", () => {
    window.sessionStorage.setItem(
      "runeconsole.system-update.queued-target",
      "v1.1.0",
    );
    setQuery({
      ...available,
      targetVersion: "v1.2.0",
      state: "succeeded",
    });

    render(<UpdateFloatingCard />);

    expect(hooks.reload).not.toHaveBeenCalled();
  });
});
