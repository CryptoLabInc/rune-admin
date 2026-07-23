import { useState } from "react";

import Button from "@/components/elements/Button";
import ModalLayout from "@/components/layout/ModalLayout";
import ModalTable from "@/components/users/ModalTable";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import { L } from "@/locales";
import type { TMemberDeleteTarget } from "@/types/userTypes";

const DELETE_FAILED_MESSAGE = L.members.deleteFailed;

/** The team/role table, or the empty-state line when the target belongs
    to no team (no group-role membership). */
const memberTeams = (memberships: TMemberDeleteTarget["memberships"]) =>
  memberships.length > 0 ? (
    /* table-fixed: the 팀/권한 columns split 50/50 regardless of content,
       so the per-user tables all line up. */
    <ModalTable
      head={[L.common.team, L.common.role]}
      rows={memberships.map((m) => [m.teamName, m.role])}
      className="table-fixed"
    />
  ) : (
    <p className="text-muted-foreground border p-2 text-sm">
      {L.members.noTeams}
    </p>
  );

interface MemberDeleteModalProps {
  /** Single entry (SC-13 [멤버 삭제]) or multi (SC-11 bulk [삭제]). */
  targets: TMemberDeleteTarget[];
  /** Delete request; on resolve the modal closes itself, on reject it
      swaps to the failure alert (state B). */
  onConfirm: () => Promise<void>;
  onClose: () => void;
}

/**
 * MemberDeleteModal is the 멤버 삭제 확인 (SC-15). Single entry shows
 * the account in the title with its team/role table; multi entry (D20)
 * titles "멤버 삭제 (N명)" and lists per-account sections — a long list
 * scrolls inside the modal (ModalLayout max-h). Deleting removes every
 * membership plus the session token and unused invite codes (D13) —
 * server-side; the dialog only names the teams. Failure swaps the
 * content for the alert message with [닫기] only. Mount conditionally.
 */
const MemberDeleteModal = ({
  targets,
  onConfirm,
  onClose,
}: MemberDeleteModalProps) => {
  const [failed, setFailed] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const single = targets.length === 1 ? targets[0] : null;
  const title = single
    ? MODAL_TITLES.deleteMemberSingle(single.account)
    : MODAL_TITLES.deleteMemberBulk(targets.length);

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
      <ModalLayout title={title} isOpen>
        <p className="text-center text-base">{DELETE_FAILED_MESSAGE}</p>
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
    <ModalLayout title={title} isOpen>
      <div className="flex flex-col gap-4">
        {single ? (
          <>
            <p className="text-base">
              {L.members.deleteSingleIntro(single.account)}
            </p>
            {memberTeams(single.memberships)}
          </>
        ) : (
          <>
            <p className="text-base">{L.members.deleteBulkIntro}</p>
            {targets.map((target) => (
              <div key={target.account} className="flex flex-col gap-2">
                <b className="text-sm">{target.account}</b>
                {memberTeams(target.memberships)}
              </div>
            ))}
          </>
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
          btnText={BTN_TEXT.delete}
          btnSize="md"
          btnColor="redFilled"
          disabled={submitting}
          handleClick={handleConfirm}
        />
      </div>
    </ModalLayout>
  );
};

export default MemberDeleteModal;
