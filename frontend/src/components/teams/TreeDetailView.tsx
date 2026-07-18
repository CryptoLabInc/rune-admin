import { useEffect, useState } from "react";

import Button from "@/components/elements/Button";
import Checkbox from "@/components/elements/Checkbox";
import Dropdown from "@/components/elements/Dropdown";
import MemberStatus from "@/components/elements/MemberStatus";
import Pagination from "@/components/elements/Pagination";
import Table from "@/components/table/Table";
import TableCell from "@/components/table/TableCell";
import TableErrorRow from "@/components/table/TableErrorRow";
import TableFoot from "@/components/table/TableFoot";
import TableHead from "@/components/table/TableHead";
import TableHeaderCell from "@/components/table/TableHeaderCell";
import TableRow from "@/components/table/TableRow";
import AddMemberModal from "@/components/teams/AddMemberModal";
import CreateTeamModal from "@/components/teams/CreateTeamModal";
import DeleteTeamModal from "@/components/teams/DeleteTeamModal";
import MemberBatchFailureModal from "@/components/teams/MemberBatchFailureModal";
import RemoveMembershipModal from "@/components/teams/RemoveMembershipModal";
import RenameTeamModal from "@/components/teams/RenameTeamModal";
import RoleChangeConfirmModal from "@/components/teams/RoleChangeConfirmModal";
import { ROLE_OPTIONS } from "@/components/teams/teamOptions";
import TeamTree from "@/components/tree/TeamTree";
import {
  useAddTeamMemberMutation,
  useBulkRoleChangeMutation,
  useRemoveTeamMembersMutation,
} from "@/hooks/mutations/useTeamMemberMutations";
import {
  useCreateTeamMutation,
  useDeleteTeamMutation,
  useRenameTeamMutation,
} from "@/hooks/mutations/useTeamMutations";
import { useTeamMembersQuery } from "@/hooks/queries/useTeamMembersQuery";
import { useTeamQuery } from "@/hooks/queries/useTeamQuery";
import { parseErrorCode } from "@/api/parseError";
import { formatDate } from "@/utils/formatDate";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import type { TMemberStatus, TTeamNode } from "@/types/commonTypes";
import type {
  TTeamMemberRole,
  TTeamMemberStatus,
  TTeamTree,
} from "@/types/teamTypes";
import { useNoticeStore } from "@/stores/noticeStore";

const styles = {
  body: "flex min-h-[340px] flex-1",
  /* Left tree panel (fixed width per wireframe) */
  side: "border-border flex w-50 flex-none flex-col gap-2.5 border-r p-3",
  /* Right detail area */
  main: "flex min-w-0 flex-1 flex-col gap-5 p-4",
  teamCard: "border-border bg-surface rounded-lg border px-4 py-3",
  teamCardRow: "flex items-center gap-2",
  teamName: "text-lg flex-1 font-semibold",
  teamMeta: "text-sm text-muted-foreground mt-1.5",
  membersRow: "flex items-center gap-2",
  membersTitle: "text-md flex-1 font-semibold",
  /* The detail panel is narrower than the users page — typical corp
     emails fit the 36% column; longer ones truncate with an ellipsis
     and keep the full address in the title tooltip. */
  accountCell: "max-w-[280px] truncate cursor-default",
  timeCell: "text-faint font-mono text-xs whitespace-nowrap",
  pendingActions: "flex flex-wrap items-center gap-2",
};

/** API wire status (snake_case) → MemberStatus chip state (kebab-case). */
const CHIP_STATUS: Record<TTeamMemberStatus, TMemberStatus> = {
  online: "online",
  invite_pending: "pending",
  invite_expired: "invite-expired",
  session_expired: "session-expired",
};

type TActiveModal =
  | "create"
  | "rename"
  | "delete"
  | "addMember"
  | "roleConfirm"
  | "removeMembers"
  | null;

/**
 * GET /teams/tree returns flat nodes — the client builds the recursive
 * TTeamNode shape the TeamTree component consumes (API design §3).
 */
