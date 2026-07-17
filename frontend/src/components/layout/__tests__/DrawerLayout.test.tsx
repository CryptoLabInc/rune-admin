import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import DrawerLayout from "@/components/layout/DrawerLayout";

describe("DrawerLayout", () => {
  it("renders a labelled dialog with title, subtitle, and footer", () => {
    render(
      <DrawerLayout
        isOpen
        title="nia@cryptolab.dev"
        eyebrow="MEMBER DETAIL"
        subtitle="최근 접속 2026-07-10 13:08"
        onClose={() => {}}
        footer={<button type="button">변경사항 업데이트</button>}
      >
        본문
      </DrawerLayout>,
    );
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAccessibleName("nia@cryptolab.dev");
    expect(screen.getByText("최근 접속 2026-07-10 13:08")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "변경사항 업데이트" }),
    ).toBeInTheDocument();
  });

  it("closes via a scrim click, not via clicks inside the panel", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(
      <DrawerLayout isOpen title="member" onClose={onClose}>
        본문
      </DrawerLayout>,
    );

    expect(
      screen.queryByRole("button", { name: "닫기" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("dialog"));
    expect(onClose).not.toHaveBeenCalled();

    await user.click(document.querySelector(".fixed") as HTMLElement);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("renders nothing while closed", () => {
    render(
      <DrawerLayout isOpen={false} title="member" onClose={() => {}}>
        본문
      </DrawerLayout>,
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("locks body scroll only while open, not when mounted closed", () => {
    const { rerender } = render(
      <DrawerLayout isOpen={false} title="member" onClose={() => {}}>
        본문
      </DrawerLayout>,
    );
    expect(document.body.style.position).toBe("");

    rerender(
      <DrawerLayout isOpen title="member" onClose={() => {}}>
        본문
      </DrawerLayout>,
    );
    expect(document.body.style.position).toBe("fixed");

    rerender(
      <DrawerLayout isOpen={false} title="member" onClose={() => {}}>
        본문
      </DrawerLayout>,
    );
    expect(document.body.style.position).toBe("");
  });
});
