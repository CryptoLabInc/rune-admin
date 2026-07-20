import { useMutation, useQueryClient } from "@tanstack/react-query";

import { createTeam, deleteTeam, renameTeam } from "@/api/teamAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import { type TTeamDetail } from "@/types/teamTypes";

/** useCreateTeamMutation creates a team (POST /teams). */
export const useCreateTeamMutation = () => {
  const queryClient = useQueryClient();
  return useMutation<
    TTeamDetail,
    Response,
    { name: string; parentId: string | null }
  >({
    mutationFn: async (body) => {
      const res = await createTeam(body);
      if (!res.ok) throw res;
      return (await res.json()) as TTeamDetail;
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.teamsTree] }),
  });
};

/** useRenameTeamMutation renames a team (PUT /teams/{id}). */
export const useRenameTeamMutation = (teamId: string) => {
  const queryClient = useQueryClient();
  return useMutation<TTeamDetail, Response, { name: string }>({
    mutationFn: async (body) => {
      const res = await renameTeam(teamId, body);
      if (!res.ok) throw res;
      return (await res.json()) as TTeamDetail;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.teamsTree] });
      queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.team, teamId] });
    },
  });
};

/** useDeleteTeamMutation deletes a team (DELETE /teams/{id}). */
export const useDeleteTeamMutation = (teamId: string) => {
  const queryClient = useQueryClient();
  return useMutation<
    void,
    Response,
    { memoryAction: "purge" | "transfer"; targetTeamId?: string }
  >({
    mutationFn: async ({ memoryAction, targetTeamId }) => {
      const res = await deleteTeam(teamId, memoryAction, targetTeamId);
      if (!res.ok) throw res;
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.teamsTree] }),
  });
};
