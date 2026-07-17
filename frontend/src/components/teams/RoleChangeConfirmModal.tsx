import Button from "@/components/elements/Button";
import ModalLayout from "@/components/layout/ModalLayout";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";

export interface TRoleChange {
  account: string;
  from: string;
  to: string;
}

interface RoleChangeConfirmModalProps {
  changes: TRoleChange[];
  onClose: () => void;
  onConfirm: () => void;
}

const styles = {
  table: "w-full border-collapse text-sm",
  th: "border-border text-faint border px-3 py-1.5 text-left font-mono text-tag font-medium tracking-[0.08em]",
  td: "border-border text-muted-foreground border px-3 py-1.5",
  arrow: "text-faint px-1",
  to: "text-foreground font-semibold",
};

/**
 * RoleChangeConfirmModal is the role 변경 confirmation (SC-06 state E):
 * staged dropdown edits are listed (account · current → new) and only
 * applied on [변경하기]. Mount conditionally.
 */
const RoleChangeConfirmModal = ({
  changes,
  onClose,
  onConfirm,
}: RoleChangeConfirmModalProps) => {
  return (
    <ModalLayout title={MODAL_TITLES.roleChange} isOpen>
      <div className="flex w-full flex-col gap-4">
        <p className="text-base">다음 멤버의 권한을 변경합니다:</p>
        <table className={styles.table}>
          <thead>
            <tr>
              <th className={styles.th}>account</th>
              <th className={styles.th}>권한 변경</th>
            </tr>
          </thead>
          <tbody>
            {changes.map((change) => (
              <tr key={change.account}>
                <td className={styles.td}>{change.account}</td>
                <td className={styles.td}>
                  {change.from}
                  <span className={styles.arrow} aria-hidden="true">
                    →
                  </span>
                  <span className={styles.to}>{change.to}</span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="flex w-full gap-2">
        <Button
          btnText={BTN_TEXT.cancel}
          btnSize="md"
          btnColor="grayOutline"
          handleClick={onClose}
        />
        <Button
          btnText={BTN_TEXT.change}
          btnSize="md"
          btnColor="mintFilled"
          handleClick={onConfirm}
        />
      </div>
    </ModalLayout>
  );
};

export default RoleChangeConfirmModal;
