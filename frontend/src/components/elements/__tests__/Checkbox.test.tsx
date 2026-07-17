import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import Checkbox from "@/components/elements/Checkbox";

describe("Checkbox", () => {
  it("toggles through onChange", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Checkbox checked={false} onChange={onChange} label="선택" />);

    await user.click(screen.getByRole("checkbox", { name: "선택" }));
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it("does not fire when disabled", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Checkbox checked disabled onChange={onChange} label="row" />);

    await user
      .click(screen.getByRole("checkbox", { name: "row" }))
      .catch(() => {});
    expect(onChange).not.toHaveBeenCalled();
  });

  it("merges an external className on the root label", () => {
    const { container } = render(
      <Checkbox checked onChange={() => {}} label="선택" className="mt-4" />,
    );
    expect(container.firstChild).toHaveClass("mt-4");
  });
});
