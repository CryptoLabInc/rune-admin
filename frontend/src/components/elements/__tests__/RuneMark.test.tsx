import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import RuneMark from "@/components/elements/RuneMark";

describe("RuneMark", () => {
  it("renders the brand mark as decorative inline SVG", () => {
    const { container } = render(<RuneMark />);
    const svg = container.querySelector("svg");
    expect(svg).toBeInTheDocument();
    expect(svg).toHaveAttribute("aria-hidden", "true");
    // Three brand gradients (mint / navy / teal) must be present.
    expect(container.querySelectorAll("linearGradient")).toHaveLength(3);
  });

  it("scales via the size prop while keeping the 490:520 ratio", () => {
    const { container } = render(<RuneMark size={52} />);
    const svg = container.querySelector("svg");
    expect(svg).toHaveAttribute("width", "52");
    expect(svg).toHaveAttribute("height", String((52 * 520) / 490));
  });

  it("gives each instance unique gradient ids so defs never collide", () => {
    const { container } = render(
      <>
        <RuneMark />
        <RuneMark />
      </>,
    );
    const ids = Array.from(container.querySelectorAll("linearGradient")).map(
      (g) => g.id,
    );
    expect(ids).toHaveLength(6);
    expect(new Set(ids).size).toBe(6);
  });
});
