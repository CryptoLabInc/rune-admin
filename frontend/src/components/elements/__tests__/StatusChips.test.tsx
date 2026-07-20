import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import MemberStatus from "@/components/elements/MemberStatus";
import WorkspaceStatus from "@/components/elements/WorkspaceStatus";

describe("MemberStatus", () => {
  it("renders the Korean label for each state", () => {
    render(<MemberStatus status="invite-expired" />);
    expect(screen.getByText("초대 코드 만료")).toBeInTheDocument();
  });

  it("merges an external className", () => {
    render(<MemberStatus status="online" className="ml-auto" />);
    expect(screen.getByText("온라인")).toHaveClass("ml-auto");
  });
});

describe("WorkspaceStatus", () => {
  it.each([
    ["provisioning", "생성 중"],
    ["running", "실행 중"],
    ["stopping", "정지 중"],
    ["stopped", "정지"],
    ["starting", "재실행 중"],
    ["deleting", "삭제 중"],
    ["error", "사용 불가"],
  ] as const)("renders the Korean label for %s", (status, label) => {
    render(<WorkspaceStatus status={status} />);
    expect(screen.getByText(label)).toBeInTheDocument();
  });

  it("merges an external className", () => {
    render(<WorkspaceStatus status="error" className="ml-auto" />);
    // The label sits inside a child span, so className merges onto the
    // pill root (the label's parent).
    expect(screen.getByText("사용 불가").parentElement).toHaveClass("ml-auto");
  });
});
