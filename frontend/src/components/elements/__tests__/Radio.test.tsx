import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import Radio from "@/components/elements/Radio";

describe("Radio", () => {
  it("renders label and desc and fires onChange when picked", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <>
        <Radio checked name="del" label="팀만 삭제" onChange={onChange} />
        <Radio
          checked={false}
          name="del"
          label="팀 내 기억 삭제"
          desc="팀에 축적된 기억을 함께 삭제합니다."
          onChange={onChange}
        />
      </>,
    );

    expect(
      screen.getByText("팀에 축적된 기억을 함께 삭제합니다."),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("radio", { name: /팀 내 기억 삭제/ }));
    expect(onChange).toHaveBeenCalledOnce();
  });

  it("merges an external className on the root label", () => {
    const { container } = render(
      <Radio
        checked
        name="del"
        label="팀만 삭제"
        onChange={() => {}}
        className="w-full"
      />,
    );
    expect(container.firstChild).toHaveClass("w-full");
  });
});
