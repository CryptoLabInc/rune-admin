import { useState } from "react";

import Button from "@/components/elements/Button";
import Dropdown from "@/components/elements/Dropdown";
import Input from "@/components/elements/Input";
import MemberStatus from "@/components/elements/MemberStatus";
import Notice from "@/components/elements/Notice";
import ModalLayout from "@/components/layout/ModalLayout";
import { ROLE_OPTIONS } from "@/components/teams/teamOptions";

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

interface AddMemberModalProps {
  teamName: string;
  /** Server-mapped error copy from the last failed invite attempt (409
      ALREADY_TEAM_MEMBER and friends) — null/undefined renders nothing. */
  error?: string | null;
  onClose: () => void;
  onInvite: (account: string, role: string) => void;
}

/**
 * AddMemberModal is the 멤버 추가 modal (SC-10): account email + role.
 * Already-member detection is the server's call (409 ALREADY_TEAM_MEMBER)
 * — this component only renders whatever error the caller maps from the
 * failed mutation. Mount conditionally.
 */
const AddMemberModal = ({
  teamName,
  error,
  onClose,
  onInvite,
}: AddMemberModalProps) => {
  const [account, setAccount] = useState("");
  const [role, setRole] = useState("");

  const trimmed = account.trim();
  const invalidFormat = trimmed.length > 0 && !EMAIL_PATTERN.test(trimmed);
  const canSubmit = trimmed.length > 0 && !invalidFormat && role !== "";

  return (
    <ModalLayout title={`멤버 추가 — ${teamName}`} isOpen isWide>
      <div className="flex w-full flex-col gap-5">
        <Input
          id="add-member-account"
          labelText="계정명 (email)"
          type="email"
          placeholder="user@corp.com"
          maxLength={100}
          value={account}
          setValue={setAccount}
          error={invalidFormat ? "올바른 이메일 형식이 아닙니다." : undefined}
        />
        <Dropdown
          label="role"
          placeholder="role 선택"
          options={ROLE_OPTIONS}
          value={role}
          onChange={setRole}
        />
        <Notice tone="info">
          초대받은 사용자가 rune을 연결하면{" "}
          <MemberStatus
            status="online"
            className="bg-mint/10 h-auto cursor-default gap-1 rounded-sm px-1.5 py-0.5 align-middle"
          />
          으로 전환됩니다.
        </Notice>
        {error && <Notice tone="error">{error}</Notice>}
      </div>
      <div className="flex w-full gap-2">
        <Button
          btnText="취소"
          btnSize="md"
          btnColor="grayOutline"
          handleClick={onClose}
        />
        <Button
          btnText="초대하기"
          btnSize="md"
          btnColor="mintFilled"
          disabled={!canSubmit}
          handleClick={() => onInvite(trimmed, role)}
        />
      </div>
    </ModalLayout>
  );
};

export default AddMemberModal;
