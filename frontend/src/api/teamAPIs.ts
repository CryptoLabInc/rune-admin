import { serviceFetch } from "@/api/createFetch";

/** getTeamsTree fetches the flat team hierarchy (GET /teams/tree). */
export const getTeamsTree = async () => await serviceFetch("teams/tree");

/** getTeam fetches one team's detail (GET /teams/{teamId}). */
export const getTeam = async (teamId: string) =>
  await serviceFetch(`teams/${teamId}`);

/** createTeam creates a team (POST /teams). */
export const createTeam = async (body: {
  name: string;
  parentId: string | null;
}) =>
  await serviceFetch("teams", { method: "POST", body: JSON.stringify(body) });

/** renameTeam renames a team (PUT /teams/{teamId}). */
export const renameTeam = async (teamId: string, body: { name: string }) =>
  await serviceFetch(`teams/${teamId}`, {
    method: "PUT",
    body: JSON.stringify(body),
  });

/**
 * deleteTeam deletes a team (DELETE /teams/{teamId}). memoryAction is required;
 * transfer additionally needs targetTeamId (its memory moves there).
 */
export const deleteTeam = async (
  teamId: string,
  memoryAction: "purge" | "transfer",
  targetTeamId?: string,
) => {
  const params = new URLSearchParams({ memoryAction });
  if (targetTeamId) params.set("targetTeamId", targetTeamId);
  return await serviceFetch(`teams/${teamId}?${params.toString()}`, {
    method: "DELETE",
  });
};
