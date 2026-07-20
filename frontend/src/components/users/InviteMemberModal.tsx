import { useRef, useState } from "react";

import Button from "@/components/elements/Button";
import Dropdown from "@/components/elements/Dropdown";
import Input from "@/components/elements/Input";
import MemberStatus from "@/components/elements/MemberStatus";
import Notice from "@/components/elements/Notice";
import ModalLayout from "@/components/layout/ModalLayout";
import { buildTeamOptions, ROLE_OPTIONS } from "@/components/teams/teamOptions";
import { buildInvitePreview } from "@/components/users/invitePreview";
import ModalTable from "@/components/users/ModalTable";
import {
  isSubmittableUsername,
  normalizeUsernameInput,
  USERNAME_MAX_LENGTH,
  validateUsername,
} from "@/utils/username";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import type { TTeamTree } from "@/types/teamTypes";
import type { TInvitePayload, TInviteResult } from "@/types/userTypes";

const styles = {
  fieldLabel: "text-sm font-semibold",
  setRow: "flex items-center gap-2",
  teamSlot: "min-w-0 flex-1",
  roleSlot: "w-[120px] flex-none",
  removeSlot: "w-9 flex-none",
};

/** Complete email format — validated on blur (SC-12 no.1). */
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

const EMAIL_FORMAT_ERROR = "올바른 이메일 형식이 아닙니다";
const DUPLICATE_ACCOUNT_ERROR =
  "이미 등록된 계정입니다. 멤버 추가 또는 초대 코드 재전송을 사용하세요.";
const SEND_FAILED_MESSAGE = "초대 전송에 실패했습니다. 다시 시도해 주세요.";

/** One editable team/role set row; id keys the row across removals. */
type TSetDraft = { id: number; teamId: string; role: string };

interface InviteMemberModalProps {
  /**
   * Sends the invite; resolves with the server verdict. "duplicate-account"
   * lands as the email warning subtext, "error" as the message-area
   * failure notice (state B) — a rejection is treated as "error" too.
   */
  onSubmit: (payload: TInvitePayload) => Promise<TInviteResult>;
  onClose: () => void;
  /** Real team tree (GET /teams/tree) — drives the sub-team preview. */
  teams: TTeamTree;
}

/**
 * InviteMemberModal is the 유저 초대 모달 (SC-12): email + team/role
 * sets (one team per set, first set not removable) with the 하위 팀
 * 권한 미리보기 showing the R1 downward copy. Duplicate-account and
 * send-failure verdicts come from the server on [초대 전송]. Mount
 * conditionally — internal state resets by unmounting.
 */
