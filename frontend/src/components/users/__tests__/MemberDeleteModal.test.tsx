import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import MemberDeleteModal from "@/components/users/MemberDeleteModal";

const noop = () => {};
const resolve = () => Promise.resolve();

describe("MemberDeleteModal", () => {
  it("shows the team/role table when the target has memberships", () => {
    render(
      <MemberDeleteModal
        targets={[
          {
            account: "kim@corp.com",
            memberships: [{ teamName: "백엔드", role: "edit" }],
          },
        ]}
        onConfirm={resolve}
        onClose={noop}
      />,
    );
    expect(screen.getByText("백엔드")).toBeInTheDocument();
    expect(screen.getByText("edit")).toBeInTheDocument();
    expect(screen.queryByText("소속된 팀이 없습니다.")).not.toBeInTheDocument();
  });

  it("shows the empty-state line instead of the table when the target has no team", () => {
    render(
      <MemberDeleteModal
        targets={[{ account: "solo@corp.com", memberships: [] }]}
        onConfirm={resolve}
        onClose={noop}
      />,
    );
    expect(screen.getByText("소속된 팀이 없습니다.")).toBeInTheDocument();
    /* No table headers when the member belongs to no team. */
    expect(screen.queryByText("팀")).not.toBeInTheDocument();
    expect(screen.queryByText("권한")).not.toBeInTheDocument();
  });

  it("renders per-target empty lines in the bulk view", () => {
    render(
      <MemberDeleteModal
        targets={[
          {
            account: "has@corp.com",
            memberships: [{ teamName: "플랫폼", role: "read" }],
          },
          { account: "none@corp.com", memberships: [] },
        ]}
        onConfirm={resolve}
        onClose={noop}
      />,
    );
    expect(screen.getByText("플랫폼")).toBeInTheDocument();
    expect(screen.getByText("소속된 팀이 없습니다.")).toBeInTheDocument();
  });
});
