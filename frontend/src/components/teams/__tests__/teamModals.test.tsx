import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import AddMemberModal from "@/components/teams/AddMemberModal";
import CreateTeamModal from "@/components/teams/CreateTeamModal";
import DeleteTeamModal from "@/components/teams/DeleteTeamModal";
import RemoveMembershipModal from "@/components/teams/RemoveMembershipModal";
import RenameTeamModal from "@/components/teams/RenameTeamModal";
import RoleChangeConfirmModal from "@/components/teams/RoleChangeConfirmModal";
import { BTN_TEXT } from "@/constants/commonConstants";
import type { TTeamTree } from "@/types/teamTypes";

/** Minimal team fixture — a root team (플랫폼), a sibling of it (디자인),
    and another root (프로덕트) used as the delete-modal transfer target. */
const TEAMS: TTeamTree = [
  {
    id: "t_a",
    name: "플랫폼",
    parentId: null,
    childrenIds: [],
    childCount: 0,
    memberCount: 2,
  },
  {
    id: "t_d",
    name: "디자인",
    parentId: null,
    childrenIds: [],
    childCount: 0,
    memberCount: 2,
  },
  {
    id: "t_prod",
    name: "프로덕트",
    parentId: null,
    childrenIds: [],
    childCount: 0,
    memberCount: 5,
  },
];

describe("CreateTeamModal", () => {
  it("disables 생성 until a valid name, warns on duplicates", async () => {
    const user = userEvent.setup();
    const onCreate = vi.fn();
    render(
      <CreateTeamModal teams={TEAMS} onClose={() => {}} onCreate={onCreate} />,
    );

    const submit = screen.getByRole("button", { name: BTN_TEXT.create });
    expect(submit).toBeDisabled();

    // Duplicate of an existing root team.
    await user.type(screen.getByLabelText("팀 이름"), "플랫폼");
    expect(
      screen.getByText("같은 상위 팀에 동일한 이름이 이미 있습니다."),
    ).toBeInTheDocument();
    expect(submit).toBeDisabled();

    await user.clear(screen.getByLabelText("팀 이름"));
    await user.type(screen.getByLabelText("팀 이름"), "Payments");
    expect(submit).toBeEnabled();

    await user.click(submit);
    expect(onCreate).toHaveBeenCalledWith("Payments", null);
  });

  it("rejects characters outside 숫자·한글·영어 and - _", async () => {
    const user = userEvent.setup();
    render(
      <CreateTeamModal teams={TEAMS} onClose={() => {}} onCreate={() => {}} />,
    );

    const input = screen.getByLabelText("팀 이름");
    const submit = screen.getByRole("button", { name: BTN_TEXT.create });

    await user.type(input, "pay!ments");
    expect(
      screen.getByText("숫자·한글·영어와 - _ 만 사용할 수 있습니다.", {
        selector: '[role="alert"], span',
      }),
    ).toBeInTheDocument();
    expect(submit).toBeDisabled();

    await user.clear(input);
    await user.type(input, "결제-team_2");
    expect(submit).toBeEnabled();
  });

  it("renders the server-mapped error passed in via the error prop", () => {
    render(
      <CreateTeamModal
        teams={TEAMS}
        error="같은 상위 팀에 동일한 이름이 이미 있습니다."
        onClose={() => {}}
        onCreate={() => {}}
      />,
    );
    expect(
      screen.getByText("같은 상위 팀에 동일한 이름이 이미 있습니다."),
    ).toBeInTheDocument();
  });
});

describe("RenameTeamModal", () => {
  it("warns on a duplicate sibling name and calls onRename with the new name", async () => {
    const user = userEvent.setup();
    const onRename = vi.fn();
    render(
      <RenameTeamModal
        currentName="백엔드"
        currentParentId={null}
        teams={TEAMS}
        onClose={() => {}}
        onRename={onRename}
      />,
    );

    await user.clear(screen.getByLabelText("팀 이름"));
    await user.type(screen.getByLabelText("팀 이름"), "플랫폼");
    expect(
      screen.getByText("같은 상위 팀에 동일한 이름이 이미 있습니다."),
    ).toBeInTheDocument();

    await user.clear(screen.getByLabelText("팀 이름"));
    await user.type(screen.getByLabelText("팀 이름"), "새이름");
    await user.click(screen.getByRole("button", { name: BTN_TEXT.save }));
    expect(onRename).toHaveBeenCalledWith("새이름");
  });

  it("renders the server-mapped error passed in via the error prop", () => {
    render(
      <RenameTeamModal
        currentName="백엔드"
        currentParentId={null}
        teams={TEAMS}
        error="팀 이름 형식이 올바르지 않습니다."
        onClose={() => {}}
        onRename={() => {}}
      />,
    );
    expect(
      screen.getByText("팀 이름 형식이 올바르지 않습니다."),
    ).toBeInTheDocument();
  });
});

