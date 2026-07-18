import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import ModalLayout from "@/components/layout/ModalLayout";
import { BTN_TEXT } from "@/constants/commonConstants";

describe("ModalLayout", () => {
  it("renders the title and children through a portal", () => {
    render(
      <ModalLayout title="팀 삭제" isOpen>
        <p>content</p>
        <button type="button">닫기</button>
      </ModalLayout>,
    );

    expect(
      screen.getByRole("heading", { name: "팀 삭제" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: BTN_TEXT.close }),
    ).toBeInTheDocument();
  });

  it("locks body scroll while mounted and restores it on unmount", () => {
    const { unmount } = render(
      <ModalLayout title="제목" isOpen>
        <p>content</p>
      </ModalLayout>,
    );

    expect(document.body.style.position).toBe("fixed");

    unmount();

    expect(document.body.style.position).toBe("");
  });
});
