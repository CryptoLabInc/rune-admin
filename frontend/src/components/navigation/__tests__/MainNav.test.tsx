import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import MainNav from "@/components/navigation/MainNav";
import * as userAPIs from "@/api/userAPIs";
import { NAV_LIST } from "@/constants/commonConstants";

vi.mock("@/api/userAPIs");

describe("MainNav", () => {
  const renderWithProviders = (component: React.ReactNode) => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });

    return render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/teams"]}>{component}</MemoryRouter>
      </QueryClientProvider>,
    );
  };

  it("renders nav links for all NAV_LIST items", async () => {
    vi.mocked(userAPIs.getUsersStats).mockResolvedValue(
      new Response(JSON.stringify({ invitePending: 0 }), { status: 200 }),
    );

    renderWithProviders(<MainNav />);

    for (const { title, url } of NAV_LIST) {
      const link = screen.getByRole("link", { name: new RegExp(title) });
      expect(link).toHaveAttribute("href", url);
    }
  });

  it("renders the pending-invite badge when invitePending > 0", async () => {
    vi.mocked(userAPIs.getUsersStats).mockResolvedValue(
      new Response(JSON.stringify({ invitePending: 3 }), { status: 200 }),
    );

    renderWithProviders(<MainNav />);

    const badge = await screen.findByText("3");
    expect(badge).toBeInTheDocument();
  });

  it("does not render the badge when invitePending is 0", async () => {
    vi.mocked(userAPIs.getUsersStats).mockResolvedValue(
      new Response(JSON.stringify({ invitePending: 0 }), { status: 200 }),
    );

    renderWithProviders(<MainNav />);

    // Give the query time to resolve
    await screen.findByRole("link", { name: /멤버 관리/ });

    // Ensure badge is not rendered
    expect(screen.queryByText("3")).not.toBeInTheDocument();
  });
});
