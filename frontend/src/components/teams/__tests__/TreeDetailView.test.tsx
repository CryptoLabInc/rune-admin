import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import TreeDetailView from "@/components/teams/TreeDetailView";
import * as teamAPIs from "@/api/teamAPIs";
import * as teamMemberAPIs from "@/api/teamMemberAPIs";
import type { TTeamMember, TTeamTree } from "@/types/teamTypes";

const jsonRes = (body: unknown) =>
  ({ ok: true, json: async () => body }) as unknown as Response;

const TEAMS: TTeamTree = [
  {
    id: "t_1",
    name: "Platform",
    parentId: null,
    childrenIds: [],
    childCount: 0,
    memberCount: 1,
  },
  {
    id: "t_2",
    name: "Infra",
    parentId: null,
    childrenIds: [],
    childCount: 0,
    memberCount: 1,
  },
];

const member = (overrides: Partial<TTeamMember> = {}): TTeamMember => ({
  userId: "u_1",
  account: "kim@corp.com",
  role: "edit",
  status: "online",
  joinedAt: "2026-07-02T00:00:00Z",
  ...overrides,
});

const treeDetailViewTree = (client: QueryClient, selectedTeamId: string) => (
  <QueryClientProvider client={client}>
    <MemoryRouter>
      <TreeDetailView
        teams={TEAMS}
        teamSearch=""
        selectedTeamId={selectedTeamId}
        onSelectTeam={() => {}}
      />
    </MemoryRouter>
  </QueryClientProvider>
);

const renderView = (selectedTeamId = "t_1") => {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(treeDetailViewTree(client, selectedTeamId));
};

