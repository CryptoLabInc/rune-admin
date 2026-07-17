import { useState } from "react";

import MembershipRow from "@/components/drawer/MembershipRow";
import Button from "@/components/elements/Button";
import Checkbox from "@/components/elements/Checkbox";
import Dropdown from "@/components/elements/Dropdown";
import MemberStatus from "@/components/elements/MemberStatus";
import DrawerLayout from "@/components/layout/DrawerLayout";
import Table from "@/components/table/Table";
import TableHead from "@/components/table/TableHead";
import TableHeaderCell from "@/components/table/TableHeaderCell";
import MemberBatchFailureModal from "@/components/teams/MemberBatchFailureModal";
import { getTeamDescendantIds } from "@/components/teams/teamHierarchy";
import { buildTeamOptions, ROLE_OPTIONS } from "@/components/teams/teamOptions";
import CancelInvitationModal from "@/components/users/CancelInvitationModal";
import MemberDeleteModal from "@/components/users/MemberDeleteModal";
import MembershipRemoveModal from "@/components/users/MembershipRemoveModal";
import { CHIP_STATUS } from "@/components/users/memberStatusMap";
import RoleChangeConfirmModal from "@/components/users/RoleChangeConfirmModal";
import SessionDeactivateModal from "@/components/users/SessionDeactivateModal";
import { parseErrorCode } from "@/api/parseError";
import { formatDate, formatDateTime } from "@/utils/formatDate";
import { BTN_TEXT } from "@/constants/commonConstants";
import type { TBatchResult, TTeamTree } from "@/types/teamTypes";
import type { TUserListItem } from "@/types/userTypes";
import { useNoticeStore } from "@/stores/noticeStore";

const styles = {
  statusRow:
    "flex flex-wrap items-center justify-between gap-3 border-b pb-[18px]",
  teamsSummary: "text-faint font-mono text-xs",
  sectionHead: "mb-2 flex items-center justify-between",
  selectedCount: "text-accent-blue text-tag font-mono",
  bulkRow: "mt-3 flex justify-end gap-2",
  /* Right-aligned action block; 멤버 삭제 (danger) sits on the second
     row in the third column, directly below 세션 비활성화. */
  actionRow: "mt-5 ml-auto grid w-fit grid-cols-[auto_auto_auto] gap-2",
  /* Inline team+role picker opened by [+ 팀 추가] (SC-13 no.2). */
  addRow:
    "bg-muted-foreground/[2%] mb-2 flex items-center gap-2 rounded-md border p-2",
};

/** Per-status header timestamp (SC-13 no.1 — D13). */
const subtitleFor = (user: TUserListItem): string => {
  switch (user.status) {
    case "online":
      return `최근 접속 ${formatDate(user.lastAccessAt)}`;
    case "invite_pending":
    case "invite_expired":
      return `최근 초대 코드 발송 ${formatDateTime(user.lastInvitedAt)}`;
    case "session_expired":
      return `세션 만료 ${formatDateTime(user.sessionExpiredAt)}`;
  }
};

/** One staged membership row: baseRole is the saved value. */
type TMembershipDraft = {
  teamId: string;
  teamName: string;
  baseRole: string;
  role: string;
  checked: boolean;
};

type TDrawerModal =
  | "role-confirm"
  | "remove"
  | "delete"
  | "deactivate"
  | "cancel-invitation"
  | null;

/** Batch-endpoint failure reasons shown by team name (SC-13 — shared
    with the team-side codes; the drawer only ever sees these two). */
const BATCH_REASON: Record<string, string> = {
  TEAM_NOT_FOUND: "팀을 찾을 수 없습니다",
  NOT_TEAM_MEMBER: "팀 멤버가 아닙니다",
};
// Any other code (e.g. a transient INTERNAL) shows a generic retry message
// rather than leaking the raw backend code into the failure modal.
const BATCH_REASON_FALLBACK = "처리에 실패했습니다. 다시 시도해 주세요.";

interface MemberDetailDrawerProps {
  user: TUserListItem;
  onClose: () => void;
  /** Applies staged role changes (this user, listed teams only); resolves
      the batch result — partial failures render inline (SC-13). */
  onUpdateRoles: (
    changes: { teamId: string; role: string }[],
  ) => Promise<TBatchResult>;
  /** Removes this user's memberships in the listed teams (no cascade);
      resolves the batch result. */
  onRemoveMemberships: (teamIds: string[]) => Promise<TBatchResult>;
  /** Adds this user to a team (SC-13 no.2 — POST /users/{id}/memberships). */
  onAddMembership: (teamId: string, role: string) => Promise<void>;
  /** Issues a new invite code (WT) — status never changes (D10). */
  onResendCode: () => Promise<void>;
  /** Deletes the account — the caller also closes this drawer (SC-15). */
  onDeleteMember: () => Promise<void>;
  /** Destroys the user's console session token (D12). */
  onDeactivateSession: () => Promise<void>;
  /** Force-expires every unused invite code for this account (D15) —
      the account itself is not deleted. */
  onCancelInvitation: () => Promise<void>;
  /** Real team tree (GET /teams/tree) — drives the sub-team notice. */
  teams: TTeamTree;
}

