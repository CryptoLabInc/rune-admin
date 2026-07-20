import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import Input from "@/components/elements/Input";

describe("Input", () => {
  it("merges an external className on the root wrapper", () => {
    const { container } = render(
      <Input
        id="team-name"
        labelText="팀 이름"
        value=""
        setValue={() => {}}
        className="max-w-sm"
      />,
    );
    expect(container.firstChild).toHaveClass("max-w-sm");
  });

  it("associates the label and forwards typed value", async () => {
    const user = userEvent.setup();
    const setValue = vi.fn();
    render(
      <Input id="team-name" labelText="팀 이름" value="" setValue={setValue} />,
    );

    await user.type(screen.getByLabelText("팀 이름"), "a");
    expect(setValue).toHaveBeenCalledWith("a");
  });

  it("shows hint by default and replaces it with error", () => {
    const { rerender } = render(
      <Input
        id="f"
        labelText="팀 이름"
        hint="숫자·한글·영어와 - _ 만 사용할 수 있습니다."
        value=""
        setValue={() => {}}
      />,
    );
    expect(
      screen.getByText("숫자·한글·영어와 - _ 만 사용할 수 있습니다."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByLabelText("팀 이름")).toHaveAccessibleDescription(
      "숫자·한글·영어와 - _ 만 사용할 수 있습니다.",
    );

    rerender(
      <Input
        id="f"
        labelText="팀 이름"
        hint="숫자·한글·영어와 - _ 만 사용할 수 있습니다."
        error="이미 존재하는 팀 이름입니다."
        value="dup"
        setValue={() => {}}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "이미 존재하는 팀 이름입니다.",
    );
    expect(
      screen.queryByText("숫자·한글·영어와 - _ 만 사용할 수 있습니다."),
    ).not.toBeInTheDocument();
    expect(screen.getByLabelText("팀 이름")).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    expect(screen.getByLabelText("팀 이름")).toHaveAccessibleDescription(
      "이미 존재하는 팀 이름입니다.",
    );
  });

  it("supports disabled", () => {
    render(
      <Input
        id="d"
        labelText="초대 코드"
        disabled
        value=""
        setValue={() => {}}
      />,
    );
    expect(screen.getByLabelText("초대 코드")).toBeDisabled();
  });
});
