import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import RequireAuth from "@/components/auth/RequireAuth";
import * as authAPIs from "@/api/authAPIs";

const jsonRes = (body: unknown) =>
  ({ ok: true, json: async () => body }) as unknown as Response;

const renderGuard = () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return render(
    <MemoryRouter initialEntries={["/teams"]}>
      <Routes>
        <Route element={<RequireAuth />}>
          <Route path="/teams" element={<div>TEAMS CONTENT</div>} />
        </Route>
        <Route path="/login" element={<div>LOGIN SCREEN</div>} />
      </Routes>
    </MemoryRouter>,
    { wrapper },
  );
};

describe("RequireAuth", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders the guarded outlet when logged in", async () => {
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({
        logged_in: true,
        expires_at: "2026-07-16T21:00:00Z",
        me: { email: "admin@corp.com", avatar: "a.png" },
      }),
    );
    renderGuard();
    expect(await screen.findByText("TEAMS CONTENT")).toBeInTheDocument();
  });

  it("redirects to /login when logged out", async () => {
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({ logged_in: false }),
    );
    renderGuard();
    await waitFor(() =>
      expect(screen.getByText("LOGIN SCREEN")).toBeInTheDocument(),
    );
    expect(screen.queryByText("TEAMS CONTENT")).not.toBeInTheDocument();
  });

  it("renders nothing while session is pending", () => {
    vi.spyOn(authAPIs, "getConsoleSession").mockReturnValue(
      new Promise<Response>(() => {}),
    );
    renderGuard();
    expect(screen.queryByText("TEAMS CONTENT")).not.toBeInTheDocument();
    expect(screen.queryByText("LOGIN SCREEN")).not.toBeInTheDocument();
  });
});