const buildTeamNodes = (
  teams: TTeamTree,
  parentId: string | null,
): TTeamNode[] =>
  teams
    .filter((team) => team.parentId === parentId)
    .map((team) => ({
      id: team.id,
      name: team.name,
      members: team.memberCount,
      children:
        team.childCount > 0 ? buildTeamNodes(teams, team.id) : undefined,
    }));

const findTeamNode = (nodes: TTeamNode[], id: string): TTeamNode | undefined =>
  nodes.reduce<TTeamNode | undefined>(
    (found, node) =>
      found ?? (node.id === id ? node : findTeamNode(node.children ?? [], id)),
    undefined,
  );

/* 10 rows per page — caps the member table height inside one screen;
   the ?size=10 GET /teams/{id}/members query param. */
const PAGE_SIZE = 10;

/**
 * TreeDetailView is the SC-06 트리·상세 view: team tree panel (left) +
 * selected-team card and member table (right). Rendered by TeamsPage
 * when the view toggle is on 트리·상세.
 */
interface TreeDetailViewProps {
  /** Flat GET /teams/tree nodes — owned by TeamsPage. Always non-empty
      when this view renders (TeamsPage handles the loading/error/empty
      states before mounting it). */
  teams: TTeamTree;
  /** Team-tree filter text — owned by TeamsPage (header search input). */
  teamSearch: string;
  /** Selected team id — owned by TeamsPage so the org chart can select
      a team and hand off to this view (SC-05 node click → SC-06). */
  selectedTeamId: string;
  onSelectTeam: (teamId: string) => void;
}

/** Ancestor ids of a team — expanded so a selection handed off from
    the org chart is actually visible in the tree. */
const ancestorIds = (teams: TTeamTree, teamId: string): string[] => {
  const flatById = new Map(teams.map((team) => [team.id, team]));
  const ids: string[] = [];
  let parentId = flatById.get(teamId)?.parentId;
  while (parentId) {
    ids.push(parentId);
    parentId = flatById.get(parentId)?.parentId;
  }
  return ids;
};

