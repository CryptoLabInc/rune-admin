import { useState } from "react";

import Button from "@/components/elements/Button";
import ModalLayout from "@/components/layout/ModalLayout";

interface SessionDeactivateModalProps {
  account: string;
  /** Deactivation request; on resolve the modal closes itself. Rejection
      is handled by the caller (toast) — this modal does not swap to an
      inline failure state (D12 — unlike the remove/delete confirms). */
  onConfirm: () => Promise<void>;
  onClose: () => void;
}

/**
 * SessionDeactivateModal is the 세션 비활성화 확인 다이얼로그 (SC-13,
 * D12): destroys the user's console session token, ending every MCP
 * session tied to it. Mount conditionally — state resets by unmounting.
 */
const SessionDeactivateModal = ({
  account,
  onConfirm,
  onClose,
}: SessionDeactivateModalProps) => {
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
    <ModalLayout title="세션 비활성화" isOpen>
      <p className="text-base">
        {account}의 세션을 비활성화하시겠습니까? 모든 MCP 세션이 종료됩니다.
      </p>
      <div className="flex w-full items-center gap-4">
        <Button
          btnText="취소"
          btnSize="md"
          btnColor="grayOutline"
          handleClick={onClose}
        />
        <Button
          btnText="비활성화"
          btnSize="md"
          btnColor="redFilled"
          disabled={submitting}
          handleClick={handleConfirm}
        />
      </div>
    </ModalLayout>
  );
};

export default SessionDeactivateModal;
