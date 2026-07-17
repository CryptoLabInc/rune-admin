import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import SessionsPage from "@/pages/SessionsPage";
import * as invitationAPIs from "@/api/invitationAPIs";
import type { TInvitationHistoryRow } from "@/types/userTypes";

/* The page talks to GET /invitations?view=history — stub the API layer
   with a mock that re-implements the server sort/paging semantics over
   a 25-row fixture (k@ reissued 3×, 5 never-connected rows). */

const at = (minute: number) =>
  `2026-07-01T00:${String(minute).padStart(2, "0")}:00Z`;
const issuedAt = (index: number) =>
  `2026-06-30T00:${String(index).padStart(2, "0")}:00Z`;

const CONNECTED_ACCOUNTS = [
  "k@corp.com",
  "k@corp.com",
  "a@corp.com",
  "b@corp.com",
  "c@corp.com",
  "d@corp.com",
  "e@corp.com",
  "f@corp.com",
  "g@corp.com",
  "h@corp.com",
  "i@corp.com",
  "j@corp.com",
  "l@corp.com",
  "m@corp.com",
  "n@corp.com",
  "o@corp.com",
  "p@corp.com",
  "q@corp.com",
  "r@corp.com",
  "s@corp.com",
];
const NULL_ACCOUNTS = [
  "k@corp.com", // third issuance — never connected
  "t@corp.com",
  "u@corp.com",
  "v@corp.com",
  "w@corp.com",
];

const ROWS: TInvitationHistoryRow[] = [
  ...CONNECTED_ACCOUNTS.map((account, i) => ({
    account,
    issuedAt: issuedAt(i),
    lastAccessAt: at(59 - i), // descending recency in list order
  })),
  ...NULL_ACCOUNTS.map((account, i) => ({
    account,
    issuedAt: issuedAt(20 + i),
    lastAccessAt: null,
  })),
];

const jsonRes = (body: unknown) =>
  ({ ok: true, json: async () => body }) as unknown as Response;

const errorRes = () => ({ ok: false }) as unknown as Response;

/** Re-implements the server sort/paging semantics (SC-16) over ROWS. */
const historyResponse = (sort: string, page: number, size: number) => {
  const rows = ROWS.slice();
  if (sort === "account") {
    rows.sort((a, b) => a.account.localeCompare(b.account));
  } else if (sort === "issued_at") {
    rows.sort((a, b) => b.issuedAt.localeCompare(a.issuedAt));
  } else {
    /* last_access — null sinks (server treats null as epoch 0). */
    rows.sort(
      (a, b) =>
        (b.lastAccessAt ? Date.parse(b.lastAccessAt) : 0) -
        (a.lastAccessAt ? Date.parse(a.lastAccessAt) : 0),
    );
  }
  return {
    total: rows.length,
    page,
    size,
    items: rows.slice((page - 1) * size, page * size),
  };
};

const mockHistorySuccess = () =>
  vi
    .spyOn(invitationAPIs, "getInvitationHistory")
    .mockImplementation(async (sort, page, size) =>
      jsonRes(historyResponse(sort, page, size)),
    );

const renderPage = () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <SessionsPage />
    </QueryClientProvider>,
  );
};

describe("SessionsPage", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows one row per issuance — a reissued account repeats (D11)", async () => {
    mockHistorySuccess();
    renderPage();

    /* k@corp.com holds 3 issuances in the fixture; under the default
       last-access sort two of them land on page 1 (the third never
       connected, so it sinks with the "—" rows). */
    expect(await screen.findAllByText("k@corp.com")).toHaveLength(2);
  });

  it("sinks never-connected rows to the bottom under the access sort", async () => {
    mockHistorySuccess();
    const user = userEvent.setup();
    const { container } = renderPage();

    /* Page 1 = most recently accessed 10 — no "—" cells yet. */
    await screen.findAllByText("k@corp.com");
    expect(screen.queryByText("—")).not.toBeInTheDocument();

    /* Last page = the tail of the null-access block ("—" only). */
    await user.click(screen.getByRole("button", { name: "3" }));
    expect(await screen.findByText("t@corp.com")).toBeInTheDocument();
    expect(screen.getAllByText("—")).toHaveLength(5);

    /* Short pages render only real rows — the frame height is pinned by
       the Table scroll area's min-h, not by filler rows. */
    expect(container.querySelectorAll("tbody tr")).toHaveLength(5);
  });

  it("resets to page 1 when the sort changes", async () => {
    const spy = mockHistorySuccess();
    const user = userEvent.setup();
    renderPage();
    await screen.findAllByText("k@corp.com");

    await user.click(screen.getByRole("button", { name: "3" }));
    expect(screen.getByRole("button", { name: "3" })).toHaveAttribute(
      "aria-current",
      "page",
    );

    await user.click(screen.getByRole("button", { name: "정렬" }));
    await user.click(screen.getByRole("option", { name: "이메일 (account)" }));

    await waitFor(() => expect(spy).toHaveBeenLastCalledWith("account", 1, 10));
    expect(screen.getByRole("button", { name: "1" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    /* account asc — a@corp.com leads the first page. */
    expect(await screen.findByText("a@corp.com")).toBeInTheDocument();
  });

  it("shows the fixed page-size footer", async () => {
    mockHistorySuccess();
    renderPage();
    expect(
      await screen.findByText("총 25건 · 10건/페이지"),
    ).toBeInTheDocument();
  });

  it("shows the empty row when there is no history", async () => {
    vi.spyOn(invitationAPIs, "getInvitationHistory").mockResolvedValue(
      jsonRes({ total: 0, page: 1, size: 10, items: [] }),
    );
    renderPage();

    expect(await screen.findByText("이력이 없습니다.")).toBeInTheDocument();
  });

  it("shows SC-16 state B when the history fails to load", async () => {
    vi.spyOn(invitationAPIs, "getInvitationHistory").mockResolvedValue(
      errorRes(),
    );
    renderPage();

    expect(
      await screen.findByText("이력 정보를 불러올 수 없습니다."),
    ).toBeInTheDocument();
  });
});
