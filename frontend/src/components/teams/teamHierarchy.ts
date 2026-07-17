import type { TTeamTree } from "@/types/teamTypes";

/**
 * Team-tree lookups over a flat `TTeamTree` — shared by the invite preview
 * (SC-12 no.3) and the membership-removal sub-team notice (SC-14 no.2).
 * Pure functions over the tree passed in (from `useTeamsTreeQuery`); trees
 * are small, so no memoized id-map is kept at module scope.
 */

/** Team name for `teamId`, or the id itself if the team is unknown. */
export const getTeamName = (teams: TTeamTree, teamId: string): string =>
  teams.find((team) => team.id === teamId)?.name ?? teamId;

/** All descendant ids of a team, in depth-first tree order. */
export const getTeamDescendantIds = (
  teams: TTeamTree,
  teamId: string,
): string[] =>
  (teams.find((team) => team.id === teamId)?.childrenIds ?? []).flatMap(
    (childId) => [childId, ...getTeamDescendantIds(teams, childId)],
  );
