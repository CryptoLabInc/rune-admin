import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import MemberStatus from "@/components/elements/MemberStatus";
import StorageStatus from "@/components/elements/StorageStatus";

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

describe("StorageStatus", () => {
  it("renders the raw state as mono EN label", () => {
    render(<StorageStatus status="running" />);
    expect(screen.getByText("running")).toBeInTheDocument();
  });

  it("merges an external className", () => {
    render(<StorageStatus status="error" className="ml-auto" />);
    expect(screen.getByText("error")).toHaveClass("ml-auto");
  });
});
