import { serviceFetch } from "@/api/createFetch";
import type { TTeamMemberRole } from "@/types/teamTypes";

/** listTeamMembers fetches one page of a team's members. */
export const listTeamMembers = async (
  teamId: string,
  page: number,
  size: number,
) => await serviceFetch(`teams/${teamId}/members?page=${page}&size=${size}`);

/** addTeamMember adds an existing user to a team (POST). */
export const addTeamMember = async (
  teamId: string,
  body: { account: string; role: TTeamMemberRole; username: string },
) =>
  await serviceFetch(`teams/${teamId}/members`, {
    method: "POST",
    body: JSON.stringify(body),
  });

/** bulkRoleChange changes multiple members' roles in one batch (PUT). */
export const bulkRoleChange = async (
  teamId: string,
  body: { updates: { userId: string; role: TTeamMemberRole }[] },
) =>
  await serviceFetch(`teams/${teamId}/members/roles`, {
    method: "PUT",
    body: JSON.stringify(body),
  });

/** removeTeamMembers bulk-removes memberships (DELETE ?userIds=csv). */
export const removeTeamMembers = async (teamId: string, userIds: string[]) => {
  const params = new URLSearchParams({ userIds: userIds.join(",") });
  return await serviceFetch(`teams/${teamId}/members?${params.toString()}`, {
    method: "DELETE",
  });
};
