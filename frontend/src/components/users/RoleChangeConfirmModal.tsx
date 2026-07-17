import { useState } from "react";

import Button from "@/components/elements/Button";
import Notice from "@/components/elements/Notice";
import ModalLayout from "@/components/layout/ModalLayout";
import ModalTable from "@/components/users/ModalTable";
import { BTN_TEXT } from "@/constants/commonConstants";
import type { TRoleChange } from "@/types/userTypes";

const UPDATE_FAILED_MESSAGE = "역할 변경에 실패했습니다. 다시 시도해주세요.";
const UPDATE_SUCCESS_MESSAGE = "role이 변경되었습니다.";

type TPhase = "confirm" | "success" | "failed";

interface RoleChangeConfirmModalProps {
  changes: TRoleChange[];
  /** First-column header — "account" (SC-06 E) or "팀" (SC-13). */
  subjectLabel: string;
  /** Sends the staged changes; the result message shows inside the
      modal (states E-1/E-2) — [닫기] alone remains after either. */
  onConfirm: () => Promise<void>;
  /** Dismiss without sending (staged dropdown values stay — SC-06 no.19). */
  onClose: () => void;
}

/**
 * RoleChangeConfirmModal is the role 변경 확인 모달 (SC-06 state E,
 * reused by the SC-13 drawer): staged changes are listed as
 * "current → new(bold)" in one role column (v0.12). After [변경하기]
 * the success/failure message renders inside the modal; retry means
 * closing and reopening via [변경사항 업데이트] (state E-2 keeps only
 * [닫기]). Mount conditionally — state resets by unmounting.
 */
const RoleChangeConfirmModal = ({
  changes,
  subjectLabel,
  onConfirm,
  onClose,
}: RoleChangeConfirmModalProps) => {
  const [phase, setPhase] = useState<TPhase>("confirm");
  const [submitting, setSubmitting] = useState(false);
  /* Snapshot the staged changes at mount: a successful apply makes the
     drawer recompute its `changes` prop to empty (baseRole catches up to
     the new role), but the success view must keep listing what changed.
     Safe to freeze — the drawer behind the scrim can't be edited while
     this modal is open, so the snapshot never goes stale. */
  const [rows] = useState(changes);

  const handleConfirm = async () => {
    setSubmitting(true);
    try {
      await onConfirm();
      setPhase("success");
    } catch {
      setPhase("failed");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <ModalLayout title="role 변경" isOpen>
      <div className="flex flex-col gap-4">
        <p className="text-base">다음 멤버의 role을 변경합니다:</p>
        <ModalTable
          head={[subjectLabel, "role"]}
          rows={rows.map((change) => [
            change.label,
            <>
              {change.from} → <b className="text-foreground">{change.to}</b>
            </>,
          ])}
        />
        {phase === "success" && (
          <Notice tone="success">{UPDATE_SUCCESS_MESSAGE}</Notice>
        )}
        {phase === "failed" && (
          <Notice tone="error">{UPDATE_FAILED_MESSAGE}</Notice>
        )}
      </div>
      <div className="flex w-full items-center gap-4">
        <Button
          btnText={BTN_TEXT.close}
          btnSize="md"
          btnColor="grayOutline"
          handleClick={onClose}
        />
        {phase === "confirm" && (
          <Button
            btnText={BTN_TEXT.change}
            btnSize="md"
            btnColor="mintFilled"
            disabled={submitting}
            handleClick={handleConfirm}
          />
        )}
      </div>
    </ModalLayout>
  );
};

export default RoleChangeConfirmModal;
