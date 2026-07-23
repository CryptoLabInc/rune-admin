import { useState } from "react";

import Button from "@/components/elements/Button";
import ModalLayout from "@/components/layout/ModalLayout";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import { L } from "@/locales";

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
    <ModalLayout title={MODAL_TITLES.deactivateSession} isOpen>
      <p className="text-center text-base">
        {L.members.deactivateConfirm(account)} <br />
        {L.members.allMcpTerminated}
      </p>
      <div className="flex w-full items-center gap-4">
        <Button
          btnText={BTN_TEXT.cancel}
          btnSize="md"
          btnColor="grayOutline"
          handleClick={onClose}
        />
        <Button
          btnText={BTN_TEXT.deactivate}
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