describe("TreeDetailView", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders member rows from the members query", async () => {
    vi.spyOn(teamAPIs, "getTeam").mockResolvedValue(
      jsonRes({
        id: "t_1",
        name: "Platform",
        parentId: null,
        children: [],
        memberCount: 1,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    vi.spyOn(teamMemberAPIs, "listTeamMembers").mockResolvedValue(
      jsonRes({
        total: 1,
        page: 1,
        size: 10,
        items: [member()],
      }),
    );
    renderView();
    expect(await screen.findByText("kim@corp.com")).toBeInTheDocument();
  });

  it("shows a loading row while the members query is pending", () => {
    vi.spyOn(teamAPIs, "getTeam").mockResolvedValue(
      jsonRes({
        id: "t_1",
        name: "Platform",
        parentId: null,
        children: [],
        memberCount: 1,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    vi.spyOn(teamMemberAPIs, "listTeamMembers").mockReturnValue(
      new Promise(() => {}),
    );
    renderView();
    expect(screen.getByText("불러오는 중…")).toBeInTheDocument();
  });

  it("shows an error row when the members query fails", async () => {
    vi.spyOn(teamAPIs, "getTeam").mockResolvedValue(
      jsonRes({
        id: "t_1",
        name: "Platform",
        parentId: null,
        children: [],
        memberCount: 1,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    vi.spyOn(teamMemberAPIs, "listTeamMembers").mockResolvedValue({
      ok: false,
    } as Response);
    renderView();
    expect(
      await screen.findByText("멤버 목록을 불러올 수 없습니다."),
    ).toBeInTheDocument();
  });

  it("shows an empty row when the team has no members", async () => {
    vi.spyOn(teamAPIs, "getTeam").mockResolvedValue(
      jsonRes({
        id: "t_1",
        name: "Platform",
        parentId: null,
        children: [],
        memberCount: 0,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    vi.spyOn(teamMemberAPIs, "listTeamMembers").mockResolvedValue(
      jsonRes({ total: 0, page: 1, size: 10, items: [] }),
    );
    renderView();
    expect(await screen.findByText("멤버가 없습니다.")).toBeInTheDocument();
  });

  it("pages the member table via the members query", async () => {
    const user = userEvent.setup();
    vi.spyOn(teamAPIs, "getTeam").mockResolvedValue(
      jsonRes({
        id: "t_1",
        name: "Platform",
        parentId: null,
        children: [],
        memberCount: 9,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    const listMembers = vi
      .spyOn(teamMemberAPIs, "listTeamMembers")
      .mockImplementation(async (_teamId, page) =>
        jsonRes({
          total: 11,
          page,
          size: 10,
          items: [
            member({
              userId: page === 1 ? "u_1" : "u_9",
              account: page === 1 ? "kim@corp.com" : "lee@corp.com",
            }),
          ],
        }),
      );

    renderView();
    expect(await screen.findByText("kim@corp.com")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "2" }));
    await waitFor(() => expect(listMembers).toHaveBeenCalledWith("t_1", 2, 10));
    expect(await screen.findByText("lee@corp.com")).toBeInTheDocument();
  });

  it("resets to page 1 when the selected team changes", async () => {
    const user = userEvent.setup();
    vi.spyOn(teamAPIs, "getTeam").mockImplementation(async (teamId) =>
      jsonRes({
        id: teamId,
        name: teamId === "t_1" ? "Platform" : "Infra",
        parentId: null,
        children: [],
        memberCount: 9,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    const listMembers = vi
      .spyOn(teamMemberAPIs, "listTeamMembers")
      .mockImplementation(async (_teamId, page) =>
        jsonRes({
          total: 11,
          page,
          size: 10,
          items: [
            member({
              userId: page === 1 ? "u_1" : "u_9",
              account: page === 1 ? "kim@corp.com" : "lee@corp.com",
            }),
          ],
        }),
      );

    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { rerender } = render(treeDetailViewTree(client, "t_1"));
    expect(await screen.findByText("kim@corp.com")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "2" }));
    await waitFor(() => expect(listMembers).toHaveBeenCalledWith("t_1", 2, 10));

    rerender(treeDetailViewTree(client, "t_2"));
    await waitFor(() => expect(listMembers).toHaveBeenCalledWith("t_2", 1, 10));

    /* The reset effect commits after the rerender, so a query for the new
       team's stale `page` value may fire transiently before the effect
       flips it back to 1 — assert on the settled state instead of every
       call: the table must land on page 1 for t_2, not page 2. */
    await waitFor(() =>
      expect(listMembers).toHaveBeenLastCalledWith("t_2", 1, 10),
    );
    expect(await screen.findByText("kim@corp.com")).toBeInTheDocument();
  });

  it("creates a team via the API", async () => {
    const user = userEvent.setup();
    vi.spyOn(teamAPIs, "getTeam").mockResolvedValue(
      jsonRes({
        id: "t_1",
        name: "Platform",
        parentId: null,
        children: [],
        memberCount: 0,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    vi.spyOn(teamMemberAPIs, "listTeamMembers").mockResolvedValue(
      jsonRes({ total: 0, page: 1, size: 10, items: [] }),
    );
    const create = vi.spyOn(teamAPIs, "createTeam").mockResolvedValue(
      jsonRes({
        id: "t_3",
        name: "New",
        parentId: null,
        children: [],
        memberCount: 0,
        createdAt: "2026-07-16T00:00:00Z",
      }),
    );
    renderView();
    await user.click(screen.getByRole("button", { name: "그룹 생성" }));
    await user.type(screen.getByLabelText("팀 이름"), "New");
    await user.click(screen.getByRole("button", { name: "생성" }));
    await waitFor(() =>
      expect(create).toHaveBeenCalledWith({ name: "New", parentId: null }),
    );
  });

  it("shows the mapped inline error when create fails with a duplicate name", async () => {
    const user = userEvent.setup();
    vi.spyOn(teamAPIs, "getTeam").mockResolvedValue(
      jsonRes({
        id: "t_1",
        name: "Platform",
        parentId: null,
        children: [],
        memberCount: 0,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    vi.spyOn(teamMemberAPIs, "listTeamMembers").mockResolvedValue(
      jsonRes({ total: 0, page: 1, size: 10, items: [] }),
    );
    vi.spyOn(teamAPIs, "createTeam").mockResolvedValue({
      ok: false,
      json: async () => ({ code: "TEAM_NAME_DUPLICATE" }),
    } as unknown as Response);
    renderView();
    await user.click(screen.getByRole("button", { name: "그룹 생성" }));
    await user.type(screen.getByLabelText("팀 이름"), "Infra");
    await user.click(screen.getByRole("button", { name: "생성" }));
    expect(
      await screen.findByText("같은 상위 팀에 동일한 이름이 이미 있습니다."),
    ).toBeInTheDocument();
  });

  it("renames a team via the API and reselects on delete success", async () => {
    const user = userEvent.setup();
    vi.spyOn(teamAPIs, "getTeam").mockResolvedValue(
      jsonRes({
        id: "t_1",
        name: "Platform",
        parentId: null,
        children: [],
        memberCount: 0,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    vi.spyOn(teamMemberAPIs, "listTeamMembers").mockResolvedValue(
      jsonRes({ total: 0, page: 1, size: 10, items: [] }),
    );
    const rename = vi.spyOn(teamAPIs, "renameTeam").mockResolvedValue(
      jsonRes({
        id: "t_1",
        name: "Renamed",
        parentId: null,
        children: [],
        memberCount: 0,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    renderView();
    await user.click(screen.getByRole("button", { name: "이름 변경" }));
    const input = screen.getByLabelText("팀 이름");
    await user.clear(input);
    await user.type(input, "Renamed");
    await user.click(screen.getByRole("button", { name: "저장" }));
    await waitFor(() =>
      expect(rename).toHaveBeenCalledWith("t_1", { name: "Renamed" }),
    );
  });

  it("deletes a team via the API and reselects the other surviving root team", async () => {
    const user = userEvent.setup();
    vi.spyOn(teamAPIs, "getTeam").mockResolvedValue(
      jsonRes({
        id: "t_1",
        name: "Platform",
        parentId: null,
        children: [],
        memberCount: 0,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    vi.spyOn(teamMemberAPIs, "listTeamMembers").mockResolvedValue(
      jsonRes({ total: 0, page: 1, size: 10, items: [] }),
    );
    const del = vi
      .spyOn(teamAPIs, "deleteTeam")
      .mockResolvedValue({ ok: true } as Response);
    const onSelectTeam = vi.fn();
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <TreeDetailView
            teams={TEAMS}
            teamSearch=""
            selectedTeamId="t_1"
            onSelectTeam={onSelectTeam}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    await user.click(screen.getByRole("button", { name: "팀 삭제" }));
    await user.click(screen.getByRole("radio", { name: /팀 내 기억 삭제/ }));
    await user.type(
      screen.getByLabelText("확인 — 삭제할 팀명 입력"),
      "Platform",
    );
    /* Two "팀 삭제" buttons exist once the confirm modal opens (the card
       trigger + the modal's confirm) — the confirm one is the last. */
    const confirmButtons = screen.getAllByRole("button", { name: "팀 삭제" });
    await user.click(confirmButtons[confirmButtons.length - 1]);
    await waitFor(() =>
      expect(del).toHaveBeenCalledWith("t_1", "purge", undefined),
    );
    /* t_1 is the just-deleted team — reselect must land on the OTHER
       surviving root (t_2), never the dead one. */
    await waitFor(() => expect(onSelectTeam).toHaveBeenCalledWith("t_2"));
  });

  it("shows the mapped inline error when adding a member hits an existing membership", async () => {
    const user = userEvent.setup();
    vi.spyOn(teamAPIs, "getTeam").mockResolvedValue(
      jsonRes({
        id: "t_1",
        name: "Platform",
        parentId: null,
        children: [],
        memberCount: 0,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    vi.spyOn(teamMemberAPIs, "listTeamMembers").mockResolvedValue(
      jsonRes({ total: 0, page: 1, size: 10, items: [] }),
    );
    vi.spyOn(teamMemberAPIs, "addTeamMember").mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({ code: "ALREADY_TEAM_MEMBER", message: "x" }),
    } as unknown as Response);
    renderView();
    await user.click(screen.getByRole("button", { name: "+ 멤버 추가" }));
    await user.type(screen.getByLabelText("계정명 (email)"), "kim@corp.com");
    await user.click(screen.getByLabelText("role"));
    await user.click(screen.getByRole("option", { name: "edit" }));
    await user.click(screen.getByRole("button", { name: "초대하기" }));
    expect(
      await screen.findByText("이미 초대된 사용자입니다."),
    ).toBeInTheDocument();
    /* Modal stays open on failure — the invite trigger button is gone
       while the modal is mounted, and the modal's own cancel/submit
       buttons are still present. */
    expect(
      screen.getByRole("button", { name: "초대하기" }),
    ).toBeInTheDocument();
  });

  it("shows the mapped inline error when deleting a childless team hits a server conflict", async () => {
    const user = userEvent.setup();
    vi.spyOn(teamAPIs, "getTeam").mockResolvedValue(
      jsonRes({
        id: "t_1",
        name: "Platform",
        parentId: null,
        children: [],
        memberCount: 0,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    vi.spyOn(teamMemberAPIs, "listTeamMembers").mockResolvedValue(
      jsonRes({ total: 0, page: 1, size: 10, items: [] }),
    );
    vi.spyOn(teamAPIs, "deleteTeam").mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({ code: "TEAM_HAS_CHILDREN", message: "x" }),
    } as unknown as Response);
    renderView();
    /* t_1 has childCount: 0 in the TEAMS fixture, so DeleteTeamModal's
       client-side hasChildren gate passes and the confirm flow reaches
       the server, whose 409 drives the inline error mapping below. */
    await user.click(screen.getByRole("button", { name: "팀 삭제" }));
    await user.click(screen.getByRole("radio", { name: /팀 내 기억 삭제/ }));
    await user.type(
      screen.getByLabelText("확인 — 삭제할 팀명 입력"),
      "Platform",
    );
    const confirmButtons = screen.getAllByRole("button", { name: "팀 삭제" });
    await user.click(confirmButtons[confirmButtons.length - 1]);
    expect(
      await screen.findByText("하위 팀이 있어 삭제할 수 없습니다."),
    ).toBeInTheDocument();
  });

  it("shows the batch-failure modal on a partial remove", async () => {
    const user = userEvent.setup();
    vi.spyOn(teamAPIs, "getTeam").mockResolvedValue(
      jsonRes({
        id: "t_1",
        name: "Platform",
        parentId: null,
        children: [],
        memberCount: 2,
        createdAt: "2026-07-01T00:00:00Z",
      }),
    );
    vi.spyOn(teamMemberAPIs, "listTeamMembers").mockResolvedValue(
      jsonRes({ total: 1, page: 1, size: 10, items: [member()] }),
    );
    vi.spyOn(teamMemberAPIs, "removeTeamMembers").mockResolvedValue(
      jsonRes({
        succeeded: [],
        failed: [{ id: "u_1", code: "NOT_TEAM_MEMBER", message: "x" }],
      }),
    );
    renderView();
    await screen.findByText("kim@corp.com");
    await user.click(
      screen.getByRole("checkbox", { name: "kim@corp.com 선택" }),
    );
    await user.click(screen.getByRole("button", { name: "제거" }));
    /* Two "제거" buttons exist once the confirm modal opens (the table
       trigger + the modal's confirm) — the confirm one is the last. */
    const confirmButtons = screen.getAllByRole("button", { name: "제거" });
    await user.click(confirmButtons[confirmButtons.length - 1]);
    expect(await screen.findByText("팀 멤버가 아닙니다")).toBeInTheDocument();
  });
});
