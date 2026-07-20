import { MemoryRouter, Route, Routes } from "react-router";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import PublicNavbar from "@/components/navigation/PublicNavbar";
import { BTN_TEXT } from "@/constants/commonConstants";

const renderPublicNavbar = (entry = "/404") =>
  render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/404" element={<PublicNavbar />} />
        <Route path="/login" element={<div>LOGIN SCREEN</div>} />
      </Routes>
    </MemoryRouter>,
  );

const renderAtLogin = () =>
  render(
    <MemoryRouter initialEntries={["/login"]}>
      <Routes>
        <Route path="/login" element={<PublicNavbar />} />
      </Routes>
    </MemoryRouter>,
  );

describe("PublicNavbar", () => {
  it("renders the brand mark, wordmark, and the 시작하기 CTA", () => {
    const { container } = renderPublicNavbar();
    expect(container.querySelector("svg")).toBeInTheDocument();
    expect(screen.getByText("RUNE CONSOLE")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: BTN_TEXT.getStarted }),
    ).toBeInTheDocument();
  });

  it("routes to /login when 시작하기 is clicked", async () => {
    const user = userEvent.setup();
    renderPublicNavbar();
    await user.click(screen.getByRole("button", { name: BTN_TEXT.getStarted }));
    await waitFor(() =>
      expect(screen.getByText("LOGIN SCREEN")).toBeInTheDocument(),
    );
  });

  it("hides the 시작하기 CTA on the login page", () => {
    renderAtLogin();
    expect(screen.getByText("RUNE CONSOLE")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: BTN_TEXT.getStarted }),
    ).not.toBeInTheDocument();
  });
});
