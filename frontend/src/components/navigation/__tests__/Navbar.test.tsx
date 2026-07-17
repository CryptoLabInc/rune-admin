import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import Navbar from "@/components/navigation/Navbar";
import * as authAPIs from "@/api/authAPIs";
import * as workspaceAPIs from "@/api/workspaceAPIs";
import { BTN_TEXT } from "@/constants/commonConstants";

const jsonRes = (body: unknown) =>
  ({ ok: true, status: 200, json: async () => body }) as unknown as Response;

const renderNavbar = () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return render(
    <MemoryRouter initialEntries={["/teams"]}>
      <Routes>
        <Route path="/teams" element={<Navbar />} />
        <Route path="/login" element={<div>LOGIN SCREEN</div>} />
      </Routes>
    </MemoryRouter>,
    { wrapper },
  );
};

describe("Navbar", () => {
  afterEach(() => vi.restoreAllMocks());

  it("signs out and navigates to /login", async () => {
    const user = userEvent.setup();
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({
        logged_in: true,
        expires_at: "2026-07-16T21:00:00Z",
        me: { email: "admin@corp.com", avatar: "https://x/a.png" },
      }),
    );
    vi.spyOn(workspaceAPIs, "getWorkspace").mockResolvedValue({
      ok: false,
      status: 404,
    } as Response);
    const logout = vi
      .spyOn(authAPIs, "postLogout")
      .mockResolvedValue({ ok: true } as Response);

    renderNavbar();

    await user.click(
      await screen.findByRole("button", { name: "프로필 메뉴" }),
    );
    await user.click(screen.getByRole("button", { name: BTN_TEXT.signOut }));
    expect(logout).toHaveBeenCalledOnce();
    await waitFor(() =>
      expect(screen.getByText("LOGIN SCREEN")).toBeInTheDocument(),
    );
  });

  it("renders the profile avatar once the session loads", async () => {
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({
        logged_in: true,
        expires_at: "2026-07-16T21:00:00Z",
        me: { email: "admin@corp.com", avatar: "https://x/a.png" },
      }),
    );
    vi.spyOn(workspaceAPIs, "getWorkspace").mockResolvedValue({
      ok: false,
      status: 404,
    } as Response);

    renderNavbar();

    const avatar = await screen.findByRole("img", { name: /프로필/ });
    expect(avatar).toHaveAttribute("src", "https://x/a.png");
    expect(await screen.findByText("워크스페이스 없음")).toBeInTheDocument();
  });
});
