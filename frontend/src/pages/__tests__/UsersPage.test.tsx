import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import UsersPage from "@/pages/UsersPage";
import * as invitationAPIs from "@/api/invitationAPIs";
import * as teamAPIs from "@/api/teamAPIs";
import * as userAPIs from "@/api/userAPIs";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import type { TUserListItem } from "@/types/userTypes";
import { useNoticeStore } from "@/stores/noticeStore";

const jsonRes = (body: unknown) =>
  ({ ok: true, json: async () => body }) as unknown as Response;

const errorRes = () => ({ ok: false }) as unknown as Response;

/** GET /teams/tree fixture backing the invite modal's team dropdown —
    "백엔드" (t_b) matches the membership fixtures below. Real API, not
    the dummy tree, per the invite-modal regression fix. */
const TEAM_TREE_FIXTURE = [
  {
    id: "t_b",
    name: "백엔드",
    parentId: null,
    childrenIds: [],
    childCount: 0,
    memberCount: 2,
  },
];

/** One membership + status shape reused across fixture rows. */
const user = (
  userId: string,
  account: string,
  overrides: Partial<TUserListItem> = {},
): TUserListItem => ({
  userId,
  account,
  status: "online",
  memberships: [{ teamId: "t_b", teamName: "백엔드", role: "edit" }],
  lastAccessAt: "2026-07-07T08:12:00Z",
  lastInvitedAt: "2026-07-06T09:00:00Z",
  sessionExpiredAt: null,
  ...overrides,
});

const PAGE_FIXTURE = {
  total: 2,
  page: 1,
  size: 10,
  items: [user("u_1", "k@corp.com"), user("u_2", "m@corp.com")],
};

const mockListSuccess = (page: unknown = PAGE_FIXTURE) =>
  vi.spyOn(userAPIs, "listUsers").mockResolvedValue(jsonRes(page));

const renderPage = () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <UsersPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
};

