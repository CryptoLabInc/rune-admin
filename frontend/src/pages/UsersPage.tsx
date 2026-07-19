import { useEffect, useState } from "react";

import Button from "@/components/elements/Button";
import Checkbox from "@/components/elements/Checkbox";
import Dropdown from "@/components/elements/Dropdown";
import Feedback from "@/components/elements/Feedback";
import MemberStatus from "@/components/elements/MemberStatus";
import Pagination from "@/components/elements/Pagination";
import SearchInput from "@/components/elements/SearchInput";
import Table from "@/components/table/Table";
import TableCell from "@/components/table/TableCell";
import TableFoot from "@/components/table/TableFoot";
import TableHead from "@/components/table/TableHead";
import TableHeaderCell from "@/components/table/TableHeaderCell";
import TableRow from "@/components/table/TableRow";
import MemberBatchFailureModal from "@/components/teams/MemberBatchFailureModal";
import { buildTeamOptions } from "@/components/teams/teamOptions";
import InviteMemberModal from "@/components/users/InviteMemberModal";
import MemberDeleteModal from "@/components/users/MemberDeleteModal";
import MemberDetailDrawer from "@/components/users/MemberDetailDrawer";
import { CHIP_STATUS } from "@/components/users/memberStatusMap";
import {
  useCancelInvitation,
  useDeleteUsers,
  useInviteMutation,
  useResendInvitation,
} from "@/hooks/mutations/useInvitationMutations";
import {
  useAddUserMembership,
  useBulkUserRoleChange,
  useDeactivateUserSession,
  useRemoveUserMemberships,
} from "@/hooks/mutations/useUserMembershipMutations";
import { useTeamsTreeQuery } from "@/hooks/queries/useTeamsTreeQuery";
import { useUserQuery } from "@/hooks/queries/useUserQuery";
import { useUsersQuery } from "@/hooks/queries/useUsersQuery";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";
import { parseErrorCode } from "@/api/parseError";
import { BTN_TEXT } from "@/constants/commonConstants";
import type { TDropdownOption } from "@/types/commonTypes";
import type { TTeamMemberRole, TTeamTree } from "@/types/teamTypes";
import type {
  TInvitePayload,
  TInviteResult,
  TUserListItem,
} from "@/types/userTypes";
import { useNoticeStore } from "@/stores/noticeStore";

const styles = {
  page: "flex flex-col gap-3.5 p-4",
  /* Wide enough for full corporate emails at the 40% column; anything
     longer (external partner domains) truncates with an ellipsis and
     keeps the full address in the title tooltip. */
  accountCell: "max-w-[400px] truncate",
  overflowChip:
    "border-border text-faint ml-1.5 rounded-full border px-2 text-xs",
};

/* Filter/sort option sets (SC-11 no.2–3). "all" stands in for 전체. */
const STATUS_OPTIONS: TDropdownOption[] = [
  { value: "all", label: "전체" },
  { value: "online", label: "온라인" },
  { value: "invite_redeemed", label: "초대코드 사용됨" },
  { value: "invite_pending", label: "초대 수락 대기" },
  { value: "invite_expired", label: "초대 코드 만료" },
  { value: "session_expired", label: "세션 만료" },
];

/* Depth indent stripped — the 150px filter trigger can't fit deep-tree
   indentation (it forces horizontal scrolling in the menu); teams list
   flush left in tree order and long names truncate with an ellipsis.
   Computed in-component (buildTeamOptions depends on the real teams
   query result — no static dummy list anymore). */
const buildGroupOptions = (teams: TTeamTree): TDropdownOption[] => [
  { value: "all", label: "전체" },
  ...buildTeamOptions(teams).map(({ value, label }) => ({ value, label })),
];

const SORT_OPTIONS: TDropdownOption[] = [
  { value: "last_invited", label: "최근 초대 코드 발송" },
  { value: "account", label: "account" },
];

