import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import Button from "@/components/elements/Button";
import { BTN_TEXT } from "@/constants/commonConstants";

describe("Button", () => {
  it("renders the label and fires handleClick", async () => {
    const user = userEvent.setup();
    const handleClick = vi.fn();
    render(
      <Button
        btnText={BTN_TEXT.invite}
        btnSize="md"
        btnColor="mintFilled"
        handleClick={handleClick}
      />,
    );

    await user.click(screen.getByRole("button", { name: BTN_TEXT.invite }));
    expect(handleClick).toHaveBeenCalledOnce();
  });

  it("is disabled and silent while a request is in flight", async () => {
    const user = userEvent.setup();
    const handleClick = vi.fn();
    render(
      <Button
        btnText={BTN_TEXT.sendInvitation}
        btnSize="md"
        btnColor="mintFilled"
        handleClick={handleClick}
        disabled
      />,
    );

    const button = screen.getByRole("button", {
      name: BTN_TEXT.sendInvitation,
    });
    expect(button).toBeDisabled();
    await user.click(button).catch(() => {});
    expect(handleClick).not.toHaveBeenCalled();
  });

  it("defaults to type=button and honors btnType", () => {
    render(
      <Button btnText={BTN_TEXT.save} btnSize="sm" btnColor="grayOutline" />,
    );
    expect(screen.getByRole("button", { name: BTN_TEXT.save })).toHaveAttribute(
      "type",
      "button",
    );
  });

  it("merges an external className, external winning on conflict", () => {
    render(
      <Button
        btnText={BTN_TEXT.save}
        btnSize="sm"
        btnColor="grayOutline"
        className="justify-start"
      />,
    );
    const button = screen.getByRole("button", { name: BTN_TEXT.save });
    expect(button).toHaveClass("justify-start");
    expect(button).not.toHaveClass("justify-center");
  });
});
