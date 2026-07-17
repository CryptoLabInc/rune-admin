import { useMutation, useQueryClient } from "@tanstack/react-query";

import {
  addUserMembership,
  bulkUserRoleChange,
  deactivateUserSession,
  removeUserMemberships,
} from "@/api/userAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import {
  type TBatchResult,
  type TTeamMemberRole,
  type TTeamMemberStatus,
} from "@/types/teamTypes";

/** Keys touched when a user's membership or session changes: its detail,
    the cross-team user list, and the Members badge count. */
const useInvalidateUserMembership = (userId: string) => {
  const queryClient = useQueryClient();
  return () => {
    queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.user, userId] });
    queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.users] });
    queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.usersStats] });
  };
};

/** useAddUserMembership adds the user to a team ([+ 팀 추가], POST). */
export const useAddUserMembership = (userId: string) => {
  const invalidate = useInvalidateUserMembership(userId);
  return useMutation<
    { teamId: string; teamName: string; role: TTeamMemberRole },
    Response,
    { teamId: string; role: TTeamMemberRole }
  >({
    mutationFn: async (body) => {
      const res = await addUserMembership(userId, body);
      if (!res.ok) throw res;
      return (await res.json()) as {
        teamId: string;
        teamName: string;
        role: TTeamMemberRole;
      };
    },
    onSuccess: invalidate,
  });
};

/** useBulkUserRoleChange changes the user's roles across teams in one batch (PUT). */
export const useBulkUserRoleChange = (userId: string) => {
  const invalidate = useInvalidateUserMembership(userId);
  return useMutation<
    TBatchResult,
    Response,
    { updates: { teamId: string; role: TTeamMemberRole }[] }
  >({
    mutationFn: async (body) => {
      const res = await bulkUserRoleChange(userId, body);
      if (!res.ok) throw res;
      return (await res.json()) as TBatchResult;
    },
    onSuccess: invalidate,
  });
};

/** useRemoveUserMemberships bulk-removes the user's team memberships (DELETE batch). */
export const useRemoveUserMemberships = (userId: string) => {
  const invalidate = useInvalidateUserMembership(userId);
  return useMutation<TBatchResult, Response, string[]>({
    mutationFn: async (teamIds) => {
      const res = await removeUserMemberships(userId, teamIds);
      if (!res.ok) throw res;
      return (await res.json()) as TBatchResult;
    },
    onSuccess: invalidate,
  });
};

/** useDeactivateUserSession destroys the user's console session token (DELETE). */
export const useDeactivateUserSession = (userId: string) => {
  const invalidate = useInvalidateUserMembership(userId);
  return useMutation<
    { userId: string; status: TTeamMemberStatus },
    Response,
    void
  >({
    mutationFn: async () => {
      const res = await deactivateUserSession(userId);
      if (!res.ok) throw res;
      return (await res.json()) as {
        userId: string;
        status: TTeamMemberStatus;
      };
    },
    onSuccess: invalidate,
  });
};
