import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import MemberDetailDrawer from "@/components/users/MemberDetailDrawer";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import type { TBatchResult, TTeamTree } from "@/types/teamTypes";
import type { TUserListItem } from "@/types/userTypes";
import { useNoticeStore } from "@/stores/noticeStore";

/** Minimal team fixture — matches the user's one membership plus a
    second, unjoined team for the add picker. */
const TEAMS: TTeamTree = [
  {
    id: "t_b",
    name: "백엔드",
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
    memberCount: 1,
  },
];

const USER: TUserListItem = {
  userId: "u_1",
  account: "k@corp.com",
  status: "online",
  memberships: [{ teamId: "t_b", teamName: "백엔드", role: "edit" }],
  lastAccessAt: "2026-07-07T08:12:00Z",
  lastInvitedAt: "2026-07-06T09:00:00Z",
  sessionExpiredAt: null,
};

const PENDING_USER: TUserListItem = {
  ...USER,
  status: "invite_pending",
  lastAccessAt: null,
};

const SUCCESS_RESULT: TBatchResult = { succeeded: ["t_b"], failed: [] };

const noop = async () => {};

const baseProps = () => ({
  user: USER,
  onClose: vi.fn(),
  onUpdateRoles: vi.fn().mockResolvedValue(SUCCESS_RESULT),
  onRemoveMemberships: vi.fn().mockResolvedValue(SUCCESS_RESULT),
  onAddMembership: vi.fn().mockResolvedValue(undefined),
  onResendCode: vi.fn().mockImplementation(noop),
  onDeleteMember: vi.fn().mockImplementation(noop),
  onDeactivateSession: vi.fn().mockResolvedValue(undefined),
  onCancelInvitation: vi.fn().mockImplementation(noop),
  teams: TEAMS,
});

