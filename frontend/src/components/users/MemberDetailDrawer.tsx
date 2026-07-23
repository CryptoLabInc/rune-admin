import { useState } from "react";

import MembershipRow from "@/components/drawer/MembershipRow";
import Button from "@/components/elements/Button";
import Checkbox from "@/components/elements/Checkbox";
import Dropdown from "@/components/elements/Dropdown";
import MemberStatus from "@/components/elements/MemberStatus";
import StatusBadge from "@/components/elements/StatusBadge";
import DrawerLayout from "@/components/layout/DrawerLayout";
import Table from "@/components/table/Table";
import TableCell from "@/components/table/TableCell";
import TableHead from "@/components/table/TableHead";
import TableHeaderCell from "@/components/table/TableHeaderCell";
import TableRow from "@/components/table/TableRow";
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
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import { INVITATION_STATUS_VAR } from "@/constants/styleConstants";
import { L } from "@/locales";
import type { TBatchResult, TTeamTree } from "@/types/teamTypes";
import type { TUserListItem } from "@/types/userTypes";
import { useNoticeStore } from "@/stores/noticeStore";

const styles = {
  /* Status chip sits to the left of the access-time text, vertically
     centered (SC-13 header row). */
  statusRow: "flex items-center gap-2",
  accessTime: "text-faint font-mono text-xs",
  sectionHead: "flex items-center gap-2",
  selectedCount: "text-accent-blue text-tag font-mono",
  /* Action bar below the table: 변경사항 업데이트 · 제거하기 · 팀 추가하기. */
  bulkRow: "pt-2 flex justify-end gap-2",
  /* Team+role picker row opened by [팀 추가하기]. */
  addRow:
    "bg-muted-foreground/[2%] mb-2 flex items-center gap-2 rounded-md border p-2",
};

/** Per-status header timestamp (SC-13 no.1 — D13). Session takes priority:
    an online member shows last access; otherwise the invitation axis drives it. */
