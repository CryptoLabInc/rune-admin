import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import LandingRedirect from "@/components/auth/LandingRedirect";
import * as workspaceAPIs from "@/api/workspaceAPIs";

const renderLanding = () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return render(
    <MemoryRouter initialEntries={["/"]}>
      <Routes>
        <Route index element={<LandingRedirect />} />
        <Route path="/workspace" element={<div>WORKSPACE PAGE</div>} />
        <Route path="/teams" element={<div>TEAMS PAGE</div>} />
      </Routes>
    </MemoryRouter>,
    { wrapper },
  );
};

describe("LandingRedirect", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lands on the empty-workspace page when no workspace exists", async () => {
    vi.spyOn(workspaceAPIs, "getWorkspace").mockResolvedValue({
      ok: false,
      status: 404,
    } as Response);

    renderLanding();

    expect(await screen.findByText("WORKSPACE PAGE")).toBeInTheDocument();
    expect(screen.queryByText("TEAMS PAGE")).not.toBeInTheDocument();
  });

  it("lands on the teams page when a workspace exists", async () => {
    vi.spyOn(workspaceAPIs, "getWorkspace").mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ phase: "running", endpointUrl: "e", rows: 1 }),
    } as unknown as Response);

    renderLanding();

    expect(await screen.findByText("TEAMS PAGE")).toBeInTheDocument();
    expect(screen.queryByText("WORKSPACE PAGE")).not.toBeInTheDocument();
  });

  it("defaults to the teams page when the workspace query errors", async () => {
    vi.spyOn(workspaceAPIs, "getWorkspace").mockResolvedValue({
      ok: false,
      status: 500,
    } as Response);

    renderLanding();

    expect(await screen.findByText("TEAMS PAGE")).toBeInTheDocument();
  });

  it("renders nothing while the workspace query is pending", () => {
    vi.spyOn(workspaceAPIs, "getWorkspace").mockReturnValue(
      new Promise<Response>(() => {}),
    );

    renderLanding();

    expect(screen.queryByText("WORKSPACE PAGE")).not.toBeInTheDocument();
    expect(screen.queryByText("TEAMS PAGE")).not.toBeInTheDocument();
  });
});