describe("MemberDetailDrawer", () => {
  it("renders the user's detail and membership list", () => {
    render(<MemberDetailDrawer {...baseProps()} />);
    expect(screen.getByText("k@corp.com")).toBeInTheDocument();
    expect(screen.getByText("백엔드")).toBeInTheDocument();
  });

  it("shows a placeholder — — row when the member belongs to no team", () => {
    render(
      <MemberDetailDrawer
        {...baseProps()}
        user={{ ...USER, memberships: [] }}
      />,
    );
    expect(screen.getByText("소속 팀 (0)")).toBeInTheDocument();
    /* Two em-dash cells (team + role) in the single placeholder row. */
    const dashes = screen.getAllByText("—");
    expect(dashes.length).toBeGreaterThanOrEqual(2);
  });

  it("stages a role change, confirms, and calls onUpdateRoles with {updates}", async () => {
    const user = userEvent.setup();
    const props = baseProps();
    render(<MemberDetailDrawer {...props} />);

    await user.click(screen.getByRole("button", { name: "백엔드 role" }));
    await user.click(screen.getByRole("option", { name: "write" }));

    await user.click(
      screen.getByRole("button", { name: BTN_TEXT.updateChanges }),
    );
    await user.click(screen.getByRole("button", { name: BTN_TEXT.change }));

    expect(props.onUpdateRoles).toHaveBeenCalledWith([
      { teamId: "t_b", role: "write" },
    ]);
  });

  it("keeps the changed team/role visible in the confirm modal after success", async () => {
    const user = userEvent.setup();
    render(<MemberDetailDrawer {...baseProps()} />);

    await user.click(screen.getByRole("button", { name: "백엔드 role" }));
    await user.click(screen.getByRole("option", { name: "write" }));
    await user.click(
      screen.getByRole("button", { name: BTN_TEXT.updateChanges }),
    );
    await user.click(screen.getByRole("button", { name: BTN_TEXT.change }));

    /* Regression: a successful apply makes the drawer recompute its
       `changes` prop to empty (baseRole catches up), which used to blank
       the modal table. The success view must still list what changed. */
    await screen.findByText("권한이 변경되었습니다.");
    const modalBody = screen
      .getByText("다음 멤버의 권한을 변경합니다:")
      .closest("div")!;
    expect(within(modalBody).getByText("백엔드")).toBeInTheDocument();
    expect(within(modalBody).getByText("write")).toBeInTheDocument();
  });

  it("shows the failure modal with the team name when the role batch partially fails", async () => {
    const user = userEvent.setup();
    const props = baseProps();
    props.onUpdateRoles.mockResolvedValue({
      succeeded: [],
      failed: [{ id: "t_b", code: "TEAM_NOT_FOUND", message: "x" }],
    } satisfies TBatchResult);
    render(<MemberDetailDrawer {...props} />);

    await user.click(screen.getByRole("button", { name: "백엔드 role" }));
    await user.click(screen.getByRole("option", { name: "write" }));
    await user.click(
      screen.getByRole("button", { name: BTN_TEXT.updateChanges }),
    );
    await user.click(screen.getByRole("button", { name: BTN_TEXT.change }));

    expect(
      await screen.findByText(MODAL_TITLES.batchFailure),
    ).toBeInTheDocument();
    expect(screen.getByText("팀을 찾을 수 없습니다")).toBeInTheDocument();
    const failureItem = screen.getByText("팀을 찾을 수 없습니다").closest("li");
    expect(failureItem).not.toBeNull();
    expect(failureItem).toHaveTextContent("백엔드");
  });

  it("removes a checked membership and calls onRemoveMemberships(teamIds)", async () => {
    const user = userEvent.setup();
    const props = baseProps();
    const showNoticeSpy = vi.spyOn(useNoticeStore.getState(), "showNotice");
    render(<MemberDetailDrawer {...props} />);

    await user.click(screen.getByRole("checkbox", { name: "백엔드 선택" }));
    await user.click(screen.getByRole("button", { name: BTN_TEXT.remove }));
    await user.click(
      screen.getAllByRole("button", { name: BTN_TEXT.remove }).slice(-1)[0],
    );

    expect(props.onRemoveMemberships).toHaveBeenCalledWith(["t_b"]);

    await waitFor(() => {
      expect(showNoticeSpy).toHaveBeenCalledWith(
        MODAL_TITLES.removeMembership,
        "멤버십이 제거되었습니다.",
        "success",
      );
    });
  });

  it("adds a membership via the picker and calls onAddMembership", async () => {
    const user = userEvent.setup();
    const props = baseProps();
    render(<MemberDetailDrawer {...props} />);

    await user.click(screen.getByRole("button", { name: BTN_TEXT.addTeam }));
    await user.click(screen.getByRole("button", { name: "추가할 팀" }));
    await user.click(screen.getByRole("option", { name: "디자인" }));
    /* No default role — the picker requires an explicit selection. */
    await user.click(screen.getByRole("button", { name: "추가할 role" }));
    await user.click(screen.getByRole("option", { name: "write" }));
    await user.click(screen.getByRole("button", { name: BTN_TEXT.add }));

    await waitFor(() =>
      expect(props.onAddMembership).toHaveBeenCalledWith("t_d", "write"),
    );
  });

  it("disables the role picker when no team is left to join", async () => {
    const user = userEvent.setup();
    /* Member already in every team in the fixture → nothing addable. */
    render(
      <MemberDetailDrawer
        {...baseProps()}
        user={{
          ...USER,
          memberships: [
            { teamId: "t_b", teamName: "백엔드", role: "edit" },
            { teamId: "t_d", teamName: "디자인", role: "read" },
          ],
        }}
      />,
    );

    await user.click(screen.getByRole("button", { name: BTN_TEXT.addTeam }));
    expect(screen.getByRole("button", { name: "추가할 팀" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "추가할 role" })).toBeDisabled();
  });

  it("deactivates the session through the confirm modal", async () => {
    const user = userEvent.setup();
    const props = baseProps();
    render(<MemberDetailDrawer {...props} />);

    await user.click(
      screen.getByRole("button", { name: BTN_TEXT.deactivateSession }),
    );
    expect(
      screen.getByRole("heading", { name: MODAL_TITLES.deactivateSession }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: BTN_TEXT.deactivate }));

    await waitFor(() => expect(props.onDeactivateSession).toHaveBeenCalled());
  });

  it("cancels the invitation through the confirm modal", async () => {
    const user = userEvent.setup();
    const props = { ...baseProps(), user: PENDING_USER };
    const showNoticeSpy = vi.spyOn(useNoticeStore.getState(), "showNotice");
    render(<MemberDetailDrawer {...props} />);

    await user.click(
      screen.getByRole("button", { name: BTN_TEXT.cancelInvitation }),
    );
    expect(
      screen.getByText(
        "k@corp.com의 미사용 초대 코드가 모두 만료됩니다. 유저는 삭제되지 않습니다.",
      ),
    ).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: BTN_TEXT.cancelAction }),
    );

    await waitFor(() => expect(props.onCancelInvitation).toHaveBeenCalled());
    await waitFor(() =>
      expect(showNoticeSpy).toHaveBeenCalledWith(
        "초대 취소",
        "초대를 취소했습니다.",
        "info",
      ),
    );
  });

  it("shows a not-pending notice when cancel rejects with INVITATION_NOT_PENDING", async () => {
    const user = userEvent.setup();
    const props = {
      ...baseProps(),
      user: PENDING_USER,
      onCancelInvitation: vi.fn().mockRejectedValue(
        new Response(JSON.stringify({ code: "INVITATION_NOT_PENDING" }), {
          status: 409,
        }),
      ),
    };
    const showNoticeSpy = vi.spyOn(useNoticeStore.getState(), "showNotice");
    render(<MemberDetailDrawer {...props} />);

    await user.click(
      screen.getByRole("button", { name: BTN_TEXT.cancelInvitation }),
    );
    await user.click(
      screen.getByRole("button", { name: BTN_TEXT.cancelAction }),
    );

    await waitFor(() =>
      expect(showNoticeSpy).toHaveBeenCalledWith(
        "초대 취소",
        "취소할 초대가 없습니다.",
        "error",
      ),
    );
  });
});
