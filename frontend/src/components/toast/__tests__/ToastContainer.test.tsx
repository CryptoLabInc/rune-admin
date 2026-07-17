import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import ToastContainer from "@/components/toast/ToastContainer";
import { useToastStore } from "@/stores/toastStore";

describe("ToastContainer", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    act(() => {
      vi.runAllTimers();
    });
    vi.useRealTimers();
  });

  it("shows a toast and removes it after the hold + exit timings", () => {
    render(<ToastContainer />);
    act(() => {
      useToastStore.getState().showToast("초대 코드를 재전송했습니다.");
    });
    expect(screen.getByText("초대 코드를 재전송했습니다.")).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(2000 + 300);
    });
    expect(
      screen.queryByText("초대 코드를 재전송했습니다."),
    ).not.toBeInTheDocument();
  });

  it("announces error toasts as alerts", () => {
    render(<ToastContainer />);
    act(() => {
      useToastStore.getState().showToast("권한 변경에 실패했습니다.", "error");
    });
    expect(screen.getByRole("alert")).toHaveTextContent(
      "권한 변경에 실패했습니다.",
    );
  });

  it("dismisses early when clicked (exit animation, then removal)", () => {
    render(<ToastContainer />);
    act(() => {
      useToastStore.getState().showToast("변경사항이 저장되었습니다.");
    });

    act(() => {
      screen.getByText("변경사항이 저장되었습니다.").click();
    });
    // Exit starts immediately — well before the 2s hold would end.
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(
      screen.queryByText("변경사항이 저장되었습니다."),
    ).not.toBeInTheDocument();
  });
});
