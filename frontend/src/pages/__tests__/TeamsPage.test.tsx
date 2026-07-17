import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import TeamsPage from "@/pages/TeamsPage";
import * as teamAPIs from "@/api/teamAPIs";

const jsonRes = (body: unknown) =>
  ({ ok: true, json: async () => body }) as unknown as Response;

/** Matches the SC-06 fixture ids used by the URL-state assertions below. */
const TREE_FIXTURE = [
  {
    id: "t_a",
    name: "플랫폼",
    parentId: null,
    childrenIds: [],
    childCount: 0,
    memberCount: 2,
  },
  {
    id: "t_e",
    name: "보안",
    parentId: null,
    childrenIds: [],
    childCount: 0,
    memberCount: 2,
  },
];

const mockTreeSuccess = () =>
  vi.spyOn(teamAPIs, "getTeamsTree").mockResolvedValue(jsonRes(TREE_FIXTURE));

const renderAt = (url: string) => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[url]}>
        <TeamsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
};

const viewButton = (name: string) => screen.getByRole("button", { name });

describe("TeamsPage tree loading", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the error state when the tree fails to load", async () => {
    vi.spyOn(teamAPIs, "getTeamsTree").mockResolvedValue({
      ok: false,
    } as Response);
    renderAt("/teams");
    expect(
      await screen.findByText("팀 정보를 불러올 수 없습니다."),
    ).toBeInTheDocument();
  });

  it("renders the view once the tree loads", async () => {
    vi.spyOn(teamAPIs, "getTeamsTree").mockResolvedValue(
      jsonRes([
        {
          id: "t_1",
          name: "Platform",
          parentId: null,
          childrenIds: [],
          childCount: 0,
          memberCount: 2,
        },
      ]),
    );
    renderAt("/teams");
    await waitFor(() =>
      expect(
        screen.getByRole("group", { name: "보기 전환" }),
      ).toBeInTheDocument(),
    );
  });
});

describe("TeamsPage empty state (SC-06 B)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the 팀 0개 empty panel when the tree loads with no teams", async () => {
    vi.spyOn(teamAPIs, "getTeamsTree").mockResolvedValue(jsonRes([]));
    /* The reported URL after deleting every team: stale ?team= + tree view. */
    renderAt("/teams?view=tree&team=");

    expect(
      await screen.findByText("새로운 팀을 만들어 주세요."),
    ).toBeInTheDocument();
    /* No teams → nothing to search, so the header search is hidden. */
    expect(screen.queryByPlaceholderText("팀 검색")).not.toBeInTheDocument();
  });

  it("opens the 새 팀 만들기 modal from the empty panel", async () => {
    vi.spyOn(teamAPIs, "getTeamsTree").mockResolvedValue(jsonRes([]));
    const user = userEvent.setup();
    renderAt("/teams?view=tree&team=");
    await screen.findByText("새로운 팀을 만들어 주세요.");

    await user.click(viewButton("새 팀 만들기"));

    expect(
      await screen.findByRole("heading", { name: "새 팀 만들기" }),
    ).toBeInTheDocument();
  });
});

describe("TeamsPage URL state", () => {
  afterEach(() => vi.restoreAllMocks());

  it("defaults to the org chart view", async () => {
    mockTreeSuccess();
    renderAt("/teams");

    await waitFor(() =>
      expect(viewButton("조직도")).toHaveAttribute("aria-pressed", "true"),
    );
    expect(viewButton("트리·상세")).toHaveAttribute("aria-pressed", "false");
  });

  it("restores the tree view from ?view=tree", async () => {
    mockTreeSuccess();
    renderAt("/teams?view=tree");

    await waitFor(() =>
      expect(viewButton("트리·상세")).toHaveAttribute("aria-pressed", "true"),
    );
    /* 플랫폼 (t_a) leads the detail panel per the SC-06 entry rule. */
    expect(screen.getByRole("heading", { name: /플랫폼/ })).toBeInTheDocument();
  });

  it("restores the selected team from ?team=", async () => {
    mockTreeSuccess();
    renderAt("/teams?view=tree&team=t_e");

    expect(
      await screen.findByRole("heading", { name: /보안/ }),
    ).toBeInTheDocument();
  });

  it("falls back to the first root team on a stale ?team=", async () => {
    mockTreeSuccess();
    renderAt("/teams?view=tree&team=t_deleted");

    expect(
      await screen.findByRole("heading", { name: /플랫폼/ }),
    ).toBeInTheDocument();
  });

  it("keeps view and selection in the URL across toggles", async () => {
    mockTreeSuccess();
    const user = userEvent.setup();
    renderAt("/teams?view=tree&team=t_e");
    await screen.findByRole("heading", { name: /보안/ });

    await user.click(viewButton("조직도"));
    expect(viewButton("조직도")).toHaveAttribute("aria-pressed", "true");

    /* Back to tree — the ?team= param survived the view switch. */
    await user.click(viewButton("트리·상세"));
    expect(screen.getByRole("heading", { name: /보안/ })).toBeInTheDocument();
  });
});
