import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import MembershipRow from "@/components/drawer/MembershipRow";

const ROLE_OPTIONS = [
  { value: "edit", label: "edit" },
  { value: "write", label: "write" },
  { value: "read", label: "read" },
];

/* MembershipRow renders a <tr> — give it a valid table context. */
const inTable = (row: ReactNode) => (
  <table>
    <tbody>{row}</tbody>
  </table>
);

describe("MembershipRow", () => {
  it("wires the checkbox and role dropdown to the team name", async () => {
    const user = userEvent.setup();
    const onCheck = vi.fn();
    render(
      inTable(
        <MembershipRow
          name="Platform"
          role="edit"
          roleOptions={ROLE_OPTIONS}
          checked={false}
          onCheck={onCheck}
        />,
      ),
    );

    await user.click(screen.getByRole("checkbox", { name: "Platform 선택" }));
    expect(onCheck).toHaveBeenCalledWith(true);
    expect(
      screen.getByRole("button", { name: /Platform role/ }),
    ).toBeInTheDocument();
  });

  it("shows the CHANGED badge only for unsaved changes", () => {
    const { rerender } = render(
      inTable(
        <MembershipRow
          name="Platform"
          role="edit"
          roleOptions={ROLE_OPTIONS}
          checked
          changed
        />,
      ),
    );
    expect(screen.getByText("CHANGED")).toBeInTheDocument();

    rerender(
      inTable(
        <MembershipRow
          name="Platform"
          role="edit"
          roleOptions={ROLE_OPTIONS}
          checked
        />,
      ),
    );
    expect(screen.queryByText("CHANGED")).not.toBeInTheDocument();
  });
});