const TreeDetailView = ({
  teams,
  teamSearch,
  selectedTeamId,
  onSelectTeam,
}: TreeDetailViewProps) => {
  const flatById = new Map(teams.map((t) => [t.id, t]));
  const teamNodes = buildTeamNodes(teams, null);
  /* Fallback selection — the first top-level team (SC-06 entry rule). */
  const defaultTeam = findTeamNode(teamNodes, "t_a") ?? teamNodes[0];

  const selectedTeam = findTeamNode(teamNodes, selectedTeamId) ?? defaultTeam;
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [page, setPage] = useState(1);

  /* Role edits are staged (SC-06): dropdown picks collect here and only
     apply on [변경사항 업데이트]. savedRoles is the committed baseline
     (stands in for the PUT /teams/{id}/members batch until wired). */
  const [pendingRoles, setPendingRoles] = useState<
    Map<string, TTeamMemberRole>
  >(new Map());
  const [savedRoles, setSavedRoles] = useState<Map<string, TTeamMemberRole>>(
    new Map(),
  );

  /* Switching teams must not leak the prior team's member-table state:
     without this, `page` can point past the new team's last page (no
     refetch flip since useTeamMembersQuery keeps the previous data
     visible), and stale userId-keyed selections/staged role edits from
     the old team would carry over into the new one. Reset on
     selectedTeam.id change only — NOT on page changes. Depends on the
     RESOLVED id (not the raw selectedTeamId prop) so the reset can
     never diverge from what the queries/mutations below actually
     target (e.g. when the prop doesn't resolve and falls back to
     defaultTeam). */
  useEffect(() => {
    setPage(1);
    setSelectedIds(new Set());
    setPendingRoles(new Map());
    setSavedRoles(new Map());
  }, [selectedTeam.id]);

  const { data: detail } = useTeamQuery(selectedTeam.id);
  const membersQuery = useTeamMembersQuery(selectedTeam.id, page, PAGE_SIZE);
  const members = membersQuery.data?.items ?? [];
  const total = membersQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const addMember = useAddTeamMemberMutation(selectedTeam.id);
  const bulkRole = useBulkRoleChangeMutation(selectedTeam.id);
  const removeMembers = useRemoveTeamMembersMutation(selectedTeam.id);
  const createTeam = useCreateTeamMutation();
  const renameTeam = useRenameTeamMutation(selectedTeam.id);
  const deleteTeam = useDeleteTeamMutation(selectedTeam.id);

  const flatTeam = flatById.get(selectedTeam.id);
  const parentName = detail?.parentId
    ? (flatById.get(detail.parentId)?.name ?? "없음")
    : flatTeam?.parentId
      ? (flatById.get(flatTeam.parentId)?.name ?? "없음")
      : "없음";
  const childCount = detail?.children.length ?? flatTeam?.childCount ?? 0;
  const childrenLabel = childCount ? `${childCount}개` : "없음";
  const memberCount = detail?.memberCount ?? selectedTeam.members;

  /* Select-all is page-scoped; selections persist across page moves. */
  const allSelected =
    members.length > 0 && members.every((m) => selectedIds.has(m.userId));

  const toggleAll = (checked: boolean) =>
    setSelectedIds((prev) => {
      const next = new Set(prev);
      members.forEach((m) =>
        checked ? next.add(m.userId) : next.delete(m.userId),
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

  const showNotice = useNoticeStore((state) => state.showNotice);

  const baseRole = (userId: string, fallback: TTeamMemberRole) =>
    savedRoles.get(userId) ?? fallback;

  const handleRoleChange = (
    userId: string,
    fallback: TTeamMemberRole,
    nextRole: string,
  ) =>
    setPendingRoles((prev) => {
      const next = new Map(prev);
      if (nextRole === baseRole(userId, fallback)) next.delete(userId);
      else next.set(userId, nextRole as TTeamMemberRole);
      return next;
    });

  const applyRoleChanges = () => {
    setSavedRoles((prev) => new Map([...prev, ...pendingRoles]));
    setPendingRoles(new Map());
    showNotice(
      MODAL_TITLES.roleChange,
      "변경사항이 저장되었습니다.",
      "success",
    );
  };

  /* Modals (SC-07~10 + SC-06 state E). All confirm handlers below call
     their real mutations. */
  const [activeModal, setActiveModal] = useState<TActiveModal>(null);
  const closeModal = () => setActiveModal(null);

  /* Team CRUD (create/rename/delete) inline error — reset whenever a
     modal opens or closes so a stale error from a prior attempt never
     leaks into a fresh one. */
  const [teamError, setTeamError] = useState<string | null>(null);
  const openTeamModal = (modal: TActiveModal) => {
    setTeamError(null);
    setActiveModal(modal);
  };
  const closeTeamModal = () => {
    setTeamError(null);
    closeModal();
  };
  const TEAM_REASON: Record<string, string> = {
    TEAM_NAME_DUPLICATE: "같은 상위 팀에 동일한 이름이 이미 있습니다.",
    TEAM_NAME_INVALID: "팀 이름 형식이 올바르지 않습니다.",
    TEAM_HAS_CHILDREN: "하위 팀이 있어 삭제할 수 없습니다.",
  };

  /* Partial-failure surface for the two batch endpoints (role change,
     remove): non-null opens MemberBatchFailureModal listing exactly
     what failed and why (API design — partial success is not an error). */
  const [batchFailures, setBatchFailures] = useState<
    { account: string; reason: string }[] | null
  >(null);
  const BATCH_REASON: Record<string, string> = {
    USER_NOT_FOUND: "사용자를 찾을 수 없습니다",
    NOT_TEAM_MEMBER: "팀 멤버가 아닙니다",
  };
  // Any other code (e.g. a transient INTERNAL) shows a generic retry message
  // instead of leaking the raw backend code into the failure modal.
  const BATCH_REASON_FALLBACK = "처리에 실패했습니다. 다시 시도해 주세요.";
  const accountOf = (userId: string) =>
    members.find((m) => m.userId === userId)?.account ?? userId;

  const [addError, setAddError] = useState<string | null>(null);
  const ADD_REASON: Record<string, string> = {
    ALREADY_TEAM_MEMBER: "이미 초대된 사용자입니다.",
    USER_NOT_FOUND: "등록되지 않은 계정입니다.",
    CANNOT_INVITE_ADMIN: "콘솔 관리자 계정은 추가할 수 없습니다.",
    MAIL_UPSTREAM_ERROR: "초대 코드 전송에 실패했습니다. 다시 시도해 주세요.",
  };

  const roleChanges = [...pendingRoles.entries()].map(([userId, to]) => {
    const member = members.find((m) => m.userId === userId);
    return {
      account: member?.account ?? userId,
      from: baseRole(userId, member?.role ?? "read"),
      to,
    };
  });

  const handleCreate = (name: string, parentId: string | null) => {
    setTeamError(null);
    createTeam.mutate(
      { name, parentId },
      {
        onSuccess: () => {
          closeModal();
          showNotice("팀 생성", "팀이 생성되었습니다.", "success");
        },
        onError: async (res) => {
          const code = await parseErrorCode(res);
          setTeamError(TEAM_REASON[code] ?? "팀 생성에 실패했습니다.");
        },
      },
    );
  };
  const handleRename = (name: string) => {
    setTeamError(null);
    renameTeam.mutate(
      { name },
      {
        onSuccess: () => {
          closeModal();
          showNotice("팀 이름 변경", "팀 이름이 변경되었습니다.", "success");
        },
        onError: async (res) => {
          const code = await parseErrorCode(res);
          setTeamError(TEAM_REASON[code] ?? "이름 변경에 실패했습니다.");
        },
      },
    );
  };
  const handleDelete = (
    action: "purge" | "transfer",
    targetTeamId?: string,
  ) => {
    setTeamError(null);
    deleteTeam.mutate(
      { memoryAction: action, targetTeamId },
      {
        onSuccess: () => {
          closeModal();
          showNotice("팀 삭제", "팀이 삭제되었습니다.", "success", () => {
            onSelectTeam(
              teams.find((t) => t.parentId === null && t.id !== selectedTeam.id)
                ?.id ?? "",
            );
          });
        },
        onError: async (res) => {
          const code = await parseErrorCode(res);
          setTeamError(TEAM_REASON[code] ?? "팀 삭제에 실패했습니다.");
        },
      },
    );
  };
  const handleInvite = (account: string, role: string) => {
    setAddError(null);
    addMember.mutate(
      { account, role: role as TTeamMemberRole },
      {
        onSuccess: () => {
          closeModal();
          showNotice("멤버 추가", "멤버를 추가했습니다.", "success");
        },
        onError: async (res) => {
          const code = await parseErrorCode(res);
          setAddError(ADD_REASON[code] ?? "멤버 추가에 실패했습니다.");
        },
      },
    );
  };
  const handleRoleConfirm = () => {
    const updates = [...pendingRoles.entries()].map(([userId, role]) => ({
      userId,
      role,
    }));
    bulkRole.mutate(
      { updates },
      {
        onSuccess: (result) => {
          closeModal();
          if (result.failed.length > 0) {
            /* Only clear staging for what actually succeeded — keep the
               failed entries pending so the user can retry them. */
            const failedIds = new Set(result.failed.map((f) => f.id));
            setSavedRoles(
              (prev) =>
                new Map([
                  ...prev,
                  ...[...pendingRoles.entries()].filter(
                    ([userId]) => !failedIds.has(userId),
                  ),
                ]),
            );
            setPendingRoles(
              (prev) =>
                new Map([...prev].filter(([userId]) => failedIds.has(userId))),
            );
            setBatchFailures(
              result.failed.map((f) => ({
                account: accountOf(f.id),
                reason: BATCH_REASON[f.code] ?? BATCH_REASON_FALLBACK,
              })),
            );
          } else {
            applyRoleChanges();
          }
        },
        onError: () => {
          closeModal();
          showNotice(
            MODAL_TITLES.roleChange,
            "권한 변경에 실패했습니다.",
            "error",
          );
        },
      },
    );
  };
  const handleRemoveMembers = () => {
    const ids = [...selectedIds];
    removeMembers.mutate(ids, {
      onSuccess: (result) => {
        closeModal();
        setSelectedIds(new Set());
        if (result.failed.length > 0) {
          setBatchFailures(
            result.failed.map((f) => ({
              account: accountOf(f.id),
              reason: BATCH_REASON[f.code] ?? f.code,
            })),
          );
        } else {
          showNotice(
            MODAL_TITLES.removeMembership,
            "멤버십이 제거되었습니다.",
            "success",
          );
        }
      },
      onError: () => {
        closeModal();
        showNotice(
          MODAL_TITLES.removeMembership,
          "멤버십 제거에 실패했습니다.",
          "error",
        );
      },
    });
  };

  /* SC-14 payload: the checked members' account · current role. */
  const membershipRemovals = members
    .filter((member) => selectedIds.has(member.userId))
    .map((member) => ({
      account: member.account,
      role:
        pendingRoles.get(member.userId) ?? baseRole(member.userId, member.role),
    }));

  return (
    <div className={styles.body}>
      {/* Left panel — create + tree (SC-06 no.3–5); search lives in the
          TeamsPage header */}
      <aside className={styles.side} aria-label="팀 트리">
        <Button
          btnText={BTN_TEXT.createGroup}
          btnSize="sm"
          btnColor="mintOutline"
          handleClick={() => openTeamModal("create")}
        />
        <TeamTree
          teams={teamNodes}
          query={teamSearch}
          selectedId={selectedTeam.id}
          onSelect={(node) => onSelectTeam(node.id)}
          defaultExpandedIds={[
            "t_a",
            "t_e",
            ...ancestorIds(teams, selectedTeam.id),
          ]}
          className="-mx-1 flex-1"
        />
      </aside>

      {/* Detail area — selected team card + members section (SC-06 no.6–13) */}
      <div className={styles.main}>
        <div className={styles.teamCard}>
          <div className={styles.teamCardRow}>
            <h3 className={styles.teamName}>
              {detail?.name ?? selectedTeam.name}
            </h3>
            <Button
              btnText={BTN_TEXT.rename}
              btnSize="sm"
              btnColor="grayOutline"
              className="w-fit"
              handleClick={() => openTeamModal("rename")}
            />
            <Button
              btnText={BTN_TEXT.deleteTeam}
              btnSize="sm"
              btnColor="redFilled"
              className="w-fit"
              handleClick={() => openTeamModal("delete")}
            />
          </div>
          <p className={styles.teamMeta}>
            상위 팀: {parentName} | 하위 팀: {childrenLabel} | 멤버:{" "}
            {memberCount}명 | 생성일: {formatDate(detail?.createdAt)}
          </p>
        </div>

        <div className={styles.membersRow}>
          <h3 className={styles.membersTitle}>멤버 ({total})</h3>{" "}
          <div className={styles.pendingActions}>
            <Button
              btnText={BTN_TEXT.updateChanges}
              btnSize="sm"
              btnColor="mintOutline"
              className="w-fit"
              disabled={pendingRoles.size === 0}
              handleClick={() => setActiveModal("roleConfirm")}
            />
            <Button
              btnText={BTN_TEXT.remove}
              btnSize="sm"
              btnColor="redFilled"
              className="w-fit"
              disabled={selectedIds.size === 0}
              handleClick={() => setActiveModal("removeMembers")}
            />
            <Button
              btnText={BTN_TEXT.addMember}
              btnSize="sm"
              btnColor="mintFilled"
              className="w-fit"
              handleClick={() => setActiveModal("addMember")}
            />
          </div>
        </div>

        <Table
          fluid
          scrollClassName="min-h-[526px]"
          foot={
            <TableFoot
              info={`총 ${total}명 · ${PAGE_SIZE}명/페이지`}
              className="flex-row items-center"
            >
              <div className="flex flex-col items-end gap-3">
                <Pagination
                  page={page}
                  totalPages={totalPages}
                  onChange={setPage}
                />
              </div>
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
            {/* Fixed column widths — auto layout would resize per
                page's content and shift the headers while paginating. */}
            <TableHeaderCell className="w-[36%]">account</TableHeaderCell>
            <TableHeaderCell className="w-[18%]">status</TableHeaderCell>
            <TableHeaderCell className="w-[28%]">role</TableHeaderCell>
            <TableHeaderCell className="w-[18%]">합류일</TableHeaderCell>
          </TableHead>
          <tbody>
            {membersQuery.isPending ? (
              <tr>
                <td
                  colSpan={5}
                  className="text-faint px-3 py-6 text-center text-sm"
                >
                  불러오는 중…
                </td>
              </tr>
            ) : membersQuery.isError ? (
              <TableErrorRow
                message="멤버 목록을 불러올 수 없습니다."
                colSpan={5}
              />
            ) : total === 0 ? (
              <tr>
                <td
                  colSpan={5}
                  className="text-faint px-3 py-6 text-center text-sm"
                >
                  멤버가 없습니다.
                </td>
              </tr>
            ) : (
              members.map((member) => (
                <TableRow
                  key={member.userId}
                  selected={selectedIds.has(member.userId)}
                  changed={pendingRoles.has(member.userId)}
                >
                  <TableCell className="w-8 pr-1">
                    <Checkbox
                      checked={selectedIds.has(member.userId)}
                      onChange={(checked) => toggleOne(member.userId, checked)}
                      ariaLabel={`${member.account} 선택`}
                    />
                  </TableCell>
                  <TableCell className={styles.accountCell}>
                    <span title={member.account}>{member.account}</span>
                  </TableCell>
                  <TableCell>
                    <MemberStatus status={CHIP_STATUS[member.status]} />
                  </TableCell>
                  <TableCell>
                    <Dropdown
                      options={ROLE_OPTIONS}
                      value={
                        pendingRoles.get(member.userId) ??
                        baseRole(member.userId, member.role)
                      }
                      onChange={(next) =>
                        handleRoleChange(member.userId, member.role, next)
                      }
                      size="sm"
                      changed={pendingRoles.has(member.userId)}
                      ariaLabel={`${member.account} role`}
                      className="w-24"
                    />
                  </TableCell>
                  <TableCell className={styles.timeCell}>
                    {formatDate(member.joinedAt)}
                  </TableCell>
                </TableRow>
              ))
            )}
          </tbody>
        </Table>
      </div>

      {/* Modals — mounted on demand so each opens with fresh state */}
      {activeModal === "create" && (
        <CreateTeamModal
          teams={teams}
          error={teamError}
          onClose={closeTeamModal}
          onCreate={handleCreate}
        />
      )}
      {activeModal === "rename" && (
        <RenameTeamModal
          currentName={selectedTeam.name}
          currentParentId={flatTeam?.parentId ?? null}
          teams={teams}
          error={teamError}
          onClose={closeTeamModal}
          onRename={handleRename}
        />
      )}
      {activeModal === "delete" && (
        <DeleteTeamModal
          teamId={selectedTeam.id}
          teamName={selectedTeam.name}
          hasChildren={(flatTeam?.childCount ?? 0) > 0}
          teams={teams}
          error={teamError}
          onClose={closeTeamModal}
          onDelete={handleDelete}
        />
      )}
      {activeModal === "addMember" && (
        <AddMemberModal
          teamName={selectedTeam.name}
          error={addError}
          onClose={() => {
            setAddError(null);
            closeModal();
          }}
          onInvite={handleInvite}
        />
      )}
      {activeModal === "roleConfirm" && (
        <RoleChangeConfirmModal
          changes={roleChanges}
          onClose={closeModal}
          onConfirm={handleRoleConfirm}
        />
      )}
      {activeModal === "removeMembers" && (
        <RemoveMembershipModal
          teamName={selectedTeam.name}
          members={membershipRemovals}
          onClose={closeModal}
          onConfirm={handleRemoveMembers}
        />
      )}
      {batchFailures && (
        <MemberBatchFailureModal
          failures={batchFailures}
          onClose={() => setBatchFailures(null)}
        />
      )}
    </div>
  );
};

export default TreeDetailView;
