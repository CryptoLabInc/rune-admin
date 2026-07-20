import { useMutation, useQueryClient } from "@tanstack/react-query";

import {
  cancelInvitation,
  postInvitation,
  resendInvitation,
} from "@/api/invitationAPIs";
import { deleteUsers } from "@/api/userAPIs";
import { QUERY_KEYS } from "@/constants/commonConstants";
import {
  type TBatchResult,
  type TInvitationStatus,
  type TSessionStatus,
  type TTeamMemberRole,
} from "@/types/teamTypes";
import { type TInvitationResponse } from "@/types/userTypes";

/** Keys touched by every invitation/deletion mutation: the cross-team user
    list, the Members badge count, and the invitation history table. */
const useInvalidateInvitations = () => {
  const queryClient = useQueryClient();
  return () => {
    queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.users] });
    queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.usersStats] });
    queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.invitations] });
  };
};

/** useInviteMutation invites an account with team/role memberships
    (POST /invitations). */
export const useInviteMutation = () => {
  const invalidate = useInvalidateInvitations();
  return useMutation<
    TInvitationResponse,
    Response,
    {
      account: string;
      username: string;
      memberships: { teamId: string; role: TTeamMemberRole }[];
    }
  >({
    mutationFn: async (body) => {
      const res = await postInvitation(body);
      if (!res.ok) throw res;
      return (await res.json()) as TInvitationResponse;
    },
    onSuccess: invalidate,
  });
};

/** useResendInvitation issues a new invite code for one user
    (POST /invitations/resend). */
export const useResendInvitation = () => {
  const queryClient = useQueryClient();
  const invalidate = useInvalidateInvitations();
  return useMutation<
    {
      userId: string;
      invitationStatus: TInvitationStatus;
      sessionStatus: TSessionStatus;
    },
    Response,
    string
  >({
    mutationFn: async (userId) => {
      const res = await resendInvitation(userId);
      if (!res.ok) throw res;
      return (await res.json()) as {
        userId: string;
        invitationStatus: TInvitationStatus;
        sessionStatus: TSessionStatus;
      };
    },
    onSuccess: (_, userId) => {
      invalidate();
      queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.user, userId] });
    },
  });
};

/** useCancelInvitation force-expires a pending invite code
    (POST /invitations/cancel). */
export const useCancelInvitation = () => {
  const queryClient = useQueryClient();
  const invalidate = useInvalidateInvitations();
  return useMutation<
    {
      userId: string;
      invitationStatus: TInvitationStatus;
      sessionStatus: TSessionStatus;
    },
    Response,
    string
  >({
    mutationFn: async (userId) => {
      const res = await cancelInvitation(userId);
      if (!res.ok) throw res;
      return (await res.json()) as {
        userId: string;
        invitationStatus: TInvitationStatus;
        sessionStatus: TSessionStatus;
      };
    },
    onSuccess: (_, userId) => {
      invalidate();
      queryClient.invalidateQueries({ queryKey: [QUERY_KEYS.user, userId] });
    },
  });
};

/** useDeleteUsers bulk-deletes accounts — memberships, session token, and
    the account itself are gone, so no per-user detail invalidation is
    needed (DELETE batch). */
export const useDeleteUsers = () => {
  const invalidate = useInvalidateInvitations();
  return useMutation<TBatchResult, Response, string[]>({
    mutationFn: async (userIds) => {
      const res = await deleteUsers(userIds);
      if (!res.ok) throw res;
      return (await res.json()) as TBatchResult;
    },
    onSuccess: invalidate,
  });
};
