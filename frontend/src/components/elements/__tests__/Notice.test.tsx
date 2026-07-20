import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import Notice from "@/components/elements/Notice";

describe("Notice", () => {
  it("renders children as an inline status banner", () => {
    render(<Notice>팀의 기억은 Research로 이전됩니다.</Notice>);
    expect(screen.getByRole("status")).toHaveTextContent(
      "팀의 기억은 Research로 이전됩니다.",
    );
  });

  it("announces the error tone as an alert", () => {
    render(<Notice tone="error">멤버 추가에 실패했습니다.</Notice>);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "멤버 추가에 실패했습니다.",
    );
  });

  it("merges an external className", () => {
    render(
      <Notice tone="success" className="mt-2">
        저장되었습니다.
      </Notice>,
    );
    expect(screen.getByRole("status")).toHaveClass("mt-2");
  });
});
