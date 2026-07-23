import { useState } from "react";

import Button from "@/components/elements/Button";
import Dropdown from "@/components/elements/Dropdown";
import Input from "@/components/elements/Input";
import MemberStatus from "@/components/elements/MemberStatus";
import Notice from "@/components/elements/Notice";
import ModalLayout from "@/components/layout/ModalLayout";
import { ROLE_OPTIONS } from "@/components/teams/teamOptions";
import {
  isSubmittableUsername,
  normalizeUsernameInput,
  USERNAME_MAX_LENGTH,
  validateUsername,
} from "@/utils/username";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import { L } from "@/locales";

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

interface AddMemberModalProps {
  teamName: string;
  /** Server-mapped error copy from the last failed invite attempt (409
      ALREADY_TEAM_MEMBER and friends) — null/undefined renders nothing. */
  error?: string | null;
  onClose: () => void;
  onInvite: (account: string, role: string, username: string) => void;
}

/**
 * AddMemberModal is the 멤버 추가 modal (SC-10): account email +
 * username + role. Already-member detection is the server's call (409
 * ALREADY_TEAM_MEMBER) — this component only renders whatever error
 * the caller maps from the failed mutation. Mount conditionally.
 */
const AddMemberModal = ({
  teamName,
  error,
  onClose,
  onInvite,
}: AddMemberModalProps) => {
  const [account, setAccount] = useState("");
  const [username, setUsername] = useState("");
  const [role, setRole] = useState("");

  const trimmed = account.trim();
  const invalidFormat = trimmed.length > 0 && !EMAIL_PATTERN.test(trimmed);
  const usernameError = validateUsername(username);
  const canSubmit =
    trimmed.length > 0 &&
    !invalidFormat &&
    isSubmittableUsername(username) &&
    role !== "";

  return (
    <ModalLayout title={MODAL_TITLES.addMember(teamName)} isOpen isWide>
      <div className="flex w-full flex-col gap-6">
        <Input
          id="add-member-account"
          labelText={L.teams.emailLabel}
          type="email"
          placeholder="user@corp.com"
          maxLength={100}
          value={account}
          setValue={setAccount}
          error={invalidFormat ? L.teams.invalidEmail : undefined}
        />
        <Input
          id="add-member-username"
          labelText={L.teams.usernameLabel}
          placeholder={L.teams.usernamePlaceholder}
          maxLength={USERNAME_MAX_LENGTH}
          value={username}
          setValue={(value) => setUsername(normalizeUsernameInput(value))}
          error={usernameError}
        />
        <Dropdown
          label={L.teams.roleLabel}
          placeholder={L.teams.selectRole}
          options={ROLE_OPTIONS}
          value={role}
          onChange={setRole}
        />
        <Notice tone="info">
          {L.teams.invitePrefix}
          <MemberStatus
            status="online"
            className="bg-mint/10 h-auto cursor-default gap-1 rounded-sm px-1.5 py-0.5 align-middle"
          />
          {L.teams.inviteSuffix}
        </Notice>
        {error && <Notice tone="error">{error}</Notice>}
      </div>
      <div className="flex w-full gap-2">
        <Button
          btnText={BTN_TEXT.cancel}
          btnSize="md"
          btnColor="grayOutline"
          handleClick={onClose}
        />
        <Button
          btnText={BTN_TEXT.invite}
          btnSize="md"
          btnColor="mintFilled"
          disabled={!canSubmit}
          handleClick={() => onInvite(trimmed, role, username.trim())}
        />
      </div>
    </ModalLayout>
  );
};

export default AddMemberModal;
