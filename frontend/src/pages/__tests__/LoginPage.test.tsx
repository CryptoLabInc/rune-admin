import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import LoginPage from "@/pages/LoginPage";
import * as authAPIs from "@/api/authAPIs";
import { redirectTo } from "@/utils/redirect";

vi.mock("@/utils/redirect", () => ({ redirectTo: vi.fn() }));

const jsonRes = (body: unknown) =>
  ({ ok: true, json: async () => body }) as unknown as Response;

const renderLogin = (entry = "/login") => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/teams" element={<div>TEAMS CONTENT</div>} />
      </Routes>
    </MemoryRouter>,
    { wrapper },
  );
};

describe("LoginPage", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders the default state A (heading + login button, no error)", async () => {
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({ logged_in: false }),
    );
    renderLogin();
    expect(
      await screen.findByRole("button", { name: "로그인하기" }),
    ).toBeInTheDocument();
    // Two "Rune Console" marks: the public navbar logo and the card heading.
    expect(screen.getAllByText("Rune Console")).toHaveLength(2);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("renders the public navbar without the 시작하기 CTA on the login page", async () => {
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({ logged_in: false }),
    );
    renderLogin();
    // The login-screen sign-in button confirms we are on /login…
    expect(
      await screen.findByRole("button", { name: "로그인하기" }),
    ).toBeInTheDocument();
    // …where the navbar CTA is hidden (it would route to nowhere).
    expect(
      screen.queryByRole("button", { name: "시작하기" }),
    ).not.toBeInTheDocument();
  });

  it("shows state B failure copy when ?error is present", async () => {
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({ logged_in: false }),
    );
    renderLogin("/login?error=exchange_failed");
    expect(await screen.findByRole("alert")).toHaveTextContent("로그인 실패");
  });

  it("starts login and redirects the browser to authorize_url", async () => {
    const user = userEvent.setup();
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({ logged_in: false }),
    );
    vi.spyOn(authAPIs, "postAuthStart").mockResolvedValue(
      jsonRes({
        authorize_url: "http://localhost:4000/mock-authorize?state=x",
      }),
    );
    renderLogin();
    await user.click(await screen.findByRole("button", { name: "로그인하기" }));
    await waitFor(() =>
      expect(redirectTo).toHaveBeenCalledWith(
        "http://localhost:4000/mock-authorize?state=x",
      ),
    );
  });

  it("shows state B when auth start fails", async () => {
    const user = userEvent.setup();
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({ logged_in: false }),
    );
    vi.spyOn(authAPIs, "postAuthStart").mockResolvedValue({
      ok: false,
    } as Response);
    renderLogin();
    await user.click(await screen.findByRole("button", { name: "로그인하기" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("로그인 실패");
  });

  it("redirects an already logged-in user to /teams", async () => {
    vi.spyOn(authAPIs, "getConsoleSession").mockResolvedValue(
      jsonRes({
        logged_in: true,
        expires_at: "2026-07-16T21:00:00Z",
        me: { email: "admin@corp.com", avatar: "a.png" },
      }),
    );
    renderLogin();
    expect(await screen.findByText("TEAMS CONTENT")).toBeInTheDocument();
  });

  it("renders nothing while session query is pending", () => {
    vi.spyOn(authAPIs, "getConsoleSession").mockReturnValue(
      new Promise<Response>(() => {}),
    );
    renderLogin();
    expect(
      screen.queryByRole("button", { name: "로그인하기" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("TEAMS CONTENT")).not.toBeInTheDocument();
  });
});
