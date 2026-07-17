import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import SearchInput from "@/components/elements/SearchInput";

describe("SearchInput", () => {
  it("forwards typed value", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<SearchInput value="" onChange={onChange} placeholder="팀 검색" />);

    await user.type(screen.getByLabelText("팀 검색"), "a");
    expect(onChange).toHaveBeenCalledWith("a");
  });

  it("shows a clear button only when there is a value, and clears", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const { rerender } = render(<SearchInput value="" onChange={onChange} />);
    expect(
      screen.queryByRole("button", { name: "지우기" }),
    ).not.toBeInTheDocument();

    rerender(<SearchInput value="acme" onChange={onChange} />);
    await user.click(screen.getByRole("button", { name: "지우기" }));
    expect(onChange).toHaveBeenCalledWith("");
  });

  it("hides the clear button when disabled", () => {
    render(<SearchInput value="acme" onChange={() => {}} disabled />);
    expect(
      screen.queryByRole("button", { name: "지우기" }),
    ).not.toBeInTheDocument();
    expect(screen.getByLabelText("검색")).toBeDisabled();
  });

  it("merges an external className on the root wrapper", () => {
    const { container } = render(
      <SearchInput value="" onChange={() => {}} className="max-w-xs" />,
    );
    expect(container.firstChild).toHaveClass("max-w-xs");
  });
});