/** First membership as "team · role"; the rest collapse into "+n". */
const membershipSummary = (user: TUserListItem) => {
  const [first, ...rest] = user.memberships;
  return first
    ? { summary: `${first.teamName} · ${first.role}`, extra: rest.length }
    : { summary: "—", extra: 0 };
};

/* 10 rows per page — caps the table height inside one screen; also the
   ?size=10 GET /users query param. */
const PAGE_SIZE = 10;

/** Batch-delete failure reasons shown by account (DELETE /users). */
const BATCH_REASON: Record<string, string> = {
  USER_NOT_FOUND: "사용자를 찾을 수 없습니다",
};

/**
 * UsersPage is the user management screen (SC-11): cross-team user
 * list with search/filters/sort, bulk actions, and pagination, plus
 * the invite modal (SC-12), member detail drawer (SC-13), and delete
 * confirm (SC-15). The list is driven by GET /users (useUsersQuery) —
 * search/status/team/sort/page all become query params, and the
 * server returns the already filtered/sorted/paged rows. The drawer's
 * detail (GET /users/{id}), role/membership batch, session deactivate,
 * invite/resend/cancel, and delete mutations are all wired to the API.
 */
const UsersPage = () => {
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [groupFilter, setGroupFilter] = useState("all");
  const [sort, setSort] = useState("last_invited");
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [page, setPage] = useState(1);
  const [inviteOpen, setInviteOpen] = useState(false);
  const [drawerUserId, setDrawerUserId] = useState<string | null>(null);
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false);
  const [batchFailures, setBatchFailures] = useState<
    { account: string; reason: string }[] | null
  >(null);
  const showNotice = useNoticeStore((state) => state.showNotice);

  const { data: teams } = useTeamsTreeQuery();
  const detailQuery = useUserQuery(drawerUserId ?? "");
  const bulkRole = useBulkUserRoleChange(drawerUserId ?? "");
  const removeMemberships = useRemoveUserMemberships(drawerUserId ?? "");
  const addMembership = useAddUserMembership(drawerUserId ?? "");
  const deactivateSession = useDeactivateUserSession(drawerUserId ?? "");
  const invite = useInviteMutation();
  const resend = useResendInvitation();
  const cancel = useCancelInvitation();
  const deleteUsersMutation = useDeleteUsers();
  const groupOptions = buildGroupOptions(teams ?? []);

  const debouncedSearch = useDebouncedValue(search, 300);
  const usersQuery = useUsersQuery({
    search: debouncedSearch.trim(),
    status: statusFilter,
    teamId: groupFilter,
    sort,
    page,
    size: PAGE_SIZE,
  });
  const users = usersQuery.data?.items ?? [];
  const total = usersQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const currentPage = Math.min(page, totalPages);

  /* keep the requested page within range so the query never asks for an out-of-range page */
  useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);

  /* A search/filter is active whenever it would narrow the server-side
     result — distinguishes "no members at all" (state B) from "no
     rows match this filter" (in-table empty row). */
  const hasActiveFilter =
    debouncedSearch.trim() !== "" ||
    statusFilter !== "all" ||
    groupFilter !== "all";

  const drawerUser = users.find((u) => u.userId === drawerUserId) ?? null;
  const selectedUsers = users.filter((u) => selectedIds.has(u.userId));

  /* Changing what's listed rejumps to page 1 (stale page = wrong slice)
     and clears the selection — the checked rows may drop out of the new
     result, so carrying them into a bulk action would be misleading. */
  const withPageReset =
    <T,>(setter: (value: T) => void) =>
    (value: T) => {
      setter(value);
      setPage(1);
      setSelectedIds(new Set());
    };
  /* Moving to another page clears the selection too — checks are
     page-scoped, and a checked row on the old page shouldn't ride along
     into a bulk action taken on a different page. */
  const goToPage = (next: number) => {
    setPage(next);
    setSelectedIds(new Set());
  };

  /* Select-all is page-scoped. */
  const allSelected =
    users.length > 0 && users.every((u) => selectedIds.has(u.userId));

  const toggleAll = (checked: boolean) =>
    setSelectedIds((prev) => {
      const next = new Set(prev);
      users.forEach((u) =>
        checked ? next.add(u.userId) : next.delete(u.userId),
      );
      return next;
    });

  const toggleOne = (userId: string, checked: boolean) =>
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (checked) next.add(userId);
      else next.delete(userId);
      return next;
    });

  /** POST /invitations — server judges duplicates and target states; only
      the staged team/role sets are sent (buildInvitePreview's sub-team
      expansion is display-only, the server performs the real expansion). */
  const inviteMember = async (
    payload: TInvitePayload,
  ): Promise<TInviteResult> => {
    try {
      await invite.mutateAsync({
        account: payload.email,
        memberships: payload.sets.map((set) => ({
          teamId: set.teamId,
          role: set.role as TTeamMemberRole,
        })),
      });
      return "success";
    } catch (err) {
      if (err instanceof Response) {
        const code = await parseErrorCode(err);
        return code === "ALREADY_TEAM_MEMBER" ? "duplicate-account" : "error";
      }
      return "error";
    }
  };

  /** POST /invitations/resend (per target) — status never changes (D10).
      Selection stays intact on partial failure so the user can retry. */
  const resendCodes = async (targets: TUserListItem[]) => {
    const results = await Promise.allSettled(
      targets.map((u) => resend.mutateAsync(u.userId)),
    );
    const failed = targets.filter((_, i) => results[i].status === "rejected");
    if (failed.length === 0) {
      showNotice("초대 코드 재전송", "초대 코드를 재전송했습니다.", "info");
      return;
    }
    setBatchFailures(
      failed.map((u) => ({ account: u.account, reason: "재전송 실패" })),
    );
  };

  /** DELETE /users (batch) — memberships, session token, and unused
      invite codes go together (D13). Full success clears the targets
      from selection and closes the drawer if it pointed at one of
      them; partial failure shows the failure modal (account + reason)
      and leaves the still-failed ids selected for retry. Throws only
      on full failure, so MemberDeleteModal/the drawer's onDeleteMember
      contract (resolve unless every target failed) is unaffected. */
  const deleteMembers = async (targets: TUserListItem[]) => {
    const userIds = targets.map((u) => u.userId);
    const result = await deleteUsersMutation.mutateAsync(userIds);
    const failedIds = new Set(result.failed.map((f) => f.id));
    const succeededIds = userIds.filter((id) => !failedIds.has(id));

    setSelectedIds((prev) => {
      const next = new Set(prev);
      succeededIds.forEach((id) => next.delete(id));
      return next;
    });
    if (drawerUserId && succeededIds.includes(drawerUserId)) {
      setDrawerUserId(null);
    }

    if (result.failed.length === 0) {
      showNotice("멤버 삭제", "멤버를 삭제했습니다.", "info");
      return;
    }
    if (succeededIds.length === 0) {
      throw new Error("delete failed for every target");
    }
    setBatchFailures(
      result.failed.map((f) => ({
        account: targets.find((u) => u.userId === f.id)?.account ?? f.id,
        reason: BATCH_REASON[f.code] ?? f.code,
      })),
    );
  };

  /* ── SC-11 state C — 조회 실패 ──────────────────────────────────── */
  if (usersQuery.isError) {
    return (
      <section className={styles.page} aria-label="멤버 관리">
        <Feedback
          state="error"
          title="멤버 정보를 불러올 수 없습니다."
          description="새로고침 후 다시 시도해 주세요."
          action={
            <Button
              btnText={BTN_TEXT.refresh}
              btnSize="sm"
              btnColor="grayOutline"
              className="w-fit"
              handleClick={() => window.location.reload()}
            />
          }
        />
      </section>
    );
  }

  /* ── SC-11 state B — 유저 0명, 검색/필터 없는 상태 (search/filters/table
     all hidden) ─── */
  if (!usersQuery.isPending && total === 0 && !hasActiveFilter) {
    return (
      <section className={styles.page} aria-label="멤버 관리">
        <Feedback
          state="empty"
          title="아직 초대한 멤버가 없습니다"
          description="멤버를 초대하면 초대 코드가 이메일로 발송됩니다"
          action={
            <Button
              btnText={BTN_TEXT.inviteMember}
              btnSize="sm"
              btnColor="mintFilled"
              className="w-fit"
              handleClick={() => setInviteOpen(true)}
            />
          }
        />
        {inviteOpen && (
          <InviteMemberModal
            onSubmit={inviteMember}
            onClose={() => setInviteOpen(false)}
            teams={teams ?? []}
          />
        )}
      </section>
    );
  }

  return (
    <section className={styles.page} aria-label="멤버 관리">
      <Table
        fluid
        /* Fixed page height: thead 36px + 10 rows × 49px (h-8 status chip
        + py-2). Short pages, empty, and loading all keep this height so
        pagination never shifts the layout. */
        scrollClassName="min-h-[526px]"
        toolbar={
          <div className="px-4 py-4">
            <div className="flex items-end justify-between gap-4">
              <div className="flex flex-col flex-wrap gap-5">
                <SearchInput
                  value={search}
                  onChange={withPageReset(setSearch)}
                  placeholder="계정 검색"
                  maxLength={100}
                  className="w-50"
                />
                {/* filter/order dropdown */}
                <div className="flex items-center gap-4">
                  <div className="flex items-center gap-2">
                    <span className="text-md text-faint">정렬 기준 </span>
                    <Dropdown
                      options={SORT_OPTIONS}
                      value={sort}
                      onChange={withPageReset(setSort)}
                      size="sm"
                      ariaLabel="정렬"
                      className="w-36"
                    />
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-md text-faint">status </span>
                    <Dropdown
                      options={STATUS_OPTIONS}
                      value={statusFilter}
                      onChange={withPageReset(setStatusFilter)}
                      size="sm"
                      ariaLabel="status 필터"
                      className="w-32"
                    />
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-md text-faint">team </span>
                    <Dropdown
                      options={groupOptions}
                      value={groupFilter}
                      onChange={withPageReset(setGroupFilter)}
                      size="sm"
                      ariaLabel="group 필터"
                      className="w-40"
                    />
                  </div>
                </div>
              </div>

              {/* Actions — second row, left-aligned (SC-11 no.4–6) */}
              <div className="flex items-center gap-2 self-end">
                <Button
                  btnText={BTN_TEXT.resendInvitationCode}
                  btnSize="sm"
                  btnColor="grayOutline"
                  className="w-fit"
                  disabled={selectedIds.size === 0}
                  handleClick={() => resendCodes(selectedUsers)}
                />
                <Button
                  btnText={BTN_TEXT.delete}
                  btnSize="sm"
                  btnColor="redFilled"
                  className="w-fit"
                  disabled={selectedIds.size === 0}
                  handleClick={() => setBulkDeleteOpen(true)}
                />
                <Button
                  btnText={BTN_TEXT.inviteMember}
                  btnSize="sm"
                  btnColor="mintFilled"
                  className="w-fit"
                  handleClick={() => setInviteOpen(true)}
                />
              </div>
            </div>
          </div>
        }
        foot={
          <TableFoot
            info={`총 ${total}명 · ${PAGE_SIZE}명/페이지`}
            className="flex-row"
          >
            <Pagination
              page={currentPage}
              totalPages={totalPages}
              onChange={goToPage}
            />
          </TableFoot>
        }
      >
        <TableHead>
          <TableHeaderCell className="w-8 pr-1">
            <Checkbox
              checked={allSelected}
              onChange={toggleAll}
              ariaLabel="전체 선택"
            />
          </TableHeaderCell>
          {/* Fixed column widths — auto layout would resize per page's
              content and shift the headers while paginating. */}
          <TableHeaderCell className="w-[40%]">account</TableHeaderCell>
          <TableHeaderCell className="w-[20%]">status</TableHeaderCell>
          <TableHeaderCell className="w-[40%]">teams (role)</TableHeaderCell>
        </TableHead>
        <tbody>
          {usersQuery.isPending && (
            <tr>
              <td
                colSpan={4}
                className="text-faint px-3 py-8 text-center text-sm"
              >
                불러오는 중…
              </td>
            </tr>
          )}
          {!usersQuery.isPending && users.length === 0 && (
            <tr>
              <td
                colSpan={4}
                className="text-muted-foreground border-t px-3 py-8 text-center text-sm"
              >
                검색 결과가 없습니다.
              </td>
            </tr>
          )}
          {users.map((user) => {
            const { summary, extra } = membershipSummary(user);
            return (
              <TableRow
                key={user.userId}
                selected={selectedIds.has(user.userId)}
                onClick={() => setDrawerUserId(user.userId)}
              >
                {/* Checkbox clicks must not open the drawer (SC-11 no.8) */}
                <TableCell className="w-8 pr-1">
                  <div onClick={(e) => e.stopPropagation()}>
                    <Checkbox
                      checked={selectedIds.has(user.userId)}
                      onChange={(checked) => toggleOne(user.userId, checked)}
                      ariaLabel={`${user.account} 선택`}
                    />
                  </div>
                </TableCell>
                <TableCell className={styles.accountCell}>
                  <span title={user.account}>{user.account}</span>
                </TableCell>
                <TableCell>
                  <MemberStatus status={CHIP_STATUS[user.status]} />
                </TableCell>
                <TableCell>
                  {summary}
                  {extra > 0 && (
                    <span
                      className={styles.overflowChip}
                      title={user.memberships
                        .map((m) => `${m.teamName} · ${m.role}`)
                        .join(", ")}
                    >
                      +{extra}
                    </span>
                  )}
                </TableCell>
              </TableRow>
            );
          })}
        </tbody>
      </Table>

      {inviteOpen && (
        <InviteMemberModal
          onSubmit={inviteMember}
          onClose={() => setInviteOpen(false)}
          teams={teams ?? []}
        />
      )}

      {drawerUser && (
        <MemberDetailDrawer
          key={drawerUser.userId}
          user={detailQuery.data ?? drawerUser}
          onClose={() => setDrawerUserId(null)}
          onUpdateRoles={(changes) =>
            bulkRole.mutateAsync({
              updates: changes.map((c) => ({
                teamId: c.teamId,
                role: c.role as TTeamMemberRole,
              })),
            })
          }
          onRemoveMemberships={(teamIds) =>
            removeMemberships.mutateAsync(teamIds)
          }
          onAddMembership={async (teamId, role) => {
            await addMembership.mutateAsync({
              teamId,
              role: role as TTeamMemberRole,
            });
          }}
          onDeactivateSession={async () => {
            await deactivateSession.mutateAsync();
          }}
          onResendCode={async () => {
            await resend.mutateAsync(drawerUser.userId);
          }}
          onCancelInvitation={async () => {
            await cancel.mutateAsync(drawerUser.userId);
          }}
          onDeleteMember={() => deleteMembers([drawerUser])}
          teams={teams ?? []}
        />
      )}

      {bulkDeleteOpen && (
        <MemberDeleteModal
          targets={selectedUsers.map((user) => ({
            account: user.account,
            memberships: user.memberships.map((m) => ({
              teamName: m.teamName,
              role: m.role,
            })),
          }))}
          onConfirm={() => deleteMembers(selectedUsers)}
          onClose={() => setBulkDeleteOpen(false)}
        />
      )}

      {batchFailures && (
        <MemberBatchFailureModal
          failures={batchFailures}
          onClose={() => setBatchFailures(null)}
        />
      )}
    </section>
  );
};

export default UsersPage;
