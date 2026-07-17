import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import * as authAPIs from "@/api/authAPIs";
import * as teamAPIs from "@/api/teamAPIs";
import App from "@/App";

const jsonRes = (body: unknown) =>
  ({ ok: true, json: async () => body }) as unknown as Response;

const renderApp = (initialEntry = "/") => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <App />
    </MemoryRouter>,
    { wrapper },
  );
};

describe("App", () => {
  afterEach(() => vi.restoreAllMocks());

  it("redirects the root route to teams inside the shell when logged in", async () => {
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({
        logged_in: true,
        expires_at: "2026-07-16T21:00:00Z",
        me: { email: "admin@corp.com", avatar: "a.png" },
      }),
    );
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
    renderApp();
    expect(
      await screen.findByRole("region", { name: "팀 관리" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("group", { name: "보기 전환" }),
    ).toBeInTheDocument();
  });

  it("redirects a logged-out visitor to the sign-in screen", async () => {
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({ logged_in: false }),
    );
    renderApp("/teams");
    expect(
      await screen.findByRole("button", { name: "로그인하기" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("region", { name: "팀 관리" }),
    ).not.toBeInTheDocument();
  });

  it("renders the 404 page on an unknown path without redirecting to sign-in", async () => {
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({ logged_in: false }),
    );
    renderApp("/no-such-route");
    expect(await screen.findByText("404 Not Found")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "로그인하기" }),
    ).not.toBeInTheDocument();
  });
});