describe("DeleteTeamModal", () => {
  it("blocks deletion for a team with sub-teams (alert variant)", () => {
    render(
      <DeleteTeamModal
        teamId="t_a"
        teamName="플랫폼"
        hasChildren
        teams={TEAMS}
        onClose={() => {}}
        onDelete={() => {}}
      />,
    );
    expect(screen.getByText("팀을 삭제할 수 없습니다")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: BTN_TEXT.deleteTeam }),
    ).not.toBeInTheDocument();
  });

  it("transfer requires a target team plus its exact typed name", async () => {
    const user = userEvent.setup();
    const onDelete = vi.fn();
    render(
      <DeleteTeamModal
        teamId="t_d"
        teamName="디자인"
        hasChildren={false}
        teams={TEAMS}
        onClose={() => {}}
        onDelete={onDelete}
      />,
    );
    const submit = screen.getByRole("button", { name: BTN_TEXT.deleteTeam });
    expect(submit).toBeDisabled();

    // pick 프로덕트 as the receiving team
    await user.click(screen.getByRole("button", { name: "이전받을 팀" }));
    await user.click(screen.getByRole("option", { name: "프로덕트" }));
    expect(submit).toBeDisabled(); // confirmation not typed yet

    const confirm = screen.getByLabelText("확인 — 타겟 팀명 입력");
    await user.type(confirm, "프로덕");
    expect(
      screen.getByText("타겟 팀명이 일치하지 않습니다."),
    ).toBeInTheDocument();
    expect(submit).toBeDisabled();

    await user.type(confirm, "트"); // now exactly 프로덕트
    expect(submit).toBeEnabled();

    await user.click(submit);
    expect(onDelete).toHaveBeenCalledWith("transfer", "t_prod");
  });

  it("purge requires typing the deleted team's own name", async () => {
    const user = userEvent.setup();
    const onDelete = vi.fn();
    render(
      <DeleteTeamModal
        teamId="t_d"
        teamName="디자인"
        hasChildren={false}
        teams={TEAMS}
        onClose={() => {}}
        onDelete={onDelete}
      />,
    );
    const submit = screen.getByRole("button", { name: BTN_TEXT.deleteTeam });

    await user.click(screen.getByRole("radio", { name: /팀 내 기억 삭제/ }));
    expect(submit).toBeDisabled(); // confirmation required now

    await user.type(screen.getByLabelText("확인 — 삭제할 팀명 입력"), "디자인");
    expect(submit).toBeEnabled();

    await user.click(submit);
    expect(onDelete).toHaveBeenCalledWith("purge", undefined);
  });

  it("renders the server-mapped error passed in via the error prop", async () => {
    const user = userEvent.setup();
    render(
      <DeleteTeamModal
        teamId="t_d"
        teamName="디자인"
        hasChildren={false}
        teams={TEAMS}
        error="하위 팀이 있어 삭제할 수 없습니다."
        onClose={() => {}}
        onDelete={() => {}}
      />,
    );
    await user.click(screen.getByRole("radio", { name: /팀 내 기억 삭제/ }));
    await user.type(screen.getByLabelText("확인 — 삭제할 팀명 입력"), "디자인");
    expect(
      screen.getByText("하위 팀이 있어 삭제할 수 없습니다."),
    ).toBeInTheDocument();
  });
});

describe("AddMemberModal", () => {
  it("validates email format", async () => {
    const user = userEvent.setup();
    render(
      <AddMemberModal
        teamName="백엔드"
        onClose={() => {}}
        onInvite={() => {}}
      />,
    );
    const submit = screen.getByRole("button", { name: BTN_TEXT.invite });

    await user.type(screen.getByLabelText("계정명 (email)"), "not-an-email");
    expect(
      screen.getByText("올바른 이메일 형식이 아닙니다."),
    ).toBeInTheDocument();
    expect(submit).toBeDisabled();
  });

  it("renders the server-mapped error passed in via the error prop", () => {
    render(
      <AddMemberModal
        teamName="백엔드"
        error="이미 초대된 사용자입니다."
        onClose={() => {}}
        onInvite={() => {}}
      />,
    );
    expect(screen.getByText("이미 초대된 사용자입니다.")).toBeInTheDocument();
  });
});

describe("RemoveMembershipModal", () => {
  it("lists removals, always shows the sub-team notice, confirms", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    render(
      <RemoveMembershipModal
        teamName="백엔드"
        members={[{ account: "k@corp.com", role: "edit" }]}
        onClose={() => {}}
        onConfirm={onConfirm}
      />,
    );
    expect(screen.getByText("멤버십 제거")).toBeInTheDocument();
    expect(screen.getByText("k@corp.com")).toBeInTheDocument();
    expect(screen.getByText(/하위 팀 소속은 유지됩니다/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: BTN_TEXT.remove }));
    expect(onConfirm).toHaveBeenCalled();
  });
});

describe("RoleChangeConfirmModal", () => {
  it("lists staged changes and confirms", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    render(
      <RoleChangeConfirmModal
        changes={[{ account: "k@corp.com", from: "edit", to: "write" }]}
        onClose={() => {}}
        onConfirm={onConfirm}
      />,
    );
    expect(screen.getByText("k@corp.com")).toBeInTheDocument();
    expect(screen.getByText("write")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: BTN_TEXT.change }));
    expect(onConfirm).toHaveBeenCalled();
  });
});
