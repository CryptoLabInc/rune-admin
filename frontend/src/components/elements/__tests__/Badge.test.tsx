import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import Badge from "@/components/elements/Badge";

describe("Badge", () => {
  it("renders nothing for zero or negative values", () => {
    const { container } = render(<Badge value={0} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("clamps values above max to max+", () => {
    render(<Badge value={120} />);
    expect(screen.getByText("99+")).toBeInTheDocument();
  });

  it("renders the plain value within max", () => {
    render(<Badge value={4} tone="neutral" />);
    expect(screen.getByText("4")).toBeInTheDocument();
  });

  it("merges an external className", () => {
    render(<Badge value={4} className="ml-2" />);
    expect(screen.getByText("4")).toHaveClass("ml-2");
  });
});
