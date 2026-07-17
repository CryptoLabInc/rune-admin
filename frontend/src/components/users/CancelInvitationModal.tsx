import { useState } from "react";

import Button from "@/components/elements/Button";
import ModalLayout from "@/components/layout/ModalLayout";

interface CancelInvitationModalProps {
  account: string;
  /** Cancel request; on resolve the modal closes itself. Rejection is
      handled by the caller (toast) — this modal does not swap to an
      inline failure state (D15 — mirrors SessionDeactivateModal). */
  onConfirm: () => Promise<void>;
  onClose: () => void;
}

/**
 * CancelInvitationModal is the 초대 취소 확인 다이얼로그 (SC-13, D15):
 * force-expires every unused invite code for the account — the user
 * itself is not deleted. Mount conditionally — state resets by
 * unmounting.
 */
const CancelInvitationModal = ({
  account,
  onConfirm,
  onClose,
}: CancelInvitationModalProps) => {
  const [submitting, setSubmitting] = useState(false);

  const handleConfirm = async () => {
    setSubmitting(true);
    try {
      await onConfirm();
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <ModalLayout title="초대 취소" isOpen>
      <p className="text-base">
        {account}의 미사용 초대 코드가 모두 만료됩니다. 유저는 삭제되지
        않습니다.
      </p>
      <div className="flex w-full items-center gap-4">
        <Button
          btnText="닫기"
          btnSize="md"
          btnColor="grayOutline"
          handleClick={onClose}
        />
        <Button
          btnText="취소하기"
          btnSize="md"
          btnColor="redFilled"
          disabled={submitting}
          handleClick={handleConfirm}
        />
      </div>
    </ModalLayout>
  );
};

export default CancelInvitationModal;
