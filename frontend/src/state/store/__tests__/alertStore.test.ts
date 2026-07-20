import { beforeEach, describe, expect, it } from "vitest";

import { useAlertStore } from "@/state/store/alertStore";

const resetAlertStore = () => {
  useAlertStore.setState({
    alert: { title: "", content: "", isOpen: false },
  });
};

describe("alertStore", () => {
  beforeEach(() => {
    resetAlertStore();
  });

  it("opens the alert with the given content", () => {
    const { setAlert } = useAlertStore.getState();
    setAlert({ title: "오류", content: "문제가 발생했습니다." });

    const { alert } = useAlertStore.getState();
    expect(alert).toEqual({
      title: "오류",
      content: "문제가 발생했습니다.",
      isOpen: true,
    });
  });

  it("resets to the closed state on closeAlert", () => {
    const { setAlert, closeAlert } = useAlertStore.getState();
    setAlert({ title: "오류", content: "문제가 발생했습니다." });
    closeAlert();

    expect(useAlertStore.getState().alert.isOpen).toBe(false);
  });
});