describe("UsersPage", () => {
  beforeEach(() => {
    vi.spyOn(teamAPIs, "getTeamsTree").mockResolvedValue(
      jsonRes(TEAM_TREE_FIXTURE),
    );
  });
  afterEach(() => vi.restoreAllMocks());

  it("renders rows from the GET /users response", async () => {
    mockListSuccess();
    renderPage();

    expect(await screen.findByText("k@corp.com")).toBeInTheDocument();
    expect(screen.getByText("m@corp.com")).toBeInTheDocument();
    expect(screen.getByText("총 2명 · 10명/페이지")).toBeInTheDocument();
  });

  it("calls listUsers with the debounced search term", async () => {
    const spy = mockListSuccess();
    const typer = userEvent.setup();
    renderPage();
    await screen.findByText("k@corp.com");

    await typer.type(screen.getByPlaceholderText("계정 검색"), "kim");

    await waitFor(() =>
      expect(spy).toHaveBeenLastCalledWith(
        expect.objectContaining({ search: "kim", page: 1 }),
      ),
    );
  });

  it("calls listUsers with the selected status filter", async () => {
    const spy = mockListSuccess();
    const typer = userEvent.setup();
    renderPage();
    await screen.findByText("k@corp.com");

    await typer.click(screen.getByRole("button", { name: "status 필터" }));
    await typer.click(screen.getByRole("option", { name: "세션 만료" }));

    await waitFor(() =>
      expect(spy).toHaveBeenLastCalledWith(
        expect.objectContaining({ status: "session_expired", page: 1 }),
      ),
    );
  });

  it("clears checked rows when a filter/sort/search changes", async () => {
    mockListSuccess();
    const typer = userEvent.setup();
    renderPage();
    await screen.findByText("k@corp.com");

    await typer.click(
      screen.getByRole("checkbox", { name: "k@corp.com 선택" }),
    );
    expect(screen.getByText("1 SELECTED")).toBeInTheDocument();

    /* Checked rows may drop out of the new result, so changing what's
       listed must reset the selection. */
    await typer.click(screen.getByRole("button", { name: "status 필터" }));
    await typer.click(screen.getByRole("option", { name: "세션 만료" }));

    await waitFor(() =>
      expect(screen.queryByText("1 SELECTED")).not.toBeInTheDocument(),
    );
  });

  it("clears checked rows when moving to another page", async () => {
    mockListSuccess({
      total: 12, // > PAGE_SIZE → a second page exists
      page: 1,
      size: 10,
      items: [user("u_1", "k@corp.com"), user("u_2", "m@corp.com")],
    });
    const typer = userEvent.setup();
    renderPage();
    await screen.findByText("k@corp.com");

    await typer.click(
      screen.getByRole("checkbox", { name: "k@corp.com 선택" }),
    );
    expect(screen.getByText("1 SELECTED")).toBeInTheDocument();

    await typer.click(screen.getByRole("button", { name: "2" }));

    await waitFor(() =>
      expect(screen.queryByText("1 SELECTED")).not.toBeInTheDocument(),
    );
  });

  it("shows SC-11 state B when the list is empty with no active filter", async () => {
    mockListSuccess({ total: 0, page: 1, size: 10, items: [] });
    renderPage();

    expect(
      await screen.findByText("아직 초대한 멤버가 없습니다"),
    ).toBeInTheDocument();
  });

  it("shows the no-results row (not state B) when a search yields nothing", async () => {
    const spy = vi
      .spyOn(userAPIs, "listUsers")
      .mockResolvedValueOnce(jsonRes(PAGE_FIXTURE))
      .mockResolvedValue(jsonRes({ total: 0, page: 1, size: 10, items: [] }));
    const typer = userEvent.setup();
    renderPage();
    await screen.findByText("k@corp.com");

    await typer.type(
      screen.getByPlaceholderText("계정 검색"),
      "nobody@nowhere",
    );

    expect(
      await screen.findByText("검색 결과가 없습니다."),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("아직 초대한 멤버가 없습니다"),
    ).not.toBeInTheDocument();
    expect(spy).toHaveBeenCalled();
  });

  it("shows SC-11 state C when the list fails to load", async () => {
    vi.spyOn(userAPIs, "listUsers").mockResolvedValue(errorRes());
    renderPage();

    expect(
      await screen.findByText("멤버 정보를 불러올 수 없습니다."),
    ).toBeInTheDocument();
  });

  it("invites a member via POST /invitations and closes the modal on success", async () => {
    mockListSuccess();
    const postSpy = vi
      .spyOn(invitationAPIs, "postInvitation")
      .mockResolvedValue(
        jsonRes({
          userId: "u_3",
          account: "new@corp.com",
          status: "invite_pending",
          codeSent: true,
        }),
      );
    const typer = userEvent.setup();
    renderPage();
    await screen.findByText("k@corp.com");

    await typer.click(
      screen.getByRole("button", { name: BTN_TEXT.inviteMember }),
    );
    await typer.type(
      screen.getByPlaceholderText("user@corp.com"),
      "new@corp.com",
    );
    await typer.click(screen.getByRole("button", { name: "세트 1 팀" }));
    await typer.click(screen.getByRole("option", { name: "백엔드" }));
    await typer.click(screen.getByRole("button", { name: "세트 1 role" }));
    await typer.click(screen.getByRole("option", { name: "edit" }));
    await typer.click(
      screen.getByRole("button", { name: BTN_TEXT.sendInvitation }),
    );

    await waitFor(() =>
      expect(postSpy).toHaveBeenCalledWith({
        account: "new@corp.com",
        memberships: [{ teamId: "t_b", role: "edit" }],
      }),
    );
    await waitFor(() =>
      expect(
        screen.queryByText(MODAL_TITLES.inviteMember),
      ).not.toBeInTheDocument(),
    );
  });

  it("sources the invite modal's team dropdown from the real teams tree, not the dummy fixture", async () => {
    mockListSuccess();
    // "그로스" / t_growth exists only in this GET /teams/tree fixture —
    // not in DUMMY_TEAMS_TREE — so offering/selecting it proves the
    // dropdown no longer reads the dummy TEAM_OPTIONS tree (regression
    // guard for the invite-modal team picker).
    vi.spyOn(teamAPIs, "getTeamsTree").mockResolvedValue(
      jsonRes([
        {
          id: "t_growth",
          name: "그로스",
          parentId: null,
          childrenIds: [],
          childCount: 0,
          memberCount: 0,
        },
      ]),
    );
    const postSpy = vi
      .spyOn(invitationAPIs, "postInvitation")
      .mockResolvedValue(
        jsonRes({
          userId: "u_4",
          account: "growth@corp.com",
          status: "invite_pending",
          codeSent: true,
        }),
      );
    const typer = userEvent.setup();
    renderPage();
    await screen.findByText("k@corp.com");

    await typer.click(
      screen.getByRole("button", { name: BTN_TEXT.inviteMember }),
    );
    await typer.type(
      screen.getByPlaceholderText("user@corp.com"),
      "growth@corp.com",
    );
    await typer.click(screen.getByRole("button", { name: "세트 1 팀" }));
    expect(
      await screen.findByRole("option", { name: "그로스" }),
    ).toBeInTheDocument();
    await typer.click(screen.getByRole("option", { name: "그로스" }));
    await typer.click(screen.getByRole("button", { name: "세트 1 role" }));
    await typer.click(screen.getByRole("option", { name: "edit" }));
    await typer.click(
      screen.getByRole("button", { name: BTN_TEXT.sendInvitation }),
    );

    await waitFor(() =>
      expect(postSpy).toHaveBeenCalledWith({
        account: "growth@corp.com",
        memberships: [{ teamId: "t_growth", role: "edit" }],
      }),
    );
  });

  it("shows the duplicate-account state when POST /invitations rejects with ALREADY_TEAM_MEMBER", async () => {
    mockListSuccess();
    vi.spyOn(invitationAPIs, "postInvitation").mockResolvedValue(
      new Response(JSON.stringify({ code: "ALREADY_TEAM_MEMBER" }), {
        status: 409,
      }),
    );
    const typer = userEvent.setup();
    renderPage();
    await screen.findByText("k@corp.com");

    await typer.click(
      screen.getByRole("button", { name: BTN_TEXT.inviteMember }),
    );
    await typer.type(
      screen.getByPlaceholderText("user@corp.com"),
      "k@corp.com",
    );
    await typer.click(screen.getByRole("button", { name: "세트 1 팀" }));
    await typer.click(screen.getByRole("option", { name: "백엔드" }));
    await typer.click(screen.getByRole("button", { name: "세트 1 role" }));
    await typer.click(screen.getByRole("option", { name: "edit" }));
    await typer.click(
      screen.getByRole("button", { name: BTN_TEXT.sendInvitation }),
    );

    expect(
      await screen.findByText(
        "이미 등록된 계정입니다. 멤버 추가 또는 초대 코드 재전송을 사용하세요.",
      ),
    ).toBeInTheDocument();
  });

  it("shows the batch failure modal listing the failed account on a mixed resend", async () => {
    mockListSuccess();
    vi.spyOn(invitationAPIs, "resendInvitation").mockImplementation(
      async (userId: string) =>
        userId === "u_1"
          ? jsonRes({ userId, status: "invite_pending" })
          : new Response(null, { status: 500 }),
    );
    const typer = userEvent.setup();
    renderPage();
    await screen.findByText("k@corp.com");

    await typer.click(screen.getByLabelText("k@corp.com 선택"));
    await typer.click(screen.getByLabelText("m@corp.com 선택"));
    await typer.click(
      screen.getByRole("button", { name: BTN_TEXT.resendInvitationCode }),
    );

    const modal = (await screen.findByText(MODAL_TITLES.batchFailure)).closest(
      "div",
    ) as HTMLElement;
    expect(within(modal).getByText("m@corp.com")).toBeInTheDocument();
    expect(within(modal).queryByText("k@corp.com")).not.toBeInTheDocument();
  });

  it("shows the batch failure modal listing the failed account on a partial bulk delete", async () => {
    mockListSuccess();
    const deleteSpy = vi.spyOn(userAPIs, "deleteUsers").mockResolvedValue(
      jsonRes({
        succeeded: ["u_1"],
        failed: [{ id: "u_2", code: "USER_NOT_FOUND", message: "x" }],
      }),
    );
    const typer = userEvent.setup();
    renderPage();
    await screen.findByText("k@corp.com");

    await typer.click(screen.getByLabelText("k@corp.com 선택"));
    await typer.click(screen.getByLabelText("m@corp.com 선택"));
    await typer.click(screen.getByRole("button", { name: BTN_TEXT.delete }));
    await typer.click(
      screen.getAllByRole("button", { name: BTN_TEXT.delete }).slice(-1)[0],
    );

    expect(deleteSpy).toHaveBeenCalledWith(["u_1", "u_2"]);
    const modal = (await screen.findByText(MODAL_TITLES.batchFailure)).closest(
      "div",
    ) as HTMLElement;
    expect(within(modal).getByText("m@corp.com")).toBeInTheDocument();
    expect(
      within(modal).getByText("사용자를 찾을 수 없습니다"),
    ).toBeInTheDocument();
    expect(within(modal).queryByText("k@corp.com")).not.toBeInTheDocument();
  });

  it("clears selection and shows success notice on a full-success bulk delete", async () => {
    mockListSuccess();
    vi.spyOn(userAPIs, "deleteUsers").mockResolvedValue(
      jsonRes({ succeeded: ["u_1", "u_2"], failed: [] }),
    );
    const showNoticeSpy = vi.spyOn(useNoticeStore.getState(), "showNotice");
    const typer = userEvent.setup();
    renderPage();
    await screen.findByText("k@corp.com");

    await typer.click(screen.getByLabelText("k@corp.com 선택"));
    await typer.click(screen.getByLabelText("m@corp.com 선택"));
    await typer.click(screen.getByRole("button", { name: BTN_TEXT.delete }));
    await typer.click(
      screen.getAllByRole("button", { name: BTN_TEXT.delete }).slice(-1)[0],
    );

    await waitFor(() =>
      expect(showNoticeSpy).toHaveBeenCalledWith(
        "멤버 삭제",
        "멤버를 삭제했습니다.",
        "info",
      ),
    );
    await waitFor(() =>
      expect(
        screen.queryByText(MODAL_TITLES.batchFailure),
      ).not.toBeInTheDocument(),
    );
    await waitFor(() =>
      expect(
        (screen.getByLabelText("k@corp.com 선택") as HTMLInputElement).checked,
      ).toBe(false),
    );
  });
});