/**
 * MemberDetailDrawer is the 멤버 상세 drawer (SC-13): per-status
 * timestamp, membership list with staged role edits (applied through
 * the role-change confirm modal) and checkbox bulk removal (SC-14),
 * invite-code actions, and member delete (SC-15). Mount with
 * key={user.userId} so switching members resets the staged state.
 * [초대 취소] (D15) and [세션 비활성화] (D12) ship with correct enable
 * rules and confirm dialogs.
 */
const MemberDetailDrawer = ({
  user,
  onClose,
  onUpdateRoles,
  onRemoveMemberships,
  onAddMembership,
  onResendCode,
  onDeleteMember,
  onDeactivateSession,
  onCancelInvitation,
  teams,
}: MemberDetailDrawerProps) => {
  const [memberships, setMemberships] = useState<TMembershipDraft[]>(() =>
    user.memberships.map((m) => ({
      teamId: m.teamId,
      teamName: m.teamName,
      baseRole: m.role,
      role: m.role,
      checked: false,
    })),
  );
  const [openModal, setOpenModal] = useState<TDrawerModal>(null);
  const [resending, setResending] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [addTeamId, setAddTeamId] = useState("");
  const [addRole, setAddRole] = useState("read");
  const [adding, setAdding] = useState(false);
  const [batchFailures, setBatchFailures] = useState<
    { account: string; reason: string }[] | null
  >(null);
  const showNotice = useNoticeStore((state) => state.showNotice);
  const teamOptions = buildTeamOptions(teams);

  const changes = memberships.filter((m) => m.role !== m.baseRole);
  const selected = memberships.filter((m) => m.checked);
  const allChecked =
    memberships.length > 0 && memberships.every((m) => m.checked);

  const [firstMembership] = memberships;
  const teamsSummary = firstMembership
    ? `teams: ${firstMembership.teamName} (${firstMembership.baseRole})${
        memberships.length > 1 ? ` +${memberships.length - 1}` : ""
      }`
    : "teams: —";

  /* Sub-team retention notice (SC-14 no.2): a selected team has a
     descendant team whose membership stays after this removal. */
  const remainingIds = memberships
    .filter((m) => !m.checked)
    .map((m) => m.teamId);
  const subteamNotice = selected.some((m) =>
    getTeamDescendantIds(teams, m.teamId).some((id) =>
      remainingIds.includes(id),
    ),
  );

  const patchMembership = (teamId: string, patch: Partial<TMembershipDraft>) =>
    setMemberships((prev) =>
      prev.map((m) => (m.teamId === teamId ? { ...m, ...patch } : m)),
    );

  const handleResend = async () => {
    setResending(true);
    try {
      await onResendCode();
      showNotice("초대 코드 재전송", "초대 코드를 재전송했습니다.", "info");
    } catch {
      showNotice(
        "초대 코드 재전송",
        "초대 코드 재전송에 실패했습니다. 다시 시도해주세요.",
        "error",
      );
    } finally {
      setResending(false);
    }
  };

  /* Teams the user already belongs to stay out of the add picker.
     Depth indent stripped — the narrow drawer dropdown can't fit
     deep-tree indentation (it forces horizontal scrolling in the
     menu); teams list flush left in tree order and long names
     truncate with an ellipsis. */
  const joinedIds = new Set(memberships.map((m) => m.teamId));
  const addableTeams = teamOptions
    .filter((o) => !joinedIds.has(o.value))
    .map(({ value, label }) => ({ value, label }));

  const resetAdd = () => {
    setAddOpen(false);
    setAddTeamId("");
    setAddRole("read");
  };

  const handleAdd = async () => {
    setAdding(true);
    try {
      await onAddMembership(addTeamId, addRole);
      const teamName =
        teamOptions.find((o) => o.value === addTeamId)?.label ?? addTeamId;
      setMemberships((prev) => [
        ...prev,
        {
          teamId: addTeamId,
          teamName,
          baseRole: addRole,
          role: addRole,
          checked: false,
        },
      ]);
      showNotice("팀 추가", "팀에 추가되었습니다.", "info");
      resetAdd();
    } catch (err) {
      const code = err instanceof Response ? await parseErrorCode(err) : "";
      showNotice(
        "팀 추가",
        code === "ALREADY_TEAM_MEMBER"
          ? "이미 소속된 팀입니다."
          : "팀 추가에 실패했습니다. 다시 시도해주세요.",
        "error",
      );
    } finally {
      setAdding(false);
    }
  };

  return (
    <>
      <DrawerLayout
        isOpen
        title={user.account}
        subtitle={subtitleFor(user)}
        onClose={onClose}
        footer={
          <Button
            btnText={BTN_TEXT.close}
            btnSize="md"
            btnColor="grayOutline"
            handleClick={onClose}
          />
        }
      >
        <div className={styles.statusRow}>
          <MemberStatus status={CHIP_STATUS[user.status]} />
          <span className={styles.teamsSummary}>{teamsSummary}</span>
        </div>

        <section className="mt-5">
          <div className={styles.sectionHead}>
            <div className="flex items-center gap-2">
              <b className="text-sm">소속 팀 ({memberships.length})</b>
              {selected.length > 0 && (
                <span className={styles.selectedCount}>
                  {selected.length} selected
                </span>
              )}
            </div>
            <Button
              btnText={BTN_TEXT.addTeam}
              btnSize="sm"
              btnColor="grayOutline"
              className="w-fit"
              handleClick={() => (addOpen ? resetAdd() : setAddOpen(true))}
            />
          </div>
          {/* Team+role picker (SC-13 no.2) — teams already joined are
              excluded; the row applies through onAddMembership. */}
          {addOpen && (
            <div className={styles.addRow}>
              <Dropdown
                options={addableTeams}
                placeholder="팀 선택"
                value={addTeamId}
                onChange={setAddTeamId}
                size="sm"
                ariaLabel="추가할 팀"
                className="flex-1"
              />
              <Dropdown
                options={ROLE_OPTIONS}
                value={addRole}
                onChange={setAddRole}
                size="sm"
                ariaLabel="추가할 role"
                className="w-[90px]"
              />
              <Button
                btnText={BTN_TEXT.add}
                btnSize="sm"
                btnColor="mintFilled"
                className="w-fit"
                disabled={addTeamId === "" || adding}
                handleClick={handleAdd}
              />
            </div>
          )}
          {/* Membership table — same shell/head/row grammar as the app's
              data tables; select-all is the same bulk model as the
              account table (SC-13 no.3). */}
          <Table fluid>
            <TableHead>
              <TableHeaderCell className="w-8 pr-1">
                <Checkbox
                  checked={allChecked}
                  onChange={(checked) =>
                    setMemberships((prev) =>
                      prev.map((m) => ({ ...m, checked })),
                    )
                  }
                  ariaLabel="전체선택"
                />
              </TableHeaderCell>
              <TableHeaderCell>팀</TableHeaderCell>
              <TableHeaderCell className="w-[104px]">role</TableHeaderCell>
            </TableHead>
            <tbody>
              {memberships.map((m) => (
                <MembershipRow
                  key={m.teamId}
                  name={m.teamName}
                  role={m.role}
                  roleOptions={ROLE_OPTIONS}
                  checked={m.checked}
                  changed={m.role !== m.baseRole}
                  onCheck={(checked) => patchMembership(m.teamId, { checked })}
                  onRoleChange={(role) => patchMembership(m.teamId, { role })}
                />
              ))}
            </tbody>
          </Table>
          <div className={styles.bulkRow}>
            <Button
              btnText={BTN_TEXT.updateChanges}
              btnSize="sm"
              btnColor="mintFilled"
              className="w-fit"
              disabled={changes.length === 0}
              handleClick={() => setOpenModal("role-confirm")}
            />
            <Button
              btnText={BTN_TEXT.remove}
              btnSize="sm"
              btnColor="redFilled"
              className="w-fit"
              disabled={selected.length === 0}
              handleClick={() => setOpenModal("remove")}
            />
          </div>
        </section>

        <div className={styles.actionRow}>
          <Button
            btnText={BTN_TEXT.resendInvitationCode}
            btnSize="sm"
            btnColor="grayOutline"
            disabled={resending}
            handleClick={handleResend}
          />
          {/* Only meaningful while an unused, unexpired code exists —
              cancel forces it to expire (D15). */}
          <Button
            btnText={BTN_TEXT.cancelInvitation}
            btnSize="sm"
            btnColor="grayFilled"
            disabled={user.status !== "invite_pending"}
            handleClick={() => setOpenModal("cancel-invitation")}
          />
          {/* Destroys the session token → 세션 만료 (D12; confirm
              dialog follows). Disabled once already expired (D13). */}
          <Button
            btnText={BTN_TEXT.deactivateSession}
            btnSize="sm"
            btnColor="grayOutline"
            disabled={user.status === "session_expired"}
            handleClick={() => setOpenModal("deactivate")}
          />
          <Button
            btnText={BTN_TEXT.deleteMember}
            btnSize="sm"
            btnColor="redFilled"
            className="col-start-3"
            handleClick={() => setOpenModal("delete")}
          />
        </div>
      </DrawerLayout>

      {openModal === "role-confirm" && (
        <RoleChangeConfirmModal
          subjectLabel="팀"
          changes={changes.map((m) => ({
            label: m.teamName,
            from: m.baseRole,
            to: m.role,
          }))}
          onConfirm={async () => {
            const changedIds = changes.map((m) => m.teamId);
            const result = await onUpdateRoles(
              changes.map((m) => ({ teamId: m.teamId, role: m.role })),
            );
            const failedIds = new Set(result.failed.map((f) => f.id));
            setMemberships((prev) =>
              prev.map((m) =>
                changedIds.includes(m.teamId) && !failedIds.has(m.teamId)
                  ? { ...m, baseRole: m.role }
                  : m,
              ),
            );
            if (result.failed.length > 0) {
              setBatchFailures(
                result.failed.map((f) => ({
                  account:
                    memberships.find((m) => m.teamId === f.id)?.teamName ??
                    f.id,
                  reason: BATCH_REASON[f.code] ?? BATCH_REASON_FALLBACK,
                })),
              );
            }
          }}
          onClose={() => setOpenModal(null)}
        />
      )}

      {openModal === "remove" && (
        <MembershipRemoveModal
          targets={selected.map((m) => ({
            account: user.account,
            teamId: m.teamId,
            teamName: m.teamName,
            role: m.role,
          }))}
          subteamNotice={subteamNotice}
          onConfirm={async () => {
            const removedIds = selected.map((m) => m.teamId);
            const result = await onRemoveMemberships(removedIds);
            const failedIds = new Set(result.failed.map((f) => f.id));
            setMemberships((prev) =>
              prev.filter(
                (m) =>
                  !removedIds.includes(m.teamId) || failedIds.has(m.teamId),
              ),
            );
            if (result.failed.length === 0) {
              showNotice("멤버십 제거", "멤버십이 제거되었습니다.", "success");
            } else {
              setBatchFailures(
                result.failed.map((f) => ({
                  account:
                    memberships.find((m) => m.teamId === f.id)?.teamName ??
                    f.id,
                  reason: BATCH_REASON[f.code] ?? BATCH_REASON_FALLBACK,
                })),
              );
            }
          }}
          onClose={() => setOpenModal(null)}
        />
      )}

      {openModal === "delete" && (
        <MemberDeleteModal
          targets={[
            {
              account: user.account,
              memberships: memberships.map((m) => ({
                teamName: m.teamName,
                role: m.baseRole,
              })),
            },
          ]}
          onConfirm={onDeleteMember}
          onClose={() => setOpenModal(null)}
        />
      )}

      {openModal === "deactivate" && (
        <SessionDeactivateModal
          account={user.account}
          onConfirm={async () => {
            try {
              await onDeactivateSession();
              setOpenModal(null);
              showNotice("세션 비활성화", "세션을 비활성화했습니다.", "info");
            } catch (err) {
              const code =
                err instanceof Response ? await parseErrorCode(err) : "";
              setOpenModal(null);
              showNotice(
                "세션 비활성화",
                code === "SESSION_NOT_ACTIVE"
                  ? "이미 만료된 세션입니다."
                  : "세션 비활성화에 실패했습니다. 다시 시도해주세요.",
                "error",
              );
            }
          }}
          onClose={() => setOpenModal(null)}
        />
      )}

      {openModal === "cancel-invitation" && (
        <CancelInvitationModal
          account={user.account}
          onConfirm={async () => {
            try {
              await onCancelInvitation();
              setOpenModal(null);
              showNotice("초대 취소", "초대를 취소했습니다.", "info");
            } catch (err) {
              const code =
                err instanceof Response ? await parseErrorCode(err) : "";
              setOpenModal(null);
              showNotice(
                "초대 취소",
                code === "INVITATION_NOT_PENDING"
                  ? "취소할 초대가 없습니다."
                  : "초대 취소에 실패했습니다. 다시 시도해주세요.",
                "error",
              );
            }
          }}
          onClose={() => setOpenModal(null)}
        />
      )}

      {batchFailures && (
        <MemberBatchFailureModal
          failures={batchFailures}
          onClose={() => setBatchFailures(null)}
        />
      )}
    </>
  );
};

export default MemberDetailDrawer;
