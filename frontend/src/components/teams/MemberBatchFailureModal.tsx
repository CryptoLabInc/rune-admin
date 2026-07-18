import Button from "@/components/elements/Button";
import ModalLayout from "@/components/layout/ModalLayout";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";

interface MemberBatchFailureModalProps {
  failures: { account: string; reason: string }[];
  onClose: () => void;
}

/**
 * MemberBatchFailureModal reports the failed items of a batch membership
 * operation (partial-failure UI — API design). The succeeded portion was
 * already applied server-side; this lists only what failed and why.
 */
const MemberBatchFailureModal = ({
  failures,
  onClose,
}: MemberBatchFailureModalProps) => {
  return (
    <ModalLayout title={MODAL_TITLES.batchFailure} isOpen>
      <ul className="my-2 flex flex-col gap-1 text-sm">
        {failures.map((f) => (
          <li key={f.account} className="flex justify-between gap-3">
            <span className="truncate">{f.account}</span>
            <span className="text-negative shrink-0">{f.reason}</span>
          </li>
        ))}
      </ul>
      <Button
        btnText={BTN_TEXT.confirm}
        btnSize="md"
        btnColor="grayOutline"
        handleClick={onClose}
      />
    </ModalLayout>
  );
};

export default MemberBatchFailureModal;
