import { describe, expect, it } from "vitest";

import {
  getTeamDescendantIds,
  getTeamName,
} from "@/components/teams/teamHierarchy";
import type { TTeamTree } from "@/types/teamTypes";

/** Minimal 3-node chain: t_1 root → t_2 child → t_3 grandchild. */
const TEAMS: TTeamTree = [
  {
    id: "t_1",
    name: "플랫폼",
    parentId: null,
    childrenIds: ["t_2"],
    childCount: 1,
    memberCount: 2,
  },
  {
    id: "t_2",
    name: "백엔드",
    parentId: "t_1",
    childrenIds: ["t_3"],
    childCount: 1,
    memberCount: 3,
  },
  {
    id: "t_3",
    name: "인프라",
    parentId: "t_2",
    childrenIds: [],
    childCount: 0,
    memberCount: 1,
  },
];

describe("getTeamName", () => {
  it("returns the matching team's name", () => {
    expect(getTeamName(TEAMS, "t_2")).toBe("백엔드");
  });

  it("falls back to the id itself for an unknown team", () => {
    expect(getTeamName(TEAMS, "t_missing")).toBe("t_missing");
  });
});

describe("getTeamDescendantIds", () => {
  it("returns all descendants in depth-first order", () => {
    expect(getTeamDescendantIds(TEAMS, "t_1")).toEqual(["t_2", "t_3"]);
  });

  it("returns an empty array for a leaf team", () => {
    expect(getTeamDescendantIds(TEAMS, "t_3")).toEqual([]);
  });

  it("returns an empty array for an unknown team", () => {
    expect(getTeamDescendantIds(TEAMS, "t_missing")).toEqual([]);
  });
});
