import { beforeEach, describe, expect, it, vi } from "vitest";

import { useNoticeStore } from "@/stores/noticeStore";

describe("noticeStore", () => {
  beforeEach(() => {
    useNoticeStore.setState({ notice: null });
  });

  it("showNotice sets the current notice with defaulted tone", () => {
    useNoticeStore.getState().showNotice("멤버 삭제", "멤버를 삭제했습니다.");
    expect(useNoticeStore.getState().notice).toEqual({
      title: "멤버 삭제",
      message: "멤버를 삭제했습니다.",
      tone: "info",
      onConfirm: undefined,
    });
  });

  it("showNotice keeps only one notice; a new call replaces the previous", () => {
    const { showNotice } = useNoticeStore.getState();
    showNotice("A", "first", "success");
    showNotice("B", "second", "error");
    expect(useNoticeStore.getState().notice).toMatchObject({
      title: "B",
      message: "second",
      tone: "error",
    });
  });

  it("dismissNotice runs onConfirm then clears the notice", () => {
    const onConfirm = vi.fn();
    useNoticeStore
      .getState()
      .showNotice("팀 삭제", "팀이 삭제되었습니다.", "success", onConfirm);
    useNoticeStore.getState().dismissNotice();
    expect(onConfirm).toHaveBeenCalledOnce();
    expect(useNoticeStore.getState().notice).toBeNull();
  });

  it("dismissNotice with no notice is a no-op", () => {
    expect(() => useNoticeStore.getState().dismissNotice()).not.toThrow();
    expect(useNoticeStore.getState().notice).toBeNull();
  });
});
