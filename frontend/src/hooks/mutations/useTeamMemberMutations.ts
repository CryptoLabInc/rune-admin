import { useMutation, useQueryClient } from "@tanstack/react-query";

import {
  addTeamMember,
  bulkRoleChange,
  removeTeamMembers,
} from "@/api/teamMemberAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import {
  type TBatchResult,
  type TPage,
  type TTeamMember,
  type TTeamMemberRole,
} from "@/types/teamTypes";

/** Keys touched when a team's membership changes: its member pages, its
    detail (memberCount), and the tree (memberCount badges). */
const useInvalidateMembership = (teamId: string) => {
  const queryClient = useQueryClient();
  return () => {
    queryClient.invalidateQueries({
      queryKey: [QUERY_KEYS.teamMembers, teamId],
    });
    queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.team, teamId] });
    queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.teamsTree] });
  };
};

/** useAddTeamMemberMutation adds an existing user to the team (POST). */
export const useAddTeamMemberMutation = (teamId: string) => {
  const invalidate = useInvalidateMembership(teamId);
  return useMutation<
    TTeamMember,
    Response,
    { account: string; role: TTeamMemberRole }
  >({
    mutationFn: async (body) => {
      const res = await addTeamMember(teamId, body);
      if (!res.ok) throw res;
      return (await res.json()) as TTeamMember;
    },
    onSuccess: invalidate,
  });
};

/** useBulkRoleChangeMutation changes selected members' roles (PUT batch). */
export const useBulkRoleChangeMutation = (teamId: string) => {
  const invalidate = useInvalidateMembership(teamId);
  return useMutation<
    TBatchResult,
    Response,
    { updates: { userId: string; role: TTeamMemberRole }[] }
  >({
    mutationFn: async (body) => {
      const res = await bulkRoleChange(teamId, body);
      if (!res.ok) throw res;
      return (await res.json()) as TBatchResult;
    },
    onSuccess: invalidate,
  });
};

/** useRemoveTeamMembersMutation bulk-removes memberships (DELETE batch). */
export const useRemoveTeamMembersMutation = (teamId: string) => {
  const queryClient = useQueryClient();
  const invalidate = useInvalidateMembership(teamId);
  return useMutation<TBatchResult, Response, string[]>({
    mutationFn: async (userIds) => {
      const res = await removeTeamMembers(teamId, userIds);
      if (!res.ok) throw res;
      return (await res.json()) as TBatchResult;
    },
    onSuccess: (result) => {
      /* Patch the cached member pages from succeeded[] right away (the
         batch response exists for client-side state patching — API design
         §batch), so the table drops the rows the moment the mutation
         lands. The invalidate below still refetches for server truth
         (paging fill-up, memberCount badges). */
      const removed = new Set(result.succeeded);
      queryClient.setQueriesData<TPage<TTeamMember>>(
        { queryKey: [QUERY_KEYS.teamMembers, teamId] },
        (old) =>
          old && {
            ...old,
            total: Math.max(0, old.total - removed.size),
            items: old.items.filter((m) => !removed.has(m.userId)),
          },
      );
      invalidate();
    },
  });
};
