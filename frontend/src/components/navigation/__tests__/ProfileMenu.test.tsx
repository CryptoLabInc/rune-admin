import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import ProfileMenu from "@/components/navigation/ProfileMenu";
import { BTN_TEXT } from "@/constants/commonConstants";

const ME = { email: "admin@corp.com", avatar: "https://x/a.png" };

describe("ProfileMenu", () => {
  it("renders the avatar image from me.avatar", () => {
    render(<ProfileMenu me={ME} onSignOut={() => {}} />);
    const img = screen.getByRole("img", { name: /프로필/ });
    expect(img).toHaveAttribute("src", ME.avatar);
  });

  it("shows the default icon when me.avatar is absent (no picture)", () => {
    render(
      <ProfileMenu me={{ email: "admin@corp.com" }} onSignOut={() => {}} />,
    );
    expect(
      screen.queryByRole("img", { name: /프로필/ }),
    ).not.toBeInTheDocument();
    expect(screen.getByTestId("avatar-fallback")).toBeInTheDocument();
  });

  it("opens the popover on avatar click with email, plan, and Sign out", async () => {
    const user = userEvent.setup();
    render(<ProfileMenu me={ME} onSignOut={() => {}} />);
    expect(screen.queryByText("admin@corp.com")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "프로필 메뉴" }));
    expect(screen.getByText("admin@corp.com")).toBeInTheDocument();
    expect(screen.getByText("플랜: Free")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: BTN_TEXT.signOut }),
    ).toBeInTheDocument();
  });

  it("closes the popover on Escape", async () => {
    const user = userEvent.setup();
    render(<ProfileMenu me={ME} onSignOut={() => {}} />);
    await user.click(screen.getByRole("button", { name: "프로필 메뉴" }));
    expect(screen.getByText("admin@corp.com")).toBeInTheDocument();
    await user.keyboard("{Escape}");
    expect(screen.queryByText("admin@corp.com")).not.toBeInTheDocument();
  });

  it("closes the popover on outside click", async () => {
    const user = userEvent.setup();
    render(
      <div>
        <button type="button">바깥</button>
        <ProfileMenu me={ME} onSignOut={() => {}} />
      </div>,
    );
    await user.click(screen.getByRole("button", { name: "프로필 메뉴" }));
    expect(screen.getByText("admin@corp.com")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "바깥" }));
    expect(screen.queryByText("admin@corp.com")).not.toBeInTheDocument();
  });

  it("calls onSignOut when Sign out is clicked", async () => {
    const user = userEvent.setup();
    const onSignOut = vi.fn();
    render(<ProfileMenu me={ME} onSignOut={onSignOut} />);
    await user.click(screen.getByRole("button", { name: "프로필 메뉴" }));
    await user.click(screen.getByRole("button", { name: BTN_TEXT.signOut }));
    expect(onSignOut).toHaveBeenCalledOnce();
  });

  it("falls back to the default icon when the avatar image errors", async () => {
    render(<ProfileMenu me={ME} onSignOut={() => {}} />);
    const img = screen.getByRole("img", { name: /프로필/ });
    fireEvent.error(img);
    // after error the <img> is replaced by the fallback glyph
    expect(
      screen.queryByRole("img", { name: /프로필/ }),
    ).not.toBeInTheDocument();
    expect(screen.getByTestId("avatar-fallback")).toBeInTheDocument();
  });
});
