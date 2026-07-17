import { serviceFetch } from "@/api/createFetch";
import type { TTeamMemberRole } from "@/types/teamTypes";
import type { TUsersQueryParams } from "@/types/userTypes";

/** listUsers fetches one page of the cross-team user list (GET /users).
    "all"/empty filter values are omitted from the query string. */
export const listUsers = async (params: TUsersQueryParams) => {
  const q = new URLSearchParams();
  if (params.search) q.set("search", params.search);
  if (params.status && params.status !== "all") q.set("status", params.status);
  if (params.teamId && params.teamId !== "all") q.set("teamId", params.teamId);
  if (params.sort) q.set("sort", params.sort);
  q.set("page", String(params.page));
  q.set("size", String(params.size));
  return await serviceFetch(`users?${q.toString()}`);
};

/** getUsersStats fetches the Members badge count (GET /users/stats). */
export const getUsersStats = async () => await serviceFetch("users/stats");

/** getUser fetches one user's detail for the drawer (GET /users/{id}). */
export const getUser = async (userId: string) =>
  await serviceFetch(`users/${userId}`);

/** addUserMembership adds the user to a team ([+ 팀 추가], POST). */
export const addUserMembership = async (
  userId: string,
  body: { teamId: string; role: TTeamMemberRole },
) =>
  await serviceFetch(`users/${userId}/members/roles`, {
    method: "POST",
    body: JSON.stringify(body),
  });

/** bulkUserRoleChange changes roles across teams in one batch (PUT). */
export const bulkUserRoleChange = async (
  userId: string,
  body: { updates: { teamId: string; role: TTeamMemberRole }[] },
) =>
  await serviceFetch(`users/${userId}/members/roles`, {
    method: "PUT",
    body: JSON.stringify(body),
  });

/** removeUserMemberships bulk-removes memberships (DELETE ?teamIds=csv). */
export const removeUserMemberships = async (
  userId: string,
  teamIds: string[],
) => {
  const q = new URLSearchParams({ teamIds: teamIds.join(",") });
  return await serviceFetch(`users/${userId}/members/roles?${q.toString()}`, {
    method: "DELETE",
  });
};

/** deleteUsers deletes accounts in batch — memberships, session token,
    and unused codes go together (DELETE /users). */
export const deleteUsers = async (userIds: string[]) => {
  const q = new URLSearchParams({ userIds: userIds.join(",") });
  return await serviceFetch(`users?${q.toString()}`, { method: "DELETE" });
};

/** deactivateUserSession destroys the user's console session token (DELETE). */
export const deactivateUserSession = async (userId: string) =>
  await serviceFetch(`users/${userId}/session`, { method: "DELETE" });
