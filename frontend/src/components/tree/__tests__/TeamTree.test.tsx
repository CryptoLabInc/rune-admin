import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import TeamTree from "@/components/tree/TeamTree";
import type { TTeamNode } from "@/types/commonTypes";

const TEAMS: TTeamNode[] = [
  {
    id: "platform",
    name: "Platform",
    members: 18,
    children: [{ id: "infra", name: "Infrastructure", members: 7 }],
  },
  { id: "ops", name: "Operations", members: 11 },
];

describe("TeamTree", () => {
  it("renders roots and reveals children after toggling open", async () => {
    const user = userEvent.setup();
    render(<TeamTree teams={TEAMS} onSelect={() => {}} />);

    expect(screen.getByText("Platform")).toBeInTheDocument();
    expect(screen.queryByText("Infrastructure")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Platform 펼치기" }));
    expect(screen.getByText("Infrastructure")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Platform 접기" }));
    expect(screen.queryByText("Infrastructure")).not.toBeInTheDocument();
  });

  it("respects defaultExpandedIds and fires onSelect with the node", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(
      <TeamTree
        teams={TEAMS}
        onSelect={onSelect}
        defaultExpandedIds={["platform"]}
      />,
    );

    await user.click(screen.getByText("Infrastructure"));
    expect(onSelect).toHaveBeenCalledWith(TEAMS[0].children![0]);
  });

  it("filters by query (descendant match keeps the root) and shows empty feedback", () => {
    const { rerender } = render(
      <TeamTree teams={TEAMS} onSelect={() => {}} query="infra" />,
    );
    expect(screen.getByText("Platform")).toBeInTheDocument();
    expect(screen.queryByText("Operations")).not.toBeInTheDocument();

    rerender(<TeamTree teams={TEAMS} onSelect={() => {}} query="없는팀" />);
    expect(screen.getByText("검색 결과가 없습니다.")).toBeInTheDocument();
  });

  it("auto-expands ancestors so a nested match is visible, then restores manual state", () => {
    const { rerender } = render(
      <TeamTree teams={TEAMS} onSelect={() => {}} query="infra" />,
    );
    // Platform was never manually expanded, yet the match is revealed
    // (name renders split around the emphasized fragment).
    expect(screen.getByText("Infra", { selector: "b" })).toBeInTheDocument();
    expect(screen.getByText("structure")).toBeInTheDocument();

    // Clearing the query restores the manual (collapsed) state.
    rerender(<TeamTree teams={TEAMS} onSelect={() => {}} query="" />);
    expect(screen.queryByText("Infrastructure")).not.toBeInTheDocument();
  });

  it("prunes non-matching siblings but keeps the whole subtree of a self-match", () => {
    const forest: TTeamNode[] = [
      {
        id: "a",
        name: "Alpha",
        members: 5,
        children: [
          { id: "b", name: "Bravo", members: 2 },
          { id: "c", name: "Charlie", members: 3 },
        ],
      },
    ];
    const { rerender } = render(
      <TeamTree teams={forest} onSelect={() => {}} query="bravo" />,
    );
    // Sibling Charlie does not match and is pruned.
    expect(screen.getByText(/Bravo/)).toBeInTheDocument();
    expect(screen.queryByText(/Charlie/)).not.toBeInTheDocument();

    // A self-matching parent keeps its whole subtree (children stay
    // reachable behind its toggle).
    rerender(<TeamTree teams={forest} onSelect={() => {}} query="alpha" />);
    expect(screen.getByText(/Alpha/)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Alpha 펼치기" }),
    ).toBeInTheDocument();
  });

  it("emphasizes the matched name fragment", () => {
    render(<TeamTree teams={TEAMS} onSelect={() => {}} query="opera" />);
    const emphasized = screen.getByText("Opera", { selector: "b" });
    expect(emphasized).toBeInTheDocument();
  });
});
