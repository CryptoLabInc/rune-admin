import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import TextButton from "@/components/elements/TextButton";

describe("TextButton", () => {
  it("fires handleClick", async () => {
    const user = userEvent.setup();
    const handleClick = vi.fn();
    render(<TextButton btnText="회원탈퇴" handleClick={handleClick} />);

    await user.click(screen.getByRole("button", { name: "회원탈퇴" }));
    expect(handleClick).toHaveBeenCalledOnce();
  });

  it("does nothing when disabled", async () => {
    const user = userEvent.setup();
    const handleClick = vi.fn();
    render(
      <TextButton
        btnText="삭제"
        tone="red"
        disabled
        handleClick={handleClick}
      />,
    );

    const button = screen.getByRole("button", { name: "삭제" });
    expect(button).toBeDisabled();
    await user.click(button).catch(() => {});
    expect(handleClick).not.toHaveBeenCalled();
  });

  it("merges an external className", () => {
    render(<TextButton btnText="회원탈퇴" className="self-end" />);
    expect(screen.getByRole("button", { name: "회원탈퇴" })).toHaveClass(
      "self-end",
    );
  });
});
