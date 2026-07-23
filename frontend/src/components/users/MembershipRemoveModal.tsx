import { useState } from "react";

import Button from "@/components/elements/Button";
import Notice from "@/components/elements/Notice";
import ModalLayout from "@/components/layout/ModalLayout";
import ModalTable from "@/components/users/ModalTable";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import { L } from "@/locales";
import type { TMembershipRemoveTarget } from "@/types/userTypes";

const REMOVE_FAILED_MESSAGE = L.members.removeFailed;

interface MembershipRemoveModalProps {
  /** SC-06 entry: selected members × current team · SC-13 entry:
      current user × selected teams. Only what is listed is removed. */
  targets: TMembershipRemoveTarget[];
  /** Show the sub-team retention notice — true when a removed team has
      sub-team memberships that stay (computed by the caller, SC-14 no.2). */
  subteamNotice?: boolean;
  /** Removal request; on resolve the modal closes itself, on reject it
      swaps to the failure alert (state B). */
  onConfirm: () => Promise<void>;
  onClose: () => void;
}

/**
 * MembershipRemoveModal is the 멤버십 제거 확인 다이얼로그 (SC-14):
 * lists exactly the memberships being removed (no sub-team cascade —
 * C10) with an optional informational notice about retained sub-team
 * memberships. Failure swaps the content for the alert message with
 * [닫기] only. Mount conditionally — state resets by unmounting.
 */
const MembershipRemoveModal = ({
  targets,
  subteamNotice = false,
  onConfirm,
  onClose,
}: MembershipRemoveModalProps) => {
  const [failed, setFailed] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const handleConfirm = async () => {
    setSubmitting(true);
    try {
      await onConfirm();
      onClose();
    } catch {
      setFailed(true);
    } finally {
      setSubmitting(false);
    }
  };

  if (failed) {
    return (
      <ModalLayout title={MODAL_TITLES.removeMembership} isOpen>
        <p className="text-center text-base">{REMOVE_FAILED_MESSAGE}</p>
        <Button
          btnText={BTN_TEXT.close}
          btnSize="md"
          btnColor="grayOutline"
          handleClick={onClose}
        />
      </ModalLayout>
    );
  }

  return (
    <ModalLayout title={MODAL_TITLES.removeMembership} isOpen>
      <div className="flex flex-col gap-4">
        <p className="text-base">{L.teams.removeIntro}</p>
        <ModalTable
          head={["account", L.common.team, L.common.role]}
          rows={targets.map((target) => [
            target.account,
            target.teamName,
            target.role,
          ])}
        />
        {subteamNotice && (
          <Notice>{L.teams.removeKeepSubteams}</Notice>
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
          btnText={BTN_TEXT.remove}
          btnSize="md"
          btnColor="redFilled"
          disabled={submitting}
          handleClick={handleConfirm}
        />
      </div>
    </ModalLayout>
  );
};

export default MembershipRemoveModal;
