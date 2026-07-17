import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import Dropdown from "@/components/elements/Dropdown";

const OPTIONS = [
  { value: "edit", label: "edit" },
  { value: "write", label: "write" },
  { value: "read", label: "read", disabled: true },
];

describe("Dropdown", () => {
  it("opens a listbox and picks an option", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <Dropdown
        options={OPTIONS}
        placeholder="role 선택"
        onChange={onChange}
      />,
    );

    await user.click(screen.getByRole("button", { name: "role 선택" }));
    expect(screen.getByRole("listbox")).toBeInTheDocument();

    await user.click(screen.getByRole("option", { name: "write" }));
    expect(onChange).toHaveBeenCalledWith("write");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("supports keyboard navigation (ArrowDown + Enter)", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <Dropdown
        options={OPTIONS}
        placeholder="role 선택"
        onChange={onChange}
      />,
    );

    const trigger = screen.getByRole("button", { name: "role 선택" });
    trigger.focus();
    await user.keyboard("{ArrowDown}"); // opens, highlights first enabled
    await user.keyboard("{ArrowDown}"); // moves to "write"
    await user.keyboard("{Enter}");
    expect(onChange).toHaveBeenCalledWith("write");
  });

  it("closes on Escape without selecting", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <Dropdown
        options={OPTIONS}
        placeholder="role 선택"
        onChange={onChange}
      />,
    );

    await user.click(screen.getByRole("button", { name: "role 선택" }));
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("ignores disabled options", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <Dropdown
        options={OPTIONS}
        placeholder="role 선택"
        onChange={onChange}
      />,
    );

    await user.click(screen.getByRole("button", { name: "role 선택" }));
    await user.click(screen.getByRole("option", { name: "read" }));
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole("listbox")).toBeInTheDocument();
  });

  it("shows error text and marks the trigger invalid", () => {
    render(
      <Dropdown
        options={OPTIONS}
        label="role"
        placeholder="role 선택"
        error="role을 선택해 주세요."
      />,
    );
    expect(screen.getByText("role을 선택해 주세요.")).toBeInTheDocument();
    expect(screen.getByRole("button")).toHaveAttribute("aria-invalid", "true");
  });

  it("applies the compact sm size to the trigger", () => {
    render(<Dropdown options={OPTIONS} placeholder="role 선택" size="sm" />);
    const trigger = screen.getByRole("button", { name: "role 선택" });
    expect(trigger).toHaveClass("h-8");
    expect(trigger).toHaveClass("text-sm");
  });

  it("merges an external className on the root field", () => {
    const { container } = render(
      <Dropdown options={OPTIONS} placeholder="role 선택" className="w-40" />,
    );
    expect(container.firstChild).toHaveClass("w-40");
  });

  it("marks the trigger border while changed (staged, unsaved value)", () => {
    const { rerender } = render(
      <Dropdown options={OPTIONS} placeholder="role 선택" changed />,
    );
    const trigger = screen.getByRole("button", { name: "role 선택" });
    expect(trigger).toHaveClass("border-accent-blue");

    rerender(<Dropdown options={OPTIONS} placeholder="role 선택" />);
    expect(trigger).not.toHaveClass("border-accent-blue");
  });
});
