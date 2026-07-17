import {
  getTeamDescendantIds,
  getTeamName,
} from "@/components/teams/teamHierarchy";
import type { TTeamTree } from "@/types/teamTypes";
import type { TInviteSet } from "@/types/userTypes";

/** One row of the 하위 팀 역할 미리보기 table (SC-12 no.3). */
export type TInvitePreviewRow = {
  teamId: string;
  teamName: string;
  role: string;
  reason: string;
  /** Copied sub-team rows render indented with a └ prefix. */
  indent: boolean;
};

/**
 * Expands directly specified team/role sets with the R1 downward copy:
 * each selected team contributes its sub-teams at the same role. A team
 * that is both directly specified and covered by another set's subtree
 * keeps its direct role ("직접 지정이 우선") and is flagged
 * "이미 초대된 팀입니다." in the descendant position (SC-12 no.3 — D3).
 * The row list doubles as the membership expansion the server would
 * produce, so the dummy invite mutation reuses it.
 */
export const buildInvitePreview = (
  sets: TInviteSet[],
  teams: TTeamTree,
): TInvitePreviewRow[] => {
  const directRole = new Map(sets.map((set) => [set.teamId, set.role]));
  const covered = new Set<string>();
  const rows: TInvitePreviewRow[] = [];

  for (const set of sets) {
    if (covered.has(set.teamId)) continue;
    covered.add(set.teamId);
    rows.push({
      teamId: set.teamId,
      teamName: getTeamName(teams, set.teamId),
      role: set.role,
      reason: "직접 지정",
      indent: false,
    });
    for (const descId of getTeamDescendantIds(teams, set.teamId)) {
      if (covered.has(descId)) continue;
      covered.add(descId);
      const direct = directRole.get(descId);
      rows.push({
        teamId: descId,
        teamName: getTeamName(teams, descId),
        role: direct ?? set.role,
        reason: direct
          ? "이미 초대된 팀입니다."
          : `${getTeamName(teams, set.teamId)} 하위 팀`,
        indent: true,
      });
    }
  }
  return rows;
};
