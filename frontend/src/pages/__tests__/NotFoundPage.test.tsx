import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import NotFoundPage from "@/pages/NotFoundPage";
import * as authAPIs from "@/api/authAPIs";
import { BTN_TEXT } from "@/constants/commonConstants";

const jsonRes = (body: unknown) =>
  ({ ok: true, json: async () => body }) as unknown as Response;

const renderNotFound = () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return render(
    <MemoryRouter initialEntries={["/does-not-exist"]}>
      <Routes>
        <Route path="*" element={<NotFoundPage />} />
        <Route path="/login" element={<div>LOGIN SCREEN</div>} />
        <Route path="/teams" element={<div>TEAMS CONTENT</div>} />
      </Routes>
    </MemoryRouter>,
    { wrapper },
  );
};

describe("NotFoundPage", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the public navbar and routes 홈으로 to /login when logged out", async () => {
    const user = userEvent.setup();
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({ logged_in: false }),
    );
    renderNotFound();
    expect(await screen.findByText("404 Not Found")).toBeInTheDocument();
    // PublicNavbar variant — its 시작하기 CTA is present, no profile menu.
    expect(
      screen.getByRole("button", { name: BTN_TEXT.getStarted }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "프로필 메뉴" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: BTN_TEXT.home }));
    await waitFor(() =>
      expect(screen.getByText("LOGIN SCREEN")).toBeInTheDocument(),
    );
  });

  it("shows the console navbar and routes 홈으로 to /teams when logged in", async () => {
    const user = userEvent.setup();
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({
        logged_in: true,
        expires_at: "2026-07-16T21:00:00Z",
        me: { email: "admin@corp.com", avatar: "a.png" },
      }),
    );
    renderNotFound();
    expect(await screen.findByText("404 Not Found")).toBeInTheDocument();
    // Navbar variant — profile menu is present, no 시작하기 CTA.
    expect(
      screen.getByRole("button", { name: "프로필 메뉴" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: BTN_TEXT.getStarted }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: BTN_TEXT.home }));
    await waitFor(() =>
      expect(screen.getByText("TEAMS CONTENT")).toBeInTheDocument(),
    );
  });
});