const subtitleFor = (user: TUserListItem): string => {
  if (user.sessionStatus === "online") {
    return L.members.lastAccessed(formatDate(user.lastAccessAt));
  }
  switch (user.invitationStatus) {
    case "invite_redeemed":
      return L.members.redeemedAwaiting;
    case "invite_pending":
    case "invite_expired":
      return L.members.lastInviteSentAt(formatDateTime(user.lastInvitedAt));
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
  TEAM_NOT_FOUND: L.members.teamNotFound,
  NOT_TEAM_MEMBER: L.teams.notTeamMember,
};
// Any other code (e.g. a transient INTERNAL) shows a generic retry message
// rather than leaking the raw backend code into the failure modal.
const BATCH_REASON_FALLBACK = L.common.processFailed;

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
  const [addRole, setAddRole] = useState("");
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
      showNotice(
        BTN_TEXT.resendInvitationCode,
        L.members.inviteCodeResent,
        "info",
      );
    } catch {
      showNotice(
        BTN_TEXT.resendInvitationCode,
        L.members.resendCodeFailed,
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
    setAddRole("");
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
      showNotice(L.members.addTeamTitle, L.members.addedToTeam, "info");
      resetAdd();
    } catch (err) {
      const code = err instanceof Response ? await parseErrorCode(err) : "";
      showNotice(
        L.members.addTeamTitle,
        code === "ALREADY_TEAM_MEMBER"
          ? L.members.alreadyTeamMember
          : L.members.addTeamFailed,
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
        title={user.username}
        subtitle={user.account}
        headerAction={<MemberStatus status={CHIP_STATUS[user.sessionStatus]} />}
        onClose={onClose}
        footer={
          <div className="col-span-2 flex justify-end">
            <Button
              btnText={BTN_TEXT.close}
              btnSize="md"
              btnColor="grayOutline"
              className="w-20"
              handleClick={onClose}
            />
          </div>
        }
      >
        <div className="flex flex-col gap-2">
          <div className={styles.statusRow}>
            <StatusBadge
              label={INVITATION_STATUS_VAR[user.invitationStatus].label}
              color={INVITATION_STATUS_VAR[user.invitationStatus].color}
            />
            <span className={styles.accessTime}>{subtitleFor(user)}</span>
          </div>

          <div className="flex flex-col gap-2 self-end">
            <div className={"flex items-center gap-2"}>
              <Button
                btnText={BTN_TEXT.resendInvitationCode}
                btnSize="sm"
                btnColor="mintOutline"
                className="w-fit"
                disabled={resending}
                handleClick={handleResend}
              />
              {/* Only meaningful while an unused, unexpired code exists —
              cancel forces it to expire (D15). */}
              <Button
                btnText={BTN_TEXT.cancelInvitation}
                btnSize="sm"
                btnColor="grayOutline"
                className="w-fit"
                disabled={user.invitationStatus !== "invite_pending"}
                handleClick={() => setOpenModal("cancel-invitation")}
              />
            </div>
          </div>
        </div>

        <hr />

        <section className="flex flex-col gap-4">
          <div className={styles.sectionHead}>
            <b className="text-md">
              {L.members.teamsHeading(memberships.length)}
            </b>
            {selected.length > 0 && (
              <span className={styles.selectedCount}>
                {selected.length} selected
              </span>
            )}
          </div>

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
                  ariaLabel={L.members.selectAll}
                />
              </TableHeaderCell>
              <TableHeaderCell>{L.common.team}</TableHeaderCell>
              <TableHeaderCell className="w-[104px]">
                {L.common.role}
              </TableHeaderCell>
            </TableHead>
            <tbody>
              {memberships.length === 0 ? (
                /* No group-role membership — a single placeholder row keeps
                   the table shape; the team/role cells read "—". */
                <TableRow>
                  <TableCell className="w-8 pr-1" />
                  <TableCell className="text-faint">—</TableCell>
                  <TableCell className="text-faint">—</TableCell>
                </TableRow>
              ) : (
                memberships.map((m) => (
                  <MembershipRow
                    key={m.teamId}
                    name={m.teamName}
                    role={m.role}
                    roleOptions={ROLE_OPTIONS}
                    checked={m.checked}
                    changed={m.role !== m.baseRole}
                    onCheck={(checked) =>
                      patchMembership(m.teamId, { checked })
                    }
                    onRoleChange={(role) => patchMembership(m.teamId, { role })}
                  />
                ))
              )}
            </tbody>
          </Table>

          {/* Action bar: 변경사항 초기화 · 변경사항 업데이트 · 제거하기 ·
              팀 추가하기 (SC-13). */}
          <div className={styles.bulkRow}>
            {/* Drops every staged (not yet applied) role pick back to its
                saved value — checkboxes and committed roles stay. */}
            <Button
              btnText={BTN_TEXT.resetChanges}
              btnSize="sm"
              btnColor="grayOutline"
              className="w-fit"
              disabled={changes.length === 0}
              handleClick={() =>
                setMemberships((prev) =>
                  prev.map((m) => ({ ...m, role: m.baseRole })),
                )
              }
            />
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
            <Button
              btnText={BTN_TEXT.addTeam}
              btnSize="sm"
              btnColor="mintFilled"
              className="w-fit"
              handleClick={() => (addOpen ? resetAdd() : setAddOpen(true))}
            />
          </div>

          {/* Team+role picker (SC-13 no.2) — opens just above the action
              bar via [팀 추가하기]; teams already joined are excluded. */}
          {addOpen && (
            <div className={`${styles.addRow} mt-3 mb-0`}>
              <Dropdown
                options={addableTeams}
                placeholder={
                  addableTeams.length === 0
                    ? L.members.noTeamsToAdd
                    : L.members.selectTeam
                }
                value={addTeamId}
                onChange={setAddTeamId}
                size="sm"
                ariaLabel={L.members.teamToAdd}
                className="flex-1"
                disabled={addableTeams.length === 0}
              />
              <Dropdown
                options={ROLE_OPTIONS}
                placeholder={L.members.selectRole}
                value={addRole}
                onChange={setAddRole}
                size="sm"
                ariaLabel={L.members.roleToAdd}
                className="w-24"
                /* No team left to join (all already joined) → the role
                   picker has nothing to apply to, so disable it too. */
                disabled={addableTeams.length === 0}
              />
              <Button
                btnText={BTN_TEXT.add}
                btnSize="sm"
                btnColor="mintFilled"
                className="w-fit"
                disabled={addTeamId === "" || addRole === "" || adding}
                handleClick={handleAdd}
              />
            </div>
          )}
        </section>

        <hr />

        <section className="flex flex-col gap-4">
          <div className={styles.sectionHead}>
            <b className="text-md">{L.nav.users}</b>
          </div>

          {/* Account-level actions: 세션 비활성화 · 멤버 삭제 (SC-15) —
              moved out of the header/status rows so they live with member
              management, still away from the footer's 닫기. */}
          <div className={styles.bulkRow}>
            {/* Destroys the session token → 세션 만료 (D12; confirm
                dialog follows). Enabled only while sessionStatus is
                "online" (D13). */}
            <Button
              btnText={BTN_TEXT.deactivateSession}
              btnSize="sm"
              btnColor="redOutline"
              className="w-fit"
              disabled={user.sessionStatus !== "online"}
              handleClick={() => setOpenModal("deactivate")}
            />
            <Button
              btnText={BTN_TEXT.deleteMember}
              btnSize="sm"
              btnColor="redFilled"
              className="w-fit"
              handleClick={() => setOpenModal("delete")}
            />
          </div>
        </section>
      </DrawerLayout>

      {openModal === "role-confirm" && (
        <RoleChangeConfirmModal
          subjectLabel={L.common.team}
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
              showNotice(
                MODAL_TITLES.removeMembership,
                L.teams.membershipsRemoved,
                "success",
              );
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
              showNotice(
                MODAL_TITLES.deactivateSession,
                L.members.sessionDeactivated,
                "info",
              );
            } catch (err) {
              const code =
                err instanceof Response ? await parseErrorCode(err) : "";
              setOpenModal(null);
              showNotice(
                MODAL_TITLES.deactivateSession,
                code === "SESSION_NOT_ACTIVE"
                  ? L.members.sessionAlreadyExpired
                  : L.members.deactivateFailed,
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
              showNotice(
                MODAL_TITLES.cancelInvitation,
                L.members.invitationCanceled,
                "info",
              );
            } catch (err) {
              const code =
                err instanceof Response ? await parseErrorCode(err) : "";
              setOpenModal(null);
              showNotice(
                MODAL_TITLES.cancelInvitation,
                code === "INVITATION_NOT_PENDING"
                  ? L.members.noInvitationToCancel
                  : L.members.cancelInvitationFailed,
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
