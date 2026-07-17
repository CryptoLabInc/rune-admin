import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import NoticeModal from "@/components/elements/NoticeModal";
import { useNoticeStore } from "@/stores/noticeStore";

afterEach(() => {
  useNoticeStore.setState({ notice: null });
});

describe("NoticeModal", () => {
  it("renders nothing when there is no notice", () => {
    const { container } = render(<NoticeModal />);
    expect(container).toBeEmptyDOMElement();
    expect(screen.queryByRole("button", { name: "확인" })).toBeNull();
  });

  it("renders the title and message of the current notice", async () => {
    render(<NoticeModal />);
    useNoticeStore.getState().showNotice("멤버 삭제", "멤버를 삭제했습니다.");
    await waitFor(() => {
      expect(screen.getByText("멤버 삭제")).toBeInTheDocument();
    });
    expect(screen.getByText("멤버를 삭제했습니다.")).toBeInTheDocument();
  });

  it("applies the negative style to error-tone content", async () => {
    render(<NoticeModal />);
    useNoticeStore
      .getState()
      .showNotice("세션 비활성화", "세션 비활성화에 실패했습니다. 다시 시도해주세요.", "error");
    await waitFor(() => {
      expect(
        screen.getByText("세션 비활성화에 실패했습니다. 다시 시도해주세요."),
      ).toHaveClass("text-negative");
    });
  });

  it("[확인] dismisses and runs the notice onConfirm", async () => {
    const onConfirm = vi.fn();
    render(<NoticeModal />);
    useNoticeStore
      .getState()
      .showNotice("팀 삭제", "팀이 삭제되었습니다.", "success", onConfirm);
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "확인" })).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("button", { name: "확인" }));
    expect(onConfirm).toHaveBeenCalledOnce();
    expect(useNoticeStore.getState().notice).toBeNull();
  });
});