const InviteMemberModal = ({
  onSubmit,
  onClose,
  teams,
}: InviteMemberModalProps) => {
  const [email, setEmail] = useState("");
  const [emailError, setEmailError] = useState("");
  const [username, setUsername] = useState("");
  const [sets, setSets] = useState<TSetDraft[]>([
    { id: 0, teamId: "", role: "" },
  ]);
  const [sendFailed, setSendFailed] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const nextSetId = useRef(1);

  const completeSets = sets.filter((set) => set.teamId && set.role);
  const previewRows = buildInvitePreview(completeSets, teams);
  /* The preview only appears once a selected team actually contributes
     sub-teams (SC-12 no.3 — "하위 팀이 달린 상위 팀 선택 시"). */
  const showPreview = previewRows.some((row) => row.indent);

  const usernameError = validateUsername(username);
  const canSubmit =
    EMAIL_PATTERN.test(email.trim()) &&
    isSubmittableUsername(username) &&
    completeSets.length >= 1 &&
    completeSets.length === sets.length &&
    !submitting;

  const validateEmail = () => {
    if (email.trim() && !EMAIL_PATTERN.test(email.trim()))
      setEmailError(EMAIL_FORMAT_ERROR);
  };

  /** Team options for one set — sourced from the real team tree (GET
      /teams/tree), not the dummy fixture, so selected ids are valid
      against the server. Teams picked in other sets are locked
      (disabled, not removed, so the tree indentation stays readable). */
  const teamOptionsFor = (setId: number) => {
    const takenElsewhere = new Set(
      sets.filter((s) => s.id !== setId && s.teamId).map((s) => s.teamId),
    );
    return buildTeamOptions(teams).map((option) =>
      takenElsewhere.has(option.value) ? { ...option, disabled: true } : option,
    );
  };

  const patchSet = (setId: number, patch: Partial<TSetDraft>) =>
    setSets((prev) =>
      prev.map((s) => (s.id === setId ? { ...s, ...patch } : s)),
    );

  const addSet = () => {
    setSets((prev) => [
      ...prev,
      { id: nextSetId.current, teamId: "", role: "" },
    ]);
    nextSetId.current += 1;
  };

  const removeSet = (setId: number) =>
    setSets((prev) => prev.filter((s) => s.id !== setId));

  const handleSubmit = async () => {
    setSubmitting(true);
    setSendFailed(false);
    setEmailError("");
    try {
      const result = await onSubmit({
        email: email.trim(),
        username: username.trim(),
        sets: completeSets.map(({ teamId, role }) => ({ teamId, role })),
      });
      if (result === "success") onClose();
      else if (result === "duplicate-account")
        setEmailError(DUPLICATE_ACCOUNT_ERROR);
      else setSendFailed(true);
    } catch {
      setSendFailed(true);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <ModalLayout title={MODAL_TITLES.inviteMember} isOpen isWide>
      <div className="flex flex-col gap-6">
        <Input
          id="invite-email"
          type="email"
          labelText="이메일 (account)"
          placeholder="user@corp.com"
          maxLength={100}
          value={email}
          setValue={(value) => {
            setEmail(value);
            setEmailError("");
          }}
          onBlur={validateEmail}
          error={emailError}
        />

        <Input
          id="invite-username"
          labelText="사용자 이름 (username)"
          placeholder="사용자 이름"
          maxLength={USERNAME_MAX_LENGTH}
          value={username}
          setValue={(value) => setUsername(normalizeUsernameInput(value))}
          error={usernameError}
        />

        <div className="flex flex-col gap-2">
          <span className={styles.fieldLabel}>팀 / 권한</span>
          {sets.map((set, index) => (
            <div key={set.id} className={styles.setRow}>
              <div className={styles.teamSlot}>
                <Dropdown
                  options={teamOptionsFor(set.id)}
                  placeholder="팀 선택"
                  value={set.teamId}
                  onChange={(teamId) => patchSet(set.id, { teamId })}
                  ariaLabel={`세트 ${index + 1} 팀`}
                />
              </div>
              <div className={styles.roleSlot}>
                <Dropdown
                  options={ROLE_OPTIONS}
                  placeholder="권한 선택"
                  value={set.role}
                  onChange={(role) => patchSet(set.id, { role })}
                  ariaLabel={`세트 ${index + 1} role`}
                />
              </div>
              {/* First set is required and carries no remove button
                  (SC-12 no.2); the slot stays as a spacer for column
                  alignment once a second set exists. */}
              {sets.length > 1 &&
                (index > 0 ? (
                  <Button
                    btnText={BTN_TEXT.removeRow}
                    btnSize="md"
                    btnColor="grayOutline"
                    className={styles.removeSlot}
                    handleClick={() => removeSet(set.id)}
                  />
                ) : (
                  <span className={styles.removeSlot} aria-hidden="true" />
                ))}
            </div>
          ))}
          <Button
            btnText={BTN_TEXT.addTeamRole}
            btnSize="sm"
            btnColor="grayOutline"
            className="w-fit"
            handleClick={addSet}
          />
        </div>

        {showPreview && (
          <div className="flex flex-col gap-2">
            <span className={styles.fieldLabel}>하위 팀 권한 미리보기</span>
            <ModalTable
              head={["팀", "권한", "사유"]}
              rows={previewRows.map((row) => [
                row.indent ? `└ ${row.teamName}` : row.teamName,
                row.role,
                row.reason,
              ])}
            />
          </div>
        )}

        {sendFailed ? (
          <Notice tone="error">{SEND_FAILED_MESSAGE}</Notice>
        ) : (
          <Notice>
            초대 시 사용자에게 초대 코드가 발송되어 사용자가 24시간 내 rune을
            연결하면{" "}
            <MemberStatus
              status="online"
              className="h-auto cursor-default p-0 align-middle"
            />{" "}
            으로 전환됩니다. <br /> 미연결 시 코드가 만료되고 초대 코드
            재전송으로 사용자를 다시 초대할 수 있습니다.
          </Notice>
        )}
      </div>

      <div className="flex w-full items-center gap-4">
        <Button
          btnText={BTN_TEXT.close}
          btnSize="md"
          btnColor="grayOutline"
          handleClick={onClose}
        />
        <Button
          btnText={BTN_TEXT.sendInvitation}
          btnSize="md"
          btnColor="mintFilled"
          disabled={!canSubmit}
          handleClick={handleSubmit}
        />
      </div>
    </ModalLayout>
  );
};

export default InviteMemberModal;
