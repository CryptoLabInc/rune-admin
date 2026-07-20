import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import Feedback from "@/components/elements/Feedback";
import { BTN_TEXT } from "@/constants/commonConstants";

describe("Feedback", () => {
  it("renders title, description, and the action slot", () => {
    render(
      <Feedback
        state="empty"
        title="새로운 팀을 만들어 주세요."
        description="첫 팀은 조직 위계의 루트가 됩니다."
        action={<button type="button">새 팀 만들기</button>}
      />,
    );
    expect(screen.getByText("새로운 팀을 만들어 주세요.")).toBeInTheDocument();
    expect(
      screen.getByText("첫 팀은 조직 위계의 루트가 됩니다."),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: BTN_TEXT.createTeam }),
    ).toBeInTheDocument();
  });

  it("announces error state as an alert, others as status", () => {
    const { rerender } = render(
      <Feedback state="error" title="팀 정보를 불러올 수 없습니다." />,
    );
    expect(screen.getByRole("alert")).toBeInTheDocument();

    rerender(<Feedback state="loading" title="불러오는 중" />);
    expect(screen.getByRole("status")).toBeInTheDocument();
  });

  it("merges an external className", () => {
    render(<Feedback state="empty" title="비어 있음" className="mt-4" />);
    expect(screen.getByRole("status")).toHaveClass("mt-4");
  });
});
